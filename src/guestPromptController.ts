import { session, type PreloadScriptRegistration } from 'electron';
import * as path from 'path';
import { isGuestAgentControlled } from './guestAgentControl';

export const WEBVIEW_PROMPT_PRELOAD_PATH = path.join(
  __dirname,
  'webviewPromptPreload.js',
);
export const WEBVIEW_PROMPT_REQUEST_CHANNEL = 'webview:prompt-request';
export const WEBVIEW_PROMPT_RESPONSE_CHANNEL = 'webview:prompt-response';
export const WEBVIEW_PROMPT_SUBSCRIBER_STATE_CHANNEL =
  'webview:prompt-subscriber-state';
export const MAX_HUMAN_PROMPT_TEXT_LENGTH = 2000;
export const DEFAULT_HUMAN_PROMPT_TIMEOUT_MS = 5 * 60000;

type GuestAbortEvent = 'did-navigate' | 'render-process-gone' | 'destroyed';

const GUEST_ABORT_EVENTS: readonly GuestAbortEvent[] = [
  'did-navigate',
  'render-process-gone',
  'destroyed',
];

const configuredPromptSessions = new WeakSet<object>();

/** Minimal session surface required by {@link configureSessionPromptPreload}. */
export interface PromptSessionLike {
  registerPreloadScript?(script: PreloadScriptRegistration): unknown;
}

/** Minimal Hub host `WebContents` surface required by {@link GuestPromptController}. */
export interface PromptHostSender {
  id?: number;
  isDestroyed(): boolean;
  send(channel: string, payload: WebviewPromptRequestPayload): void;
  once?(event: 'destroyed', listener: () => void): unknown;
  removeListener?(event: 'destroyed', listener: () => void): unknown;
}

/** Minimal navigation details surface passed to Electron's `did-start-navigation` event. */
export interface DidStartNavigationDetailsLike {
  isSameDocument?: boolean;
  isMainFrame?: boolean;
  frame?: unknown;
}

/** Listener signature for guest `did-start-navigation` events. */
export type DidStartNavigationListener = (
  details?: DidStartNavigationDetailsLike | null,
  url?: string,
  isInPlace?: boolean,
  isMainFrame?: boolean,
) => void;

/** Minimal frame metadata read from an IPC `senderFrame`. */
export interface PromptSenderFrame {
  url?: string;
  origin?: string;
}

/** Minimal guest `WebContents` surface required by {@link GuestPromptController}. */
export interface PromptGuestWebContents {
  id: number;
  hostWebContents?: PromptHostSender | null;
  isDestroyed?(): boolean;
  getURL?(): string;
  getType?(): string;
  on?(
    event: 'did-start-navigation',
    listener: DidStartNavigationListener,
  ): unknown;
  on?(event: GuestAbortEvent, listener: () => void): unknown;
  removeListener?(
    event: 'did-start-navigation',
    listener: DidStartNavigationListener,
  ): unknown;
  removeListener?(event: GuestAbortEvent, listener: () => void): unknown;
}

/** Resolved Hub host context for a guest `WebContents`. */
export interface PromptHostContext {
  hostSender: PromptHostSender;
  tabKey?: string;
  visible?: boolean;
}

/** Resolves the owning Hub host context for a guest `WebContents`. */
export type PromptHostResolver = (
  guest: PromptGuestWebContents,
) => PromptHostContext | null | undefined;

/** Notified when a human prompt card opens or closes for a `tabKey`. */
export type PromptVisibilityListener = (tabKey: string, open: boolean) => void;

/** Payload forwarded to the Hub host renderer over {@link WEBVIEW_PROMPT_REQUEST_CHANNEL}. */
export interface WebviewPromptRequestPayload {
  promptId: string;
  webContentsId: number;
  tabKey?: string;
  origin: string;
  url: string;
  message: string;
  defaultValue: string;
  dismissed?: boolean;
}

/** Minimal synchronous IPC event surface consumed by {@link GuestPromptController.handleSyncPrompt}. */
export interface PromptSyncIpcEvent {
  sender?: PromptGuestWebContents | null;
  senderFrame?: PromptSenderFrame | null;
  returnValue?: unknown;
}

/** Options accepted by {@link GuestPromptController}. */
export interface GuestPromptControllerOptions {
  timeoutMs?: number;
  requireHostSubscriber?: boolean;
  hostResolver?: PromptHostResolver;
  onPromptVisibilityChange?: PromptVisibilityListener;
}

interface PendingPromptRecord {
  promptId: string;
  hostSender: PromptHostSender;
  settle: (value: string | null, notifyDismissed: boolean) => void;
}

/** Registers `webviewPromptPreload.js` once on non-default browser guest sessions. */
export function configureSessionPromptPreload(
  targetSession: PromptSessionLike | null | undefined,
  defaultSession: PromptSessionLike | null | undefined = session?.defaultSession,
): void {
  if (
    !targetSession ||
    typeof targetSession !== 'object' ||
    targetSession === defaultSession ||
    configuredPromptSessions.has(targetSession) ||
    typeof targetSession.registerPreloadScript !== 'function'
  ) {
    return;
  }
  configuredPromptSessions.add(targetSession);
  try {
    targetSession.registerPreloadScript({
      type: 'frame',
      filePath: WEBVIEW_PROMPT_PRELOAD_PATH,
    });
  } catch (err) {
    console.warn('[Prompt] Failed to register guest prompt preload:', err);
  }
}

function clampText(raw: unknown): string {
  return typeof raw === 'string'
    ? raw.slice(0, MAX_HUMAN_PROMPT_TEXT_LENGTH)
    : '';
}

function deriveVerifiedOrigin(
  frameOrigin: string | null | undefined,
  url: string,
): string {
  for (const candidate of [frameOrigin, url]) {
    if (!candidate || candidate === 'null') {
      continue;
    }
    try {
      const parsed = new URL(candidate);
      if (parsed.protocol === 'http:' || parsed.protocol === 'https:') {
        return parsed.origin;
      }
      if (candidate === url) {
        return parsed.protocol.replace(/:$/, '') || 'unknown';
      }
    } catch {
      // Fall through.
    }
  }
  return 'unknown';
}

function isSameHost(a: PromptHostSender, b: PromptHostSender): boolean {
  return a === b || (a.id !== undefined && a.id === b.id);
}

/**
 * Coordinates synchronous guest `window.prompt()` requests for human-controlled
 * browser tabs, pausing only the requesting guest renderer while forwarding an
 * asynchronous prompt card lifecycle to the owning Hub host renderer.
 */
export class GuestPromptController {
  private readonly pendingByPromptId = new Map<string, PendingPromptRecord>();
  private readonly pendingByWcId = new Map<number, PendingPromptRecord>();
  private readonly subscribedHosts = new Set<PromptHostSender>();
  private readonly destroyHookedHosts = new WeakSet<PromptHostSender>();
  private nextPromptSeq = 1;
  private readonly timeoutMs: number;
  private readonly requireHostSubscriber: boolean;
  private hostResolver?: PromptHostResolver;
  private onPromptVisibilityChange?: PromptVisibilityListener;

  constructor(options?: GuestPromptControllerOptions) {
    this.timeoutMs = options?.timeoutMs ?? DEFAULT_HUMAN_PROMPT_TIMEOUT_MS;
    this.requireHostSubscriber = options?.requireHostSubscriber ?? false;
    this.hostResolver = options?.hostResolver;
    this.onPromptVisibilityChange = options?.onPromptVisibilityChange;
  }

  /** Tracks whether a Hub host renderer is subscribed to prompt requests. */
  setHostSubscribed(
    hostSender: PromptHostSender | null | undefined,
    subscribed: boolean,
  ): void {
    if (!hostSender || hostSender.isDestroyed()) {
      return;
    }
    for (const existing of [...this.subscribedHosts]) {
      if (isSameHost(existing, hostSender)) {
        this.subscribedHosts.delete(existing);
      }
    }
    if (!subscribed) {
      this.cancelForHost(hostSender);
      return;
    }
    this.subscribedHosts.add(hostSender);
    if (!this.destroyHookedHosts.has(hostSender)) {
      this.destroyHookedHosts.add(hostSender);
      hostSender.once?.('destroyed', () => {
        for (const existing of [...this.subscribedHosts]) {
          if (isSameHost(existing, hostSender)) {
            this.subscribedHosts.delete(existing);
          }
        }
        this.cancelForHost(hostSender);
      });
    }
  }

  /** Returns `true` when `hostSender` is alive and currently subscribed. */
  isHostSubscribed(hostSender: PromptHostSender | null | undefined): boolean {
    if (!hostSender || hostSender.isDestroyed()) {
      return false;
    }
    for (const existing of this.subscribedHosts) {
      if (!existing.isDestroyed() && isSameHost(existing, hostSender)) {
        return true;
      }
    }
    return false;
  }

  /** Cancels all pending prompts owned by `hostSender` without sending dismissals. */
  cancelForHost(hostSender: PromptHostSender): void {
    for (const pending of [...this.pendingByPromptId.values()]) {
      if (isSameHost(pending.hostSender, hostSender)) {
        pending.settle(null, false);
      }
    }
  }

  /** Sets a custom resolver mapping guest `WebContents` to their owning Hub host context. */
  setHostResolver(resolver?: PromptHostResolver): void {
    this.hostResolver = resolver;
  }

  /** Sets a listener notified when a prompt opens or closes for a `tabKey`. */
  setPromptVisibilityListener(listener?: PromptVisibilityListener): void {
    this.onPromptVisibilityChange = listener;
  }

  /** Handles a synchronous `window.prompt()` IPC request from a human-controlled guest frame. */
  handleSyncPrompt(event: PromptSyncIpcEvent, rawJson?: unknown): void {
    const guest = event?.sender;
    const resolved =
      guest &&
      typeof guest.id === 'number' &&
      guest.id > 0 &&
      !guest.isDestroyed?.() &&
      !isGuestAgentControlled(guest.id)
        ? this.resolveHostContext(guest)
        : null;
    if (
      !guest ||
      !resolved?.hostSender ||
      resolved.hostSender.isDestroyed() ||
      resolved.visible === false ||
      (this.requireHostSubscriber &&
        !this.isHostSubscribed(resolved.hostSender))
    ) {
      event.returnValue = null;
      return;
    }
    let message = '';
    let defaultValue = '';
    if (typeof rawJson === 'string') {
      try {
        const parsed: unknown = JSON.parse(rawJson);
        if (parsed && typeof parsed === 'object') {
          const payload = parsed as {
            message?: unknown;
            defaultValue?: unknown;
          };
          message = clampText(payload.message);
          defaultValue = clampText(payload.defaultValue);
        }
      } catch {
        // Ignore malformed guest JSON payloads.
      }
    }
    this.cancelForGuest(guest.id);
    const url = event.senderFrame?.url || guest.getURL?.() || '';
    const origin = deriveVerifiedOrigin(event.senderFrame?.origin, url);
    const promptId = `hprompt_${this.nextPromptSeq++}`;
    const { hostSender, tabKey } = resolved;
    const payloadBase: WebviewPromptRequestPayload = {
      promptId,
      webContentsId: guest.id,
      ...(tabKey ? { tabKey } : {}),
      origin,
      url,
      message,
      defaultValue,
    };
    let settled = false;
    const onDidStartNav: DidStartNavigationListener = (
      navEv,
      _navUrl,
      isInPlace,
      isMainFrame,
    ) => {
      const sameDoc =
        typeof isInPlace === 'boolean'
          ? isInPlace
          : Boolean(navEv?.isSameDocument);
      const mainFrame =
        typeof isMainFrame === 'boolean'
          ? isMainFrame
          : navEv?.isMainFrame !== false;
      const callingFrame = Boolean(
        event.senderFrame && navEv?.frame === event.senderFrame,
      );
      if (!sameDoc && (mainFrame || callingFrame)) {
        settle(null, true);
      }
    };
    const onGuestAbort = (): void => settle(null, true);
    const onHostDestroyed = (): void => settle(null, false);
    const timer = setTimeout(() => settle(null, true), this.timeoutMs);
    timer.unref?.();
    const settle = (
      value: string | null,
      notifyDismissed: boolean,
    ): void => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timer);
      this.pendingByPromptId.delete(promptId);
      if (this.pendingByWcId.get(guest.id)?.promptId === promptId) {
        this.pendingByWcId.delete(guest.id);
      }
      guest.removeListener?.('did-start-navigation', onDidStartNav);
      for (const ev of GUEST_ABORT_EVENTS) {
        guest.removeListener?.(ev, onGuestAbort);
      }
      hostSender.removeListener?.('destroyed', onHostDestroyed);
      if (tabKey) {
        this.onPromptVisibilityChange?.(tabKey, false);
      }
      if (notifyDismissed && !hostSender.isDestroyed()) {
        try {
          hostSender.send(WEBVIEW_PROMPT_REQUEST_CHANNEL, {
            ...payloadBase,
            dismissed: true,
          });
        } catch {
          // Ignore send errors on tearing-down host renderers.
        }
      }
      event.returnValue = typeof value === 'string' ? value : null;
    };
    const record: PendingPromptRecord = { promptId, hostSender, settle };
    this.pendingByPromptId.set(promptId, record);
    this.pendingByWcId.set(guest.id, record);
    guest.on?.('did-start-navigation', onDidStartNav);
    for (const ev of GUEST_ABORT_EVENTS) {
      guest.on?.(ev, onGuestAbort);
    }
    hostSender.once?.('destroyed', onHostDestroyed);
    if (tabKey) {
      this.onPromptVisibilityChange?.(tabKey, true);
    }
    try {
      hostSender.send(WEBVIEW_PROMPT_REQUEST_CHANNEL, payloadBase);
    } catch {
      settle(null, false);
    }
  }

  /** Settles a pending prompt from the owning Hub host renderer. */
  respondPrompt(
    callerSenderId: number,
    promptId: string | null | undefined,
    value?: string | null,
  ): boolean {
    const pending = promptId
      ? this.pendingByPromptId.get(promptId)
      : undefined;
    if (!pending) {
      return false;
    }
    if (
      pending.hostSender.id !== undefined &&
      callerSenderId !== pending.hostSender.id
    ) {
      throw new Error(
        `Prompt ${promptId} is not owned by the calling renderer`,
      );
    }
    pending.settle(typeof value === 'string' ? clampText(value) : null, false);
    return true;
  }

  /** Cancels any pending prompt for `webContentsId`, notifying the host renderer. */
  cancelForGuest(webContentsId: number): boolean {
    const pending = this.pendingByWcId.get(webContentsId);
    pending?.settle(null, true);
    return Boolean(pending);
  }

  /** Returns `true` when `webContentsId` currently has an active pending prompt. */
  hasPendingForGuest(webContentsId: number): boolean {
    return this.pendingByWcId.has(webContentsId);
  }

  /** Resolves the owning Hub host context via the configured resolver or `<webview>` host fallback. */
  resolveHostContext(guest: PromptGuestWebContents): PromptHostContext | null {
    return (
      this.hostResolver?.(guest) ??
      (guest.getType?.() === 'webview' && guest.hostWebContents
        ? { hostSender: guest.hostWebContents, visible: true }
        : null)
    );
  }
}

export const guestPromptController = new GuestPromptController({
  requireHostSubscriber: true,
});
