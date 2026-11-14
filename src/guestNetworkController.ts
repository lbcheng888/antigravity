import {
  CDP_NETWORK_ENABLE_PARAMS,
  ensureGuestDebuggerAttached,
  type GuestDebugger,
} from './guestDebugger';
import {
  MAX_RESPONSE_BODY_PREVIEW_CHARS,
  canCaptureCrossOriginDetails,
  extractHttpOrigin,
  isCrossOriginSensitiveGoogleUrl,
  isProtectedNetworkUrl,
  isSafeTextualMime,
  normalizeResourceType,
  sanitizeNetworkUrl,
  sanitizeUntrustedNetworkText,
  scrubNetworkHeaders,
} from './guestNetworkSanitizer';

export { MAX_RESPONSE_BODY_PREVIEW_CHARS } from './guestNetworkSanitizer';

export const WEBVIEW_NETWORK_REQUEST_CHANNEL = 'webview:network-request-updated';
export const MAX_NETWORK_ENTRIES_PER_GUEST = 50;
const MAX_IN_FLIGHT_REQUESTS_PER_GUEST = 128;
const GET_RESPONSE_BODY_TIMEOUT_MS = 750;

/** Sanitized network request entry captured for a tracked guest `WebContents`. */
export interface NetworkRequestEntry {
  seq: number;
  url: string;
  method: string;
  resourceType: string;
  status: number;
  statusText: string;
  ok: boolean;
  requestHeaders: Record<string, string>;
  responseHeaders: Record<string, string>;
  documentOrigin?: string;
  errorText?: string;
  responseBodyPreview?: string;
  finished: boolean;
}

/** IPC payload emitted on {@link WEBVIEW_NETWORK_REQUEST_CHANNEL}. */
export interface NetworkRequestUpdatePayload {
  webContentsId: number;
  entry: NetworkRequestEntry;
}

/** Host sender surface used to emit guest network updates. */
export interface NetworkRequestSender {
  isDestroyed(): boolean;
  send(channel: string, payload: NetworkRequestUpdatePayload): void;
}

/** Minimal `Debugger` surface required for CDP `Network.*` tracking. */
export interface NetworkTrackableDebugger extends GuestDebugger {
  sendCommand(
    method: string,
    commandParams?: Record<string, unknown>,
  ): Promise<unknown>;
  on(
    event: 'detach',
    listener: (event: unknown, reason: string) => void,
  ): unknown;
  on(
    event: 'message',
    listener: (
      event: unknown,
      method: string,
      params: unknown,
      sessionId?: string,
    ) => void,
  ): unknown;
}

/** Minimal `WebContents` surface required by {@link GuestNetworkController}. */
export interface NetworkTrackableContents {
  id: number;
  isDestroyed(): boolean;
  getURL?(): string;
  debugger: NetworkTrackableDebugger;
  on(
    event: 'did-start-navigation' | 'did-navigate' | 'destroyed',
    listener: () => void,
  ): unknown;
}

/** Options accepted by {@link GuestNetworkController.trackGuest}. */
export interface TrackGuestOptions {
  ownerId?: number;
  sender?: NetworkRequestSender;
}

interface InFlightRequestRecord {
  entry: NetworkRequestEntry;
  rawUrl: string;
  corsAllowOrigin?: string;
  mimeType?: string;
  tombstoned: boolean;
}

interface TrackedGuestState {
  contents: NetworkTrackableContents;
  sender?: NetworkRequestSender;
  networkEnabled: boolean;
  entries: NetworkRequestEntry[];
  byRequestId: Map<string, InFlightRequestRecord>;
  tombstonedIds: Set<string>;
}

interface CdpRequestPayload {
  url?: unknown;
  method?: unknown;
  headers?: unknown;
}

interface CdpResponsePayload {
  url?: unknown;
  status?: unknown;
  statusText?: unknown;
  mimeType?: unknown;
  headers?: unknown;
}

interface CdpNetworkEventParams {
  requestId?: unknown;
  type?: unknown;
  headers?: unknown;
  response?: CdpResponsePayload | null;
  statusCode?: unknown;
  errorText?: unknown;
  corsErrorStatus?: { corsError?: unknown } | null;
  blockedReason?: unknown;
  request?: CdpRequestPayload | null;
  documentURL?: unknown;
  initiator?: { url?: unknown } | null;
}

function cloneEntry(e: NetworkRequestEntry): NetworkRequestEntry {
  return {
    ...e,
    requestHeaders: { ...e.requestHeaders },
    responseHeaders: { ...e.responseHeaders },
  };
}

function canCaptureRecDetails(rec: InFlightRequestRecord): boolean {
  return canCaptureCrossOriginDetails(
    rec.rawUrl,
    rec.entry.documentOrigin,
    rec.corsAllowOrigin,
  );
}

function scrubHeadersAny(raw: unknown): Record<string, string> {
  if (!raw || typeof raw !== 'object') {
    return {};
  }
  return scrubNetworkHeaders(raw);
}

function applyResponseHeaders(
  rec: InFlightRequestRecord,
  rawHeaders: unknown,
): void {
  const scrubbed = scrubHeadersAny(rawHeaders);
  if (scrubbed['access-control-allow-origin']) {
    rec.corsAllowOrigin = scrubbed['access-control-allow-origin'];
  }
  const merged: Record<string, string> = {
    ...rec.entry.responseHeaders,
    ...scrubbed,
  };
  if (canCaptureRecDetails(rec)) {
    rec.entry.responseHeaders = merged;
    return;
  }
  rec.entry.requestHeaders = {};
  const isPreflight =
    rec.entry.method === 'OPTIONS' || rec.entry.resourceType === 'preflight';
  if (!isPreflight) {
    rec.entry.responseHeaders = {};
    return;
  }
  const corsOnly: Record<string, string> = {};
  for (const [k, v] of Object.entries(merged)) {
    if (k.startsWith('access-control-') || k === 'content-type') {
      corsOnly[k] = v;
    }
  }
  rec.entry.responseHeaders = corsOnly;
}

/** Checks whether `contents` supports CDP `Network` tracking. */
export function isNetworkTrackableContents(
  contents: unknown,
): contents is NetworkTrackableContents {
  const c = contents as Partial<NetworkTrackableContents> | null | undefined;
  return (
    typeof c?.id === 'number' &&
    typeof c.on === 'function' &&
    typeof c.isDestroyed === 'function' &&
    typeof c.debugger?.attach === 'function' &&
    typeof c.debugger.isAttached === 'function' &&
    typeof c.debugger.sendCommand === 'function' &&
    typeof c.debugger.on === 'function'
  );
}

/** Tracks a bounded per-guest network ring buffer over CDP `Network.*`. */
export class GuestNetworkController {
  private readonly guests = new Map<number, TrackedGuestState>();
  private nextSeq = 1;

  trackGuest(
    contents: NetworkTrackableContents,
    options?: TrackGuestOptions,
  ): void {
    if (contents.isDestroyed()) {
      return;
    }
    const existing = this.guests.get(contents.id);
    if (existing && existing.contents === contents) {
      if (options?.sender) {
        existing.sender = options.sender;
      }
      return;
    }
    const state: TrackedGuestState = {
      contents,
      sender: options?.sender,
      networkEnabled: false,
      entries: [],
      byRequestId: new Map(),
      tombstonedIds: new Set(),
    };
    this.guests.set(contents.id, state);
    this.enableNetwork(state);
    contents.debugger.on('detach', () => {
      state.networkEnabled = false;
      state.byRequestId.clear();
    });
    contents.debugger.on('message', (_ev, method, params) => {
      if (params && typeof params === 'object') {
        this.handleCdpMessage(state, method, params as CdpNetworkEventParams);
      }
    });
    const onNav = (): void => {
      if (!contents.isDestroyed() && !state.networkEnabled) {
        this.enableNetwork(state, true);
      }
    };
    contents.on('did-start-navigation', onNav);
    contents.on('did-navigate', onNav);
    contents.on('destroyed', () => {
      if (this.guests.get(contents.id)?.contents === contents) {
        this.guests.delete(contents.id);
      }
    });
  }

  getRequests(webContentsId: number): NetworkRequestEntry[] {
    const state = this.guests.get(webContentsId);
    return state ? state.entries.map(cloneEntry) : [];
  }

  clearRequests(webContentsId: number): boolean {
    const state = this.guests.get(webContentsId);
    if (!state) {
      return false;
    }
    state.entries.length = 0;
    state.byRequestId.clear();
    // Keep tombstones so later redirect hops of an in-flight protected request
    // (e.g. an IdP callback carrying `?code=`) stay suppressed after a clear.
    return true;
  }

  private enableNetwork(state: TrackedGuestState, navStarted = false): void {
    const liveUrl = state.contents.getURL?.();
    if (!navStarted && typeof liveUrl === 'string' && !liveUrl.trim()) {
      return;
    }
    if (!ensureGuestDebuggerAttached(state.contents.debugger)) {
      return;
    }
    state.networkEnabled = true;
    void state.contents.debugger
      .sendCommand('Network.enable', CDP_NETWORK_ENABLE_PARAMS)
      .catch(() => {
        state.networkEnabled = false;
      });
  }

  private purgeRequest(state: TrackedGuestState, requestId: string): void {
    state.tombstonedIds.add(requestId);
    if (state.tombstonedIds.size > MAX_IN_FLIGHT_REQUESTS_PER_GUEST) {
      const oldest = state.tombstonedIds.values().next().value;
      if (oldest !== undefined) {
        state.tombstonedIds.delete(oldest);
      }
    }
    const existing = state.byRequestId.get(requestId);
    if (existing) {
      existing.tombstoned = true;
      state.byRequestId.delete(requestId);
      const idx = state.entries.indexOf(existing.entry);
      if (idx >= 0) {
        state.entries.splice(idx, 1);
      }
    }
  }

  private handleCdpMessage(
    state: TrackedGuestState,
    method: string,
    params: CdpNetworkEventParams,
  ): void {
    const requestId = typeof params.requestId === 'string' ? params.requestId : '';
    if (!requestId || state.tombstonedIds.has(requestId)) {
      return;
    }
    if (method === 'Network.requestWillBeSent') {
      this.handleRequestWillBeSent(state, requestId, params);
      return;
    }
    const rec = state.byRequestId.get(requestId);
    if (!rec || rec.tombstoned) {
      return;
    }
    if (method === 'Network.requestWillBeSentExtraInfo') {
      if (canCaptureRecDetails(rec)) {
        const extra = scrubHeadersAny(params.headers);
        rec.entry.requestHeaders = { ...rec.entry.requestHeaders, ...extra };
      }
      return;
    }
    if (method === 'Network.responseReceived') {
      const resp = params.response;
      const respUrl = typeof resp?.url === 'string' ? resp.url.trim() : rec.rawUrl;
      if (
        isProtectedNetworkUrl(respUrl) ||
        isCrossOriginSensitiveGoogleUrl(respUrl, rec.entry.documentOrigin)
      ) {
        this.purgeRequest(state, requestId);
        return;
      }
      const status =
        typeof resp?.status === 'number' && Number.isFinite(resp.status)
          ? Math.trunc(resp.status)
          : 0;
      const rawStatusText =
        typeof resp?.statusText === 'string' ? resp.statusText : '';
      rec.rawUrl = respUrl;
      rec.entry.url = sanitizeNetworkUrl(respUrl) || rec.entry.url;
      rec.entry.status = status;
      rec.entry.statusText = sanitizeUntrustedNetworkText(rawStatusText, 128);
      rec.entry.ok = status >= 200 && status < 300;
      if (typeof params.type === 'string' && params.type) {
        rec.entry.resourceType = normalizeResourceType(
          params.type,
          rec.entry.method,
        );
      }
      if (typeof resp?.mimeType === 'string') {
        rec.mimeType = resp.mimeType;
      }
      applyResponseHeaders(rec, resp?.headers);
      return;
    }
    if (method === 'Network.responseReceivedExtraInfo') {
      if (
        rec.entry.status === 0 &&
        typeof params.statusCode === 'number' &&
        Number.isFinite(params.statusCode)
      ) {
        rec.entry.status = Math.trunc(params.statusCode);
        rec.entry.ok = rec.entry.status >= 200 && rec.entry.status < 300;
      }
      applyResponseHeaders(rec, params.headers);
      return;
    }
    if (method === 'Network.loadingFailed') {
      state.byRequestId.delete(requestId);
      const errBase =
        typeof params.errorText === 'string' && params.errorText
          ? params.errorText
          : 'net::ERR_FAILED';
      const corsErr = params.corsErrorStatus?.corsError;
      const blocked =
        typeof params.blockedReason === 'string' ? params.blockedReason : '';
      let detail = errBase;
      if (typeof corsErr === 'string' && corsErr) {
        detail = `${errBase} (CORS: ${corsErr})`;
      } else if (blocked) {
        detail = `${errBase} (${blocked})`;
      }
      rec.entry.errorText = sanitizeUntrustedNetworkText(detail, 256);
      rec.entry.ok = false;
      rec.entry.finished = true;
      rec.entry.seq = this.nextSeq++;
      this.emitEntry(state, rec.entry);
      return;
    }
    if (method === 'Network.loadingFinished') {
      state.byRequestId.delete(requestId);
      void this.finalizeLoadingFinished(state, requestId, rec);
    }
  }

  private handleRequestWillBeSent(
    state: TrackedGuestState,
    requestId: string,
    params: CdpNetworkEventParams,
  ): void {
    const req = params.request;
    const rawUrl = typeof req?.url === 'string' ? req.url.trim() : '';
    const docUrl =
      typeof params.documentURL === 'string' && params.documentURL
        ? params.documentURL
        : (state.contents.getURL?.() ?? '');
    const initiatorUrl =
      typeof params.initiator?.url === 'string' ? params.initiator.url : '';
    const docOrigin = extractHttpOrigin(docUrl);
    const hasBlockedDoc =
      Boolean(docUrl) &&
      docUrl !== 'about:blank' &&
      isProtectedNetworkUrl(docUrl);
    if (
      isProtectedNetworkUrl(rawUrl) ||
      hasBlockedDoc ||
      (Boolean(initiatorUrl) && isProtectedNetworkUrl(initiatorUrl)) ||
      isCrossOriginSensitiveGoogleUrl(rawUrl, docOrigin)
    ) {
      this.purgeRequest(state, requestId);
      return;
    }
    const cleanUrl = sanitizeNetworkUrl(rawUrl);
    if (!cleanUrl) {
      this.purgeRequest(state, requestId);
      return;
    }
    const rawMethod =
      typeof req?.method === 'string' ? req.method.toUpperCase() : 'GET';
    const httpMethod = sanitizeUntrustedNetworkText(rawMethod, 24) || 'GET';
    const resourceType = normalizeResourceType(
      typeof params.type === 'string' ? params.type : undefined,
      httpMethod,
    );
    const allowDetails = canCaptureCrossOriginDetails(
      rawUrl,
      docOrigin,
      undefined,
    );
    const scrubbedReqHeaders = allowDetails
      ? scrubHeadersAny(req?.headers)
      : {};
    const existing = state.byRequestId.get(requestId);
    if (existing) {
      // A repeated requestId is the next redirect hop. ExtraInfo events fire
      // per hop, so drop the previous hop's response state; otherwise its
      // `Access-Control-Allow-Origin` would make this hop look SOP-safe.
      existing.rawUrl = rawUrl;
      existing.corsAllowOrigin = undefined;
      existing.mimeType = undefined;
      existing.entry.url = cleanUrl;
      existing.entry.method = httpMethod;
      existing.entry.resourceType = resourceType;
      existing.entry.requestHeaders = scrubbedReqHeaders;
      existing.entry.responseHeaders = {};
      existing.entry.status = 0;
      existing.entry.statusText = '';
      existing.entry.ok = false;
      return;
    }
    const entry: NetworkRequestEntry = {
      seq: this.nextSeq++,
      url: cleanUrl,
      method: httpMethod,
      resourceType,
      status: 0,
      statusText: '',
      ok: false,
      requestHeaders: scrubbedReqHeaders,
      responseHeaders: {},
      ...(docOrigin ? { documentOrigin: docOrigin } : {}),
      finished: false,
    };
    state.byRequestId.set(requestId, { entry, rawUrl, tombstoned: false });
    if (state.byRequestId.size > MAX_IN_FLIGHT_REQUESTS_PER_GUEST) {
      const oldestKey = state.byRequestId.keys().next().value;
      if (oldestKey !== undefined) {
        state.byRequestId.delete(oldestKey);
      }
    }
    state.entries.push(entry);
    if (state.entries.length > MAX_NETWORK_ENTRIES_PER_GUEST) {
      state.entries.shift();
    }
  }

  private async finalizeLoadingFinished(
    state: TrackedGuestState,
    requestId: string,
    rec: InFlightRequestRecord,
  ): Promise<void> {
    const isBodyCandidate =
      rec.entry.status >= 400 ||
      rec.entry.resourceType === 'xhr' ||
      rec.entry.resourceType === 'fetch';
    const shouldFetchBody =
      canCaptureRecDetails(rec) &&
      !rec.tombstoned &&
      rec.entry.method !== 'OPTIONS' &&
      rec.entry.method !== 'HEAD' &&
      isBodyCandidate &&
      isSafeTextualMime(
        rec.mimeType ?? rec.entry.responseHeaders['content-type'],
      );
    if (shouldFetchBody && !state.contents.isDestroyed()) {
      const preview = await this.fetchBodyPreview(state, requestId);
      if (!rec.tombstoned && preview !== undefined) {
        rec.entry.responseBodyPreview = preview;
      }
    }
    if (!rec.tombstoned) {
      rec.entry.finished = true;
      rec.entry.seq = this.nextSeq++;
      this.emitEntry(state, rec.entry);
    }
  }

  private async fetchBodyPreview(
    state: TrackedGuestState,
    requestId: string,
  ): Promise<string | undefined> {
    let timer: ReturnType<typeof setTimeout> | undefined;
    try {
      const timeoutSym = Symbol('body-timeout');
      const res = await Promise.race([
        state.contents.debugger
          .sendCommand('Network.getResponseBody', { requestId })
          .catch(() => undefined),
        new Promise<symbol>((resolve) => {
          timer = setTimeout(
            () => resolve(timeoutSym),
            GET_RESPONSE_BODY_TIMEOUT_MS,
          );
        }),
      ]);
      if (!res || res === timeoutSym || typeof res !== 'object') {
        return undefined;
      }
      const raw = res as { body?: unknown; base64Encoded?: unknown };
      if (typeof raw.body !== 'string' || !raw.body) {
        return undefined;
      }
      const isB64 = raw.base64Encoded === true;
      const buf = isB64
        ? Buffer.from(raw.body.slice(0, 12288), 'base64')
        : Buffer.from(raw.body.slice(0, 8192), 'utf8');
      if (buf.includes(0)) {
        return undefined;
      }
      const text = isB64
        ? buf.toString('utf8')
        : raw.body.slice(0, MAX_RESPONSE_BODY_PREVIEW_CHARS * 2);
      const clean = sanitizeUntrustedNetworkText(
        text,
        MAX_RESPONSE_BODY_PREVIEW_CHARS,
        true,
      );
      return clean || undefined;
    } finally {
      if (timer !== undefined) {
        clearTimeout(timer);
      }
    }
  }

  private emitEntry(
    state: TrackedGuestState,
    entry: NetworkRequestEntry,
  ): void {
    if (!state.sender || state.sender.isDestroyed()) {
      return;
    }
    try {
      state.sender.send(WEBVIEW_NETWORK_REQUEST_CHANNEL, {
        webContentsId: state.contents.id,
        entry: cloneEntry(entry),
      });
    } catch {
      // Ignore send errors on tearing-down renderer frames.
    }
  }
}

export const guestNetworkController = new GuestNetworkController();
