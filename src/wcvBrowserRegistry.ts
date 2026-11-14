import {
  app,
  shell,
  WebContentsView,
  type BrowserWindow,
  type ContextMenuParams,
  type NavigationEntry,
  type Rectangle,
  type RenderProcessGoneDetails,
  type RestoreOptions,
  type View,
  type WebContents,
  type WebPreferences,
} from 'electron';
import {
  configureWebviewSessionProxy,
  type WebviewProxySession,
} from './beyondcorpPac';
import {
  deviceChooserController,
  type DeviceChooserHostContext,
} from './deviceChooser';
import {
  registerGuestAgentControlPredicate,
  registerGuestAgentOwnershipPredicate,
} from './guestAgentControl';
import {
  guestDialogController,
  type DialogGuestWebContents,
} from './guestDialogController';
import {
  guestNetworkController,
  isNetworkTrackableContents,
} from './guestNetworkController';
import {
  configureSessionPromptPreload,
  guestPromptController,
  type PromptHostContext,
} from './guestPromptController';
import { guestWebMcpController } from './guestWebMcpController';
import {
  dismissNativeGuestPopups,
  executeJavaScriptInIsolatedWorldWhenReady,
  POPUP_GUARD_ISOLATED_WORLD_ID,
} from './isolatedWorldExec';
import {
  getHubClipView,
  getHubCurtainView,
  getHubView,
  registerWindowWebContents,
  setHubSurfaceTransparent,
} from './utils';
import {
  attachWebviewWebAuthnUserActivationTracking,
  configureSessionWebAuthn,
} from './webauthn';
import {
  configureWebviewBrowserContents,
  isExternalOnlyAuthUrl,
  validateWebviewNavigationUrl,
} from './webviewBrowser';

export const MAX_HIDDEN_WCV_TABS = 20;
export const DEFAULT_MIN_HIDDEN_WCV_TABS = 3;
export const DEFAULT_HIDDEN_WCV_MEMORY_BUDGET_BYTES = 2 * 1024 * 1024 * 1024;
export const FALLBACK_HIDDEN_WCV_MEMORY_BYTES = 50 * 1024 * 1024;
export const DEFAULT_LOAD_SETTLED_REVEAL_TIMEOUT_MS = 500;

/** Guest identifier paired with its optional renderer OS PID for memory accounting. */
export interface WcvGuestPidInfo {
  webContentsId: number;
  pid?: number;
}

/** Minimal `app.getAppMetrics()` entry inspected for working-set memory size in KB. */
export interface WcvProcessMetricLike {
  pid?: number;
  memory?: {
    workingSetSize?: number;
  };
}

/**
 * Computes physical working-set memory in bytes per guest `webContentsId` from
 * Electron `app.getAppMetrics()` (`workingSetSize` is reported in KB), dividing
 * shared renderer OS PIDs evenly across guests sharing that PID so physical RAM
 * is not double-counted.
 */
export function computeGuestMemoryBytesByWebContentsId(
  guests: readonly WcvGuestPidInfo[],
  metrics: readonly WcvProcessMetricLike[] | null | undefined,
): Map<number, number> {
  const result = new Map<number, number>();
  if (!Array.isArray(metrics) || metrics.length === 0 || guests.length === 0) {
    return result;
  }
  const bytesByPid = new Map<number, number>();
  for (const metric of metrics) {
    const pid = metric?.pid;
    const kb = metric?.memory?.workingSetSize;
    if (
      typeof pid === 'number' &&
      pid > 0 &&
      typeof kb === 'number' &&
      Number.isFinite(kb) &&
      kb >= 0
    ) {
      bytesByPid.set(pid, Math.round(kb * 1024));
    }
  }
  const uniqueGuests: WcvGuestPidInfo[] = [];
  const seenIds = new Set<number>();
  for (const guest of guests) {
    if (!seenIds.has(guest.webContentsId)) {
      seenIds.add(guest.webContentsId);
      uniqueGuests.push(guest);
    }
  }
  const pidCounts = new Map<number, number>();
  for (const guest of uniqueGuests) {
    if (typeof guest.pid === 'number' && bytesByPid.has(guest.pid)) {
      pidCounts.set(guest.pid, (pidCounts.get(guest.pid) ?? 0) + 1);
    }
  }
  for (const guest of uniqueGuests) {
    if (typeof guest.pid !== 'number') {
      continue;
    }
    const pidBytes = bytesByPid.get(guest.pid);
    const sharedCount = pidCounts.get(guest.pid) ?? 1;
    if (pidBytes !== undefined) {
      result.set(guest.webContentsId, Math.round(pidBytes / sharedCount));
    }
  }
  return result;
}

export const DEFAULT_HIDDEN_WCV_BOUNDS: Rectangle = {
  x: 0,
  y: 0,
  width: 1280,
  height: 800,
};

export const WCV_GUEST_EVENT_CHANNEL = 'wcv:guest-event';
export const NO_DRAG_USER_CSS =
  '*, *::before, *::after { -webkit-app-region: no-drag !important; app-region: no-drag !important; }';

const VALID_PARTITION_RE = /^persist:hub-browser-[a-zA-Z0-9_-]+$/;

/** Maximum UTF-16 length of a forwarded guest `console-message` string. */
export const MAX_WCV_CONSOLE_MESSAGE_LENGTH = 512;

/** Maximum UTF-16 length of a forwarded guest `console-message` `sourceId`. */
export const MAX_WCV_CONSOLE_SOURCE_ID_LENGTH = 256;

/** Maximum UTF-16 length of an isolated-world selection watcher payload. */
export const MAX_WCV_SELECTION_CONSOLE_MESSAGE_LENGTH = 65536;

const GUEST_SELECTION_CONSOLE_PREFIX = '__AGY_BROWSER_SELECTION__';

/** Normalized string severity for a forwarded guest console event. */
export type WcvConsoleLevel = 'debug' | 'info' | 'warning' | 'error';

/** Sanitized, bounded payload emitted to the Hub renderer for guest `console-message` events. */
export interface NormalizedWcvConsoleMessage {
  level: 0 | 1 | 2 | 3;
  consoleLevel: WcvConsoleLevel;
  message: string;
  line: number;
  lineNumber: number;
  sourceId: string;
}

function parseConsoleSeverity(rawLevel: unknown): {
  level: 0 | 1 | 2 | 3;
  consoleLevel: WcvConsoleLevel;
} {
  if (typeof rawLevel === 'string') {
    switch (rawLevel.toLowerCase()) {
      case 'error':
        return { level: 3, consoleLevel: 'error' };
      case 'warning':
      case 'warn':
        return { level: 2, consoleLevel: 'warning' };
      case 'debug':
      case 'verbose':
        return { level: 0, consoleLevel: 'debug' };
      default:
        return { level: 1, consoleLevel: 'info' };
    }
  }
  if (typeof rawLevel === 'number' && Number.isFinite(rawLevel)) {
    if (rawLevel >= 3) {
      return { level: 3, consoleLevel: 'error' };
    }
    if (rawLevel === 2) {
      return { level: 2, consoleLevel: 'warning' };
    }
    if (rawLevel <= 0) {
      return { level: 0, consoleLevel: 'debug' };
    }
  }
  return { level: 1, consoleLevel: 'info' };
}

/**
 * Normalizes and bounds an Electron `WebContents` `'console-message'` event
 * (supporting both Electron 35+/44 `Event<WebContentsConsoleMessageEventParams>`
 * objects and legacy positional arguments) for safe IPC forwarding.
 *
 * Drops empty lines, internal Electron/DevTools bundle warnings, and high-rate
 * `debug`/`info` guest log chatter (except isolated-world selection payloads),
 * while truncating `warning`/`error` messages and `sourceId` at the IPC boundary.
 */
export function normalizeWcvConsoleMessageEvent(
  details: unknown,
  legacyArgs: readonly unknown[] = [],
): NormalizedWcvConsoleMessage | null {
  const obj =
    details && typeof details === 'object'
      ? (details as Record<string, unknown>)
      : undefined;
  const eventObjectMessage =
    typeof obj?.message === 'string' ? obj.message : undefined;
  const hasEventObjectMessage = eventObjectMessage !== undefined;
  const rawMessage =
    eventObjectMessage !== undefined
      ? eventObjectMessage
      : typeof legacyArgs[1] === 'string'
        ? legacyArgs[1]
        : '';
  if (rawMessage.length === 0) {
    return null;
  }
  const rawSourceId =
    typeof obj?.sourceId === 'string'
      ? obj.sourceId
      : typeof legacyArgs[3] === 'string'
        ? legacyArgs[3]
        : '';
  if (
    rawSourceId.startsWith('node:electron/') ||
    rawSourceId.startsWith('devtools://') ||
    rawSourceId.startsWith('chrome-extension://')
  ) {
    return null;
  }
  const rawLevel = hasEventObjectMessage
    ? (obj?.level ?? legacyArgs[0])
    : legacyArgs[0];
  const { level, consoleLevel } = parseConsoleSeverity(rawLevel);
  const rawLine = hasEventObjectMessage
    ? (obj?.lineNumber ?? obj?.line ?? legacyArgs[2])
    : legacyArgs[2];
  const lineNumber =
    typeof rawLine === 'number' && Number.isFinite(rawLine) && rawLine >= 0
      ? Math.floor(rawLine)
      : 0;
  const sourceId =
    rawSourceId.length > MAX_WCV_CONSOLE_SOURCE_ID_LENGTH
      ? rawSourceId.slice(0, MAX_WCV_CONSOLE_SOURCE_ID_LENGTH)
      : rawSourceId;
  if (rawMessage.startsWith(GUEST_SELECTION_CONSOLE_PREFIX)) {
    if (rawMessage.length > MAX_WCV_SELECTION_CONSOLE_MESSAGE_LENGTH) {
      return null;
    }
    return {
      level,
      consoleLevel,
      message: rawMessage,
      line: lineNumber,
      lineNumber,
      sourceId,
    };
  }
  if (level < 2) {
    return null;
  }
  const message =
    rawMessage.length > MAX_WCV_CONSOLE_MESSAGE_LENGTH
      ? `${rawMessage.slice(0, MAX_WCV_CONSOLE_MESSAGE_LENGTH - 1)}…`
      : rawMessage;
  return {
    level,
    consoleLevel,
    message,
    line: lineNumber,
    lineNumber,
    sourceId,
  };
}

interface CapturedNavigationHistory {
  entries: NavigationEntry[];
  index: number;
}

type RestoreHistoryResult = 'restored' | 'cache-miss' | 'not-restored';

/**
 * Drops history entries the guest may not navigate to (the same checks a fresh
 * `loadURL` gets) and re-points `index` at the active entry. Returns undefined
 * when the active entry itself is dropped, so the caller falls back to
 * loading the last URL.
 */
function filterRestorableHistory(
  history: CapturedNavigationHistory | undefined,
): RestoreOptions | undefined {
  if (!history) {
    return undefined;
  }
  const entries: NavigationEntry[] = [];
  let index = -1;
  history.entries.forEach((navEntry, i) => {
    if (
      isExternalOnlyAuthUrl(navEntry.url) ||
      !validateWebviewNavigationUrl(navEntry.url, 'http://127.0.0.1').allowed
    ) {
      return;
    }
    if (i === history.index) {
      index = entries.length;
    }
    entries.push(navEntry);
  });
  return index >= 0 ? { entries, index } : undefined;
}

/** Captures a live guest's back/forward stack so eviction can restore it later. */
function captureHistory(wc: WebContents): CapturedNavigationHistory | undefined {
  if (wc.isDestroyed()) {
    return undefined;
  }
  try {
    const entries = wc.navigationHistory?.getAllEntries?.() ?? [];
    const index = wc.navigationHistory?.getActiveIndex?.() ?? -1;
    return entries.length > 0 && index >= 0 && index < entries.length
      ? { entries, index }
      : undefined;
  } catch {
    return undefined;
  }
}

function isCacheMissError(err: unknown): boolean {
  if (!err || typeof err !== 'object') {
    return false;
  }
  const { code, errno } = err as { code?: unknown; errno?: unknown };
  return code === 'ERR_CACHE_MISS' || errno === -10;
}

/** Restores a captured stack without re-posting form data. */
async function restoreHistory(
  wc: WebContents,
  history: RestoreOptions | undefined,
): Promise<RestoreHistoryResult> {
  if (!history || typeof wc.navigationHistory?.restore !== 'function') {
    return 'not-restored';
  }
  try {
    await wc.navigationHistory.restore(history);
    return 'restored';
  } catch (err) {
    // restore() also rejects when the active page fails to load (including
    // ERR_ABORTED from a superseding navigation) after the stack is already in
    // place; loading the last URL then would clobber it. The tab may also
    // have been closed while restore() was pending.
    if (wc.isDestroyed()) {
      return 'not-restored';
    }
    let stackInPlace: boolean;
    try {
      stackInPlace = (wc.navigationHistory.getAllEntries?.().length ?? 0) > 0;
    } catch {
      return 'not-restored';
    }
    if (!stackInPlace) {
      return 'not-restored';
    }
    return isCacheMissError(err) ? 'cache-miss' : 'restored';
  }
}

/** Request payload accepted by {@link WcvBrowserRegistry.acquire}. */
export interface WcvAcquireRequest {
  tabKey: string;
  partition: string;
  initialUrl?: string;
  title?: string;
}

/** Normalized main-frame load failure metadata retained per tab entry. */
export interface WcvLoadError {
  errorCode: number;
  errorDescription: string;
  validatedURL: string;
}

/** Partial or complete DIP bounds accepted when showing or resizing a tab view. */
export interface WcvBoundsInput {
  x?: number;
  y?: number;
  width?: number;
  height?: number;
}

/** Positive DIP dimensions cached for a guest view. */
export interface WcvSize {
  width: number;
  height: number;
}

/** Reason codes emitted when a window's `clipView` reveal state changes. */
export type WcvClipVisibilityReason =
  | 'load-settled'
  | 'load-settled-timeout'
  | 'tab-reparented'
  | 'bounds-changed'
  | 'guest-load-failed'
  | 'guest-render-process-gone';

/** Event names forwarded to the owning Hub renderer on {@link WCV_GUEST_EVENT_CHANNEL}. */
export type WcvGuestEventType =
  | 'will-navigate'
  | 'did-start-navigation'
  | 'did-redirect-navigation'
  | 'dom-ready'
  | 'did-start-loading'
  | 'did-stop-loading'
  | 'did-navigate'
  | 'did-navigate-in-page'
  | 'page-title-updated'
  | 'page-favicon-updated'
  | 'context-menu'
  | 'console-message'
  | 'did-fail-load'
  | 'render-process-gone';

/** Live registry record for a managed built-in browser `WebContentsView` tab. */
export interface WcvEntry extends WcvAcquireRequest {
  view: WebContentsView;
  webContentsId: number;
  parentWindow: BrowserWindow;
  hubSender: WebContents;
  lastShownAt: number;
  lastUrl: string;
  lastTitle: string;
  clipRevealReady: boolean;
  visible?: boolean;
  pinned?: boolean;
  crashed?: boolean;
  htmlFullscreen?: boolean;
  isAgentStepActive?: boolean;
  boundsDip?: Rectangle;
  appliedBoundsDip?: Rectangle;
  lastNonZeroSize?: WcvSize;
  shownInVisibleClip?: boolean;
  clipRevealTimer?: ReturnType<typeof setTimeout>;
  hasCommittedNonBlankNav?: boolean;
  lastHttpResponseCode?: number | null;
  lastLoadError?: WcvLoadError | null;
}

/** Serializable state snapshot returned to callers and included in guest events. */
export interface WcvTabSnapshot {
  tabKey: string;
  webContentsId: number;
  url: string;
  title: string;
  canGoBack: boolean;
  canGoForward: boolean;
  isLoading: boolean;
  visible: boolean;
  pinned: boolean;
  crashed: boolean;
  lastHttpResponseCode: number | null;
  lastLoadError: WcvLoadError | null;
}

/** Dependency-injection and eviction budget options for {@link WcvBrowserRegistry}. */
export interface WcvBrowserRegistryOptions {
  now?: () => number;
  createView?: (
    partition: string,
    webPreferences: WebPreferences,
  ) => WebContentsView;
  configureProxy?: (
    targetSession: WebviewProxySession | null | undefined,
  ) => Promise<void>;
  openExternal?: (url: string) => Promise<void> | void;
  dialogController?: {
    disposeGuest(wc: DialogGuestWebContents | null | undefined): void;
  };
  getAppMetrics?: () => readonly WcvProcessMetricLike[];
  maxHiddenTabs?: number;
  minHiddenTabs?: number;
  memoryBudgetBytes?: number;
  fallbackGuestMemoryBytes?: number;
}

interface EvictedWcvRecord {
  url: string;
  title: string;
  partition: string;
  hubSenderId: number;
  parentWindow: BrowserWindow;
  history?: CapturedNavigationHistory;
}

interface GuestNavigationEventDetails {
  url?: string;
  isMainFrame?: boolean;
  isSameDocument?: boolean;
  isInPlace?: boolean;
}

const contentViewOf = (win: BrowserWindow | null | undefined): View | undefined =>
  !win || win.isDestroyed?.() ? undefined : win.contentView;

const sameRect = (a: Rectangle | undefined, b: Rectangle): boolean =>
  Boolean(
    a &&
      a.x === b.x &&
      a.y === b.y &&
      a.width === b.width &&
      a.height === b.height,
  );

const isNonBlankDocUrl = (raw: string | undefined): boolean =>
  Boolean(
    raw &&
      raw.toLowerCase() !== 'about:blank' &&
      !raw.startsWith('chrome-error://'),
  );

const hasRevealableClipDoc = (
  entry: WcvEntry,
  url: string | undefined,
): boolean => Boolean(entry.hasCommittedNonBlankNav || isNonBlankDocUrl(url));

/** Main-process registry managing built-in browser `WebContentsView` instances keyed by `tabKey`. */
export class WcvBrowserRegistry {
  private readonly options: WcvBrowserRegistryOptions;
  private readonly entries = new Map<string, WcvEntry>();
  private readonly byWebContentsId = new Map<number, WcvEntry>();
  private readonly evicted = new Map<string, EvictedWcvRecord>();
  private readonly rehydrating = new Map<string, Promise<WcvTabSnapshot | null>>();
  private readonly operationPins = new Map<string, number>();
  private readonly activeRequestedTabByWindow = new WeakMap<
    BrowserWindow,
    string
  >();
  private readonly windowResetEpoch = new WeakMap<BrowserWindow, number>();
  private readonly monitoredWindows = new WeakSet<BrowserWindow>();
  private readonly userMutedTabs = new Set<string>();
  private readonly bootstrappedBlankEntries = new WeakSet<WcvEntry>();
  private readonly externallyNavigatedEntries = new WeakSet<WcvEntry>();
  private readonly htmlFullscreenListeners = new Set<
    (entry: WcvEntry, active: boolean) => void
  >();
  private readonly tabAcquiredListeners = new Set<
    (entry: WcvEntry) => Promise<void> | void
  >();
  private readonly clipVisibilityListeners = new Set<
    (win: BrowserWindow, reason: WcvClipVisibilityReason) => void
  >();
  private readonly windowSlotBounds = new WeakMap<BrowserWindow, Rectangle>();

  constructor(options: WcvBrowserRegistryOptions = {}) {
    this.options = options;
  }

  private getVisibleEntryForWindow(win: BrowserWindow): WcvEntry | undefined {
    const isVis = (e: WcvEntry | undefined): e is WcvEntry =>
      Boolean(
        e &&
          e.parentWindow === win &&
          e.visible &&
          !e.view.webContents.isDestroyed(),
      );
    const reqKey = this.activeRequestedTabByWindow.get(win);
    const req = reqKey !== undefined ? this.entries.get(reqKey) : undefined;
    if (isVis(req)) {
      return req;
    }
    for (const e of this.entries.values()) {
      if (isVis(e)) {
        return e;
      }
    }
    return undefined;
  }

  private resolveInnerSlotSize(
    win: BrowserWindow,
    preferred?: Rectangle,
  ): WcvSize {
    if (preferred && preferred.width > 0 && preferred.height > 0) {
      return { width: preferred.width, height: preferred.height };
    }
    const s = this.windowSlotBounds.get(win);
    return s && s.width > 0 && s.height > 0
      ? { width: s.width, height: s.height }
      : {
          width: DEFAULT_HIDDEN_WCV_BOUNDS.width,
          height: DEFAULT_HIDDEN_WCV_BOUNDS.height,
        };
  }

  private applyClipInnerBounds(entry: WcvEntry, sz: WcvSize): void {
    const inner: Rectangle = { x: 0, y: 0, width: sz.width, height: sz.height };
    if (!sameRect(entry.appliedBoundsDip, inner)) {
      entry.appliedBoundsDip = { ...inner };
      entry.view.setBounds?.(inner);
    }
  }

  private canHoldClipTarget(e: WcvEntry): boolean {
    return !e.crashed && !e.lastLoadError && !e.view.webContents.isDestroyed();
  }

  private isClipEntryRevealReady(entry: WcvEntry): boolean {
    return this.canHoldClipTarget(entry) && Boolean(entry.clipRevealReady);
  }

  isClipEntryRevealed(entry: WcvEntry): boolean {
    const win = entry.parentWindow;
    if (
      !getHubClipView(win) ||
      win.isDestroyed?.() ||
      !entry.visible ||
      !this.isClipEntryRevealReady(entry) ||
      this.getVisibleEntryForWindow(win) !== entry
    ) {
      return false;
    }
    const b = entry.boundsDip;
    return Boolean(entry.htmlFullscreen || (b && b.width > 0 && b.height > 0));
  }

  private clearClipRevealTimer(entry: WcvEntry): void {
    if (entry.clipRevealTimer !== undefined) {
      clearTimeout(entry.clipRevealTimer);
      entry.clipRevealTimer = undefined;
    }
  }

  private armClipRevealTimerIfNeeded(
    entry: WcvEntry,
    resetExisting = false,
  ): void {
    const win = entry.parentWindow;
    if (
      !getHubClipView(win) ||
      win.isDestroyed?.() ||
      !this.canHoldClipTarget(entry) ||
      entry.clipRevealReady ||
      this.getVisibleEntryForWindow(win) !== entry
    ) {
      this.clearClipRevealTimer(entry);
      return;
    }
    if (entry.clipRevealTimer !== undefined) {
      if (!resetExisting) {
        return;
      }
      this.clearClipRevealTimer(entry);
    }
    entry.clipRevealTimer = setTimeout(() => {
      entry.clipRevealTimer = undefined;
      if (
        this.entries.get(entry.tabKey) === entry &&
        this.canHoldClipTarget(entry) &&
        this.getVisibleEntryForWindow(entry.parentWindow) === entry
      ) {
        this.markClipEntryRevealReady(entry, 'load-settled-timeout');
      }
    }, DEFAULT_LOAD_SETTLED_REVEAL_TIMEOUT_MS);
  }

  private markClipEntryRevealReady(
    entry: WcvEntry,
    reason: WcvClipVisibilityReason,
  ): void {
    this.clearClipRevealTimer(entry);
    if (!this.canHoldClipTarget(entry)) {
      return;
    }
    const wasRevealReady = Boolean(entry.clipRevealReady);
    entry.clipRevealReady = true;
    const win = entry.parentWindow;
    if (
      !wasRevealReady &&
      getHubClipView(win) &&
      !win.isDestroyed?.() &&
      this.getVisibleEntryForWindow(win) === entry
    ) {
      this.syncWindowClipAndEntryBounds(win);
      this.notifyClipVisibilityChanged(win, reason);
    }
  }

  private hideClipEntryOnFault(
    entry: WcvEntry,
    reason: WcvClipVisibilityReason,
  ): void {
    entry.clipRevealReady = false;
    this.clearClipRevealTimer(entry);
    const win = entry.parentWindow;
    if (!getHubClipView(win)) {
      return;
    }
    if (entry.htmlFullscreen) {
      this.clearHtmlFullscreen(entry, false);
    }
    entry.view.setVisible?.(false);
    if (entry.visible && !win.isDestroyed?.()) {
      this.syncWindowClipAndEntryBounds(win);
      this.notifyClipVisibilityChanged(win, reason);
    }
  }

  /**
   * Synchronizes `clipView` and inner guest `WebContentsView` bounds for `win`;
   * holds `appliedBoundsDip` steady while `isAgentStepActive` is true (preserving
   * screenshot-to-CDP coordinate correspondence) until `setAgentStepActive` clears.
   */
  private syncWindowClipAndEntryBounds(win: BrowserWindow): void {
    const clipView = getHubClipView(win);
    if (!win || win.isDestroyed?.() || !clipView) {
      return;
    }
    const active = this.getVisibleEntryForWindow(win);
    for (const e of this.getEntriesForWindow(win)) {
      if (e !== active && !e.view.webContents.isDestroyed()) {
        e.view.setVisible?.(false);
      }
    }
    if (!active) {
      clipView.setVisible?.(false);
      return;
    }
    const b = active.boundsDip;
    const hasSlot = Boolean(b && b.width > 0 && b.height > 0);
    const fs = active.htmlFullscreen
      ? this.resolveFullscreenBounds(active)
      : undefined;
    const targetInnerSize = fs ?? this.resolveInnerSlotSize(win, b);
    const deferAgentResize = Boolean(
      active.isAgentStepActive &&
        !fs &&
        active.appliedBoundsDip &&
        active.appliedBoundsDip.width > 0 &&
        active.appliedBoundsDip.height > 0,
    );
    if (!deferAgentResize) {
      this.applyClipInnerBounds(active, targetInnerSize);
    }
    if (!this.isClipEntryRevealReady(active) || (!fs && !hasSlot)) {
      active.view.setVisible?.(false);
      if (b) {
        clipView.setBounds?.(b);
      }
      clipView.setVisible?.(false);
      return;
    }
    const kids = clipView.children;
    if (!Array.isArray(kids) || kids[kids.length - 1] !== active.view) {
      clipView.addChildView?.(active.view);
    }
    active.view.setVisible?.(true);
    const clipTargetBounds = fs ?? b;
    if (clipTargetBounds) {
      clipView.setBounds?.(clipTargetBounds);
    }
    clipView.setVisible?.(true);
    active.shownInVisibleClip = true;
  }

  /** Registers a listener notified when a window's `clipView` reveal state changes. */
  onClipVisibilityChanged(
    listener: (win: BrowserWindow, reason: WcvClipVisibilityReason) => void,
  ): () => void {
    this.clipVisibilityListeners.add(listener);
    return () => {
      this.clipVisibilityListeners.delete(listener);
    };
  }

  private notifyClipVisibilityChanged(
    win: BrowserWindow,
    reason: WcvClipVisibilityReason,
  ): void {
    for (const listener of this.clipVisibilityListeners) {
      listener(win, reason);
    }
  }

  /** Registers a listener notified when a guest enters or exits HTML fullscreen. */
  onHtmlFullscreenChanged(
    listener: (entry: WcvEntry, active: boolean) => void,
  ): () => void {
    this.htmlFullscreenListeners.add(listener);
    return () => {
      this.htmlFullscreenListeners.delete(listener);
    };
  }

  /** Registers a listener awaited after `acquire` attaches or re-parents a tab. */
  onTabAcquired(
    listener: (entry: WcvEntry) => Promise<void> | void,
  ): () => void {
    this.tabAcquiredListeners.add(listener);
    return () => {
      this.tabAcquiredListeners.delete(listener);
    };
  }

  private async notifyTabAcquired(entry: WcvEntry): Promise<void> {
    for (const fn of this.tabAcquiredListeners) {
      await Promise.resolve(fn(entry)).catch(() => undefined);
    }
  }

  private now(): number {
    return this.options.now ? this.options.now() : Date.now();
  }

  async acquire(
    parentWindow: BrowserWindow,
    hubSender: WebContents,
    request: WcvAcquireRequest,
  ): Promise<WcvTabSnapshot> {
    if (!request.tabKey || !VALID_PARTITION_RE.test(request.partition)) {
      throw new Error(`Invalid WCV tabKey or partition: ${request.tabKey}`);
    }
    const existing = this.entries.get(request.tabKey);
    if (
      existing &&
      !existing.view.webContents.isDestroyed() &&
      existing.partition === request.partition
    ) {
      if (
        !existing.hubSender.isDestroyed() &&
        existing.hubSender.id !== hubSender.id &&
        existing.parentWindow !== parentWindow &&
        !existing.parentWindow.isDestroyed?.()
      ) {
        throw new Error(
          `Tab ${request.tabKey} is already owned by another Hub window`,
        );
      }
      const oldWindow = existing.parentWindow;
      const wasVisibleOnOld = Boolean(existing.visible);
      const hadOldClip = Boolean(getHubClipView(oldWindow));
      const reparentClip =
        oldWindow !== parentWindow &&
        Boolean(hadOldClip || getHubClipView(parentWindow));
      if (reparentClip) {
        this.clearClipRevealTimer(existing);
        if (this.activeRequestedTabByWindow.get(oldWindow) === request.tabKey) {
          this.activeRequestedTabByWindow.delete(oldWindow);
        }
        existing.visible = false;
        if (existing.htmlFullscreen) {
          this.clearHtmlFullscreen(existing, false);
        }
      }
      existing.parentWindow = parentWindow;
      existing.hubSender = hubSender;
      existing.lastShownAt = this.now();
      if (reparentClip) {
        registerWindowWebContents(parentWindow, existing.view.webContents);
        this.ensureWindowMonitored(parentWindow);
        getHubClipView(oldWindow)?.removeChildView?.(existing.view);
        contentViewOf(oldWindow)?.removeChildView?.(existing.view);
        if (hadOldClip && !oldWindow.isDestroyed?.()) {
          this.syncWindowClipAndEntryBounds(oldWindow);
          if (wasVisibleOnOld) {
            this.notifyClipVisibilityChanged(oldWindow, 'tab-reparented');
          }
        }
        existing.boundsDip = undefined;
        existing.appliedBoundsDip = undefined;
        existing.shownInVisibleClip = false;
        if (hadOldClip && !getHubClipView(parentWindow)) {
          existing.view.setVisible?.(true);
        } else if (!hadOldClip && Boolean(getHubClipView(parentWindow))) {
          existing.clipRevealReady =
            this.canHoldClipTarget(existing) &&
            !existing.view.webContents.isLoading?.() &&
            hasRevealableClipDoc(existing, existing.lastUrl);
        }
        this.parkHiddenView(existing);
      }
      if (isNetworkTrackableContents(existing.view.webContents)) {
        guestNetworkController.trackGuest(existing.view.webContents, {
          ownerId: hubSender.id,
          sender: hubSender,
        });
      }
      if (reparentClip) {
        await this.notifyTabAcquired(existing);
      }
      return this.toSnapshot(existing);
    }

    const saved = this.evicted.get(request.tabKey);
    if (
      saved &&
      saved.hubSenderId !== hubSender.id &&
      saved.parentWindow !== parentWindow &&
      !saved.parentWindow.isDestroyed?.()
    ) {
      throw new Error(
        `Tab ${request.tabKey} is already owned by another Hub window`,
      );
    }
    const preservedUrl = existing?.lastUrl || saved?.url || '';
    const lastTitle =
      existing?.lastTitle || saved?.title || request.title || '';
    if (existing) {
      this.dispose(request.tabKey, false);
    }
    this.evicted.delete(request.tabKey);

    const blinkWebMcp = guestWebMcpController.isEnabled();
    const webPreferences: WebPreferences = {
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
      webviewTag: false,
      navigateOnDragDrop: false,
      focusOnNavigation: false,
      backgroundThrottling: false,
      partition: request.partition,
      // Chromium fixes `enableBlinkFeatures` at view creation; `setEnabled(false)`
      // disables CDP and settles calls on already-created tabs.
      ...(blinkWebMcp ? { enableBlinkFeatures: 'WebMCP' } : {}),
    };
    const view = this.options.createView
      ? this.options.createView(request.partition, webPreferences)
      : new WebContentsView({ webPreferences });
    const wc = view.webContents;
    guestWebMcpController.noteGuestCreated(wc.id, blinkWebMcp);
    wc.setMaxListeners?.(32);

    const rawTargetUrl =
      preservedUrl && preservedUrl !== 'about:blank'
        ? preservedUrl
        : request.initialUrl || '';
    const isExternalAuth = isExternalOnlyAuthUrl(rawTargetUrl);
    const lastUrl = isExternalAuth ? '' : rawTargetUrl;
    const entry: WcvEntry = {
      ...request,
      view,
      webContentsId: wc.id,
      parentWindow,
      hubSender,
      lastShownAt: this.now(),
      lastUrl,
      lastTitle,
      clipRevealReady: false,
    };
    this.entries.set(request.tabKey, entry);
    this.byWebContentsId.set(wc.id, entry);

    const openExternal = (url: string): Promise<void> | void =>
      this.openExternalUrl(url);
    registerWindowWebContents(parentWindow, wc);
    configureSessionWebAuthn(wc.session);
    configureSessionPromptPreload(wc.session);
    attachWebviewWebAuthnUserActivationTracking(wc);
    configureWebviewBrowserContents(wc, {
      allowWebContentsView: true,
      hostWebContents: hubSender,
      getParentWindow: () => entry.parentWindow,
      openExternal,
      now: this.options.now,
      isAgentControlled: () => Boolean(entry.isAgentStepActive),
    });
    wc.setAudioMuted?.(true);
    wc.setZoomLevel?.(0);
    void Promise.resolve(wc.setVisualZoomLevelLimits?.(1, 1)).catch(
      () => undefined,
    );
    wc.setIgnoreMenuShortcuts?.(true);
    this.attachGuestListeners(entry);
    this.ensureWindowMonitored(parentWindow);
    this.parkHiddenView(entry);

    let guestNavigationStarted = false;
    const onPreProxyNav = (
      details?: GuestNavigationEventDetails,
      url?: string,
      _isInPlace?: boolean,
      isMainFrame = true,
    ): void => {
      const navUrl = (url ?? details?.url ?? '').trim();
      const mainFrame = (details?.isMainFrame ?? isMainFrame) !== false;
      if (mainFrame && navUrl && navUrl.toLowerCase() !== 'about:blank') {
        guestNavigationStarted = true;
      }
    };
    wc.on?.('did-start-navigation', onPreProxyNav);
    wc.on?.('will-navigate', onPreProxyNav);
    const configureProxy =
      this.options.configureProxy ?? configureWebviewSessionProxy;
    try {
      await configureProxy(wc.session);
    } finally {
      wc.removeListener?.('did-start-navigation', onPreProxyNav);
      wc.removeListener?.('will-navigate', onPreProxyNav);
    }

    if (!wc.isDestroyed() && this.entries.get(entry.tabKey) === entry) {
      const currentLiveUrl = (wc.getURL?.() || '').trim();
      const hasConcurrentNavigation =
        guestNavigationStarted ||
        this.externallyNavigatedEntries.has(entry) ||
        Boolean(wc.isLoading?.()) ||
        (Boolean(currentLiveUrl) &&
          currentLiveUrl.toLowerCase() !== 'about:blank') ||
        (Boolean(entry.lastUrl) && entry.lastUrl !== lastUrl);
      if (isExternalAuth && !hasConcurrentNavigation) {
        void openExternal(rawTargetUrl);
      }
      if (hasConcurrentNavigation) {
        if (hasRevealableClipDoc(entry, currentLiveUrl) && !wc.isLoading?.()) {
          this.markClipEntryRevealReady(entry, 'load-settled');
        }
      } else {
        // A restored stack is the tab's real history, so it must skip the
        // blank bootstrap whose first did-navigate clears history.
        const restoreResult = isExternalAuth
          ? 'not-restored'
          : await restoreHistory(wc, filterRestorableHistory(saved?.history));
        // restore() is async: the tab may have closed, been replaced, or been
        // navigated elsewhere meanwhile, and none of those may be clobbered.
        const superseded =
          wc.isDestroyed() ||
          this.entries.get(entry.tabKey) !== entry ||
          this.externallyNavigatedEntries.has(entry);
        const lastUrlAllowed =
          !isExternalAuth &&
          validateWebviewNavigationUrl(lastUrl, 'http://127.0.0.1').allowed;
        if (!superseded && restoreResult !== 'restored') {
          if (lastUrlAllowed) {
            // On a cache miss this reloads the POST result's URL with GET on
            // top of the restored back stack, instead of leaving Chromium's
            // form-resubmission page.
            await wc.loadURL(lastUrl).catch(() => undefined);
          } else if (restoreResult === 'not-restored') {
            this.bootstrappedBlankEntries.add(entry);
            await wc.loadURL('about:blank').catch(() => undefined);
          }
        }
        if (
          !superseded &&
          (restoreResult === 'restored' || lastUrlAllowed) &&
          !wc.isDestroyed() &&
          this.entries.get(entry.tabKey) === entry &&
          !wc.isLoading?.()
        ) {
          this.markClipEntryRevealReady(entry, 'load-settled');
        }
      }
      if (isNetworkTrackableContents(wc)) {
        guestNetworkController.trackGuest(wc, {
          ownerId: hubSender.id,
          sender: hubSender,
        });
      }
    }

    this.evictHiddenIfNeeded();
    if (
      !wc.isDestroyed() &&
      this.entries.get(entry.tabKey) === entry &&
      getHubClipView(entry.parentWindow)
    ) {
      await this.notifyTabAcquired(entry);
    }
    return this.toSnapshot(entry);
  }

  show(
    tabKey: string,
    boundsDip: WcvBoundsInput,
    parentWindow?: BrowserWindow,
  ): WcvTabSnapshot | null {
    const entry = this.entries.get(tabKey);
    const targetWindow = entry?.parentWindow ?? parentWindow;
    if (targetWindow) {
      this.activeRequestedTabByWindow.set(targetWindow, tabKey);
    }
    if (!entry || entry.view.webContents.isDestroyed()) {
      if (targetWindow) {
        this.detachVisibleSiblings(targetWindow);
        if (getHubClipView(targetWindow)) {
          this.syncWindowClipAndEntryBounds(targetWindow);
        }
      }
      return null;
    }
    entry.visible = true;
    entry.lastShownAt = this.now();
    // The tab shows through the Hub UI's hole, so `hubWcv` must be see-through
    // before the tab is placed; `reconcileWindow` makes it opaque again once
    // no tab on the window is visible.
    setHubSurfaceTransparent(entry.parentWindow, true);
    entry.view.webContents.setAudioMuted?.(this.userMutedTabs.has(tabKey));

    const clipView = getHubClipView(entry.parentWindow);
    if (clipView) {
      const wasInClip =
        Array.isArray(clipView.children) &&
        clipView.children.includes(entry.view);
      if (!wasInClip) {
        entry.appliedBoundsDip = undefined;
        entry.view.setVisible?.(false);
        clipView.addChildView?.(entry.view, 0);
      }
      this.recordNormalizedBoundsDip(entry, boundsDip);
      this.detachVisibleSiblings(entry.parentWindow, entry);
      this.armClipRevealTimerIfNeeded(entry);
      this.syncWindowClipAndEntryBounds(entry.parentWindow);
      return this.toSnapshot(entry);
    }

    const contentView = contentViewOf(entry.parentWindow);
    const hubView = getHubView(entry.parentWindow);
    const children = contentView?.children;
    if (hubView && Array.isArray(children)) {
      const withoutEntry = children.filter((child) => child !== entry.view);
      const hubIdx = withoutEntry.indexOf(hubView);
      if (hubIdx >= 0) {
        contentView?.addChildView?.(entry.view, hubIdx);
      } else {
        contentView?.addChildView?.(entry.view);
        contentView?.addChildView?.(hubView);
      }
    } else {
      contentView?.addChildView?.(entry.view);
      if (hubView) {
        contentView?.addChildView?.(hubView);
      }
    }
    // Apply bounds after `addChildView` so `views::View::SetBoundsRect` updates
    // the attached native widget's screen origin (required for Blink `PagePopup`
    // widgets such as `<input type="date">`); calling `setBounds` while detached
    // caches `bounds_` at `(0, 0)` and causes subsequent same-rect calls to no-op.
    this.setBounds(tabKey, boundsDip);
    this.detachVisibleSiblings(entry.parentWindow, entry);
    return this.toSnapshot(entry);
  }

  getEvictedSenderId(tabKey: string): number | undefined {
    return this.evicted.get(tabKey)?.hubSenderId;
  }

  hasEvicted(tabKey: string, hubSenderId?: number): boolean {
    const saved = this.evicted.get(tabKey);
    if (!saved) {
      return false;
    }
    return hubSenderId === undefined || saved.hubSenderId === hubSenderId;
  }

  /**
   * Recreates an evicted tab as a parked hidden guest at its saved URL, so
   * main-process agent operations (CDP input, `capturePage`) can reach it
   * again. Concurrent callers share one in-flight rehydration and resolve once
   * the guest has loaded. Returns null when `tabKey` is neither evicted by
   * `hubSender` nor being rehydrated.
   */
  rehydrateEvicted(
    parentWindow: BrowserWindow,
    hubSender: WebContents,
    tabKey: string,
  ): Promise<WcvTabSnapshot | null> {
    const inFlight = this.rehydrating.get(tabKey);
    if (inFlight) {
      return inFlight;
    }
    const saved = this.evicted.get(tabKey);
    if (!saved || saved.hubSenderId !== hubSender.id) {
      return Promise.resolve(null);
    }
    const pending: Promise<WcvTabSnapshot | null> = this.acquire(
      parentWindow,
      hubSender,
      {
        tabKey,
        partition: saved.partition,
        initialUrl: saved.url,
        title: saved.title,
      },
    ).finally(() => {
      if (this.rehydrating.get(tabKey) === pending) {
        this.rehydrating.delete(tabKey);
      }
    });
    this.rehydrating.set(tabKey, pending);
    return pending;
  }

  /**
   * Runs `op` with `tabKey` exempt from hidden-tab eviction, so a concurrent
   * acquire cannot evict the tab mid-operation and rehydrating it for the
   * operation does not evict another hidden tab. Releasing the pin does not
   * evict; the next budget check (acquire, hide, or tab state change) does.
   */
  async withOperationPin<T>(
    tabKey: string,
    op: () => Promise<T> | T,
  ): Promise<T> {
    this.operationPins.set(tabKey, (this.operationPins.get(tabKey) ?? 0) + 1);
    try {
      return await op();
    } finally {
      const remaining = (this.operationPins.get(tabKey) ?? 1) - 1;
      if (remaining > 0) {
        this.operationPins.set(tabKey, remaining);
      } else {
        this.operationPins.delete(tabKey);
      }
    }
  }

  /**
   * Cancels any active or in-flight tab show request for `parentWindow` and
   * parks currently visible tabs on that window below the curtain.
   */
  cancelWindowShowRequests(parentWindow: BrowserWindow): void {
    this.windowResetEpoch.set(
      parentWindow,
      (this.windowResetEpoch.get(parentWindow) ?? 0) + 1,
    );
    this.activeRequestedTabByWindow.delete(parentWindow);
    const visibleKeys: string[] = [];
    for (const entry of this.entries.values()) {
      if (entry.parentWindow === parentWindow && entry.visible) {
        visibleKeys.push(entry.tabKey);
      }
    }
    for (const tabKey of visibleKeys) {
      this.hide(tabKey);
    }
    if (visibleKeys.length === 0 && getHubClipView(parentWindow)) {
      this.syncWindowClipAndEntryBounds(parentWindow);
    }
  }

  async rehydrateEvictedAndShow(
    parentWindow: BrowserWindow,
    hubSender: WebContents,
    tabKey: string,
    boundsDip: WcvBoundsInput,
  ): Promise<WcvTabSnapshot | null> {
    if (!this.hasEvicted(tabKey, hubSender.id)) {
      return null;
    }
    const startEpoch = this.windowResetEpoch.get(parentWindow) ?? 0;
    this.activeRequestedTabByWindow.set(parentWindow, tabKey);
    this.detachVisibleSiblings(parentWindow);
    if (getHubClipView(parentWindow)) {
      this.syncWindowClipAndEntryBounds(parentWindow);
    }
    const rehydrated = await this.rehydrateEvicted(
      parentWindow,
      hubSender,
      tabKey,
    );
    const current = rehydrated ? this.entries.get(tabKey) : undefined;
    if (
      !rehydrated ||
      !current ||
      current.webContentsId !== rehydrated.webContentsId
    ) {
      return null;
    }
    if (parentWindow.isDestroyed?.() || hubSender.isDestroyed?.()) {
      this.dispose(tabKey);
      return null;
    }
    const epochChanged =
      (this.windowResetEpoch.get(parentWindow) ?? 0) !== startEpoch;
    const activeTab = this.activeRequestedTabByWindow.get(parentWindow);
    if (epochChanged || activeTab !== tabKey) {
      if (activeTab !== tabKey) {
        this.hide(tabKey);
      } else if (getHubClipView(parentWindow)) {
        this.syncWindowClipAndEntryBounds(parentWindow);
      }
      return null;
    }
    return this.show(tabKey, boundsDip, parentWindow);
  }

  hide(tabKey: string): void {
    this.mutate(tabKey, (entry) => {
      if (this.activeRequestedTabByWindow.get(entry.parentWindow) === tabKey) {
        this.activeRequestedTabByWindow.delete(entry.parentWindow);
      }
      this.detachView(entry);
    });
  }

  private recordNormalizedBoundsDip(
    entry: WcvEntry,
    bounds: WcvBoundsInput,
  ): Rectangle {
    const collapsed =
      !(typeof bounds.width === 'number' && Math.round(bounds.width) > 0) ||
      !(typeof bounds.height === 'number' && Math.round(bounds.height) > 0);
    if (
      collapsed &&
      !entry.htmlFullscreen &&
      !entry.view.webContents.isDestroyed()
    ) {
      dismissNativeGuestPopups(entry.view.webContents, {
        onlyPopupControls: false,
        shouldStillDismiss: () => {
          const cur = entry.boundsDip;
          return !entry.visible || !cur || cur.width <= 0 || cur.height <= 0;
        },
      });
    }
    const round = (value: number | undefined): number =>
      typeof value === 'number' && Number.isFinite(value)
        ? Math.round(value)
        : 0;
    const nextBounds: Rectangle = {
      x: round(bounds.x),
      y: round(bounds.y),
      width: Math.max(0, round(bounds.width)),
      height: Math.max(0, round(bounds.height)),
    };
    entry.boundsDip = nextBounds;
    if (nextBounds.width > 0 && nextBounds.height > 0) {
      const win = entry.parentWindow;
      const clipView = getHubClipView(win);
      if (!clipView) {
        entry.lastNonZeroSize = {
          width: nextBounds.width,
          height: nextBounds.height,
        };
      } else if (
        entry.visible ||
        !this.getVisibleEntryForWindow(win) ||
        !this.windowSlotBounds.has(win)
      ) {
        this.windowSlotBounds.set(win, { ...nextBounds });
      }
    }
    return nextBounds;
  }

  setBounds(tabKey: string, bounds: WcvBoundsInput): void {
    const entry = this.entries.get(tabKey);
    if (!entry) {
      return;
    }
    const win = entry.parentWindow;
    const hasClip = Boolean(getHubClipView(win));
    const prevRevealed = hasClip && this.isClipEntryRevealed(entry);
    const nextBounds = this.recordNormalizedBoundsDip(entry, bounds);
    if (hasClip) {
      const hasArea = nextBounds.width > 0 && nextBounds.height > 0;
      if (entry.visible || (hasArea && !this.getVisibleEntryForWindow(win))) {
        if (entry.visible && hasArea) {
          this.armClipRevealTimerIfNeeded(entry);
        }
        this.syncWindowClipAndEntryBounds(win);
        if (this.isClipEntryRevealed(entry) !== prevRevealed) {
          this.notifyClipVisibilityChanged(win, 'bounds-changed');
        }
      }
      return;
    }
    if (entry.visible && !entry.view.webContents.isDestroyed()) {
      if (entry.htmlFullscreen) {
        entry.appliedBoundsDip = undefined;
        entry.view.setBounds(this.resolveFullscreenBounds(entry));
      } else if (!sameRect(entry.appliedBoundsDip, nextBounds)) {
        entry.appliedBoundsDip = { ...nextBounds };
        entry.view.setBounds(nextBounds);
      }
    }
  }

  setPinned(tabKey: string, pinned: boolean): void {
    this.mutate(tabKey, (entry) => {
      entry.pinned = pinned;
    });
  }

  setAgentStepActive(tabKey: string, active: boolean): void {
    const wasActive = Boolean(this.entries.get(tabKey)?.isAgentStepActive);
    if (active) {
      const entry = this.entries.get(tabKey);
      if (entry) {
        guestPromptController.cancelForGuest(entry.webContentsId);
      }
    }
    this.mutate(tabKey, (entry) => {
      entry.isAgentStepActive = active;
      if (active && entry.htmlFullscreen) {
        this.clearHtmlFullscreen(entry, true);
      } else if (
        wasActive &&
        !active &&
        getHubClipView(entry.parentWindow) &&
        !entry.parentWindow.isDestroyed?.() &&
        this.getVisibleEntryForWindow(entry.parentWindow) === entry
      ) {
        this.syncWindowClipAndEntryBounds(entry.parentWindow);
      }
    });
  }

  /** Exits HTML fullscreen for `tabKey` and restores its split-pane bounds. */
  exitHtmlFullscreen(tabKey: string): void {
    const entry = this.entries.get(tabKey);
    if (entry?.htmlFullscreen) {
      this.clearHtmlFullscreen(entry, true);
    }
  }

  /**
   * Resolves the owning Hub renderer and visibility state for `webContentsId`
   * when it belongs to a live registered `WebContentsView` tab.
   */
  resolvePromptHostContext(webContentsId: number): PromptHostContext | null {
    const entry = this.byWebContentsId.get(webContentsId);
    if (!entry || entry.view.webContents.isDestroyed()) {
      return null;
    }
    return {
      hostSender: entry.hubSender,
      tabKey: entry.tabKey,
      visible: Boolean(entry.visible),
    };
  }

  /**
   * Whether the WCV guest belongs to an agent: it runs an agent step now or
   * the agent pinned it (pinned tabs stay agent-owned between steps).
   */
  isGuestAgentOwned(webContentsId: number): boolean {
    const entry = this.byWebContentsId.get(webContentsId);
    return Boolean(
      entry &&
        (entry.isAgentStepActive || entry.pinned) &&
        !entry.view.webContents.isDestroyed(),
    );
  }

  /**
   * Returns `true` when `webContentsId` belongs to a live registered
   * `WebContentsView` tab currently undergoing an agent step.
   */
  isGuestAgentActive(webContentsId: number): boolean {
    const entry = this.byWebContentsId.get(webContentsId);
    return Boolean(
      entry &&
        entry.isAgentStepActive &&
        !entry.view.webContents.isDestroyed(),
    );
  }

  setUserMuted(tabKey: string, muted: boolean): void {
    if (muted) {
      this.userMutedTabs.add(tabKey);
    } else {
      this.userMutedTabs.delete(tabKey);
    }
    const entry = this.entries.get(tabKey);
    if (entry && !entry.view.webContents.isDestroyed()) {
      entry.view.webContents.setAudioMuted?.(muted || !entry.visible);
    }
  }

  isUserMuted(tabKey: string): boolean {
    return this.userMutedTabs.has(tabKey);
  }

  openExternalUrl(url: string): Promise<void> | void {
    const fn =
      this.options.openExternal ??
      ((targetUrl: string) => shell.openExternal(targetUrl));
    return fn(url);
  }

  /**
   * Records an explicit caller-initiated navigation for `tabKey` so an
   * in-flight `acquire()` awaiting `configureProxy` will not overwrite it with
   * an `about:blank` bootstrap or stale `initialUrl`.
   */
  noteExternalNavigation(tabKey: string, url: string): void {
    const entry = this.entries.get(tabKey);
    if (!entry) {
      return;
    }
    this.externallyNavigatedEntries.add(entry);
    if (url && url !== 'about:blank') {
      entry.lastUrl = url;
      if (getHubClipView(entry.parentWindow) && !this.isClipEntryRevealed(entry)) {
        entry.clipRevealReady = false;
        this.armClipRevealTimerIfNeeded(entry, true);
      }
    }
  }

  noteExternalNavigationSettled(tabKey: string, succeeded: boolean): void {
    const entry = this.entries.get(tabKey);
    if (!entry || entry.view.webContents.isDestroyed()) {
      return;
    }
    const wc = entry.view.webContents;
    if (wc.isLoading?.()) {
      return;
    }
    const liveUrl = (wc.getURL?.() || '').trim();
    if (succeeded || hasRevealableClipDoc(entry, liveUrl)) {
      this.markClipEntryRevealReady(entry, 'load-settled');
    }
  }

  dispose(tabKey: string, clearEvicted = true): void {
    const existingEntry = this.entries.get(tabKey);
    this.rehydrating.delete(tabKey);
    if (clearEvicted) {
      const ownerWindow =
        existingEntry?.parentWindow ?? this.evicted.get(tabKey)?.parentWindow;
      if (
        ownerWindow &&
        this.activeRequestedTabByWindow.get(ownerWindow) === tabKey
      ) {
        this.activeRequestedTabByWindow.delete(ownerWindow);
      }
      this.evicted.delete(tabKey);
      this.userMutedTabs.delete(tabKey);
    }
    const entry = existingEntry;
    if (!entry) {
      return;
    }
    this.clearClipRevealTimer(entry);
    const ownerWindow = entry.parentWindow;
    const clipView = getHubClipView(ownerWindow);
    if (clipView) {
      entry.visible = false;
    }
    if (entry.htmlFullscreen) {
      this.clearHtmlFullscreen(entry, false);
    }
    entry.visible = false;
    guestPromptController.cancelForGuest(entry.webContentsId);
    dismissNativeGuestPopups(entry.view.webContents, {
      onlyPopupControls: false,
    });
    (this.options.dialogController ?? guestDialogController).disposeGuest(
      entry.view.webContents,
    );
    clipView?.removeChildView?.(entry.view);
    contentViewOf(ownerWindow)?.removeChildView?.(entry.view);
    this.entries.delete(tabKey);
    this.byWebContentsId.delete(entry.webContentsId);
    guestWebMcpController.noteGuestDisposed(entry.webContentsId);
    if (!entry.view.webContents.isDestroyed()) {
      entry.view.webContents.close?.();
    }
    deviceChooserController.cancelForGuest(entry.webContentsId);
    if (clipView) {
      this.syncWindowClipAndEntryBounds(ownerWindow);
    }
  }

  disposeForWindow(win: BrowserWindow): void {
    this.windowResetEpoch.set(win, (this.windowResetEpoch.get(win) ?? 0) + 1);
    this.activeRequestedTabByWindow.delete(win);
    this.windowSlotBounds.delete(win);
    for (const [tabKey, entry] of this.entries.entries()) {
      if (entry.parentWindow === win) {
        this.dispose(tabKey);
      }
    }
    for (const [tabKey, saved] of this.evicted.entries()) {
      if (saved.parentWindow === win) {
        this.evicted.delete(tabKey);
        this.userMutedTabs.delete(tabKey);
      }
    }
  }

  getEntry(tabKey: string): WcvEntry | undefined {
    return this.entries.get(tabKey);
  }

  getEntriesForWindow(win: BrowserWindow): WcvEntry[] {
    const result: WcvEntry[] = [];
    for (const entry of this.entries.values()) {
      if (entry.parentWindow === win) {
        result.push(entry);
      }
    }
    return result;
  }

  computeGuestMemoryBytes(
    guests: readonly WcvGuestPidInfo[],
    metrics?: readonly WcvProcessMetricLike[],
  ): Map<number, number> {
    let resolved = metrics;
    if (resolved === undefined) {
      try {
        resolved = (
          this.options.getAppMetrics ?? (() => app.getAppMetrics?.() ?? [])
        )();
      } catch {
        resolved = [];
      }
    }
    return computeGuestMemoryBytesByWebContentsId(guests, resolved);
  }

  isRegisteredWcv(webContentsId: number): boolean {
    const entry = this.byWebContentsId.get(webContentsId);
    return Boolean(entry && !entry.view.webContents.isDestroyed());
  }

  resolveDeviceChooserHost(
    webContentsId: number,
  ): DeviceChooserHostContext | null {
    const entry = this.byWebContentsId.get(webContentsId);
    return entry && !entry.view.webContents.isDestroyed()
      ? {
          hostSender: entry.hubSender,
          tabKey: entry.tabKey,
          visible: Boolean(entry.visible),
          isAgentStepActive: Boolean(entry.isAgentStepActive),
        }
      : null;
  }

  isOwnedBySender(
    webContentsId: number,
    senderId: number | undefined,
  ): boolean {
    const entry = this.byWebContentsId.get(webContentsId);
    return Boolean(
      senderId !== undefined &&
        entry &&
        !entry.view.webContents.isDestroyed() &&
        entry.hubSender.id === senderId,
    );
  }

  private mutate(tabKey: string, fn: (entry: WcvEntry) => void): void {
    const entry = this.entries.get(tabKey);
    if (entry) {
      fn(entry);
      if (entry.isAgentStepActive) {
        deviceChooserController.cancelForGuest(entry.webContentsId);
      }
      this.evictHiddenIfNeeded();
    }
  }

  private resolveFullscreenBounds(entry: WcvEntry): Rectangle {
    const win = entry.parentWindow;
    const contentSize = win?.getContentSize?.();
    const winBounds = !contentSize ? win?.getBounds?.() : undefined;
    const width = Math.max(
      1,
      Math.round(
        contentSize?.[0] ??
          winBounds?.width ??
          entry.lastNonZeroSize?.width ??
          DEFAULT_HIDDEN_WCV_BOUNDS.width,
      ),
    );
    const height = Math.max(
      1,
      Math.round(
        contentSize?.[1] ??
          winBounds?.height ??
          entry.lastNonZeroSize?.height ??
          DEFAULT_HIDDEN_WCV_BOUNDS.height,
      ),
    );
    return { x: 0, y: 0, width, height };
  }

  private requestGuestExitDomFullscreen(wc: WebContents): void {
    if (
      wc.isDestroyed?.() ||
      typeof wc.executeJavaScriptInIsolatedWorld !== 'function'
    ) {
      return;
    }
    void executeJavaScriptInIsolatedWorldWhenReady(
      wc,
      POPUP_GUARD_ISOLATED_WORLD_ID,
      [
        {
          code: 'if (document.fullscreenElement) { void document.exitFullscreen().catch(() => {}); }',
        },
      ],
    ).catch(() => undefined);
  }

  private ensureWindowMonitored(parentWindow: BrowserWindow): void {
    if (this.monitoredWindows.has(parentWindow)) {
      return;
    }
    this.monitoredWindows.add(parentWindow);
    const onResize = (): void => {
      if (parentWindow.isDestroyed?.()) {
        return;
      }
      for (const e of this.entries.values()) {
        if (
          e.parentWindow === parentWindow &&
          !e.view.webContents.isDestroyed() &&
          e.visible &&
          e.htmlFullscreen
        ) {
          if (getHubClipView(parentWindow)) {
            e.appliedBoundsDip = undefined;
            this.syncWindowClipAndEntryBounds(parentWindow);
          } else {
            e.view.setBounds?.(this.resolveFullscreenBounds(e));
          }
        }
      }
    };
    const onLeave = (): void => {
      if (!parentWindow.isDestroyed?.()) {
        for (const e of this.entries.values()) {
          if (e.parentWindow === parentWindow && e.htmlFullscreen) {
            this.clearHtmlFullscreen(e, true);
          }
        }
      }
    };
    parentWindow.on?.('resize', onResize);
    parentWindow.on?.('enter-full-screen', onResize);
    parentWindow.on?.('enter-html-full-screen', onResize);
    parentWindow.on?.('leave-full-screen', onLeave);
    parentWindow.on?.('leave-html-full-screen', onLeave);
    parentWindow.once?.('closed', () => {
      parentWindow.off?.('resize', onResize);
      parentWindow.off?.('enter-full-screen', onResize);
      parentWindow.off?.('enter-html-full-screen', onResize);
      parentWindow.off?.('leave-full-screen', onLeave);
      parentWindow.off?.('leave-html-full-screen', onLeave);
      this.disposeForWindow(parentWindow);
    });
  }

  private clearHtmlFullscreen(entry: WcvEntry, exitGuestDom: boolean): void {
    const wasFullscreen = Boolean(entry.htmlFullscreen);
    entry.htmlFullscreen = false;
    if (exitGuestDom) {
      this.requestGuestExitDomFullscreen(entry.view.webContents);
    }
    if (!wasFullscreen) {
      return;
    }
    const clipView = getHubClipView(entry.parentWindow);
    if (clipView) {
      if (!entry.view.webContents.isDestroyed()) {
        entry.appliedBoundsDip = undefined;
        if (
          exitGuestDom &&
          !entry.visible &&
          Array.isArray(clipView.children) &&
          clipView.children.includes(entry.view)
        ) {
          this.applyClipInnerBounds(
            entry,
            this.resolveInnerSlotSize(entry.parentWindow, entry.boundsDip),
          );
        }
        this.syncWindowClipAndEntryBounds(entry.parentWindow);
      }
    } else if (
      entry.visible &&
      entry.boundsDip &&
      !entry.view.webContents.isDestroyed()
    ) {
      entry.appliedBoundsDip = { ...entry.boundsDip };
      entry.view.setBounds(entry.boundsDip);
    }
    for (const listener of this.htmlFullscreenListeners) {
      listener(entry, false);
    }
  }

  private resolveHiddenBounds(entry: WcvEntry): Rectangle {
    const win = entry.parentWindow;
    const contentSize = win?.getContentSize?.();
    const winBounds = !contentSize ? win?.getBounds?.() : undefined;
    const maxW =
      contentSize?.[0] ?? winBounds?.width ?? DEFAULT_HIDDEN_WCV_BOUNDS.width;
    const maxH =
      contentSize?.[1] ?? winBounds?.height ?? DEFAULT_HIDDEN_WCV_BOUNDS.height;
    const desiredW =
      entry.lastNonZeroSize?.width ?? DEFAULT_HIDDEN_WCV_BOUNDS.width;
    const desiredH =
      entry.lastNonZeroSize?.height ?? DEFAULT_HIDDEN_WCV_BOUNDS.height;
    const width = Math.max(1, Math.min(desiredW, maxW > 0 ? maxW : desiredW));
    const height = Math.max(1, Math.min(desiredH, maxH > 0 ? maxH : desiredH));
    const bounds = entry.boundsDip;
    const rawX = bounds && bounds.width > 0 ? bounds.x : 0;
    const rawY = bounds && bounds.height > 0 ? bounds.y : 0;
    const x =
      maxW > 0
        ? Math.max(0, Math.min(rawX, Math.max(0, maxW - width)))
        : Math.max(0, rawX);
    const y =
      maxH > 0
        ? Math.max(0, Math.min(rawY, Math.max(0, maxH - height)))
        : Math.max(0, rawY);
    return { x, y, width, height };
  }

  /**
   * Moves hidden tab `tabKey` directly below the curtain so no other hidden
   * tab occludes it, keeping its compositor surface capturable.
   */
  prepareHiddenCapture(tabKey: string): void {
    const entry = this.entries.get(tabKey);
    if (entry && !entry.visible && !entry.view.webContents.isDestroyed()) {
      this.parkHiddenView(entry);
    }
  }

  private parkHiddenView(entry: WcvEntry): void {
    entry.visible = false;
    const win = entry.parentWindow;
    const clipView = getHubClipView(win);
    if (clipView) {
      this.clearClipRevealTimer(entry);
      deviceChooserController.cancelForGuest(entry.webContentsId);
      if (entry.view.webContents.isDestroyed()) {
        clipView.removeChildView?.(entry.view);
        contentViewOf(win)?.removeChildView?.(entry.view);
      } else {
        entry.view.setVisible?.(false);
        if (
          !Array.isArray(clipView.children) ||
          !clipView.children.includes(entry.view)
        ) {
          entry.appliedBoundsDip = undefined;
          clipView.addChildView?.(entry.view, 0);
          this.applyClipInnerBounds(entry, this.resolveInnerSlotSize(win));
        }
      }
      this.syncWindowClipAndEntryBounds(win);
      return;
    }
    entry.appliedBoundsDip = undefined;
    const contentView = contentViewOf(entry.parentWindow);
    deviceChooserController.cancelForGuest(entry.webContentsId);
    if (entry.view.webContents.isDestroyed()) {
      contentView?.removeChildView?.(entry.view);
      return;
    }
    const curtainView = getHubCurtainView(entry.parentWindow);
    if (!curtainView) {
      contentView?.removeChildView?.(entry.view);
      return;
    }
    // Insert directly below the curtain (above older hidden tabs) so that
    // stacked hidden tabs never occlude the most recently parked one, and set
    // bounds after `addChildView` so the attached view updates its native origin.
    const children = contentView?.children ?? [];
    const withoutEntry = children.filter((child) => child !== entry.view);
    const curtainIdx = withoutEntry.indexOf(curtainView);
    contentView?.addChildView?.(entry.view, Math.max(0, curtainIdx));
    entry.view.setBounds?.(this.resolveHiddenBounds(entry));
  }

  private detachVisibleSiblings(
    parentWindow: BrowserWindow,
    exceptEntry?: WcvEntry,
  ): void {
    for (const sibling of this.entries.values()) {
      if (
        sibling !== exceptEntry &&
        sibling.parentWindow === parentWindow &&
        sibling.visible
      ) {
        this.detachView(sibling);
      }
    }
  }

  private detachView(entry: WcvEntry): void {
    if (getHubClipView(entry.parentWindow)) {
      entry.visible = false;
    }
    guestPromptController.cancelForGuest(entry.webContentsId);
    if (entry.htmlFullscreen) {
      this.clearHtmlFullscreen(entry, true);
    }
    if (!entry.view.webContents.isDestroyed()) {
      dismissNativeGuestPopups(entry.view.webContents, {
        onlyPopupControls: false,
        shouldStillDismiss: () => !entry.visible,
      });
      entry.view.webContents.setAudioMuted?.(true);
    }
    this.parkHiddenView(entry);
  }

  private evictHiddenIfNeeded(): void {
    const maxHidden = this.options.maxHiddenTabs ?? MAX_HIDDEN_WCV_TABS;
    const minHidden = Math.min(
      maxHidden,
      this.options.minHiddenTabs ?? DEFAULT_MIN_HIDDEN_WCV_TABS,
    );
    const memoryBudget =
      this.options.memoryBudgetBytes ?? DEFAULT_HIDDEN_WCV_MEMORY_BUDGET_BYTES;
    const fallbackBytes =
      this.options.fallbackGuestMemoryBytes ?? FALLBACK_HIDDEN_WCV_MEMORY_BYTES;

    const idleHidden = [...this.entries.values()]
      .filter(
        (e) =>
          !e.visible &&
          !e.pinned &&
          !e.isAgentStepActive &&
          !this.operationPins.has(e.tabKey),
      )
      .sort((a, b) => a.lastShownAt - b.lastShownAt);
    if (idleHidden.length === 0) {
      return;
    }

    const allGuests: WcvGuestPidInfo[] = [...this.entries.values()]
      .filter((entry) => !entry.view.webContents.isDestroyed())
      .map((entry) => {
        let pid: number | undefined;
        try {
          pid = entry.view.webContents.getOSProcessId?.();
        } catch {
          pid = undefined;
        }
        return { webContentsId: entry.webContentsId, pid };
      });
    const bytesByWcId = this.computeGuestMemoryBytes(allGuests);
    const memoryOf = (entry: WcvEntry): number =>
      bytesByWcId.get(entry.webContentsId) ?? fallbackBytes;
    let totalMemory = idleHidden.reduce((sum, e) => sum + memoryOf(e), 0);

    while (idleHidden.length > 0) {
      const exceedsMaxCount = idleHidden.length > maxHidden;
      const exceedsMemoryBudget =
        idleHidden.length > minHidden && totalMemory > memoryBudget;
      if (!exceedsMaxCount && !exceedsMemoryBudget) {
        break;
      }
      const victim = idleHidden.shift();
      if (!victim) {
        break;
      }
      totalMemory -= memoryOf(victim);
      const snap = this.toSnapshot(victim);
      this.evicted.set(victim.tabKey, {
        url: snap.url,
        title: snap.title,
        partition: victim.partition,
        hubSenderId: victim.hubSender.id,
        parentWindow: victim.parentWindow,
        history: captureHistory(victim.view.webContents),
      });
      this.dispose(victim.tabKey, false);
    }
  }

  private attachGuestListeners(entry: WcvEntry): void {
    const wc = entry.view.webContents;
    const injectNoDragCss = (): void => {
      if (!wc.isDestroyed()) {
        void wc
          .insertCSS?.(NO_DRAG_USER_CSS, { cssOrigin: 'user' })
          .catch(() => undefined);
      }
    };
    const emit = (
      type: WcvGuestEventType,
      extra?: Record<string, unknown>,
    ): void => {
      if (!entry.hubSender.isDestroyed()) {
        entry.hubSender.send(WCV_GUEST_EVENT_CHANNEL, {
          type,
          ...this.toSnapshot(entry),
          ...extra,
        });
      }
    };
    const createNavHandler =
      (
        type:
          | 'will-navigate'
          | 'did-start-navigation'
          | 'did-redirect-navigation',
        resetError = false,
      ) =>
      (
        details?: GuestNavigationEventDetails,
        url?: string,
        isInPlace?: boolean,
        isMainFrame = true,
      ): void => {
        const navUrl = url ?? details?.url;
        const mainFrame = (details?.isMainFrame ?? isMainFrame) !== false;
        if (!mainFrame) {
          return;
        }
        const inPlace = Boolean(details?.isInPlace ?? isInPlace);
        const isSameDocument = Boolean(details?.isSameDocument ?? inPlace);
        const isRedirect = type === 'did-redirect-navigation';
        if (
          resetError &&
          !inPlace &&
          !isSameDocument &&
          navUrl &&
          !navUrl.startsWith('chrome-error://')
        ) {
          entry.crashed = false;
          entry.lastLoadError = null;
          entry.lastHttpResponseCode = null;
          if (navUrl.toLowerCase() !== 'about:blank') {
            if (!this.isClipEntryRevealed(entry) && wc.isLoading?.()) {
              entry.clipRevealReady = false;
            }
            this.armClipRevealTimerIfNeeded(entry, true);
          }
        }
        emit(type, {
          url: navUrl,
          isInPlace: inPlace,
          isSameDocument,
          isRedirect,
          isMainFrame: true,
        });
      };
    const enforceGuestZoomLock = (): void => {
      if (!wc.isDestroyed()) {
        if (typeof wc.getZoomLevel !== 'function' || wc.getZoomLevel() !== 0) {
          wc.setZoomLevel?.(0);
        }
      }
    };

    wc.on?.('will-navigate', createNavHandler('will-navigate'));
    wc.on?.('did-start-navigation', createNavHandler('did-start-navigation', true));
    wc.on?.(
      'did-redirect-navigation',
      createNavHandler('did-redirect-navigation'),
    );
    wc.on?.('zoom-changed', (event?: { preventDefault?: () => void }) => {
      event?.preventDefault?.();
      if (!wc.isDestroyed()) {
        wc.setZoomLevel?.(0);
      }
    });
    wc.on?.('dom-ready', () => {
      enforceGuestZoomLock();
      injectNoDragCss();
      emit('dom-ready');
    });
    wc.on?.('did-finish-load', () => {
      enforceGuestZoomLock();
    });
    wc.on?.('did-start-loading', () => emit('did-start-loading'));
    wc.on?.('did-stop-loading', () => {
      const liveUrl = (wc.getURL?.() || '').trim();
      if (
        !wc.isDestroyed() &&
        !wc.isLoading?.() &&
        hasRevealableClipDoc(entry, liveUrl)
      ) {
        this.markClipEntryRevealReady(entry, 'load-settled');
      }
      emit('did-stop-loading');
    });
    wc.on?.(
      'did-navigate',
      (
        _event: unknown,
        url?: string,
        httpResponseCode?: number,
        httpStatusText?: string,
      ) => {
        const hadErrorOrCrash = Boolean(entry.crashed || entry.lastLoadError);
        if (!url?.startsWith('chrome-error://')) {
          entry.crashed = false;
          entry.lastLoadError = null;
          entry.lastHttpResponseCode =
            typeof httpResponseCode === 'number' && httpResponseCode > 0
              ? httpResponseCode
              : null;
          entry.hasCommittedNonBlankNav = Boolean(
            url && url.toLowerCase() !== 'about:blank',
          );
          if (url && url !== 'about:blank') {
            entry.lastUrl = url;
            if (this.bootstrappedBlankEntries.delete(entry)) {
              wc.navigationHistory?.clear?.();
            }
          }
          if (hadErrorOrCrash) {
            this.armClipRevealTimerIfNeeded(entry);
          }
        }
        if (entry.htmlFullscreen) {
          this.clearHtmlFullscreen(entry, false);
        }
        enforceGuestZoomLock();
        void Promise.resolve(wc.setVisualZoomLevelLimits?.(1, 1)).catch(
          () => undefined,
        );
        injectNoDragCss();
        emit('did-navigate', {
          url,
          ...(typeof httpResponseCode === 'number'
            ? { httpResponseCode }
            : undefined),
          ...(typeof httpStatusText === 'string'
            ? { httpStatusText }
            : undefined),
        });
      },
    );
    wc.on?.(
      'did-navigate-in-page',
      (_event: unknown, url?: string, isMainFrame = true) => {
        if (isMainFrame && url) {
          entry.lastUrl = url;
          emit('did-navigate-in-page', { url });
        }
      },
    );
    wc.on?.('page-title-updated', (_event: unknown, title: string) => {
      entry.lastTitle = title;
      emit('page-title-updated', { title });
    });
    wc.on?.('page-favicon-updated', (_event: unknown, favicons: string[]) =>
      emit('page-favicon-updated', { favicons }),
    );
    wc.on?.('enter-html-full-screen', () => {
      if (
        !entry.visible ||
        entry.isAgentStepActive ||
        (getHubClipView(entry.parentWindow) &&
          !this.isClipEntryRevealReady(entry))
      ) {
        this.clearHtmlFullscreen(entry, true);
        return;
      }
      entry.htmlFullscreen = true;
      for (const listener of this.htmlFullscreenListeners) {
        listener(entry, true);
      }
      if (entry.htmlFullscreen && entry.visible && !wc.isDestroyed()) {
        entry.appliedBoundsDip = undefined;
        if (getHubClipView(entry.parentWindow)) {
          this.syncWindowClipAndEntryBounds(entry.parentWindow);
        } else {
          entry.view.setBounds(this.resolveFullscreenBounds(entry));
        }
      }
    });
    wc.on?.('leave-html-full-screen', () => {
      this.clearHtmlFullscreen(entry, false);
    });
    wc.on?.(
      'context-menu',
      (_event: unknown, params: ContextMenuParams) =>
        emit('context-menu', { params }),
    );
    wc.on?.('console-message', (details: unknown, ...legacyArgs: unknown[]) => {
      const normalized = normalizeWcvConsoleMessageEvent(details, legacyArgs);
      if (normalized) {
        emit('console-message', normalized as unknown as Record<string, unknown>);
      }
    });
    wc.on?.(
      'did-fail-load',
      (
        _e: unknown,
        errorCode: number,
        errorDescription: string,
        validatedURL: string,
        isMainFrame = true,
      ) => {
        if (isMainFrame && errorCode !== -3) {
          if (
            validatedURL &&
            validatedURL !== 'about:blank' &&
            !validatedURL.startsWith('chrome-error://')
          ) {
            entry.lastUrl = validatedURL;
          }
          if (errorCode !== -379) {
            entry.lastLoadError = {
              errorCode,
              errorDescription,
              validatedURL,
            };
            this.hideClipEntryOnFault(entry, 'guest-load-failed');
          }
          emit('did-fail-load', {
            errorCode,
            errorDescription,
            validatedURL,
          });
        }
      },
    );
    wc.on?.(
      'render-process-gone',
      (_e: unknown, details?: Partial<RenderProcessGoneDetails>) => {
        entry.crashed = true;
        if (getHubClipView(entry.parentWindow)) {
          entry.shownInVisibleClip = false;
        }
        this.hideClipEntryOnFault(entry, 'guest-render-process-gone');
        emit('render-process-gone', { reason: details?.reason ?? 'crashed' });
      },
    );
    wc.once?.('destroyed', () => {
      this.clearClipRevealTimer(entry);
      guestWebMcpController.noteGuestDisposed(entry.webContentsId);
      this.byWebContentsId.delete(entry.webContentsId);
      const isCurrent = this.entries.get(entry.tabKey) === entry;
      const win = entry.parentWindow;
      if (isCurrent) {
        this.entries.delete(entry.tabKey);
      }
      const clipView = getHubClipView(win);
      if (clipView) {
        if (isCurrent) {
          entry.visible = false;
          if (this.activeRequestedTabByWindow.get(win) === entry.tabKey) {
            this.activeRequestedTabByWindow.delete(win);
          }
          if (entry.htmlFullscreen) {
            this.clearHtmlFullscreen(entry, false);
          }
        }
        clipView.removeChildView?.(entry.view);
        contentViewOf(win)?.removeChildView?.(entry.view);
        if (!win.isDestroyed?.()) {
          this.syncWindowClipAndEntryBounds(win);
        }
      }
    });
  }

  private toSnapshot(entry: WcvEntry): WcvTabSnapshot {
    const wc = entry.view.webContents;
    const isAlive = !wc.isDestroyed();
    const liveUrl = isAlive ? wc.getURL?.() || '' : '';
    const hasValidLiveUrl =
      Boolean(liveUrl) &&
      liveUrl !== 'about:blank' &&
      !liveUrl.startsWith('chrome-error://');
    const history = isAlive ? wc.navigationHistory : null;
    return {
      tabKey: entry.tabKey,
      webContentsId: entry.webContentsId,
      url: !entry.lastLoadError && hasValidLiveUrl ? liveUrl : entry.lastUrl,
      title: (isAlive ? wc.getTitle?.() || '' : '') || entry.lastTitle,
      canGoBack: Boolean(history?.canGoBack?.() ?? (isAlive && wc.canGoBack?.())),
      canGoForward: Boolean(
        history?.canGoForward?.() ?? (isAlive && wc.canGoForward?.()),
      ),
      isLoading: Boolean(isAlive && wc.isLoading?.()),
      visible: Boolean(entry.visible),
      pinned: Boolean(entry.pinned),
      crashed: Boolean(entry.crashed),
      lastHttpResponseCode: entry.lastHttpResponseCode ?? null,
      lastLoadError: entry.lastLoadError ?? null,
    };
  }
}

export const wcvBrowserRegistry = new WcvBrowserRegistry();

/**
 * Returns `true` when `webContentsId` belongs to a live `WebContentsView` tab
 * in the default registry with an active agent step.
 */
export function isWcvGuestAgentActive(webContentsId: number): boolean {
  return wcvBrowserRegistry.isGuestAgentActive(webContentsId);
}

registerGuestAgentControlPredicate(isWcvGuestAgentActive);
registerGuestAgentOwnershipPredicate((webContentsId) =>
  wcvBrowserRegistry.isGuestAgentOwned(webContentsId),
);
guestPromptController.setHostResolver((guest) =>
  wcvBrowserRegistry.resolvePromptHostContext(guest.id),
);
