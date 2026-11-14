import { randomBytes } from 'crypto';
import { registerGuestAgentControlPredicate } from './guestAgentControl';
import {
  ensureGuestDebuggerAttached,
  type GuestDebugger,
} from './guestDebugger';
import { guestPromptController } from './guestPromptController';
import {
  executeJavaScriptWhenReady,
  type CanExecuteJsWebContents,
  type MainWorldWebContents,
  type WebContentsLifecycleEvent,
} from './isolatedWorldExec';

/** Renderer push channel for `Page.javascriptDialogOpening` events. */
export const WEBVIEW_DIALOG_OPENING_CHANNEL =
  'webview:javascript-dialog-opening';

/** Default fail-safe timeout (ms) for an unhandled `'manual'` dialog before auto-resolving. */
export const DEFAULT_MANUAL_DIALOG_TIMEOUT_MS = 10000;

/** Default timeout (ms) bounding CDP `Page.enable` so uninitialized targets cannot hang callers. */
export const DEFAULT_PAGE_ENABLE_TIMEOUT_MS = 1500;

/** JavaScript dialog types supported by CDP and main-world interception. */
export type DialogType = 'alert' | 'confirm' | 'prompt' | 'beforeunload';

/** Policy actions controlling how guest JavaScript dialogs are resolved. */
export type DialogPolicyAction = 'accept' | 'dismiss' | 'manual';

/** Fallback actions used when a `'manual'` dialog cannot pause synchronously. */
export type DialogFallbackAction = 'accept' | 'dismiss';

/** Per-guest dialog interception policy configured by the embedder renderer. */
export interface DialogPolicy {
  enabled: boolean;
  action?: DialogPolicyAction;
  promptText?: string;
  tabKey?: string;
  fallbackAction?: DialogFallbackAction;
  fallbackPromptText?: string;
}

/** Resolution decision supplied for a pending `'manual'` dialog. */
export interface DialogDecision {
  dialogId?: string;
  accept: boolean;
  promptText?: string;
}

/** Agent step fallback metadata supplied by the WCV z-order controller. */
export interface DialogFallbackContext {
  tabKey?: string;
  isAgentStepActive?: boolean;
}

/** Lazy provider returning current fallback context for a guest `WebContents`. */
export type DialogFallbackContextProvider = () =>
  | DialogFallbackContext
  | null
  | undefined;

/** Main-world dialog configuration serialized into the bootstrap script. */
export interface MainWorldDialogConfig {
  action: 'accept' | 'dismiss';
  hasPromptText: boolean;
  promptText: string;
}

/** Payload emitted on {@link WEBVIEW_DIALOG_OPENING_CHANNEL} to the host renderer. */
export interface DialogOpeningPayload {
  dialogId: string;
  webContentsId: number;
  type: DialogType;
  message: string;
  defaultPrompt: string;
  url: string;
  handled: boolean;
  accepted?: boolean;
  tabKey?: string;
  userInput?: string;
}

/** Options accepted by {@link GuestDialogController}. */
export interface GuestDialogControllerOptions {
  manualTimeoutMs?: number;
  pageEnableTimeoutMs?: number;
}

/** Host renderer `WebContents` surface used to deliver dialog notifications. */
export interface DialogHostSender {
  isDestroyed(): boolean;
  send(channel: string, payload: DialogOpeningPayload): void;
}

/** Minimal `Debugger` surface required for CDP dialog and binding control. */
export interface DialogGuestDebugger extends GuestDebugger {
  sendCommand?(
    method: string,
    commandParams?: Record<string, unknown>,
  ): Promise<unknown>;
  on?(event: 'detach', listener: () => void): unknown;
  on?(
    event: 'message',
    listener: (event: unknown, method: string, params: unknown) => void,
  ): unknown;
}

/** Navigation details surface read from Electron's `did-start-navigation` event. */
export interface DialogDidStartNavigationDetailsLike {
  url?: string;
  isMainFrame?: boolean;
}

/** Listener signature for guest `did-start-navigation` events. */
export type DialogDidStartNavigationListener = (
  details?: DialogDidStartNavigationDetailsLike | null,
  url?: string,
  isInPlace?: boolean,
  isMainFrame?: boolean,
) => void;

/** Listener signature for guest `did-navigate` events. */
export type DialogDidNavigateListener = (
  event?: unknown,
  url?: string,
) => void;

/** Minimal guest `WebContents` surface required by {@link GuestDialogController}. */
export interface DialogGuestWebContents extends CanExecuteJsWebContents {
  id: number;
  debugger?: DialogGuestDebugger | null;
  executeJavaScript?(code: string, userGesture?: boolean): Promise<unknown>;
  on?(event: WebContentsLifecycleEvent, listener: () => void): unknown;
  on?(
    event: 'did-start-navigation',
    listener: DialogDidStartNavigationListener,
  ): unknown;
  on?(event: 'did-navigate', listener: DialogDidNavigateListener): unknown;
  removeListener?(
    event: WebContentsLifecycleEvent,
    listener: () => void,
  ): unknown;
  removeListener?(
    event: 'did-start-navigation',
    listener: DialogDidStartNavigationListener,
  ): unknown;
  removeListener?(
    event: 'did-navigate',
    listener: DialogDidNavigateListener,
  ): unknown;
}

/** Window surface passed into {@link runMainWorldDialogBootstrap} and {@link runMainWorldDialogDisarm}. */
export interface MainWorldDialogWindow {
  Object?: typeof Object;
  location?: { href?: unknown } | null;
  frames?: ArrayLike<MainWorldDialogWindow | null | undefined>;
}

/** Global object surface passed into {@link runMainWorldDialogBootstrap} and {@link runMainWorldDialogDisarm}. */
export type MainWorldDialogGlobal = object;

type DynamicWindow = MainWorldDialogWindow & {
  [key: string]: unknown;
  [key: symbol]: unknown;
};

interface PendingManualDialog {
  dialogId: string;
  type: DialogType;
  defaultPrompt: string;
  timer: ReturnType<typeof setTimeout>;
}

interface GuestDialogRecord {
  wc: DialogGuestWebContents;
  sender: DialogHostSender | null | undefined;
  policy?: DialogPolicy;
  fallbackContext?: DialogFallbackContextProvider;
  listenerAttached: boolean;
  navListenersAttached: boolean;
  detachNavListeners?: () => void;
  pageDomainEnabled: boolean;
  pageEnableEpoch: number;
  pageEnableInFlight?: Promise<boolean>;
  pendingManual?: PendingManualDialog;
  bindingName: string;
  authToken: string;
  controlKey: string;
  controlToken: string;
  bindingAdded: boolean;
  preloadScriptId?: string;
  mainWorldSyncChain?: Promise<void>;
}

function isBlankOrEmptyGuestUrl(rawUrl: string | null | undefined): boolean {
  const trimmed = (rawUrl ?? '').trim();
  return trimmed === '' || trimmed.toLowerCase() === 'about:blank';
}

function isGuestOnBlankOrEmptyUrl(wc: DialogGuestWebContents): boolean {
  return typeof wc.getURL === 'function' && isBlankOrEmptyGuestUrl(wc.getURL());
}

function normalizeDialogType(raw: unknown): DialogType {
  return raw === 'alert' ||
    raw === 'confirm' ||
    raw === 'prompt' ||
    raw === 'beforeunload'
    ? raw
    : 'alert';
}

function defaultAcceptForDismissPolicy(dialogType: DialogType): boolean {
  return dialogType === 'alert' || dialogType === 'beforeunload';
}

function hasMainWorldExecution(
  wc: DialogGuestWebContents,
): wc is DialogGuestWebContents & MainWorldWebContents {
  return typeof wc.executeJavaScript === 'function';
}

async function executeMainWorldFallback(
  wc: DialogGuestWebContents,
  code: string,
): Promise<void> {
  if (hasMainWorldExecution(wc)) {
    await executeJavaScriptWhenReady(wc, code).catch(() => undefined);
  }
}

function removePreloadScriptSafely(
  dbg: DialogGuestDebugger,
  identifier: string,
): Promise<void> {
  if (typeof dbg.sendCommand !== 'function') {
    return Promise.resolve();
  }
  return dbg
    .sendCommand('Page.removeScriptToEvaluateOnNewDocument', { identifier })
    .then(
      () => undefined,
      () => undefined,
    );
}

function dismissNativeDialogSafely(
  wc: DialogGuestWebContents,
  dialogType: DialogType,
): void {
  void wc.debugger
    ?.sendCommand?.('Page.handleJavaScriptDialog', {
      accept: defaultAcceptForDismissPolicy(dialogType),
    })
    .catch(() => undefined);
}

function resetRecordLifecycle(rec: GuestDialogRecord): void {
  rec.pageEnableEpoch++;
  rec.detachNavListeners?.();
  if (rec.pendingManual) {
    clearTimeout(rec.pendingManual.timer);
    rec.pendingManual = undefined;
  }
}

/**
 * Self-contained main-world bootstrap function executed inside the guest frame
 * (and serialized via `.toString()` for CDP `Page.addScriptToEvaluateOnNewDocument`).
 */
export function runMainWorldDialogBootstrap(
  winRoot: MainWorldDialogWindow | null | undefined,
  globalRoot: MainWorldDialogGlobal | null | undefined,
  bindingName: string,
  authToken: string,
  controlKey: string,
  controlToken: string,
  initialCfg: MainWorldDialogConfig | null | undefined,
): void {
  let capturedBind: ((payload: string) => void) | null = null;
  try {
    const candidate = (globalRoot as Record<string, unknown>)[bindingName];
    if (typeof candidate === 'function') {
      capturedBind = candidate as (payload: string) => void;
    }
    delete (globalRoot as Record<string, unknown>)[bindingName];
  } catch {
    // Ignore frozen globalThis properties.
  }
  if (!winRoot || !initialCfg) {
    return;
  }
  const sym = Symbol.for(controlKey);
  const toJson = JSON.stringify;
  const Str = String;
  let sharedReportFn = capturedBind;
  const sharedQueue: string[] = [];
  const flushSharedQueue = (): void => {
    if (typeof sharedReportFn !== 'function') {
      return;
    }
    while (sharedQueue.length > 0) {
      const nextItem = sharedQueue.shift();
      if (nextItem !== undefined) {
        try {
          sharedReportFn(nextItem);
        } catch {
          // Ignore binding errors during navigation teardown.
        }
      }
    }
  };
  type DialogFnKey = 'alert' | 'confirm' | 'prompt';
  interface SavedDialogDescriptor {
    own: boolean;
    desc: PropertyDescriptor | undefined;
    fn: unknown;
  }
  const installOnWindow = (targetWin: MainWorldDialogWindow): void => {
    const WinObj = targetWin.Object || Object;
    const defProp = WinObj.defineProperty;
    const getDesc = WinObj.getOwnPropertyDescriptor;
    const hasOwn = WinObj.prototype.hasOwnProperty;
    const snap = (key: DialogFnKey): SavedDialogDescriptor => ({
      own: hasOwn.call(targetWin, key),
      desc: getDesc(targetWin, key),
      fn: (targetWin as DynamicWindow)[key],
    });
    const saved: Record<DialogFnKey, SavedDialogDescriptor> = {
      alert: snap('alert'),
      confirm: snap('confirm'),
      prompt: snap('prompt'),
    };
    let cur: MainWorldDialogConfig | null = initialCfg;
    let active = true;
    const setFallbackSym = Symbol.for('__agy_set_prompt_fallback__');
    const restore = (key: DialogFnKey): void => {
      const entry = saved[key];
      try {
        if (entry.own && entry.desc) {
          defProp(targetWin, key, entry.desc);
        } else {
          delete (targetWin as DynamicWindow)[key];
          if (
            (targetWin as DynamicWindow)[key] !== entry.fn &&
            typeof entry.fn === 'function'
          ) {
            (targetWin as DynamicWindow)[key] = entry.fn;
          }
        }
      } catch {
        // Ignore frozen window teardown errors.
      }
    };
    const emit = (
      type: DialogType,
      msg: string,
      def: string,
      acc: boolean,
      inp: string | undefined,
    ): void => {
      try {
        let u = '';
        try {
          const loc = targetWin.location || winRoot.location;
          u = loc ? Str(loc.href || '') : '';
        } catch {
          u = '';
        }
        const payload: {
          token: string;
          type: DialogType;
          message: string;
          defaultPrompt: string;
          url: string;
          accepted: boolean;
          userInput?: string;
        } = {
          token: authToken,
          type,
          message: msg,
          defaultPrompt: def,
          url: u,
          accepted: acc,
        };
        if (inp !== undefined) {
          payload.userInput = inp;
        }
        const raw = toJson(payload);
        if (typeof sharedReportFn === 'function') {
          sharedReportFn(raw);
        } else if (sharedQueue.length < 64) {
          sharedQueue.push(raw);
        }
      } catch {
        // Ignore serialization errors.
      }
    };
    const wAlert = function (...args: unknown[]): unknown {
      if (!active || !cur) {
        return typeof saved.alert.fn === 'function'
          ? saved.alert.fn.apply(targetWin, args)
          : undefined;
      }
      emit('alert', args.length === 0 ? '' : Str(args[0]), '', true, undefined);
      return undefined;
    };
    const wConfirm = function (message?: unknown): boolean {
      if (!active || !cur) {
        return typeof saved.confirm.fn === 'function'
          ? Boolean(saved.confirm.fn.call(targetWin, message))
          : false;
      }
      const accepted = cur.action === 'accept';
      emit(
        'confirm',
        message === undefined ? '' : Str(message),
        '',
        accepted,
        undefined,
      );
      return accepted;
    };
    const wPrompt = function (
      message?: unknown,
      defaultValue?: unknown,
    ): string | null {
      if (!active || !cur) {
        return typeof saved.prompt.fn === 'function'
          ? ((saved.prompt.fn.call(targetWin, message, defaultValue) as
              | string
              | null
              | undefined) ?? null)
          : null;
      }
      const ms = message === undefined ? '' : Str(message);
      const ds = defaultValue === undefined ? '' : Str(defaultValue);
      if (cur.action !== 'accept') {
        emit('prompt', ms, ds, false, undefined);
        return null;
      }
      const ans = cur.hasPromptText ? Str(cur.promptText) : ds;
      emit('prompt', ms, ds, true, ans);
      return ans;
    };
    const ctrl = (
      tok: string,
      next: MainWorldDialogConfig | null,
      nextBind?: ((payload: string) => void) | null,
    ): boolean => {
      if (tok !== controlToken) {
        return false;
      }
      if (typeof nextBind === 'function') {
        sharedReportFn = nextBind;
        flushSharedQueue();
      }
      if (!next) {
        if (active) {
          active = false;
          cur = null;
          restore('alert');
          restore('confirm');
          restore('prompt');
          try {
            delete (targetWin as DynamicWindow)[sym];
          } catch {
            // Ignore frozen window teardown errors.
          }
        }
        return true;
      }
      cur = next;
      return true;
    };
    try {
      defProp(wPrompt, setFallbackSym, {
        value: (fn: unknown) => {
          if (typeof fn === 'function') {
            saved.prompt = {
              own: true,
              desc: {
                value: fn,
                writable: true,
                enumerable: true,
                configurable: true,
              },
              fn,
            };
          }
        },
        writable: false,
        enumerable: false,
        configurable: false,
      });
    } catch {
      // Ignore frozen function prototypes.
    }
    defProp(targetWin, sym, {
      value: ctrl,
      writable: false,
      enumerable: false,
      configurable: true,
    });
    defProp(targetWin, 'alert', {
      value: wAlert,
      writable: true,
      enumerable: true,
      configurable: true,
    });
    defProp(targetWin, 'confirm', {
      value: wConfirm,
      writable: true,
      enumerable: true,
      configurable: true,
    });
    defProp(targetWin, 'prompt', {
      value: wPrompt,
      writable: true,
      enumerable: true,
      configurable: true,
    });
  };
  const seen: MainWorldDialogWindow[] = [];
  const visitFrame = (w: MainWorldDialogWindow, depth: number): void => {
    if (!w || depth > 8 || seen.indexOf(w) !== -1) {
      return;
    }
    seen.push(w);
    let updated = false;
    try {
      const existing = (w as DynamicWindow)[sym];
      if (typeof existing === 'function') {
        updated = existing(controlToken, initialCfg, capturedBind) === true;
      }
    } catch {
      updated = false;
    }
    if (!updated) {
      try {
        installOnWindow(w);
      } catch {
        // Cross-origin frame access denied; handled via CDP Page.javascriptDialogOpening.
      }
    }
    let frameCount = 0;
    try {
      const frames = w.frames;
      frameCount = typeof frames?.length === 'number' ? frames.length : 0;
    } catch {
      frameCount = 0;
    }
    for (let i = 0; i < frameCount; i++) {
      let child: MainWorldDialogWindow | null | undefined = null;
      try {
        child = w.frames?.[i];
      } catch {
        child = null;
      }
      if (child) {
        visitFrame(child, depth + 1);
      }
    }
  };
  visitFrame(winRoot, 0);
}

/**
 * Self-contained main-world disarm function restoring native dialog descriptors
 * across `winRoot` and all accessible nested same-origin subframes.
 */
export function runMainWorldDialogDisarm(
  winRoot: MainWorldDialogWindow | null | undefined,
  globalRoot: MainWorldDialogGlobal | null | undefined,
  bindingName: string,
  controlKey: string,
  controlToken: string,
): void {
  try {
    delete (globalRoot as Record<string, unknown>)[bindingName];
  } catch {
    // Ignore frozen globalThis properties.
  }
  if (!winRoot) {
    return;
  }
  const sym = Symbol.for(controlKey);
  const seen: MainWorldDialogWindow[] = [];
  const visitFrame = (w: MainWorldDialogWindow, depth: number): void => {
    if (!w || depth > 8 || seen.indexOf(w) !== -1) {
      return;
    }
    seen.push(w);
    try {
      const fn = (w as DynamicWindow)[sym];
      if (typeof fn === 'function' && fn(controlToken, null) === true) {
        delete (w as DynamicWindow)[sym];
      }
    } catch {
      // Ignore cross-origin frame errors.
    }
    let frameCount = 0;
    try {
      const frames = w.frames;
      frameCount = typeof frames?.length === 'number' ? frames.length : 0;
    } catch {
      frameCount = 0;
    }
    for (let i = 0; i < frameCount; i++) {
      let child: MainWorldDialogWindow | null | undefined = null;
      try {
        child = w.frames?.[i];
      } catch {
        child = null;
      }
      if (child) {
        visitFrame(child, depth + 1);
      }
    }
  };
  visitFrame(winRoot, 0);
}

/**
 * Builds the main-world bootstrap script that synchronously intercepts
 * `window.alert`, `window.confirm`, and `window.prompt` while armed so no macOS
 * `NSAlert` sheet opens and `window.prompt()` works in sandboxed Electron WCVs.
 */
export function buildMainWorldDialogBootstrapScript(
  bindingName: string,
  authToken: string,
  controlKey: string,
  controlToken: string,
  cfg: MainWorldDialogConfig,
): string {
  return `(${runMainWorldDialogBootstrap.toString()})(typeof window!=='undefined'?window:undefined,globalThis,${JSON.stringify(bindingName)},${JSON.stringify(authToken)},${JSON.stringify(controlKey)},${JSON.stringify(controlToken)},${JSON.stringify(cfg)});`;
}

/** Builds the main-world teardown script restoring native dialog descriptors. */
export function buildMainWorldDialogDisarmScript(
  bindingName: string,
  controlKey: string,
  controlToken: string,
): string {
  return `(${runMainWorldDialogDisarm.toString()})(typeof window!=='undefined'?window:undefined,globalThis,${JSON.stringify(bindingName)},${JSON.stringify(controlKey)},${JSON.stringify(controlToken)});`;
}

/**
 * Coordinates non-blocking synchronous main-world `alert`/`confirm`/`prompt`
 * interception plus CDP `Page.javascriptDialogOpening` capture and
 * `Page.handleJavaScriptDialog` resolution across `<webview>` and `WebContentsView`.
 */
export class GuestDialogController {
  private readonly records = new Map<number, GuestDialogRecord>();
  private nextDialogSeq = 1;
  private readonly manualTimeoutMs: number;
  private readonly pageEnableTimeoutMs: number;

  constructor(opts?: GuestDialogControllerOptions) {
    this.manualTimeoutMs =
      opts?.manualTimeoutMs ?? DEFAULT_MANUAL_DIALOG_TIMEOUT_MS;
    this.pageEnableTimeoutMs =
      opts?.pageEnableTimeoutMs ?? DEFAULT_PAGE_ENABLE_TIMEOUT_MS;
  }

  /** Registers a guest `WebContents` and optional agent-step fallback context. */
  registerGuest(
    wc: DialogGuestWebContents,
    sender: DialogHostSender | null | undefined,
    fallbackContext?: DialogFallbackContextProvider,
  ): void {
    const rec = this.getOrCreateRecord(wc, sender);
    rec.sender = sender;
    if (fallbackContext) {
      rec.fallbackContext = fallbackContext;
    }
    this.attachListenersIfNeeded(rec);
  }

  /** Re-evaluates interception state after a change in the guest's fallback context. */
  async syncFallbackContext(
    wc: DialogGuestWebContents | null | undefined,
  ): Promise<boolean> {
    if (!wc || typeof wc.id !== 'number') {
      return false;
    }
    const rec = this.records.get(wc.id);
    if (!rec || rec.wc !== wc) {
      return false;
    }
    if (!this.isInterceptionArmed(rec)) {
      this.flushPendingManual(rec);
      if (this.supportsMainWorldInterception(rec)) {
        await this.syncMainWorldInterceptor(rec);
      }
      return true;
    }
    guestPromptController.cancelForGuest(wc.id);
    if (!this.supportsMainWorldInterception(rec)) {
      return true;
    }
    return this.ensureArmedDomainAndSync(rec);
  }

  /** Updates the per-guest dialog policy and arms or disarms interception. */
  async setPolicy(
    wc: DialogGuestWebContents,
    sender: DialogHostSender | null | undefined,
    policy: DialogPolicy,
  ): Promise<boolean> {
    const rec = this.getOrCreateRecord(wc, sender);
    rec.sender = sender;
    rec.policy = { ...policy };
    this.attachListenersIfNeeded(rec);
    if (!policy.enabled) {
      this.flushPendingManual(rec);
      if (this.supportsMainWorldInterception(rec)) {
        await this.syncMainWorldInterceptor(rec);
      }
      return true;
    }
    guestPromptController.cancelForGuest(wc.id);
    return this.ensureArmedDomainAndSync(rec);
  }

  private async ensureArmedDomainAndSync(
    rec: GuestDialogRecord,
  ): Promise<boolean> {
    const dbg = rec.wc.debugger;
    if (!dbg || rec.wc.isDestroyed?.()) {
      return false;
    }
    if (!ensureGuestDebuggerAttached(dbg)) {
      return false;
    }
    this.attachListenersIfNeeded(rec);
    this.attachNavigationListenersIfNeeded(rec);
    if (!rec.pageDomainEnabled) {
      if (isGuestOnBlankOrEmptyUrl(rec.wc)) {
        return true;
      }
      return this.enablePageDomain(rec);
    }
    if (this.supportsMainWorldInterception(rec)) {
      await this.syncMainWorldInterceptor(rec);
    }
    return true;
  }

  /** Resolves a pending `'manual'` dialog via CDP `Page.handleJavaScriptDialog`. */
  async handleDialog(
    wc: DialogGuestWebContents,
    decision: DialogDecision,
  ): Promise<boolean> {
    const rec = this.records.get(wc.id);
    if (!rec?.pendingManual) {
      return false;
    }
    if (decision.dialogId && rec.pendingManual.dialogId !== decision.dialogId) {
      return false;
    }
    clearTimeout(rec.pendingManual.timer);
    rec.pendingManual = undefined;
    const dbg = wc.debugger;
    if (!dbg || wc.isDestroyed?.()) {
      return false;
    }
    const params: { accept: boolean; promptText?: string } = {
      accept: Boolean(decision.accept),
    };
    if (decision.promptText !== undefined) {
      params.promptText = String(decision.promptText);
    }
    try {
      await dbg.sendCommand?.('Page.handleJavaScriptDialog', params);
      return true;
    } catch {
      return false;
    }
  }

  /**
   * Dismisses any open native JavaScript dialog and cleans up CDP dialog state
   * for `wc` immediately before `WebContents` teardown (`wcvBrowserRegistry.dispose`).
   */
  disposeGuest(wc: DialogGuestWebContents | null | undefined): void {
    if (!wc || typeof wc.id !== 'number') {
      return;
    }
    const rec = this.records.get(wc.id);
    const preloadScriptId = rec?.preloadScriptId;
    const bindingName = rec?.bindingAdded ? rec.bindingName : undefined;
    if (rec && rec.wc === wc) {
      resetRecordLifecycle(rec);
      rec.preloadScriptId = undefined;
      rec.bindingAdded = false;
      this.records.delete(wc.id);
    }
    const dbg = wc.debugger;
    if (
      !wc.isDestroyed?.() &&
      dbg?.isAttached?.() &&
      typeof dbg.sendCommand === 'function'
    ) {
      if (preloadScriptId) {
        void removePreloadScriptSafely(dbg, preloadScriptId);
      }
      if (bindingName) {
        void dbg
          .sendCommand('Runtime.removeBinding', { name: bindingName })
          .catch(() => undefined);
      }
      void dbg
        .sendCommand('Page.handleJavaScriptDialog', { accept: false })
        .catch(() => undefined);
    }
  }

  private supportsMainWorldInterception(rec: GuestDialogRecord): boolean {
    return hasMainWorldExecution(rec.wc);
  }

  private isInterceptionArmed(rec: GuestDialogRecord): boolean {
    return (
      rec.policy?.enabled === true ||
      rec.fallbackContext?.()?.isAgentStepActive === true
    );
  }

  private resolveMainWorldConfig(
    rec: GuestDialogRecord,
  ): MainWorldDialogConfig | null {
    if (rec.policy?.enabled === true) {
      const action = rec.policy.action ?? 'dismiss';
      const effectiveAction =
        action === 'accept' ||
        (action === 'manual' && rec.policy.fallbackAction === 'accept')
          ? 'accept'
          : 'dismiss';
      const rawPrompt =
        action === 'manual'
          ? (rec.policy.fallbackPromptText ?? rec.policy.promptText)
          : rec.policy.promptText;
      return {
        action: effectiveAction,
        hasPromptText: rawPrompt !== undefined,
        promptText: rawPrompt ?? '',
      };
    }
    if (rec.fallbackContext?.()?.isAgentStepActive === true) {
      return { action: 'dismiss', hasPromptText: false, promptText: '' };
    }
    return null;
  }

  private syncMainWorldInterceptor(rec: GuestDialogRecord): Promise<void> {
    if (!this.supportsMainWorldInterception(rec)) {
      return Promise.resolve();
    }
    const run = async (): Promise<void> => {
      const dbg = rec.wc.debugger;
      if (!dbg || rec.wc.isDestroyed?.()) {
        return;
      }
      const cfg = this.resolveMainWorldConfig(rec);
      if (cfg !== null) {
        if (!rec.pageDomainEnabled || typeof dbg.sendCommand !== 'function') {
          return;
        }
        const epoch = rec.pageEnableEpoch;
        if (!rec.bindingAdded) {
          try {
            await dbg.sendCommand('Runtime.addBinding', {
              name: rec.bindingName,
            });
            if (rec.pageEnableEpoch !== epoch || rec.wc.isDestroyed?.()) {
              return;
            }
            rec.bindingAdded = true;
          } catch {
            return;
          }
        }
        if (rec.preloadScriptId) {
          const prevId = rec.preloadScriptId;
          rec.preloadScriptId = undefined;
          await removePreloadScriptSafely(dbg, prevId);
          if (rec.pageEnableEpoch !== epoch || rec.wc.isDestroyed?.()) {
            return;
          }
        }
        const latestCfg = this.resolveMainWorldConfig(rec);
        if (!latestCfg) {
          return;
        }
        const source = buildMainWorldDialogBootstrapScript(
          rec.bindingName,
          rec.authToken,
          rec.controlKey,
          rec.controlToken,
          latestCfg,
        );
        try {
          const res = await dbg.sendCommand(
            'Page.addScriptToEvaluateOnNewDocument',
            { source, runImmediately: true },
          );
          if (rec.pageEnableEpoch !== epoch || rec.wc.isDestroyed?.()) {
            return;
          }
          const identifier =
            res && typeof res === 'object'
              ? (res as { identifier?: unknown }).identifier
              : undefined;
          if (typeof identifier === 'string') {
            rec.preloadScriptId = identifier;
          } else if (!isGuestOnBlankOrEmptyUrl(rec.wc)) {
            await executeMainWorldFallback(rec.wc, source);
          }
        } catch {
          if (!isGuestOnBlankOrEmptyUrl(rec.wc)) {
            await executeMainWorldFallback(rec.wc, source);
          }
        }
        return;
      }
      if (typeof dbg.sendCommand === 'function' && dbg.isAttached?.()) {
        if (rec.preloadScriptId) {
          const prevId = rec.preloadScriptId;
          rec.preloadScriptId = undefined;
          await removePreloadScriptSafely(dbg, prevId);
        }
        if (rec.bindingAdded) {
          rec.bindingAdded = false;
          await dbg
            .sendCommand('Runtime.removeBinding', { name: rec.bindingName })
            .catch(() => undefined);
        }
        if (!isGuestOnBlankOrEmptyUrl(rec.wc) && !rec.wc.isDestroyed?.()) {
          const disarmSource = buildMainWorldDialogDisarmScript(
            rec.bindingName,
            rec.controlKey,
            rec.controlToken,
          );
          try {
            const evalRes = await dbg.sendCommand('Runtime.evaluate', {
              expression: disarmSource,
            });
            const evalResult =
              evalRes && typeof evalRes === 'object'
                ? (evalRes as { result?: unknown }).result
                : undefined;
            if (!evalRes || typeof evalResult !== 'object') {
              await executeMainWorldFallback(rec.wc, disarmSource);
            }
          } catch {
            await executeMainWorldFallback(rec.wc, disarmSource);
          }
        }
      }
    };
    const next = (rec.mainWorldSyncChain ?? Promise.resolve()).then(run, run);
    rec.mainWorldSyncChain = next.finally(() => {
      if (rec.mainWorldSyncChain === next) {
        rec.mainWorldSyncChain = undefined;
      }
    });
    return next;
  }

  private getOrCreateRecord(
    wc: DialogGuestWebContents,
    sender: DialogHostSender | null | undefined,
  ): GuestDialogRecord {
    let rec = this.records.get(wc.id);
    if (!rec || rec.wc !== wc) {
      if (rec) {
        resetRecordLifecycle(rec);
      }
      rec = {
        wc,
        sender,
        listenerAttached: false,
        navListenersAttached: false,
        pageDomainEnabled: false,
        pageEnableEpoch: 0,
        bindingName: `__agyDlgBind_${randomBytes(16).toString('hex')}`,
        authToken: randomBytes(32).toString('hex'),
        controlKey: `__agyDlgCtrl_${randomBytes(32).toString('hex')}`,
        controlToken: randomBytes(32).toString('hex'),
        bindingAdded: false,
      };
      this.records.set(wc.id, rec);
      wc.once?.('destroyed', () => {
        const cur = this.records.get(wc.id);
        if (cur?.wc !== wc) {
          return;
        }
        resetRecordLifecycle(cur);
        this.records.delete(wc.id);
      });
    }
    return rec;
  }

  private attachNavigationListenersIfNeeded(rec: GuestDialogRecord): void {
    if (rec.navListenersAttached || !rec.wc.on) {
      return;
    }
    rec.navListenersAttached = true;
    const onDidStartNavigation: DialogDidStartNavigationListener = (
      details,
      url,
      _isInPlace,
      isMainFrame,
    ) => {
      const navUrl = typeof url === 'string' ? url : details?.url;
      const mainFrame = (details?.isMainFrame ?? isMainFrame) !== false;
      if (!mainFrame || isBlankOrEmptyGuestUrl(navUrl)) {
        return;
      }
      if (this.isInterceptionArmed(rec) && !rec.pageDomainEnabled) {
        void this.enablePageDomain(rec);
      }
    };
    const onDidNavigate: DialogDidNavigateListener = (_event, url) => {
      const navUrl = typeof url === 'string' ? url : rec.wc.getURL?.();
      if (isBlankOrEmptyGuestUrl(navUrl) || !this.isInterceptionArmed(rec)) {
        return;
      }
      if (!rec.pageDomainEnabled) {
        void this.enablePageDomain(rec);
      } else if (this.supportsMainWorldInterception(rec)) {
        // `Runtime.addBinding` without `Runtime.enable` is not replayed by
        // Chromium across cross-process RenderFrameHost swaps; re-bind and
        // flush any queued early-document dialog reports on navigation commit.
        rec.bindingAdded = false;
        void this.syncMainWorldInterceptor(rec);
      }
    };
    rec.wc.on('did-start-navigation', onDidStartNavigation);
    rec.wc.on('did-navigate', onDidNavigate);
    rec.detachNavListeners = () => {
      rec.wc.removeListener?.('did-start-navigation', onDidStartNavigation);
      rec.wc.removeListener?.('did-navigate', onDidNavigate);
      rec.detachNavListeners = undefined;
    };
  }

  private enablePageDomain(rec: GuestDialogRecord): Promise<boolean> {
    if (rec.pageDomainEnabled) {
      return Promise.resolve(true);
    }
    if (rec.pageEnableInFlight) {
      return rec.pageEnableInFlight;
    }
    const dbg = rec.wc.debugger;
    if (!dbg || rec.wc.isDestroyed?.()) {
      return Promise.resolve(false);
    }
    if (!ensureGuestDebuggerAttached(dbg)) {
      return Promise.resolve(false);
    }
    this.attachListenersIfNeeded(rec);
    const sendCommand = dbg.sendCommand;
    if (typeof sendCommand !== 'function') {
      rec.pageDomainEnabled = true;
      return Promise.resolve(true);
    }
    const epoch = rec.pageEnableEpoch;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const inFlight = (async (): Promise<boolean> => {
      try {
        const sendFailed = Symbol('page-enable-failed');
        const timedOut = Symbol('page-enable-timeout');
        const result = await Promise.race([
          sendCommand.call(dbg, 'Page.enable').catch(() => sendFailed),
          new Promise<typeof timedOut>((resolve) => {
            timer = setTimeout(
              () => resolve(timedOut),
              this.pageEnableTimeoutMs,
            );
          }),
        ]);
        if (
          result === sendFailed ||
          result === timedOut ||
          rec.pageEnableEpoch !== epoch ||
          rec.wc.isDestroyed?.()
        ) {
          return false;
        }
        rec.pageDomainEnabled = true;
        if (this.supportsMainWorldInterception(rec)) {
          await this.syncMainWorldInterceptor(rec);
        }
        return true;
      } finally {
        if (timer !== undefined) {
          clearTimeout(timer);
        }
      }
    })();
    const tracked = inFlight.finally(() => {
      if (rec.pageEnableInFlight === tracked) {
        rec.pageEnableInFlight = undefined;
      }
    });
    rec.pageEnableInFlight = tracked;
    return tracked;
  }

  private attachListenersIfNeeded(rec: GuestDialogRecord): void {
    if (rec.listenerAttached) {
      return;
    }
    const dbg = rec.wc.debugger;
    if (!dbg?.on) {
      return;
    }
    rec.listenerAttached = true;
    dbg.on('detach', () => {
      rec.pageEnableEpoch++;
      rec.pageDomainEnabled = false;
      rec.pageEnableInFlight = undefined;
      rec.bindingAdded = false;
      rec.preloadScriptId = undefined;
      if (rec.pendingManual) {
        clearTimeout(rec.pendingManual.timer);
        rec.pendingManual = undefined;
      }
    });
    dbg.on('message', (_event, method, rawParams) => {
      if (method === 'Runtime.bindingCalled') {
        this.onBindingCalled(rec, rawParams);
        return;
      }
      if (method === 'Page.javascriptDialogClosed') {
        if (rec.pendingManual) {
          clearTimeout(rec.pendingManual.timer);
          rec.pendingManual = undefined;
        }
        return;
      }
      if (method === 'Page.javascriptDialogOpening') {
        this.onDialogOpening(rec, rawParams);
      }
    });
  }

  private onBindingCalled(rec: GuestDialogRecord, rawParams: unknown): void {
    const params =
      rawParams && typeof rawParams === 'object'
        ? (rawParams as { name?: unknown; payload?: unknown })
        : null;
    if (
      params?.name !== rec.bindingName ||
      typeof params.payload !== 'string' ||
      !this.isInterceptionArmed(rec)
    ) {
      return;
    }
    let parsed: unknown;
    try {
      parsed = JSON.parse(params.payload);
    } catch {
      return;
    }
    if (!parsed || typeof parsed !== 'object') {
      return;
    }
    const parsedObj = parsed as {
      token?: unknown;
      type?: unknown;
      message?: unknown;
      defaultPrompt?: unknown;
      url?: unknown;
      accepted?: unknown;
      userInput?: unknown;
    };
    if (parsedObj.token !== rec.authToken) {
      return;
    }
    const tabKey = rec.policy?.tabKey ?? rec.fallbackContext?.()?.tabKey;
    const eventPayload: DialogOpeningPayload = {
      dialogId: `dlg_${this.nextDialogSeq++}`,
      webContentsId: rec.wc.id,
      type: normalizeDialogType(parsedObj.type),
      message: typeof parsedObj.message === 'string' ? parsedObj.message : '',
      defaultPrompt:
        typeof parsedObj.defaultPrompt === 'string'
          ? parsedObj.defaultPrompt
          : '',
      url:
        typeof parsedObj.url === 'string' && parsedObj.url
          ? parsedObj.url
          : rec.wc.getURL?.() || '',
      handled: true,
      accepted: Boolean(parsedObj.accepted),
    };
    if (tabKey) {
      eventPayload.tabKey = tabKey;
    }
    if (typeof parsedObj.userInput === 'string') {
      eventPayload.userInput = parsedObj.userInput;
    }
    this.emitToSender(rec.sender, eventPayload);
  }

  private onDialogOpening(rec: GuestDialogRecord, rawParams: unknown): void {
    const fallback = rec.fallbackContext?.();
    const policyEnabled = rec.policy?.enabled === true;
    const wcvStepActive = fallback?.isAgentStepActive === true;
    if (!policyEnabled && !wcvStepActive) {
      return;
    }
    const params =
      rawParams && typeof rawParams === 'object'
        ? (rawParams as {
            type?: unknown;
            message?: unknown;
            defaultPrompt?: unknown;
            url?: unknown;
          })
        : null;
    const dialogType = normalizeDialogType(params?.type);
    const message = typeof params?.message === 'string' ? params.message : '';
    const defaultPrompt =
      typeof params?.defaultPrompt === 'string' ? params.defaultPrompt : '';
    const url =
      typeof params?.url === 'string' && params.url
        ? params.url
        : rec.wc.getURL?.() || '';
    const tabKey = rec.policy?.tabKey ?? fallback?.tabKey;
    const dialogId = `dlg_${this.nextDialogSeq++}`;
    const action: DialogPolicyAction = policyEnabled
      ? (rec.policy?.action ?? 'dismiss')
      : 'dismiss';
    const manualFallbackAction = policyEnabled
      ? rec.policy?.fallbackAction
      : undefined;
    if (
      action === 'manual' &&
      manualFallbackAction === undefined &&
      dialogType !== 'beforeunload'
    ) {
      this.flushPendingManual(rec);
      const timer = setTimeout(() => {
        if (rec.pendingManual?.dialogId === dialogId) {
          rec.pendingManual = undefined;
          dismissNativeDialogSafely(rec.wc, dialogType);
        }
      }, this.manualTimeoutMs);
      rec.pendingManual = { dialogId, type: dialogType, defaultPrompt, timer };
      const manualPayload: DialogOpeningPayload = {
        dialogId,
        webContentsId: rec.wc.id,
        type: dialogType,
        message,
        defaultPrompt,
        url,
        handled: false,
      };
      if (tabKey) {
        manualPayload.tabKey = tabKey;
      }
      this.emitToSender(rec.sender, manualPayload);
      return;
    }
    const effectiveAction =
      action === 'manual' ? (manualFallbackAction ?? 'dismiss') : action;
    const effectivePrompt =
      action === 'manual'
        ? (rec.policy?.fallbackPromptText ?? rec.policy?.promptText)
        : rec.policy?.promptText;
    const accepted =
      effectiveAction === 'accept'
        ? true
        : defaultAcceptForDismissPolicy(dialogType);
    const cmdParams: { accept: boolean; promptText?: string } = {
      accept: accepted,
    };
    let userInput: string | undefined;
    if (policyEnabled && effectivePrompt !== undefined) {
      cmdParams.promptText = effectivePrompt;
      if (dialogType === 'prompt' && accepted) {
        userInput = effectivePrompt;
      }
    } else if (dialogType === 'prompt' && accepted) {
      if (defaultPrompt) {
        cmdParams.promptText = defaultPrompt;
      }
      userInput = defaultPrompt;
    }
    const dbg = rec.wc.debugger;
    const canSend = Boolean(
      dbg &&
        typeof dbg.sendCommand === 'function' &&
        !rec.wc.isDestroyed?.(),
    );
    if (canSend && dbg?.sendCommand) {
      void dbg
        .sendCommand('Page.handleJavaScriptDialog', cmdParams)
        .catch(() => undefined);
    }
    const autoPayload: DialogOpeningPayload = {
      dialogId,
      webContentsId: rec.wc.id,
      type: dialogType,
      message,
      defaultPrompt,
      url,
      handled: canSend,
      accepted: canSend ? accepted : false,
    };
    if (tabKey) {
      autoPayload.tabKey = tabKey;
    }
    if (canSend && userInput !== undefined) {
      autoPayload.userInput = userInput;
    }
    this.emitToSender(rec.sender, autoPayload);
  }

  private flushPendingManual(rec: GuestDialogRecord): void {
    const pending = rec.pendingManual;
    if (!pending) {
      return;
    }
    clearTimeout(pending.timer);
    rec.pendingManual = undefined;
    dismissNativeDialogSafely(rec.wc, pending.type);
  }

  private emitToSender(
    sender: DialogHostSender | null | undefined,
    payload: DialogOpeningPayload,
  ): void {
    if (!sender || sender.isDestroyed()) {
      return;
    }
    try {
      sender.send(WEBVIEW_DIALOG_OPENING_CHANNEL, payload);
    } catch {
      // Ignore send errors on tearing-down renderer frames.
    }
  }

  /**
   * Returns `true` when guest `webContentsId` currently has agent dialog
   * interception armed.
   */
  isAgentDialogArmed(webContentsId: number): boolean {
    const rec = this.records.get(webContentsId);
    if (!rec || rec.wc.isDestroyed?.()) {
      return false;
    }
    return this.isInterceptionArmed(rec);
  }
}

export const guestDialogController = new GuestDialogController();

registerGuestAgentControlPredicate((webContentsId) =>
  guestDialogController.isAgentDialogArmed(webContentsId),
);
