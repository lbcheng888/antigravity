import { ensureGuestDebuggerAttached } from './guestDebugger';
import {
  WEBVIEW_FILE_CHOOSER_OPENED_CHANNEL,
  actionErr,
  applyInterceptedChooserDecision,
  attachStagedFilesByDomPath,
  idpBlockedErr,
  isGuestOnBlankOrEmptyUrl,
  resolveConnectedDomPathForBackendNode,
  resolveGuestUploadFiles,
  syncPopupGuardIsolatedFlag,
  verifySameOriginChooserFrame,
  type ApplyInterceptedChooserResult,
  type AttachStagedFilesRequest,
  type AttachStagedFilesResult,
  type GuestFileActionError,
  type GuestFileAttachmentDebugger,
  type GuestFileAttachmentWebContents,
  type GuestFileChooserDecision,
  type GuestIdpBlockedError,
  type PendingFileChooserTarget,
  type ResolveGuestUploadFilesRequest,
  type VerifySameOriginChooserFrameResult,
} from './guestFileAttachment';
import {
  isGuestOnProtectedIdp,
  isProtectedIdentityProviderOriginUrl,
  type DomPathStep,
} from './webviewUploadValidation';

export { WEBVIEW_FILE_CHOOSER_OPENED_CHANNEL, resolveGuestUploadFiles };
export type {
  ApplyInterceptedChooserResult,
  AttachStagedFilesRequest,
  AttachStagedFilesResult,
  GuestFileChooserDecision,
  ResolveGuestUploadFilesRequest,
};

/** Default fail-safe timeout (ms) for an unhandled intercepted file chooser before auto-cancelling. */
export const DEFAULT_FILE_CHOOSER_TIMEOUT_MS = 10000;
/** Default timeout (ms) bounding CDP `Page.enable` so uninitialized targets cannot hang callers. */
export const DEFAULT_FILE_CHOOSER_PAGE_ENABLE_TIMEOUT_MS = 1500;

/** Options accepted by {@link GuestFileChooserController}. */
export interface GuestFileChooserControllerOptions {
  chooserTimeoutMs?: number;
  pageEnableTimeoutMs?: number;
}

/** Per-guest file chooser interception policy configured by the embedder renderer. */
export interface GuestFileChooserPolicy {
  enabled: boolean;
  tabKey?: string;
}

/** Fallback context resolved from the host view controller when no active policy is set. */
export interface GuestFileChooserFallbackContext {
  tabKey?: string;
  suppressNativePopups?: boolean;
}

/** Callback returning the current {@link GuestFileChooserFallbackContext} for a guest. */
export type GuestFileChooserFallbackContextProvider =
  () => GuestFileChooserFallbackContext;

/** IPC payload emitted over {@link WEBVIEW_FILE_CHOOSER_OPENED_CHANNEL} when an intercepted chooser opens. */
export interface WebviewFileChooserOpenedPayload {
  chooserId: string;
  webContentsId: number;
  tabKey?: string;
  multiple: boolean;
  url: string;
  frameOrigin: string;
  frameUrl?: string;
  domPath?: DomPathStep[];
}

/** Host sender surface used to push intercepted file chooser notifications. */
export interface GuestFileChooserSender {
  isDestroyed(): boolean;
  send(channel: string, payload: WebviewFileChooserOpenedPayload): void;
}

/** Minimal CDP debugger surface required by {@link GuestFileChooserController}. */
export interface GuestFileChooserDebugger extends GuestFileAttachmentDebugger {
  on?(
    event: 'detach',
    listener: (event: unknown, reason?: string) => void,
  ): unknown;
  on?(
    event: 'message',
    listener: (
      event: unknown,
      method: string,
      params: unknown,
      sessionId?: string,
    ) => void,
  ): unknown;
}

/** Minimal guest `WebContents` surface required by {@link GuestFileChooserController}. */
export interface GuestFileChooserWebContents
  extends GuestFileAttachmentWebContents {
  id: number;
  debugger?: GuestFileChooserDebugger | null;
  once?(event: 'destroyed', listener: () => void): unknown;
  on?(
    event: 'did-navigate',
    listener: (event?: unknown, url?: unknown) => void,
  ): unknown;
  removeListener?(
    event: 'did-navigate',
    listener: (event?: unknown, url?: unknown) => void,
  ): unknown;
}

/** Return type for {@link GuestFileChooserController.handleFileChooser}. */
export type HandleFileChooserResult =
  | ApplyInterceptedChooserResult
  | GuestIdpBlockedError
  | GuestFileActionError;

interface PendingChooserRecord extends PendingFileChooserTarget {
  chooserId: string;
  mainOrigin: string;
  verifyPromise: Promise<VerifySameOriginChooserFrameResult>;
  timer: ReturnType<typeof setTimeout>;
}

interface GuestFileChooserRecord {
  wc: GuestFileChooserWebContents;
  sender?: GuestFileChooserSender;
  fallbackContext?: GuestFileChooserFallbackContextProvider;
  policy?: GuestFileChooserPolicy;
  lastNavigatedUrl?: string;
  listenerAttached: boolean;
  navListenersAttached: boolean;
  detachNavListeners?: () => void;
  pageDomainEnabled: boolean;
  pageEnableEpoch: number;
  pageEnableInFlight?: Promise<boolean>;
  interceptEnabled?: boolean;
  pendingChooser?: PendingChooserRecord;
}

interface CdpFileChooserOpenedParams {
  backendNodeId?: unknown;
  mode?: unknown;
  frameId?: unknown;
}

/** Coordinates main-process CDP `DOM.setFileInputFiles` and `Page.fileChooserOpened` handling. */
export class GuestFileChooserController {
  private readonly records = new Map<number, GuestFileChooserRecord>();
  private nextChooserSeq = 1;
  private readonly chooserTimeoutMs: number;
  private readonly pageEnableTimeoutMs: number;

  constructor(opts?: GuestFileChooserControllerOptions) {
    this.chooserTimeoutMs =
      opts?.chooserTimeoutMs ?? DEFAULT_FILE_CHOOSER_TIMEOUT_MS;
    this.pageEnableTimeoutMs =
      opts?.pageEnableTimeoutMs ?? DEFAULT_FILE_CHOOSER_PAGE_ENABLE_TIMEOUT_MS;
  }

  /** Registers a guest `WebContents`, its host sender, and optional fallback suppression context. */
  registerGuest(
    wc: GuestFileChooserWebContents,
    sender?: GuestFileChooserSender,
    fallbackContext?: GuestFileChooserFallbackContextProvider,
  ): void {
    const rec = this.getOrCreateRecord(wc, sender);
    rec.sender = sender;
    if (fallbackContext) {
      rec.fallbackContext = fallbackContext;
    }
    this.attachListenersIfNeeded(rec);
  }

  /** Synchronizes CDP `Page.setInterceptFileChooserDialog` state for `wc`. */
  async syncInterception(wc: GuestFileChooserWebContents): Promise<boolean> {
    const rec = this.getOrCreateRecord(wc);
    this.attachListenersIfNeeded(rec);
    return this.syncInterceptDialogState(rec);
  }

  /** Updates the per-guest file chooser interception policy and arms CDP interception when enabled. */
  async setPolicy(
    wc: GuestFileChooserWebContents,
    sender: GuestFileChooserSender | undefined,
    policy: GuestFileChooserPolicy,
  ): Promise<boolean> {
    const rec = this.getOrCreateRecord(wc, sender);
    rec.sender = sender;
    rec.lastNavigatedUrl = undefined;
    rec.policy = { ...policy };
    this.attachListenersIfNeeded(rec);
    if (!policy.enabled) {
      this.cancelPendingChooser(rec);
      await this.syncPopupGuardAllowFlag(rec, false);
      await this.syncInterceptDialogState(rec);
      return true;
    }
    if (isGuestOnProtectedIdp(wc)) {
      rec.policy = { enabled: false, tabKey: policy.tabKey };
      await this.syncPopupGuardAllowFlag(rec, false);
      await this.syncInterceptDialogState(rec);
      return false;
    }
    const dbg = wc.debugger;
    if (!dbg || wc.isDestroyed?.() || !ensureGuestDebuggerAttached(dbg)) {
      await this.syncPopupGuardAllowFlag(rec, false);
      return false;
    }
    this.attachNavigationListenersIfNeeded(rec);
    if (isGuestOnBlankOrEmptyUrl(wc)) {
      return true;
    }
    return this.armInterceptionThenAllow(rec);
  }

  /**
   * Enables `Page` + file chooser interception and only then lets the
   * isolated-world popup guard pass `<input type=file>` clicks through, so a
   * failed interception can never fall back to a native OS picker.
   */
  private async armInterceptionThenAllow(
    rec: GuestFileChooserRecord,
  ): Promise<boolean> {
    const armed =
      (await this.enablePageDomain(rec)) &&
      (await this.syncInterceptDialogState(rec)) &&
      rec.interceptEnabled === true;
    await this.syncPopupGuardAllowFlag(rec, armed);
    return armed;
  }

  /** Whether the popup guard may let `<input type=file>` clicks reach the intercepted chooser. */
  isFileChooserAllowed(webContentsId: number): boolean {
    const rec = this.records.get(webContentsId);
    return Boolean(
      rec?.policy?.enabled &&
        rec.interceptEnabled === true &&
        !this.isRecordOnProtectedIdp(rec),
    );
  }

  /** Resolves or cancels the active intercepted file chooser for `wc`. */
  async handleFileChooser(
    wc: GuestFileChooserWebContents,
    decision: GuestFileChooserDecision,
  ): Promise<HandleFileChooserResult> {
    const rec = this.records.get(wc.id);
    if (
      isGuestOnProtectedIdp(wc) ||
      (rec && this.isRecordOnProtectedIdp(rec))
    ) {
      if (rec) {
        this.cancelPendingChooser(rec);
      }
      return idpBlockedErr();
    }
    const pending = rec?.pendingChooser;
    if (!rec || !pending) {
      return actionErr(
        'No open file chooser to handle (resolved or timed out)',
      );
    }
    if (typeof decision.chooserId !== 'string' || !decision.chooserId) {
      return actionErr('chooserId is required to handle a file chooser');
    }
    if (pending.chooserId !== decision.chooserId) {
      return actionErr(`File chooser ${decision.chooserId} is no longer active`);
    }
    const clearPending = (): void => {
      clearTimeout(pending.timer);
      if (rec.pendingChooser === pending) {
        rec.pendingChooser = undefined;
      }
    };
    const initCheck = await pending.verifyPromise;
    if (rec.pendingChooser !== pending) {
      return actionErr('File chooser was superseded or cancelled');
    }
    if (!initCheck.allowed || !pending.mainOrigin) {
      this.cancelPendingChooser(rec);
      return actionErr('Intercepted file chooser is cross-origin or inactive');
    }
    const dbg = wc.debugger;
    if (!dbg || wc.isDestroyed?.() || !ensureGuestDebuggerAttached(dbg)) {
      clearPending();
      return actionErr('Guest debugger is unavailable for file chooser');
    }
    const live = await verifySameOriginChooserFrame(dbg, wc, pending.frameId);
    if (rec.pendingChooser !== pending) {
      return actionErr('File chooser was superseded or cancelled');
    }
    if (!live.allowed || live.mainOrigin !== pending.mainOrigin) {
      this.cancelPendingChooser(rec);
      return actionErr(
        'Page or frame origin changed since file chooser opened',
      );
    }
    return applyInterceptedChooserDecision(dbg, pending, decision, clearPending);
  }

  /** Attaches staged `uploads/` files (or clears when empty) directly onto an `<input type="file">` DOM path. */
  async setInputFiles(
    wc: GuestFileAttachmentWebContents,
    request: AttachStagedFilesRequest,
  ): Promise<AttachStagedFilesResult> {
    return attachStagedFilesByDomPath(wc, request);
  }

  private isRecordOnProtectedIdp(rec: GuestFileChooserRecord): boolean {
    return Boolean(
      (rec.lastNavigatedUrl &&
        isProtectedIdentityProviderOriginUrl(rec.lastNavigatedUrl)) ||
        isGuestOnProtectedIdp(rec.wc),
    );
  }

  private getOrCreateRecord(
    wc: GuestFileChooserWebContents,
    sender?: GuestFileChooserSender,
  ): GuestFileChooserRecord {
    let rec = this.records.get(wc.id);
    if (!rec || rec.wc !== wc) {
      if (rec) {
        rec.pageEnableEpoch++;
        rec.detachNavListeners?.();
        this.cancelPendingChooser(rec);
      }
      rec = {
        wc,
        sender,
        listenerAttached: false,
        navListenersAttached: false,
        pageDomainEnabled: false,
        pageEnableEpoch: 0,
      };
      this.records.set(wc.id, rec);
      wc.once?.('destroyed', () => {
        const cur = this.records.get(wc.id);
        if (cur?.wc !== wc) {
          return;
        }
        cur.pageEnableEpoch++;
        cur.detachNavListeners?.();
        if (cur.pendingChooser) {
          clearTimeout(cur.pendingChooser.timer);
        }
        cur.pendingChooser = undefined;
        this.records.delete(wc.id);
      });
    }
    return rec;
  }

  private attachNavigationListenersIfNeeded(rec: GuestFileChooserRecord): void {
    if (rec.navListenersAttached || !rec.wc.on) {
      return;
    }
    rec.navListenersAttached = true;
    const onDidNavigate = (_event: unknown, url?: unknown): void => {
      this.cancelPendingChooser(rec);
      if (rec.wc.isDestroyed?.()) {
        return;
      }
      const targetUrl = (
        typeof url === 'string' && url.trim() !== ''
          ? url
          : (rec.wc.getURL?.() ?? '')
      ).trim();
      rec.lastNavigatedUrl = targetUrl;
      if (this.isRecordOnProtectedIdp(rec)) {
        void this.syncPopupGuardAllowFlag(rec, false);
        void this.syncInterceptDialogState(rec);
        return;
      }
      if (
        !rec.policy?.enabled ||
        !targetUrl ||
        /^about:blank(?:[?#].*)?$/i.test(targetUrl) ||
        !rec.wc.debugger ||
        !ensureGuestDebuggerAttached(rec.wc.debugger)
      ) {
        return;
      }
      void this.armInterceptionThenAllow(rec);
    };
    rec.wc.on('did-navigate', onDidNavigate);
    rec.detachNavListeners = () => {
      rec.wc.removeListener?.('did-navigate', onDidNavigate);
      rec.navListenersAttached = false;
      rec.detachNavListeners = undefined;
    };
  }

  private async enablePageDomain(rec: GuestFileChooserRecord): Promise<boolean> {
    if (rec.pageDomainEnabled) {
      return true;
    }
    if (rec.pageEnableInFlight) {
      return rec.pageEnableInFlight;
    }
    const dbg = rec.wc.debugger;
    if (!dbg || rec.wc.isDestroyed?.()) {
      return false;
    }
    if (typeof dbg.sendCommand !== 'function') {
      rec.pageDomainEnabled = true;
      return true;
    }
    const epoch = rec.pageEnableEpoch;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const tracked: Promise<boolean> = (async (): Promise<boolean> => {
      try {
        const bad = Symbol('bad');
        const res = await Promise.race([
          Promise.resolve(dbg.sendCommand?.('Page.enable')).catch(() => bad),
          new Promise<typeof bad>((r) => {
            timer = setTimeout(() => r(bad), this.pageEnableTimeoutMs);
          }),
        ]);
        if (
          res === bad ||
          rec.pageEnableEpoch !== epoch ||
          rec.wc.isDestroyed?.()
        ) {
          return false;
        }
        rec.pageDomainEnabled = true;
        return true;
      } finally {
        if (timer !== undefined) {
          clearTimeout(timer);
        }
      }
    })().finally(() => {
      if (rec.pageEnableInFlight === tracked) {
        rec.pageEnableInFlight = undefined;
      }
    });
    rec.pageEnableInFlight = tracked;
    return tracked;
  }

  private shouldInterceptFileChooser(rec: GuestFileChooserRecord): boolean {
    if (this.isRecordOnProtectedIdp(rec)) {
      // Keep suppressing native pickers on IdP pages; onFileChooserOpened
      // auto-cancels the intercepted chooser there.
      return rec.fallbackContext?.().suppressNativePopups === true;
    }
    return (
      rec.policy?.enabled === true ||
      rec.fallbackContext?.().suppressNativePopups === true
    );
  }

  private async syncInterceptDialogState(
    rec: GuestFileChooserRecord,
  ): Promise<boolean> {
    const dbg = rec.wc.debugger;
    if (!dbg || rec.wc.isDestroyed?.()) {
      return false;
    }
    const shouldIntercept = this.shouldInterceptFileChooser(rec);
    if (!shouldIntercept && !dbg.isAttached()) {
      rec.interceptEnabled = false;
      return true;
    }
    if (!ensureGuestDebuggerAttached(dbg)) {
      return false;
    }
    if (rec.interceptEnabled === shouldIntercept) {
      return true;
    }
    try {
      await dbg.sendCommand?.('Page.setInterceptFileChooserDialog', {
        enabled: shouldIntercept,
      });
      rec.interceptEnabled = shouldIntercept;
      return true;
    } catch {
      rec.interceptEnabled = undefined;
      return false;
    }
  }

  private async syncPopupGuardAllowFlag(
    rec: GuestFileChooserRecord,
    allow: boolean,
  ): Promise<void> {
    await syncPopupGuardIsolatedFlag(rec.wc, allow);
  }

  private attachListenersIfNeeded(rec: GuestFileChooserRecord): void {
    const dbg = rec.wc.debugger;
    if (rec.listenerAttached || !dbg?.on) {
      return;
    }
    rec.listenerAttached = true;
    dbg.on('detach', () => {
      rec.pageEnableEpoch++;
      rec.pageDomainEnabled = false;
      rec.pageEnableInFlight = undefined;
      rec.interceptEnabled = undefined;
      void this.syncPopupGuardAllowFlag(rec, false);
      if (rec.pendingChooser) {
        clearTimeout(rec.pendingChooser.timer);
        rec.pendingChooser = undefined;
      }
    });
    dbg.on('message', (_event, method, rawParams) => {
      if (method === 'Page.fileChooserOpened') {
        this.onFileChooserOpened(rec, rawParams);
      }
    });
  }

  private onFileChooserOpened(
    rec: GuestFileChooserRecord,
    rawParams: unknown,
  ): void {
    const params =
      rawParams && typeof rawParams === 'object'
        ? (rawParams as CdpFileChooserOpenedParams)
        : undefined;
    const backendNodeId =
      typeof params?.backendNodeId === 'number' && params.backendNodeId > 0
        ? params.backendNodeId
        : undefined;
    const policyEnabled =
      rec.policy?.enabled === true && !this.isRecordOnProtectedIdp(rec);
    if (!policyEnabled) {
      if (
        backendNodeId !== undefined &&
        (rec.fallbackContext?.().suppressNativePopups === true ||
          rec.policy?.enabled === true)
      ) {
        void rec.wc.debugger
          ?.sendCommand?.('DOM.setFileInputFiles', { files: [], backendNodeId })
          .catch(() => undefined);
      }
      return;
    }
    this.cancelPendingChooser(rec);
    const chooserId = `fc_${this.nextChooserSeq++}`;
    const multiple = params?.mode === 'selectMultiple';
    const frameId =
      typeof params?.frameId === 'string' && params.frameId
        ? params.frameId
        : undefined;
    const url = rec.wc.getURL?.() || '';
    const tabKey = rec.policy?.tabKey ?? rec.fallbackContext?.().tabKey;
    const dbg = rec.wc.debugger;
    const timer = setTimeout(() => {
      if (rec.pendingChooser?.chooserId === chooserId) {
        rec.pendingChooser = undefined;
        if (backendNodeId !== undefined) {
          void dbg
            ?.sendCommand?.('DOM.setFileInputFiles', {
              files: [],
              backendNodeId,
            })
            .catch(() => undefined);
        }
      }
    }, this.chooserTimeoutMs);
    const verifyPromise = verifySameOriginChooserFrame(dbg, rec.wc, frameId);
    rec.pendingChooser = {
      chooserId,
      frameId,
      backendNodeId,
      multiple,
      mainOrigin: '',
      verifyPromise,
      timer,
    };
    void verifyPromise.then(
      async ({ allowed, mainOrigin, frameOrigin, frameUrl }) => {
        if (rec.pendingChooser?.chooserId !== chooserId) {
          return;
        }
        if (
          !allowed ||
          !rec.policy?.enabled ||
          this.isRecordOnProtectedIdp(rec)
        ) {
          this.cancelPendingChooser(rec);
          return;
        }
        rec.pendingChooser.mainOrigin = mainOrigin;
        const domPath = dbg
          ? await resolveConnectedDomPathForBackendNode(
              dbg,
              frameId,
              backendNodeId,
            )
          : undefined;
        if (
          rec.pendingChooser?.chooserId !== chooserId ||
          rec.wc.isDestroyed?.()
        ) {
          return;
        }
        if (!rec.sender || rec.sender.isDestroyed()) {
          this.cancelPendingChooser(rec);
          return;
        }
        try {
          rec.sender.send(WEBVIEW_FILE_CHOOSER_OPENED_CHANNEL, {
            chooserId,
            webContentsId: rec.wc.id,
            ...(tabKey ? { tabKey } : {}),
            multiple,
            url,
            frameOrigin: frameOrigin || mainOrigin,
            ...(frameUrl ? { frameUrl } : {}),
            ...(domPath ? { domPath } : {}),
          });
        } catch {
          // Ignore send errors on tearing-down renderer frames.
        }
      },
    );
  }

  private cancelPendingChooser(rec: GuestFileChooserRecord): void {
    const pending = rec.pendingChooser;
    if (!pending) {
      return;
    }
    clearTimeout(pending.timer);
    rec.pendingChooser = undefined;
    if (pending.backendNodeId !== undefined) {
      void rec.wc.debugger
        ?.sendCommand?.('DOM.setFileInputFiles', {
          files: [],
          backendNodeId: pending.backendNodeId,
        })
        .catch(() => undefined);
    }
  }
}

/** Singleton main-process controller coordinating intercepted guest file choosers. */
export const guestFileChooserController = new GuestFileChooserController();
