import { ensureGuestDebuggerAttached, type GuestDebugger } from './guestDebugger';
import {
  extractComparableOrigin,
  getGuestMainOrigin,
  isGuestOnBlankOrEmptyUrl,
} from './guestFileAttachment';
import { isProtectedIdpFrameUrl } from './guestFramePolicy';
import {
  isGuestOnProtectedIdp,
  isProtectedIdentityProviderOriginUrl,
  type ProtectedIdpWebContentsLike,
} from './webviewUploadValidation';

const MAX_PAYLOAD_BYTES = 64 * 1024;
const DISABLED = 'WebMCP is disabled';
const BLOCKED_MSG = 'WebMCP is blocked on identity provider and payment pages';
const IPV4_LOOPBACK_RE = /^127(?:\.(?:25[0-5]|2[0-4]\d|1?\d?\d)){3}$/;
const UNSAFE_NAME_CHAR_RE = /[\p{Cc}\p{Cf}\u2028\u2029]/u;
const UNSAFE_DESC_CHARS_RE = /[\p{Cc}\p{Cf}\u2028\u2029]/gu;
const PROTECTED_PAYMENT_HOSTS = [
  'stripe.com,m.stripe.network,paypal.com,paypalobjects.com,adyen.com',
  'braintreegateway.com,braintree-api.com,js.checkout.com,pay.checkout.com',
  'squareup.com,squareupsandbox.com,square.site,klarna.com,affirm.com',
  'pay.google.com,payments.google.com,wallet.google.com,afterpay.com',
  'pay.amazon.com,payments.amazon.com,shopifycs.com,shop.app',
  'checkout.shopify.com',
].flatMap((row) => row.split(','));

/** Error codes returned by WebMCP controller operations. */
export type WebMcpErrorCode =
  | 'WEBMCP_DISABLED'
  | 'GUEST_UNAVAILABLE'
  | 'ORIGIN_MISMATCH'
  | 'INSECURE_CONTEXT'
  | 'IDP_OR_PAYMENT_BLOCKED'
  | 'INVALID_ARGS'
  | 'TOOL_NOT_FOUND'
  | 'TOOL_EXECUTION_ERROR'
  | 'INVALID_RESULT'
  | 'CANCELLED'
  | 'TIMEOUT'
  | 'NAVIGATED'
  | 'OUTCOME_UNKNOWN';

/** Failure response returned by WebMCP operations. */
export interface WebMcpErrorResult {
  ok: false;
  errorCode: WebMcpErrorCode;
  error: string;
}

/** Sanitized metadata describing a guest-registered WebMCP tool. */
export interface WebMcpToolInfo {
  name: string;
  description: string;
  inputSchema?: Record<string, unknown>;
  readOnlyHint?: boolean;
}

/** Result returned by {@link GuestWebMcpController.listTools}. */
export type WebMcpListToolsResult =
  | { ok: true; nativeAvailable: boolean; tools: WebMcpToolInfo[] }
  | WebMcpErrorResult;

/** Result returned by {@link GuestWebMcpController.callTool}. */
export type WebMcpCallToolResult =
  | { ok: true; resultJson: string }
  | WebMcpErrorResult;

/** Expected parameters shape for {@link GuestWebMcpController.callTool}. */
export interface WebMcpCallToolParams {
  callId: string;
  name: string;
  argsJson: string;
  timeoutMs?: number;
}

/** Minimal IPC sender surface identifying the caller that owns a tool invocation. */
export interface WebMcpSender {
  id: number;
}

/** Minimal CDP debugger surface required by {@link GuestWebMcpController}. */
export interface WebMcpDebugger extends GuestDebugger {
  sendCommand(
    method: string,
    commandParams?: Record<string, unknown>,
  ): Promise<unknown>;
  on(event: 'detach', listener: () => void): unknown;
  on(
    event: 'message',
    listener: (event: unknown, method: string, params: unknown) => void,
  ): unknown;
}

/** Minimal guest `WebContents` surface required by {@link GuestWebMcpController}. */
export interface WebMcpGuestWebContents extends ProtectedIdpWebContentsLike {
  id: number;
  isDestroyed(): boolean;
  debugger: WebMcpDebugger;
  on(event: 'did-navigate', listener: () => void): unknown;
  on(event: 'render-process-gone', listener: () => void): unknown;
  on(event: 'destroyed', listener: () => void): unknown;
}

interface PendingCall {
  callId: string;
  owner: number;
  epoch: number;
  resolve: (result: WebMcpCallToolResult) => void;
  settled: boolean;
  cancelRequested: boolean;
  timer?: ReturnType<typeof setTimeout>;
  invocationId?: string;
}

interface TabState {
  wc: WebMcpGuestWebContents;
  epoch: number;
  synced: boolean;
  everEnabled: boolean;
  topMainFrameId: string;
  tools: Map<string, WebMcpToolInfo>;
  inflight: Map<string, PendingCall>;
  byInvocation: Map<string, PendingCall>;
  awaitingInvoke: number;
  early: Map<string, Record<string, unknown>>;
  syncing?: Promise<WebMcpErrorResult | null>;
}

const noop = (): undefined => undefined;

const err = (errorCode: WebMcpErrorCode, error: string): WebMcpErrorResult => ({
  ok: false,
  errorCode,
  error,
});

const utf8Bytes = (s: string): number => Buffer.byteLength(s, 'utf8');

const isObject = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);

/** Stops at the cap, so hostile nesting cannot overflow the stack. */
const depthOk = (v: unknown, depth = 1): boolean =>
  typeof v !== 'object' ||
  v === null ||
  (depth <= 32 &&
    Object.values(v).every((c: unknown) => depthOk(c, depth + 1)));

const sendCancel = (wc: WebMcpGuestWebContents, invocationId: string): void =>
  void wc.debugger
    .sendCommand('WebMCP.cancelInvocation', { invocationId })
    .catch(noop);

const normHost = (u: URL | null): string =>
  (u?.hostname ?? '')
    .replace(/^\[|\]$|\.+$/g, '')
    .replace(/%.*$/, '')
    .toLowerCase();

function unwrapHttpUrl(raw?: string | null): URL | null {
  const s = (raw || '').trim();
  const skip = !s || s.toLowerCase() === 'null' || /^about:/i.test(s);
  try {
    const outer = skip ? null : new URL(s);
    const u = outer?.protocol === 'blob:' ? new URL(outer.pathname) : outer;
    return u && /^https?:$/.test(u.protocol) ? u : null;
  } catch {
    return null;
  }
}

function isSecureWebMcpUrl(raw?: string | null): boolean {
  const u = unwrapHttpUrl(raw);
  const h = normHost(u);
  const loop =
    h === 'localhost' ||
    h.endsWith('.localhost') ||
    h === '::1' ||
    IPV4_LOOPBACK_RE.test(h);
  return Boolean(u && h && (u.protocol === 'https:' || loop));
}

function isProtectedUrl(raw?: string | null): boolean {
  const s = raw || '';
  const h = normHost(unwrapHttpUrl(s)).replace(/^www\./, '');
  return (
    isProtectedIdentityProviderOriginUrl(s) ||
    isProtectedIdpFrameUrl(s) ||
    (h !== '' &&
      PROTECTED_PAYMENT_HOSTS.some((d) => h === d || h.endsWith(`.${d}`)))
  );
}

// TODO(b/581816623): Calibrate passive widgets vs active checkout/auth subframes.
function isGuestOnProtectedSurface(wc: WebMcpGuestWebContents): boolean {
  const sub = wc.mainFrame?.framesInSubtree;
  const frames = [wc.mainFrame, ...(Array.isArray(sub) ? sub : [])];
  return (
    isGuestOnProtectedIdp(wc) ||
    isProtectedUrl(wc.getURL?.()) ||
    frames.some(
      (f) =>
        !f?.isDestroyed?.() &&
        (isProtectedUrl(f?.url) || isProtectedUrl(f?.origin)),
    )
  );
}

function hasProtectedCdpFrame(node: unknown): boolean {
  const n = isObject(node) ? node : null;
  const f = isObject(n?.frame) ? n.frame : null;
  const kids = Array.isArray(n?.childFrames) ? n.childFrames : [];
  return (
    (typeof f?.url === 'string' && isProtectedUrl(f.url)) ||
    (typeof f?.securityOrigin === 'string' &&
      isProtectedUrl(f.securityOrigin)) ||
    kids.some(hasProtectedCdpFrame)
  );
}

const cleanText = (s: string): string =>
  s.replace(/[\r\n\t]/g, ' ').replace(UNSAFE_DESC_CHARS_RE, '');

function scrubSchema(v: Record<string, unknown>): Record<string, unknown>;
function scrubSchema(v: unknown): unknown;
function scrubSchema(v: unknown): unknown {
  if (typeof v === 'string') {
    return cleanText(v);
  }
  if (Array.isArray(v)) {
    return v.map((item: unknown) => scrubSchema(item));
  }
  return isObject(v)
    ? Object.fromEntries(
        Object.entries(v)
          .map(([k, val]): [string, unknown] => [cleanText(k), scrubSchema(val)])
          .filter(([k]) => k !== '__proto__'),
      )
    : v;
}

function sanitizeDescription(raw: unknown): string {
  const s = typeof raw === 'string' ? cleanText(raw) : '';
  if (utf8Bytes(s) <= 4096) {
    return s;
  }
  let bytes = 0;
  let end = 0;
  for (const ch of s) {
    if ((bytes += utf8Bytes(ch)) > 4096) {
      break;
    }
    end += ch.length;
  }
  return s.slice(0, end);
}

function parseArgs(json: unknown): Record<string, unknown> | null {
  if (typeof json !== 'string' || utf8Bytes(json) > MAX_PAYLOAD_BYTES) {
    return null;
  }
  try {
    const v: unknown = JSON.parse(json);
    return isObject(v) && depthOk(v) ? v : null;
  } catch {
    return null;
  }
}

/**
 * Sanitizes a guest-registered tool descriptor (`readOnlyHint` is untrusted
 * page metadata and must never bypass URL policy, permissions, or consent).
 */
function toToolInfo(t: Record<string, unknown>): WebMcpToolInfo | null {
  const { name, description, inputSchema: s } = t;
  const okSchema =
    s === undefined ||
    (isObject(s) && depthOk(s) && utf8Bytes(JSON.stringify(s)) <= 16384);
  if (
    typeof name !== 'string' ||
    !name.trim() ||
    name.length > 128 ||
    UNSAFE_NAME_CHAR_RE.test(name) ||
    !okSchema
  ) {
    return null;
  }
  const ro = isObject(t.annotations) ? t.annotations.readOnly : undefined;
  const schema = isObject(s) ? scrubSchema(s) : undefined;
  return {
    name,
    description: sanitizeDescription(description),
    ...(schema ? { inputSchema: schema } : {}),
    ...(typeof ro === 'boolean' ? { readOnlyHint: ro } : {}),
  };
}

function toCallResult(r: Record<string, unknown>): WebMcpCallToolResult {
  if (r.status === 'Canceled') {
    return err('CANCELLED', 'Tool invocation was cancelled');
  }
  if (r.status !== 'Completed') {
    const msg =
      typeof r.errorText === 'string' && r.errorText
        ? r.errorText
        : 'Tool execution failed';
    return err('TOOL_EXECUTION_ERROR', msg.slice(0, 2048));
  }
  // Chromium caps CDP JSON nesting, so stringify cannot overflow the stack.
  const json = JSON.stringify(r.output ?? null);
  return utf8Bytes(json) <= MAX_PAYLOAD_BYTES
    ? { ok: true, resultJson: json }
    : err('INVALID_RESULT', 'Tool result exceeds 64 KiB');
}

const clampTimeout = (ms: unknown): number =>
  typeof ms === 'number' && Number.isFinite(ms)
    ? Math.min(120000, Math.max(1, Math.trunc(ms)))
    : 30000;

/**
 * Bridges the host renderer to Chromium's native WebMCP in guest tabs over the
 * shared CDP session, sending `WebMCP.enable` only on tabs the agent lists or
 * calls tools on. Nothing is injected into guests.
 */
export class GuestWebMcpController {
  private enabled = false;
  private readonly tabs = new Map<number, TabState>();
  private readonly blinkState = new Map<number, boolean>();

  isEnabled(): boolean {
    return this.enabled;
  }

  /** Records or clears whether guest `wcId` was created with Blink `WebMCP`. */
  noteGuestCreated(wcId: number, blinkWebMcpEnabled: boolean): void {
    this.blinkState.set(wcId, blinkWebMcpEnabled);
  }

  noteGuestDisposed(wcId: number): void {
    this.blinkState.delete(wcId);
  }

  /**
   * Updates the WebMCP gate; `enableBlinkFeatures` is fixed at view creation,
   * while `false` immediately disables CDP and settles calls on open tabs.
   */
  setEnabled(enabled: boolean): void {
    if (enabled === this.enabled) {
      return;
    }
    this.enabled = enabled;
    for (const tab of enabled ? [] : this.tabs.values()) {
      [...tab.inflight.values()].forEach((p) =>
        this.cancel(tab, p, 'OUTCOME_UNKNOWN', DISABLED),
      );
      if (tab.everEnabled) {
        tab.everEnabled = false;
        void tab.wc.debugger.sendCommand('WebMCP.disable').catch(noop);
      }
      this.reset(tab, err('OUTCOME_UNKNOWN', DISABLED));
    }
  }

  async listTools(
    wc: WebMcpGuestWebContents,
    origin: unknown,
  ): Promise<WebMcpListToolsResult> {
    const tab = await this.ready(wc, origin);
    if (tab === 'blank') {
      return { ok: true, nativeAvailable: false, tools: [] };
    }
    return 'ok' in tab
      ? tab
      : { ok: true, nativeAvailable: true, tools: [...tab.tools.values()] };
  }

  async callTool(
    wc: WebMcpGuestWebContents,
    sender: WebMcpSender,
    origin: unknown,
    params: unknown,
  ): Promise<WebMcpCallToolResult> {
    const tab = await this.ready(wc, origin);
    if (tab !== 'blank' && 'ok' in tab) {
      return tab;
    }
    const pObj = isObject(params) ? params : null;
    const args = parseArgs(pObj?.argsJson);
    const callId = pObj?.callId;
    const name = pObj?.name;
    if (
      !pObj ||
      !args ||
      typeof callId !== 'string' ||
      !callId ||
      typeof name !== 'string'
    ) {
      return err('INVALID_ARGS', 'Invalid tool call parameters');
    }
    if (tab === 'blank' || !tab.tools.has(name)) {
      return err('TOOL_NOT_FOUND', `Tool "${name}" not found`);
    }
    if (tab.inflight.has(callId)) {
      return err('INVALID_ARGS', 'Duplicate callId');
    }
    return new Promise((resolve) => {
      const p: PendingCall = {
        callId,
        owner: sender.id,
        epoch: tab.epoch,
        resolve,
        settled: false,
        cancelRequested: false,
      };
      tab.inflight.set(callId, p);
      const timeout = (): void =>
        this.cancel(tab, p, 'TIMEOUT', 'Tool timed out');
      p.timer = setTimeout(timeout, clampTimeout(pObj.timeoutMs));
      p.timer.unref?.();
      tab.awaitingInvoke++;
      const invoke = {
        frameId: tab.topMainFrameId,
        toolName: name,
        input: args,
      };
      tab.wc.debugger
        .sendCommand('WebMCP.invokeTool', invoke)
        .then(
          (r) =>
            this.onInvoked(tab, p, isObject(r) ? r.invocationId : undefined),
          () => this.onInvoked(tab, p, undefined),
        );
    });
  }

  /** Cancels `callId` on guest `wcId` if `senderId` started it. */
  cancelToolCall(wcId: number, callId: unknown, senderId: number): boolean {
    const tab = typeof callId === 'string' ? this.tabs.get(wcId) : undefined;
    const p =
      typeof callId === 'string' && callId
        ? tab?.inflight.get(callId)
        : undefined;
    if (!tab || !p || p.owner !== senderId) {
      return false;
    }
    this.cancel(tab, p, 'CANCELLED', 'Tool call cancelled');
    return true;
  }

  private gate(
    wc: WebMcpGuestWebContents,
    origin: unknown,
  ): WebMcpErrorResult | 'blank' | null {
    if (!this.enabled) {
      return err('WEBMCP_DISABLED', DISABLED);
    }
    if (wc.isDestroyed()) {
      return err('GUEST_UNAVAILABLE', 'Guest is unavailable');
    }
    if (isGuestOnBlankOrEmptyUrl(wc)) {
      return 'blank';
    }
    // Opaque, `null`, and `about:` origins normalize to '' and never match.
    const mainUrl = wc.mainFrame?.url || wc.getURL?.() || '';
    const guestOrigin = getGuestMainOrigin(wc);
    const want =
      typeof origin === 'string' ? extractComparableOrigin(origin) : '';
    if (!want || want !== guestOrigin) {
      return err('ORIGIN_MISMATCH', 'Guest origin does not match');
    }
    const urls = [mainUrl, wc.getURL?.() || mainUrl, guestOrigin];
    if (!urls.every(isSecureWebMcpUrl)) {
      return err('INSECURE_CONTEXT', 'WebMCP requires https or loopback http');
    }
    return isGuestOnProtectedSurface(wc)
      ? err('IDP_OR_PAYMENT_BLOCKED', BLOCKED_MSG)
      : null;
  }

  /** Gates, syncs, then re-gates against the synced document. */
  private async ready(
    wc: WebMcpGuestWebContents,
    origin: unknown,
  ): Promise<TabState | 'blank' | WebMcpErrorResult> {
    const pre = this.gate(wc, origin);
    if (pre || this.blinkState.get(wc.id) !== true) {
      return pre ?? 'blank';
    }
    const tab = this.tabFor(wc);
    tab.syncing ??= tab.synced
      ? undefined
      : this.sync(tab, 1).finally(() => (tab.syncing = undefined));
    const syncErr = tab.syncing ? await tab.syncing : null;
    return syncErr ?? this.gate(wc, origin) ?? tab;
  }

  /** Re-enables the domain so Chromium replays the document's tools. */
  private async sync(
    tab: TabState,
    retries: number,
  ): Promise<WebMcpErrorResult | null> {
    const epoch = tab.epoch;
    const stale = (): boolean => tab.epoch !== epoch;
    const dbg = tab.wc.debugger;
    const unavailable = (): WebMcpErrorResult =>
      this.enabled
        ? err('GUEST_UNAVAILABLE', 'WebMCP is unavailable in this guest')
        : err('WEBMCP_DISABLED', DISABLED);
    if (!this.enabled || !ensureGuestDebuggerAttached(dbg)) {
      return unavailable();
    }
    try {
      if (tab.everEnabled) {
        await dbg.sendCommand('WebMCP.disable').catch(noop);
      }
      const tree = stale() ? null : await dbg.sendCommand('Page.getFrameTree');
      const root = isObject(tree) ? tree.frameTree : undefined;
      if (!stale() && hasProtectedCdpFrame(root)) {
        return err('IDP_OR_PAYMENT_BLOCKED', BLOCKED_MSG);
      }
      const rootFrame =
        isObject(root) && isObject(root.frame) ? root.frame : null;
      const id = rootFrame?.id;
      if (!stale() && typeof id === 'string') {
        tab.topMainFrameId = id;
        tab.tools.clear();
        tab.everEnabled = true;
        await dbg.sendCommand('WebMCP.enable');
        tab.synced = !stale();
      }
    } catch {
      // The domain is missing or the session went away.
    }
    if (tab.synced) {
      return null;
    }
    return this.enabled && stale() && retries > 0
      ? this.sync(tab, retries - 1)
      : unavailable();
  }

  private tabFor(wc: WebMcpGuestWebContents): TabState {
    const found = this.tabs.get(wc.id);
    if (found) {
      return found;
    }
    const tab: TabState = {
      wc,
      epoch: 0,
      synced: false,
      everEnabled: false,
      topMainFrameId: '',
      tools: new Map(),
      inflight: new Map(),
      byInvocation: new Map(),
      awaitingInvoke: 0,
      early: new Map(),
    };
    const lost =
      (error: string): (() => void) =>
      () =>
        this.reset(tab, err('OUTCOME_UNKNOWN', error));
    // Listeners die with the guest, so `destroyed` only drops per-tab maps.
    wc.on('did-navigate', () =>
      this.reset(tab, err('NAVIGATED', 'Guest navigated during tool call')),
    );
    wc.on('render-process-gone', lost('Guest exited'));
    wc.on('destroyed', () => {
      lost('Guest destroyed')();
      this.tabs.delete(wc.id);
      this.blinkState.delete(wc.id);
    });
    wc.debugger.on('detach', () => {
      tab.everEnabled = false;
      lost('Guest debugger detached')();
    });
    wc.debugger.on('message', (_e, method, params) =>
      this.onMessage(tab, method, params),
    );
    this.tabs.set(wc.id, tab);
    return tab;
  }

  private onMessage(tab: TabState, method: string, params: unknown): void {
    const r = isObject(params) ? params : {};
    if (
      method === 'WebMCP.toolResponded' &&
      typeof r.invocationId === 'string'
    ) {
      const p = tab.byInvocation.get(r.invocationId);
      if (p) {
        this.settle(tab, p, toCallResult(r));
      } else if (tab.awaitingInvoke > 0) {
        tab.early.set(r.invocationId, r);
      }
      return;
    }
    if (method !== 'WebMCP.toolsAdded' && method !== 'WebMCP.toolsRemoved') {
      return;
    }
    for (const raw of Array.isArray(r.tools) ? r.tools : []) {
      if (!isObject(raw) || raw.frameId !== tab.topMainFrameId) {
        continue;
      }
      if (method === 'WebMCP.toolsRemoved') {
        if (typeof raw.name === 'string') {
          tab.tools.delete(raw.name);
        }
        continue;
      }
      const info = toToolInfo(raw);
      if (!info || (!tab.tools.has(info.name) && tab.tools.size >= 64)) {
        continue;
      }
      const next = [...new Map(tab.tools).set(info.name, info).values()];
      if (utf8Bytes(JSON.stringify(next)) <= 256 * 1024) {
        tab.tools.set(info.name, info);
      }
    }
  }

  /** Runs synchronously on the `invokeTool` reply so no event interleaves. */
  private onInvoked(tab: TabState, p: PendingCall, id: unknown): void {
    if (typeof id !== 'string') {
      this.settle(tab, p, err('TOOL_EXECUTION_ERROR', 'Invocation failed'));
    } else if (p.settled || p.epoch !== tab.epoch) {
      tab.early.delete(id);
      // Only an explicit cancel reaches the page; navigation does not.
      if (p.cancelRequested) {
        sendCancel(tab.wc, id);
      }
    } else {
      p.invocationId = id;
      tab.byInvocation.set(id, p);
      const early = tab.early.get(id);
      if (early) {
        tab.early.delete(id);
        this.settle(tab, p, toCallResult(early));
      }
    }
    if (--tab.awaitingInvoke === 0) {
      tab.early.clear();
    }
  }

  private cancel(
    tab: TabState,
    p: PendingCall,
    code: WebMcpErrorCode,
    message: string,
  ): void {
    if (p.settled) {
      return;
    }
    p.cancelRequested = true;
    if (p.invocationId !== undefined) {
      sendCancel(tab.wc, p.invocationId);
    }
    this.settle(tab, p, err(code, message));
  }

  private settle(
    tab: TabState,
    p: PendingCall,
    result: WebMcpCallToolResult,
  ): void {
    if (p.settled) {
      return;
    }
    p.settled = true;
    clearTimeout(p.timer);
    tab.inflight.delete(p.callId);
    tab.byInvocation.delete(p.invocationId ?? '');
    p.resolve(result);
  }

  /** Starts a new epoch and settles every in-flight call without CDP traffic. */
  private reset(tab: TabState, result: WebMcpErrorResult): void {
    tab.epoch++;
    tab.synced = false;
    tab.tools.clear();
    tab.early.clear();
    [...tab.inflight.values()].forEach((p) => this.settle(tab, p, result));
  }
}

export const guestWebMcpController = new GuestWebMcpController();
