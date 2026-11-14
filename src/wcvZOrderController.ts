import {
  nativeImage,
  type Input,
  type MouseInputEvent,
  type Rectangle,
  type View,
} from 'electron';
import {
  WCV_CATCHER_RESET_DRAG_CHANNEL,
  WCV_CATCHER_SET_CURSOR_CHANNEL,
} from './catcherChannels';
import {
  DEFAULT_PAGE_ENABLE_TIMEOUT_MS,
  guestDialogController,
  type DialogFallbackContextProvider,
  type DialogGuestWebContents,
  type DialogHostSender,
} from './guestDialogController';
import {
  POPUP_GUARD_ISOLATED_WORLD_ID,
  dismissNativeGuestPopups,
  executeJavaScriptInIsolatedWorldWhenReady,
  waitForWebContentsCanExecuteJs,
  type CanExecuteJsWebContents,
  type GuestPopupWebContents,
  type IsolatedWorldWebContents,
} from './isolatedWorldExec';
import {
  GuestFileChooserController,
  guestFileChooserController,
  type GuestFileChooserFallbackContextProvider,
  type GuestFileChooserSender,
  type GuestFileChooserWebContents,
} from './guestFileChooserController';
import {
  getHubCatcherView,
  getHubClipView,
  getHubView,
  getHubWebContents,
  isMacOS,
  setHubSurfaceTransparent,
  type HubWindowInput,
} from './utils';
import {
  applyHubWindowZoom,
  resolveHubZoomShortcutAction,
} from './hubZoom';
import {
  isExternalOnlyAuthUrl,
  validateWebviewNavigationUrl,
} from './webviewBrowser';
import {
  DEFAULT_HIDDEN_WCV_BOUNDS,
  wcvBrowserRegistry,
  type WcvLoadError,
  type WcvSize,
} from './wcvBrowserRegistry';
import { isProtectedIdentityProviderOriginUrl } from './webviewUploadValidation';

export { POPUP_GUARD_ISOLATED_WORLD_ID };

export const WCV_Z_ORDER_CHANGED_CHANNEL = 'wcv:z-order-changed';
export const WCV_SHORTCUT_FORWARDED_CHANNEL = 'wcv:shortcut-forwarded';
export const WCV_CURSOR_CHANGED_CHANNEL = 'wcv:cursor-changed';
export const WCV_HUB_CURSOR_HOLE_STATE_CHANNEL = 'wcv:hub-cursor-hole-state';
export const WCV_HUB_PROBE_POINT_CHANNEL = 'wcv:hub-probe-point';
export const WCV_HUB_PROBE_POINT_RESPONSE_CHANNEL =
  'wcv:hub-probe-point-response';

/** Delay before the single capturePage() retry, about two frames at 60 Hz. */
export const CAPTURE_RETRY_MS = 35;

/** Maximum wait in milliseconds for the CDP screenshot fallback on hidden views. */
export const CDP_CAPTURE_TIMEOUT_MS = 2000;

/** Maximum wait in milliseconds for synthetic CDP trackpad pinch-zoom dispatch. */
export const PINCH_GESTURE_TIMEOUT_MS = 250;

/** Maximum wait in milliseconds for an in-flight hover point probe. */
export const HOVER_PROBE_TIMEOUT_MS = 1000;

/** Maximum wait in milliseconds for a pointerdown point probe before falling back to lastKnownIsHole. */
export const POINTERDOWN_PROBE_TIMEOUT_MS = 12;

/** Maximum wait in milliseconds for a guest CDP mouse dispatch in pointerChain. */
export const PRESS_OP_TIMEOUT_MS = 100;

const MIN_PINCH_SCALE_FACTOR = 0.25;
const MAX_PINCH_SCALE_FACTOR = 4;
const PINCH_RELATIVE_SPEED_PX_PER_S = 5000;

/**
 * User-origin stylesheet active while a `WebContentsView` is not elevated at
 * `z=1`. Renders single-select dropdown menus in-page (`appearance: base-select`)
 * inside the view's own compositor surface (so `curtainView` / `hubWcv` occlude
 * them and `capturePage()` captures the open option list) and hides the
 * external OS calendar/time indicator button.
 */
export const NON_ELEVATED_POPUP_SUPPRESSION_CSS = [
  'select:not([multiple]):not([size]),',
  'select:not([multiple])[size="1"] { appearance: base-select !important; }',
  'select:not([multiple]):not([size])::picker(select),',
  'select:not([multiple])[size="1"]::picker(select) { appearance: base-select !important; background-color: Canvas; color: CanvasText; }',
  'input::-webkit-calendar-picker-indicator { display: none !important; pointer-events: none !important; }',
  'input[type="color" i] { pointer-events: none !important; }',
].join('\n');

function buildIsolatedWorldPopupGuardScript(
  suppress: boolean,
  allowFileChooser = false,
): string {
  const flag = suppress ? 'true' : 'false';
  const allowFile = allowFileChooser ? 'true' : 'false';
  return `(() => {
    window.__wcvSuppressNativePopups = ${flag};
    window.__wcvAllowInterceptedFileChooser = ${allowFile};
    if (window.__wcvPopupGuardInstalled) return;
    window.__wcvPopupGuardInstalled = true;
    const isBlockedPickerInput = (node) => {
      if (!node || node.tagName !== 'INPUT') return false;
      const t = (node.type || '').toLowerCase();
      return t === 'color' || (t === 'file' && !window.__wcvAllowInterceptedFileChooser);
    };
    const isDateLikeInput = (node) => {
      if (!node || node.tagName !== 'INPUT') return false;
      const t = (node.type || '').toLowerCase();
      return t === 'date' || t === 'time' || t === 'datetime-local' || t === 'month' || t === 'week';
    };
    const resolveTarget = (e) => {
      const path = typeof e.composedPath === 'function' ? e.composedPath() : null;
      const target = (path && path.length > 0 ? path[0] : null) || e.target;
      return target && target.nodeType === 3 ? target.parentElement : target;
    };
    window.addEventListener('click', (e) => {
      if (!window.__wcvSuppressNativePopups) return;
      const raw = resolveTarget(e);
      const labelControl = raw && typeof raw.closest === 'function' ? raw.closest('label')?.control : null;
      if (isBlockedPickerInput(raw) || isBlockedPickerInput(labelControl)) {
        e.preventDefault();
        e.stopImmediatePropagation();
      }
    }, true);
    window.addEventListener('keydown', (e) => {
      if (!window.__wcvSuppressNativePopups) return;
      const raw = resolveTarget(e);
      const labelControl = raw && typeof raw.closest === 'function' ? raw.closest('label')?.control : null;
      if ((isBlockedPickerInput(raw) || isBlockedPickerInput(labelControl)) && (e.key === ' ' || e.key === 'Enter')) {
        e.preventDefault();
        e.stopImmediatePropagation();
        return;
      }
      if (isDateLikeInput(raw) && ((e.altKey && e.key === 'ArrowDown') || e.key === 'F4')) {
        e.preventDefault();
        e.stopImmediatePropagation();
      }
    }, true);
  })()`;
}

const MIN_VIEWPORT_DIMENSION = 1;
const MAX_VIEWPORT_DIMENSION = 8192;
const VIEWPORT_INVALIDATE_SETTLE_MS = 34;

/**
 * Returns `true` when a viewport override must not apply to `rawUrl`: external-only
 * OAuth endpoints and protected IdP hosts (shared list in `webviewUploadValidation`),
 * following UberProxy `/uberproxy/pen?url=` redirects to their target first.
 */
function isProtectedViewportOverrideUrl(rawUrl?: string | null): boolean {
  let current = (rawUrl || '').trim();
  for (let depth = 0; depth < 8 && current; depth++) {
    if (isExternalOnlyAuthUrl(current)) {
      return true;
    }
    let target: string | null = null;
    try {
      const parsed = new URL(current);
      if (
        parsed.protocol === 'https:' &&
        parsed.hostname.toLowerCase().replace(/\.+$/, '') ===
          'uberproxy-pen-redirect.corp.google.com' &&
        parsed.pathname === '/uberproxy/pen'
      ) {
        target = parsed.searchParams.get('url');
      }
    } catch {
      return false;
    }
    if (!target || !/^https?:\/\//i.test(target) || target === current) {
      return isProtectedIdentityProviderOriginUrl(current);
    }
    current = target;
  }
  return false;
}

/**
 * macOS Cocoa/Blink editing commands for synthetic CDP `Input.dispatchKeyEvent`
 * modifier chords. On macOS, AppKit normally translates `Cmd`/`Option`/`Ctrl`
 * key equivalents into Blink editor commands before Blink sees the event; CDP
 * bypasses AppKit, so `commands` must be supplied explicitly on `keyDown` /
 * `rawKeyDown` for `Meta+A` (`selectAll`), clipboard, undo/redo, and word/line
 * navigation/deletion to take effect (while still honoring page `keydown`
 * `Event.preventDefault()`).
 */
const MAC_EDITING_COMMANDS: Readonly<Record<string, readonly string[]>> = {
  'Meta+KeyA': ['selectAll'],
  'Meta+KeyC': ['copy'],
  'Meta+KeyV': ['paste'],
  'Shift+Alt+Meta+KeyV': ['pasteAndMatchStyle'],
  'Meta+KeyX': ['cut'],
  'Meta+KeyZ': ['undo'],
  'Shift+Meta+KeyZ': ['redo'],
  'Meta+Backspace': ['deleteToBeginningOfLine'],
  'Alt+Backspace': ['deleteWordBackward'],
  'Alt+Delete': ['deleteWordForward'],
  'Control+KeyH': ['deleteBackward'],
  'Control+KeyD': ['deleteForward'],
  'Control+KeyK': ['deleteToEndOfParagraph'],
  'Meta+ArrowLeft': ['moveToLeftEndOfLine'],
  'Shift+Meta+ArrowLeft': ['moveToLeftEndOfLineAndModifySelection'],
  'Meta+ArrowRight': ['moveToRightEndOfLine'],
  'Shift+Meta+ArrowRight': ['moveToRightEndOfLineAndModifySelection'],
  'Meta+ArrowUp': ['moveToBeginningOfDocument'],
  'Shift+Meta+ArrowUp': ['moveToBeginningOfDocumentAndModifySelection'],
  'Meta+ArrowDown': ['moveToEndOfDocument'],
  'Shift+Meta+ArrowDown': ['moveToEndOfDocumentAndModifySelection'],
  'Alt+ArrowLeft': ['moveWordLeft'],
  'Shift+Alt+ArrowLeft': ['moveWordLeftAndModifySelection'],
  'Alt+ArrowRight': ['moveWordRight'],
  'Shift+Alt+ArrowRight': ['moveWordRightAndModifySelection'],
  'Alt+ArrowUp': ['moveToBeginningOfParagraph'],
  'Shift+Alt+ArrowUp': ['moveToBeginningOfParagraphAndModifySelection'],
  'Alt+ArrowDown': ['moveToEndOfParagraph'],
  'Shift+Alt+ArrowDown': ['moveToEndOfParagraphAndModifySelection'],
  'Control+KeyA': ['moveToBeginningOfParagraph'],
  'Shift+Control+KeyA': ['moveToBeginningOfParagraphAndModifySelection'],
  'Control+KeyE': ['moveToEndOfParagraph'],
  'Shift+Control+KeyE': ['moveToEndOfParagraphAndModifySelection'],
  'Control+KeyB': ['moveBackward'],
  'Shift+Control+KeyB': ['moveBackwardAndModifySelection'],
  'Control+KeyF': ['moveForward'],
  'Shift+Control+KeyF': ['moveForwardAndModifySelection'],
  'Control+KeyP': ['moveUp'],
  'Shift+Control+KeyP': ['moveUpAndModifySelection'],
  'Control+KeyN': ['moveDown'],
  'Shift+Control+KeyN': ['moveDownAndModifySelection'],
};

export interface WcvCdpKeyParams {
  type: 'keyDown' | 'keyUp' | 'rawKeyDown' | 'char' | (string & {});
  modifiers?: number;
  key?: string;
  code?: string;
  text?: string;
  unmodifiedText?: string;
  windowsVirtualKeyCode?: number;
  nativeVirtualKeyCode?: number;
  autoRepeat?: boolean;
  isKeypad?: boolean;
  isSystemKey?: boolean;
  commands?: string[];
  [key: string]: unknown;
}

function normalizeMacCommandCode(params: {
  code?: string;
  key?: string;
}): string {
  const rawCode = (params.code ?? '').trim();
  if (rawCode) {
    return rawCode;
  }
  const rawKey = (params.key ?? '').trim();
  if (/^[a-zA-Z]$/.test(rawKey)) {
    return `Key${rawKey.toUpperCase()}`;
  }
  if (/^[0-9]$/.test(rawKey)) {
    return `Digit${rawKey}`;
  }
  return rawKey;
}

/**
 * Resolves Blink `EditorCommand` names for a macOS CDP `Input.dispatchKeyEvent`
 * payload when `params` is a `keyDown` or `rawKeyDown` without explicit
 * `commands`, or `undefined` when no macOS editing command applies.
 */
export function resolveMacEditingCommands(
  params: WcvCdpKeyParams,
): string[] | undefined {
  if (
    (params.type !== 'keyDown' && params.type !== 'rawKeyDown') ||
    params.commands !== undefined
  ) {
    return undefined;
  }
  const mods = params.modifiers ?? 0;
  if ((mods & 7) === 0) {
    // Unmodified or Shift-only keys are handled natively by Blink.
    return undefined;
  }
  const code = normalizeMacCommandCode(params);
  if (!code) {
    return undefined;
  }
  const parts: string[] = [];
  if (mods & 8) {
    parts.push('Shift');
  }
  if (mods & 2) {
    parts.push('Control');
  }
  if (mods & 1) {
    parts.push('Alt');
  }
  if (mods & 4) {
    parts.push('Meta');
  }
  parts.push(code);
  const matched = MAC_EDITING_COMMANDS[parts.join('+')];
  return matched ? [...matched] : undefined;
}

const VALID_CSS_CURSORS = new Set([
  'alias',
  'all-scroll',
  'cell',
  'col-resize',
  'context-menu',
  'copy',
  'crosshair',
  'e-resize',
  'ew-resize',
  'grab',
  'grabbing',
  'help',
  'move',
  'n-resize',
  'ne-resize',
  'nesw-resize',
  'no-drop',
  'none',
  'not-allowed',
  'ns-resize',
  'nw-resize',
  'nwse-resize',
  'pointer',
  'progress',
  'row-resize',
  's-resize',
  'se-resize',
  'sw-resize',
  'text',
  'vertical-text',
  'w-resize',
  'wait',
  'zoom-in',
  'zoom-out',
]);

/** Translates an Electron `'cursor-changed'` token into a standard CSS `cursor` keyword, falling back to `'default'`. */
export function mapElectronCursorToCss(electronCursor: unknown): string {
  const token =
    typeof electronCursor === 'string'
      ? electronCursor.trim().toLowerCase()
      : '';
  switch (token) {
    case '':
    case 'null':
    case 'default':
    case 'pointer':
    case 'custom':
      return 'default';
    case 'hand':
      return 'pointer';
    case 'ibeam':
      return 'text';
    case 'cross':
      return 'crosshair';
    case 'nodrop':
    case 'drag-drop-none':
      return 'no-drop';
    case 'drag-drop-copy':
      return 'copy';
    case 'drag-drop-move':
      return 'move';
    case 'drag-drop-link':
      return 'alias';
    default:
      return VALID_CSS_CURSORS.has(token) ? token : 'default';
  }
}

const HUB_GLOBAL_SHORTCUT_KEYS = new Set(['l', 't', 'w', 'k', ',', 'b', 'q']);

// How long a tab ignores its own focus event after a hidden tab hands focus
// back to it.
const FOCUS_HAND_BACK_GRACE_MS = 250;

export type WcvZOrderLevel = 0 | 1;

export type WcvZOrderReason = string;

export interface WcvZOrderState {
  tabKey: string;
  zOrder: WcvZOrderLevel;
  userElevationLatch: boolean;
  dragElevationActive: boolean;
  overlayActive: boolean;
  portalOverlayOpen: boolean;
  isAgentStepActive: boolean;
}

export interface WcvCdpMouseParams {
  type: 'mousePressed' | 'mouseReleased' | 'mouseMoved' | 'mouseWheel' | (string & {});
  x: number;
  y: number;
  button?: 'none' | 'left' | 'middle' | 'right' | 'back' | 'forward' | (string & {});
  clickCount?: number;
  modifiers?: number;
  deltaX?: number;
  deltaY?: number;
  [key: string]: unknown;
}

export interface WcvDispatchMouseOptions {
  allowOutOfBounds?: boolean;
}

export interface WcvPinchGestureParams {
  x: number;
  y: number;
  scaleFactor: number;
}

export interface WcvCdpDragDataItem {
  mimeType: string;
  data: string;
}

export interface WcvCdpDragData {
  items: WcvCdpDragDataItem[];
  files: string[];
  dragOperationsMask: number;
}

export interface WcvCdpDragParams {
  type: 'dragEnter' | 'dragOver' | 'drop' | 'dragCancel';
  x: number;
  y: number;
  data: WcvCdpDragData;
  modifiers?: number;
}

export interface HubCursorHoleStatePayload {
  isHole: boolean;
  dipX?: number;
  dipY?: number;
  reason?: 'pointermove' | 'stationary-overlay' | 'dragover' | (string & {});
}

export interface HubProbePointResponsePayload {
  requestId: number;
  isHole: boolean;
}

export interface CatcherWheelPayload {
  x: number;
  y: number;
  deltaX: number;
  deltaY: number;
  modifiers?: number;
}

export interface CatcherPinchPayload {
  x: number;
  y: number;
  scaleFactor: number;
}

export interface CatcherDragPayload {
  phase: 'enter' | 'over' | 'leave' | 'drop' | (string & {});
  x: number;
  y: number;
  modifiers?: number;
  types?: string[];
  hasFiles?: boolean;
  files?: string[];
  items?: Array<{ mimeType?: unknown; data?: unknown }>;
}

export interface CatcherPointerDownPayload {
  pressId: number;
  button: 'left' | 'middle' | 'right' | 'back' | 'forward' | (string & {});
  x: number;
  y: number;
  clickCount?: number;
  modifiers?: number;
}

export interface CatcherPointerMovePayload {
  x: number;
  y: number;
  modifiers?: number;
  isDragging?: boolean;
  pressId?: number;
  button?: string;
}

export interface CatcherPointerUpPayload {
  pressId: number;
  button: 'left' | 'middle' | 'right' | 'back' | 'forward' | (string & {});
  x: number;
  y: number;
  clickCount?: number;
  modifiers?: number;
  outOfBounds?: boolean;
}

export interface WcvZOrderControllerOptions {
  pageEnableTimeoutMs?: number;
  fileChooserController?: WcvZOrderFileChooserController;
}

export interface WcvNativeImageLike {
  isEmpty?(): boolean;
  getSize?(): { width: number; height: number };
  resize?(options: {
    width?: number;
    height?: number;
    quality?: 'good' | 'better' | 'best';
  }): WcvNativeImageLike;
  toDataURL(): string;
}

export interface WcvNavigationHistoryLike {
  canGoBack?(): boolean;
  goBack?(): void;
  canGoForward?(): boolean;
  goForward?(): void;
}

export interface WcvZOrderWebFrameMain {
  readonly processId?: number;
  readonly framesInSubtree?: readonly WcvZOrderWebFrameMain[];
  isDestroyed?(): boolean;
  executeJavaScriptInIsolatedWorld?(
    worldId: number,
    scripts: Array<{ code: string }>,
  ): Promise<unknown> | unknown;
}

export type WcvZOrderWebContents = DialogGuestWebContents &
  GuestFileChooserWebContents &
  IsolatedWorldWebContents &
  GuestPopupWebContents &
  CanExecuteJsWebContents & {
    readonly id: number;
    isDestroyed(): boolean;
    isCrashed?(): boolean;
    getURL?(): string;
    isLoadingMainFrame?(): boolean;
    focus?(): void;
    loadURL?(url: string): Promise<unknown>;
    capturePage?(): Promise<WcvNativeImageLike>;
    invalidate?(): void;
    insertCSS?(
      css: string,
      options?: { cssOrigin?: 'user' | 'author' },
    ): Promise<string>;
    removeInsertedCSS?(key: string): Promise<unknown> | void;
    setIgnoreMenuShortcuts?(ignore: boolean): void;
    setVisualZoomLevelLimits?(
      minimumLevel: number,
      maximumLevel: number,
    ): Promise<unknown> | void;
    setZoomLevel?(level: number): void;
    navigationHistory?: WcvNavigationHistoryLike;
    mainFrame?: WcvZOrderWebFrameMain;
  };

export interface WcvZOrderView {
  webContents: WcvZOrderWebContents;
}

export type WcvZOrderChildView = View | WcvZOrderView;

export interface WcvZOrderContentView {
  readonly children?: readonly unknown[];
  addChildView?(view: WcvZOrderChildView, index?: number): void;
  removeChildView?(view: WcvZOrderChildView): void;
}

export type WcvZOrderWindow = HubWindowInput & {
  contentView?: WcvZOrderContentView;
  isDestroyed?(): boolean;
  once?(event: string, listener: () => void): unknown;
  off?(event: string, listener: () => void): unknown;
  removeListener?(event: string, listener: () => void): unknown;
};

export interface WcvZOrderHubSender
  extends DialogHostSender,
    GuestFileChooserSender {
  isDestroyed(): boolean;
  send(channel: string, ...args: unknown[]): void;
}

export interface WcvZOrderEntry {
  tabKey: string;
  webContentsId: number;
  parentWindow: WcvZOrderWindow;
  view: WcvZOrderView;
  hubSender: WcvZOrderHubSender;
  visible?: boolean;
  crashed?: boolean;
  htmlFullscreen?: boolean;
  isAgentStepActive?: boolean;
  shownInVisibleClip?: boolean;
  boundsDip?: Rectangle | null;
  appliedBoundsDip?: Rectangle | null;
  lastNonZeroSize?: WcvSize | null;
  lastUrl?: string | null;
  lastLoadError?: WcvLoadError | { validatedURL?: string } | null;
}

export interface WcvZOrderRegistry {
  getEntry(tabKey: string): WcvZOrderEntry | undefined;
  getEntriesForWindow(win: WcvZOrderWindow): WcvZOrderEntry[];
  isClipEntryRevealed(entry: WcvZOrderEntry): boolean;
  prepareHiddenCapture(tabKey: string): void;
  hide(tabKey: string): void;
  setAgentStepActive(tabKey: string, active: boolean): void;
  noteExternalNavigation(tabKey: string, url: string): void;
  noteExternalNavigationSettled(tabKey: string, succeeded: boolean): void;
  openExternalUrl(url: string): Promise<unknown> | void;
  exitHtmlFullscreen?(tabKey: string): void;
  cancelWindowShowRequests?(win: WcvZOrderWindow): void;
  onHtmlFullscreenChanged?(
    listener: (entry: WcvZOrderEntry, active: boolean) => void,
  ): () => void;
  onClipVisibilityChanged(
    listener: (win: WcvZOrderWindow, reason: WcvZOrderReason) => void,
  ): () => void;
  onTabAcquired?(
    listener: (entry: WcvZOrderEntry) => void | Promise<void>,
  ): () => void;
}

export interface WcvZOrderDialogController {
  registerGuest(
    guestContents: DialogGuestWebContents,
    hostSender: DialogHostSender,
    fallbackContextProvider?: DialogFallbackContextProvider,
  ): void;
  syncFallbackContext?(
    guestContents: DialogGuestWebContents,
  ): Promise<unknown> | void;
}

export interface WcvZOrderFileChooserController {
  registerGuest(
    guestContents: GuestFileChooserWebContents,
    hostSender: GuestFileChooserSender,
    fallbackContextProvider?: GuestFileChooserFallbackContextProvider,
  ): void;
  syncInterception(
    guestContents: GuestFileChooserWebContents,
  ): Promise<unknown> | void;
  isFileChooserAllowed(webContentsId: number): boolean;
}

interface EventEmitterLike {
  on?(event: string, listener: (...args: unknown[]) => void): unknown;
  once?(event: string, listener: (...args: unknown[]) => void): unknown;
  off?(event: string, listener: (...args: unknown[]) => void): unknown;
  removeListener?(
    event: string,
    listener: (...args: unknown[]) => void,
  ): unknown;
}

interface CdpDebuggerLike extends EventEmitterLike {
  isAttached?(): boolean;
  attach?(protocolVersion?: string): void;
  sendCommand?(
    method: string,
    commandParams?: Record<string, unknown>,
  ): Promise<unknown>;
}

interface NavigationEventDetails {
  url?: string;
  isMainFrame?: boolean;
  isSameDocument?: boolean;
  isInPlace?: boolean;
  defaultPrevented?: boolean;
}

interface GuestBeforeInput extends Partial<Input> {
  type?: string;
}

interface PageLayoutMetricsResult {
  cssVisualViewport?: {
    scale?: number;
    clientWidth?: number;
    clientHeight?: number;
  };
  visualViewport?: {
    scale?: number;
  };
}

interface DipPoint {
  x: number;
  y: number;
}

type CatcherDragButton = 'left' | 'middle' | 'right';
type CatcherDownButton = CatcherDragButton | 'back' | 'forward';

interface TabRef {
  tabKey: string;
  webContentsId: number;
  commitSeq: number;
}

interface PendingProbe {
  epoch: number;
  isDown: boolean;
  timer: ReturnType<typeof setTimeout>;
  onSettle: (val: boolean | null, fromHub?: boolean) => void;
}

interface CatcherPress extends TabRef {
  epoch: number;
  button: CatcherDragButton;
  originPt: DipPoint;
  lastPt: DipPoint;
  lastModifiers: number;
  accepted?: boolean;
  upQueued?: boolean;
  hubReplay?: {
    originDip: DipPoint;
    lastDip: DipPoint;
    skip?: Set<number>;
  };
  interceptedDrag?: {
    data: WcvCdpDragData;
    entered: boolean;
  };
}

interface ExternalDragState extends TabRef {
  lastPt: DipPoint;
  modifiers: number;
  placeholderData: WcvCdpDragData;
}

interface QueuedHoverMove {
  x: number;
  y: number;
  modifiers?: number;
  oob?: boolean;
}

interface CatcherState {
  catcherEpoch: number;
  probeSeq: number;
  latestAppliedProbeId: number;
  pendingProbes: Map<number, PendingProbe>;
  presses: Map<number, CatcherPress>;
  pendingPressOps: number;
  chainGen: number;
  pointerChain: Promise<void>;
  slotBoundsDip?: Rectangle;
  visibleTabKey?: string;
  visibleWebContentsId?: number;
  lastKnownIsHole?: boolean;
  lastCursorDip?: DipPoint;
  hoverProbeId?: number;
  queuedHoverDip?: DipPoint;
  deferredHover?: {
    x: number;
    y: number;
    modifiers: number;
  };
  deferredLeave?: boolean;
  externalDrag?: ExternalDragState;
}

interface TabStateRecord {
  webContentsId: number;
  zOrder: WcvZOrderLevel;
  userElevationLatch: boolean;
  overlayActive: boolean;
  cdpMouseInFlight: number;
  guestCssCursor: string;
  humanPromptOpen?: boolean;
  listenersAttached?: boolean;
  clipNavListenersAttached?: boolean;
  committedMainFrameUrl?: string;
  pendingMainFrameUrl?: string;
  clipCommitSeq?: number;
  clipNavGen?: number;
  cdpAttached?: boolean;
  cdpAttachEpoch?: number;
  cdpAttachInFlight?: Promise<void>;
  popupSuppressionCssKey?: string;
  popupSuppressionCssInFlight?: Promise<void>;
  popupCssGeneration?: number;
  isolatedGuardSuppressed?: boolean;
  isolatedGuardInFlight?: Promise<void>;
  isolatedGuardFlushPending?: boolean;
  focusEmulationEnabled?: boolean;
  focusEmulationSeq?: number;
  dragInterceptionEnabled?: boolean;
  dragInterceptionSeq?: number;
  dragInterceptListenerAttached?: boolean;
  focusHandBackUntil?: number;
  visualZoomGen?: number;
  visualZoomUnlocked?: boolean;
  pinchZoomed?: boolean;
  pinchInFlight?: Promise<void>;
  queuedPinch?: WcvPinchGestureParams;
  hoverInFlight?: Promise<void>;
  queuedHoverMove?: QueuedHoverMove;
}

interface ViewportOverrideRecord {
  webContentsId: number;
  callerDesired: WcvSize | null;
  fallbackDesired: WcvSize | null;
  appliedKey?: string;
  pipeDirty?: boolean;
  syncClearSeq?: number;
  inFlight?: Promise<void>;
}

interface GuestHookBag {
  off: Array<() => void>;
  armed?: boolean;
  after?: () => void;
}

interface BestEffortDragCancelArgs {
  e: WcvZOrderEntry | undefined;
  rec: TabStateRecord | undefined;
  pt: DipPoint;
  data: WcvCdpDragData;
  modifiers?: number;
}

const contentViewOf = (
  win: WcvZOrderWindow | null | undefined,
): WcvZOrderContentView | undefined =>
  !win || win.isDestroyed?.() ? undefined : win.contentView;

function placeAt(
  cv: WcvZOrderContentView,
  view: WcvZOrderChildView,
  index: number,
): void {
  cv.addChildView?.(view, index);
  const after = cv.children;
  if (
    Array.isArray(after) &&
    after.indexOf(view) !== index &&
    typeof cv.removeChildView === 'function'
  ) {
    // Electron only documents addChildView(existingChild) as reordering to
    // topmost; detach and re-insert when index is ignored on an existing child.
    cv.removeChildView(view);
    cv.addChildView?.(view, index);
  }
}

const isTransientFullscreenFocusReason = (reason: string): boolean =>
  reason === 'window-blur' || reason === 'hub-focus';

const DRAG_BTNS = new Set<string>(['left', 'middle', 'right']);
const DOWN_BTNS = new Set<string>([...DRAG_BTNS, 'back', 'forward']);
const MOD_NAMES = ['alt', 'control', 'meta', 'shift'] as const;
type ModName = (typeof MOD_NAMES)[number];

const noop = (): void => {};

const makeCatcherState = (): CatcherState => {
  const [pendingProbes, presses] = [
    new Map<number, PendingProbe>(),
    new Map<number, CatcherPress>(),
  ];
  const z = { catcherEpoch: 0, probeSeq: 0, latestAppliedProbeId: 0 };
  const c = {
    pendingPressOps: 0,
    chainGen: 0,
    pointerChain: Promise.resolve(),
  };
  return { ...z, pendingProbes, presses, ...c };
};

const withPressTimeout = (p: Promise<unknown>): Promise<void> => {
  let t: ReturnType<typeof setTimeout> | undefined;
  const cap = new Promise<void>(
    (r) => void (t = setTimeout(r, PRESS_OP_TIMEOUT_MS)),
  );
  return Promise.race([p.then(noop, noop), cap]).finally(() => {
    if (t !== undefined) {
      clearTimeout(t);
    }
  });
};

const isNum = (v: unknown): v is number =>
  typeof v === 'number' && Number.isFinite(v);

const isPressId = (v: unknown): v is number =>
  typeof v === 'number' && Number.isInteger(v) && v > 0;

const isDragBtn = (v: unknown): v is CatcherDragButton =>
  typeof v === 'string' && DRAG_BTNS.has(v);

const isDownBtn = (v: unknown): v is CatcherDownButton =>
  typeof v === 'string' && DOWN_BTNS.has(v);

const hasXY = <T extends { x?: unknown; y?: unknown }>(
  p: T | null | undefined,
): p is T & DipPoint => isNum(p?.x) && isNum(p?.y);

const toDip = (v: number): number => Math.round(v) || 0;
const toMods = (v: unknown): number => (isNum(v) ? v & 15 : 0);
const toClicks = (v: unknown): number => (v === 2 || v === 3 ? v : 1);
const toInputMods = (v: unknown): ModName[] =>
  MOD_NAMES.filter((_, i) => Boolean((toMods(v) >> i) & 1));

const sameRect = (a: Rectangle | undefined, b: Rectangle): boolean =>
  a?.x === b.x && a.y === b.y && a.width === b.width && a.height === b.height;

const isNonEmptyStr = (v: unknown): v is string =>
  typeof v === 'string' && v.length > 0;

const sameTabRef = (
  a: { tabKey: string; webContentsId: number; commitSeq?: number } | undefined,
  b: { tabKey: string; webContentsId: number; commitSeq?: number } | undefined,
  seq?: number,
): boolean =>
  Boolean(
    a &&
      b &&
      a.tabKey === b.tabKey &&
      a.webContentsId === b.webContentsId &&
      (seq === undefined || a.commitSeq === seq),
  );

const buildPlaceholderDragData = (
  p: Pick<CatcherDragPayload, 'types' | 'hasFiles'>,
): WcvCdpDragData => {
  const types = Array.isArray(p.types) ? p.types.filter(isNonEmptyStr) : [];
  const items = types
    .filter((s) => s !== 'Files')
    .map((mimeType) => ({ mimeType, data: '' }));
  const files = p.hasFiles ? ['drag-file'] : [];
  return { items, files, dragOperationsMask: 0xffff };
};

const buildDropDragData = (
  p: Pick<CatcherDragPayload, 'files' | 'items'>,
): WcvCdpDragData => {
  const okFile = (f: unknown): f is string =>
    isNonEmptyStr(f) && !f.includes('\0');
  const files = Array.isArray(p.files) ? p.files.filter(okFile) : [];
  const items: WcvCdpDragDataItem[] = [];
  for (const it of Array.isArray(p.items) ? p.items : []) {
    if (it && isNonEmptyStr(it.mimeType) && typeof it.data === 'string') {
      items.push({ mimeType: it.mimeType, data: it.data });
    }
  }
  return { files, items, dragOperationsMask: 0xffff };
};

const hasCrossProcessSubframe = (e: WcvZOrderEntry): boolean => {
  const wc = e.view?.webContents;
  if (!wc || wc.isDestroyed?.()) {
    return false;
  }
  try {
    const mf: WcvZOrderWebFrameMain | null | undefined = wc.mainFrame;
    return Boolean(
      mf?.framesInSubtree?.some((f) => f.processId !== mf.processId),
    );
  } catch {
    return false;
  }
};

const detachEv = (
  t: EventEmitterLike | null | undefined,
  e: string,
  f: (...args: unknown[]) => void,
): void => void (t?.off ?? t?.removeListener)?.call(t, e, f);

const getCdpDebugger = (target: unknown): CdpDebuggerLike | undefined => {
  if (
    typeof target !== 'object' ||
    target === null ||
    !('debugger' in target)
  ) {
    return undefined;
  }
  const dbg = (target as { debugger?: unknown }).debugger;
  return typeof dbg === 'object' && dbg !== null
    ? (dbg as CdpDebuggerLike)
    : undefined;
};

/** Coordinates derived z-order layering, keyboard focus grants, and main-process CDP input for browser `WebContentsView`s. */
export class WcvZOrderController {
  private readonly tabStates = new Map<string, TabStateRecord>();
  private readonly viewportOverrides = new Map<
    string,
    ViewportOverrideRecord
  >();
  private readonly windowPortalOpen = new WeakMap<WcvZOrderWindow, boolean>();
  private readonly windowCatcherCursor = new WeakMap<
    WcvZOrderWindow,
    string
  >();
  private readonly catcherStates = new Map<WcvZOrderWindow, CatcherState>();
  private readonly disposers = new Set<() => void>();
  private readonly trackedWindows = new WeakSet<WcvZOrderWindow>();
  private readonly trackedCatcherContents = new WeakSet<object>();
  private readonly guestHooks = new WeakMap<WcvZOrderWebContents, GuestHookBag>();
  private disposed = false;
  private readonly pageEnableTimeoutMs: number;
  private readonly fileChooserController: WcvZOrderFileChooserController;

  constructor(
    private readonly registry: WcvZOrderRegistry = wcvBrowserRegistry,
    private readonly dialogController: WcvZOrderDialogController = guestDialogController,
    options?: WcvZOrderControllerOptions,
  ) {
    this.pageEnableTimeoutMs =
      options?.pageEnableTimeoutMs ?? DEFAULT_PAGE_ENABLE_TIMEOUT_MS;
    this.fileChooserController =
      options?.fileChooserController ??
      new GuestFileChooserController({
        pageEnableTimeoutMs: this.pageEnableTimeoutMs,
      });

    const u1 = this.registry.onHtmlFullscreenChanged?.((entry, active) => {
      this.handleHtmlFullscreenChanged(entry, active);
    });
    const u2 = this.registry.onClipVisibilityChanged((win, reason) => {
      if (!win.isDestroyed?.()) {
        this.reconcileWindow(win, reason);
      }
    });
    const u3 = this.registry.onTabAcquired?.(async (entry) => {
      const existingRec = this.tabStates.get(entry.tabKey);
      if (!entry.visible && existingRec) {
        existingRec.guestCssCursor = 'default';
        if (
          existingRec.focusEmulationEnabled &&
          !getHubClipView(entry.parentWindow)
        ) {
          await this.syncFocusEmulation(entry, existingRec);
        }
        if (existingRec.zOrder === 1 || existingRec.userElevationLatch) {
          this.reconcileWindow(entry.parentWindow, 'tab-reparented');
        }
      }
      const hasClip = Boolean(getHubClipView(entry.parentWindow));
      const hasCatcher = Boolean(getHubCatcherView(entry.parentWindow));
      const hasVp = this.viewportOverrides.has(entry.tabKey);
      if (hasClip || hasCatcher || hasVp) {
        const rec = this.ensureRecord(entry);
        if (rec.cdpAttached) {
          await this.syncDragInterception(entry, rec);
        }
        if (!this.isCrashedClipEntry(entry) && (hasClip || hasVp)) {
          await this.applyViewportOverride(entry);
        }
      } else if (existingRec?.cdpAttached) {
        await this.syncDragInterception(entry, existingRec);
      }
    });
    [u1, u2, u3].forEach((fn) => {
      if (fn) {
        this.disposers.add(fn);
      }
    });
  }

  private syncMenuShortcutSuppression(
    entry: WcvZOrderEntry,
    rec: TabStateRecord,
  ): void {
    const wc = entry.view.webContents;
    if (!wc || wc.isDestroyed?.()) {
      return;
    }
    wc.setIgnoreMenuShortcuts?.(
      rec.zOrder === 0 || Boolean(entry.isAgentStepActive),
    );
  }

  private handleHtmlFullscreenChanged(
    entry: WcvZOrderEntry,
    active: boolean,
  ): void {
    if (entry.view.webContents.isDestroyed()) {
      return;
    }
    const rec = this.ensureRecord(entry);
    if (active) {
      if (this.isElevationBlocked(entry, rec)) {
        this.registry.exitHtmlFullscreen?.(entry.tabKey);
        return;
      }
      rec.userElevationLatch = true;
      this.reconcileWindow(entry.parentWindow, 'enter-html-full-screen');
      entry.view.webContents.focus?.();
      return;
    }
    this.reconcileWindow(entry.parentWindow, 'leave-html-full-screen');
    this.syncMenuShortcutSuppression(entry, rec);
  }

  private resetTabPinchState(rec: TabStateRecord): void {
    rec.visualZoomGen = (rec.visualZoomGen ?? 0) + 1;
    rec.visualZoomUnlocked = false;
    rec.pinchZoomed = false;
    rec.queuedPinch = undefined;
  }

  private clearTabInputQueues(rec: TabStateRecord | undefined): void {
    if (rec) {
      rec.pinchInFlight =
        rec.queuedPinch =
        rec.hoverInFlight =
        rec.queuedHoverMove =
          undefined;
    }
  }

  private ensureRecord(entry: WcvZOrderEntry): TabStateRecord {
    let rec = this.tabStates.get(entry.tabKey);
    let isNewOrReplaced = false;
    let isReplaced = false;
    if (!rec) {
      rec = {
        webContentsId: entry.webContentsId,
        zOrder: 0,
        userElevationLatch: false,
        overlayActive: false,
        cdpMouseInFlight: 0,
        guestCssCursor: 'default',
      };
      this.tabStates.set(entry.tabKey, rec);
      isNewOrReplaced = true;
    } else if (rec.webContentsId !== entry.webContentsId) {
      // Preserve rec.overlayActive: it tracks host UI occlusion of the tabKey
      // slot rather than the replaced guest WebContents instance.
      rec.webContentsId = entry.webContentsId;
      rec.zOrder = 0;
      rec.userElevationLatch = false;
      rec.cdpMouseInFlight = 0;
      rec.guestCssCursor = 'default';
      this.resetTabPinchState(rec);
      this.clearTabInputQueues(rec);
      rec.humanPromptOpen = false;
      rec.listenersAttached = false;
      rec.clipNavListenersAttached = false;
      rec.committedMainFrameUrl = undefined;
      rec.pendingMainFrameUrl = undefined;
      rec.clipCommitSeq = 0;
      rec.clipNavGen = 0;
      rec.cdpAttached = false;
      rec.cdpAttachEpoch = (rec.cdpAttachEpoch ?? 0) + 1;
      rec.cdpAttachInFlight = undefined;
      rec.popupSuppressionCssKey = undefined;
      rec.popupSuppressionCssInFlight = undefined;
      rec.popupCssGeneration = 0;
      rec.isolatedGuardSuppressed = undefined;
      rec.isolatedGuardInFlight = undefined;
      rec.isolatedGuardFlushPending = false;
      rec.focusEmulationEnabled = undefined;
      rec.focusEmulationSeq = 0;
      rec.dragInterceptionEnabled = undefined;
      rec.dragInterceptionSeq = 0;
      rec.dragInterceptListenerAttached = false;
      rec.focusHandBackUntil = undefined;
      this.viewportOverrides.delete(entry.tabKey);
      isNewOrReplaced = true;
      isReplaced = true;
    }
    this.attachWindowHooks(entry.parentWindow);
    this.attachGuestHooks(entry, rec);
    if (isNewOrReplaced) {
      this.syncMenuShortcutSuppression(entry, rec);
      void this.syncPopupSuppression(entry, rec);
    }
    if (isReplaced && getHubCatcherView(entry.parentWindow)) {
      this.syncWindowCatcherCursor(entry.parentWindow);
    }
    return rec;
  }

  getState(tabKey: string): WcvZOrderState | null {
    const entry = this.registry.getEntry(tabKey);
    if (!entry) {
      return null;
    }
    const rec = this.ensureRecord(entry);
    return {
      tabKey,
      zOrder: rec.zOrder,
      userElevationLatch: rec.userElevationLatch,
      // TODO(yushis): Drop once renderers no longer read the legacy drag
      // elevation field from WcvZOrderState.
      dragElevationActive: false,
      overlayActive: rec.overlayActive,
      portalOverlayOpen: Boolean(this.windowPortalOpen.get(entry.parentWindow)),
      isAgentStepActive: Boolean(entry.isAgentStepActive),
    };
  }

  private isCrashedClipEntry(entry: WcvZOrderEntry): boolean {
    return Boolean(getHubClipView(entry.parentWindow) && entry.crashed);
  }

  private isElevationBlocked(
    entry: WcvZOrderEntry,
    rec: TabStateRecord,
  ): boolean {
    return Boolean(
      !entry.visible ||
        (getHubClipView(entry.parentWindow) &&
          !this.registry.isClipEntryRevealed(entry)) ||
        entry.isAgentStepActive ||
        rec.overlayActive ||
        rec.humanPromptOpen ||
        this.windowPortalOpen.get(entry.parentWindow),
    );
  }

  /**
   * Reconciles every tab in `win` so visible tabs sit immediately above or
   * below `hubView` according to `isElevationBlocked`, without moving `hubView`
   * or unrelated sibling views.
   */
  reconcileWindow(win: WcvZOrderWindow, reason: WcvZOrderReason): void {
    if (this.disposed || !win || win.isDestroyed?.()) {
      return;
    }
    this.attachWindowHooks(win);
    for (const tabKey of this.tabStates.keys()) {
      if (!this.registry.getEntry(tabKey)) {
        this.clearTabInputQueues(this.tabStates.get(tabKey));
        this.tabStates.delete(tabKey);
      }
    }
    for (const tabKey of this.viewportOverrides.keys()) {
      if (!this.registry.getEntry(tabKey)) {
        this.viewportOverrides.delete(tabKey);
      }
    }
    const cv = contentViewOf(win);
    const hubView = getHubView(win);
    const clipView = getHubClipView(win);
    const dismissedIds = clipView ? new Set<number>() : undefined;
    let windowDesired: WcvZOrderLevel = 0;
    let anyTabVisible = false;
    for (const entry of this.registry.getEntriesForWindow(win)) {
      const rec = this.ensureRecord(entry);
      const skipCrashedClipGuest = this.isCrashedClipEntry(entry);
      if (!entry.visible || entry.view.webContents.isDestroyed()) {
        const prev = rec.zOrder;
        rec.zOrder = 0;
        rec.userElevationLatch = false;
        if (prev === 1) {
          this.emitZOrderChanged(entry, 0, reason);
        }
        this.syncMenuShortcutSuppression(entry, rec);
        if (!skipCrashedClipGuest) {
          void this.syncPopupSuppression(entry, rec);
        }
        if (
          clipView &&
          !skipCrashedClipGuest &&
          !entry.view.webContents.isDestroyed()
        ) {
          this.syncTabFocusEmulation(entry, rec);
        }
        if (
          !skipCrashedClipGuest &&
          !entry.view.webContents.isDestroyed() &&
          (clipView || this.viewportOverrides.has(entry.tabKey))
        ) {
          void this.applyViewportOverride(entry).catch(() => undefined);
        }
        continue;
      }
      anyTabVisible = true;
      if (
        !skipCrashedClipGuest &&
        (clipView || this.viewportOverrides.has(entry.tabKey))
      ) {
        void this.applyViewportOverride(entry).catch(() => undefined);
      }
      const hadUserLatch = rec.userElevationLatch;
      if (clipView && !this.registry.isClipEntryRevealed(entry)) {
        rec.userElevationLatch = false;
      }
      if (clipView && !skipCrashedClipGuest) {
        this.syncTabFocusEmulation(entry, rec);
      }
      const desired: WcvZOrderLevel = this.isElevationBlocked(entry, rec)
        ? 0
        : 1;
      if (clipView && desired === 1) {
        windowDesired = 1;
      }
      const kids = !clipView ? cv?.children : undefined;
      if (cv && hubView && Array.isArray(kids)) {
        const hubIdx = kids.indexOf(hubView);
        if (hubIdx >= 0) {
          const viewIdx = kids.indexOf(entry.view);
          const physical = viewIdx < 0 ? -1 : viewIdx > hubIdx ? 1 : 0;
          if (physical !== desired) {
            const withoutTab = kids.filter((child) => child !== entry.view);
            const idx = withoutTab.indexOf(hubView);
            placeAt(cv, entry.view, desired === 1 ? idx + 1 : idx);
          }
        }
      }
      const prev = rec.zOrder;
      if (rec.zOrder !== desired) {
        rec.zOrder = desired;
        this.emitZOrderChanged(entry, desired, reason);
      }
      this.syncMenuShortcutSuppression(entry, rec);
      if (!skipCrashedClipGuest) {
        void this.syncPopupSuppression(entry, rec);
      }
      if (desired === 0 && entry.htmlFullscreen) {
        this.registry.exitHtmlFullscreen?.(entry.tabKey);
      }
      if (prev === 1 && desired === 0 && !skipCrashedClipGuest) {
        dismissNativeGuestPopups(entry.view.webContents, {
          onlyPopupControls: true,
          shouldStillDismiss: () => rec.zOrder === 0,
        });
        dismissedIds?.add(entry.webContentsId);
      }
      if (clipView && hadUserLatch && desired === 0 && entry.crashed) {
        const hubContents = getHubWebContents(win);
        if (hubContents && !hubContents.isDestroyed?.()) {
          hubContents.focus?.();
        }
      }
    }
    if (clipView && cv && hubView && Array.isArray(cv.children)) {
      const hubIdx = cv.children.indexOf(hubView);
      const clipIdx = cv.children.indexOf(clipView);
      const physical = clipIdx < 0 ? -1 : clipIdx > hubIdx ? 1 : 0;
      if (hubIdx >= 0 && physical !== windowDesired) {
        const idx = cv.children.filter((c) => c !== clipView).indexOf(hubView);
        placeAt(cv, clipView, windowDesired === 1 ? idx + 1 : idx);
      }
    }
    // Keep `hubWcv` see-through only while a tab is visible; an opaque
    // `hubWcv` saves the OS compositor a full-window blend on every frame.
    setHubSurfaceTransparent(win, anyTabVisible);
    this.syncCatcher(win, dismissedIds);
    this.syncWindowCatcherCursor(win);
  }

  private liveCatcher(w: WcvZOrderWindow | null | undefined): boolean {
    return !this.disposed && !w?.isDestroyed?.() && Boolean(getHubCatcherView(w));
  }

  private onWinClosed(win: WcvZOrderWindow, fn: () => void): () => void {
    const run = (): void => {
      if (this.disposers.delete(run)) {
        detachEv(win as unknown as EventEmitterLike, 'closed', run);
        fn();
      }
    };
    this.disposers.add(run);
    win.once?.('closed', run);
    return run;
  }

  private cancelQueuedHover(k: string | undefined): void {
    if (k !== undefined) {
      const rec = this.tabStates.get(k);
      if (rec) {
        rec.queuedHoverMove = undefined;
      }
    }
  }

  private fireHover(
    k: string,
    x: number,
    y: number,
    m?: number,
    oob = false,
  ): void {
    const e = !this.disposed ? this.registry.getEntry(k) : undefined;
    const isOk = (): boolean =>
      Boolean(e && !e.crashed && !e.view.webContents.isDestroyed());
    if (!e || !isOk() || (!oob && !e.visible)) {
      return;
    }
    const [rec, wcId] = [this.ensureRecord(e), e.webContentsId];
    if (rec.hoverInFlight) {
      rec.queuedHoverMove = { x, y, modifiers: m, oob };
      return;
    }
    const send = (
      mx: number,
      my: number,
      mm?: number,
      mo = false,
    ): Promise<void> => {
      const base: WcvCdpMouseParams = {
        type: 'mouseMoved',
        x: mx,
        y: my,
        button: 'none',
      };
      const ev: WcvCdpMouseParams =
        mm === undefined ? base : { ...base, modifiers: mm };
      return this.dispatchMouseEvent(
        k,
        ev,
        mo ? { allowOutOfBounds: true } : undefined,
      ).catch(noop);
    };
    let p = Promise.resolve();
    p = (async () => {
      try {
        await send(x, y, m, oob);
        while (rec.hoverInFlight === p && rec.queuedHoverMove) {
          const n = rec.queuedHoverMove;
          rec.queuedHoverMove = undefined;
          if (
            this.disposed ||
            !isOk() ||
            this.tabStates.get(k) !== rec ||
            this.registry.getEntry(k) !== e ||
            rec.webContentsId !== wcId
          ) {
            break;
          }
          if (n.oob || e.visible) {
            await send(n.x, n.y, n.modifiers, n.oob);
          }
        }
      } finally {
        if (rec.hoverInFlight === p) {
          rec.hoverInFlight = undefined;
        }
      }
    })();
    rec.hoverInFlight = p;
  }

  private liveHub(w: WcvZOrderWindow) {
    const hub = !w.isDestroyed?.() ? getHubWebContents(w) : undefined;
    return hub && !hub.isDestroyed?.() ? hub : undefined;
  }

  private isEpochLive(
    w: WcvZOrderWindow,
    st: CatcherState,
    ep: number,
  ): boolean {
    return !this.disposed && !w.isDestroyed?.() && st.catcherEpoch === ep;
  }

  private isPressIdle(st: CatcherState): boolean {
    return st.pendingPressOps === 0 && st.presses.size === 0;
  }

  private hasActiveHoldOrReplay(st: CatcherState): boolean {
    return [...st.presses.values()].some((p) => !p.upQueued || Boolean(p.hubReplay));
  }

  private canPreemptChain(st: CatcherState): boolean {
    const down = [...st.pendingProbes.values()].some((p) => Boolean(p.isDown));
    const live = [...st.presses.values()].some(
      (p) =>
        !p.upQueued ||
        !p.accepted ||
        Boolean(p.hubReplay) ||
        Boolean(p.interceptedDrag),
    );
    return !down && !live;
  }

  private resetPressChain(st: CatcherState): void {
    [st.chainGen, st.pendingPressOps] = [st.chainGen + 1, 0];
    st.pointerChain = Promise.resolve();
  }

  private emitBestEffortDragCancel(a: BestEffortDragCancelArgs): void {
    const wc = a.e?.view.webContents;
    const dbg = getCdpDebugger(wc);
    if (
      !this.disposed &&
      a.e &&
      a.rec?.cdpAttached &&
      !a.e.crashed &&
      wc &&
      !wc.isDestroyed?.() &&
      !wc.isCrashed?.() &&
      dbg?.isAttached?.()
    ) {
      const { x, y } = this.clampMousePointToViewport(a.e, a.pt.x, a.pt.y);
      const modifiers = toMods(a.modifiers);
      const p: Record<string, unknown> = {
        type: 'dragCancel',
        x,
        y,
        data: a.data,
        modifiers,
      };
      void Promise.resolve(
        dbg.sendCommand?.('Input.dispatchDragEvent', p),
      ).catch(noop);
    }
  }

  private cancelExternalDrag(st: CatcherState): void {
    const ext = st.externalDrag;
    st.externalDrag = undefined;
    const e = ext ? this.registry.getEntry(ext.tabKey) : undefined;
    const rec = ext ? this.tabStates.get(ext.tabKey) : undefined;
    if (ext && sameTabRef(ext, e, rec?.clipCommitSeq ?? 0)) {
      const { lastPt: pt, placeholderData: data, modifiers } = ext;
      this.emitBestEffortDragCancel({ e, rec, pt, data, modifiers });
    }
  }

  private cancelInterceptedPresses(
    st: CatcherState,
    only?: WcvZOrderEntry,
    emit = true,
  ): boolean {
    let hit = false;
    for (const [id, p] of [...st.presses.entries()]) {
      if (
        (only && !sameTabRef(p, only)) ||
        !p.interceptedDrag ||
        (emit && p.hubReplay)
      ) {
        continue;
      }
      const drag = p.interceptedDrag;
      p.interceptedDrag = undefined;
      st.presses.delete(id);
      hit = true;
      const e = emit ? (only ?? this.registry.getEntry(p.tabKey)) : undefined;
      const rec = emit ? this.tabStates.get(p.tabKey) : undefined;
      if (sameTabRef(p, e, rec?.clipCommitSeq ?? 0)) {
        const a: BestEffortDragCancelArgs = {
          e,
          rec,
          pt: p.lastPt,
          data: drag.data,
          modifiers: p.lastModifiers,
        };
        this.emitBestEffortDragCancel(a);
        if (
          e &&
          hasCrossProcessSubframe(e) &&
          (p.originPt.x !== p.lastPt.x || p.originPt.y !== p.lastPt.y)
        ) {
          this.emitBestEffortDragCancel({ ...a, pt: p.originPt });
        }
      }
    }
    return hit;
  }

  private cancelActiveDrags(st: CatcherState): void {
    this.cancelExternalDrag(st);
    this.cancelInterceptedPresses(st);
  }

  private releaseActivePresses(w: WcvZOrderWindow, st: CatcherState): void {
    const hub = this.liveHub(w);
    for (const p of st.presses.values()) {
      if (!p.accepted) {
        continue;
      }
      if (p.hubReplay) {
        if (hub) {
          const { x, y } = p.hubReplay.lastDip;
          hub.sendInputEvent?.({
            type: 'mouseUp',
            x,
            y,
            button: p.button,
            clickCount: 0,
          });
        }
        continue;
      }
      const e = !this.disposed ? this.registry.getEntry(p.tabKey) : undefined;
      const seq = this.tabStates.get(p.tabKey)?.clipCommitSeq ?? 0;
      if (
        e &&
        e.webContentsId === p.webContentsId &&
        !e.crashed &&
        !e.view.webContents.isDestroyed() &&
        seq === p.commitSeq
      ) {
        const ev: WcvCdpMouseParams = {
          type: 'mouseReleased',
          x: p.lastPt.x,
          y: p.lastPt.y,
          button: p.button,
          clickCount: 0,
        };
        void this.dispatchMouseEvent(p.tabKey, ev, {
          allowOutOfBounds: true,
        }).catch(noop);
      }
    }
    st.presses.clear();
  }

  private syncNonHole(w: WcvZOrderWindow, st: CatcherState): void {
    if (!this.isPressIdle(st) || st.externalDrag) {
      this.resetPressChain(st);
      this.cancelActiveDrags(st);
      this.releaseActivePresses(w, st);
    }
    this.syncCatcher(w);
  }

  private applyHoverMove(
    w: WcvZOrderWindow,
    x: number,
    y: number,
    m: number,
  ): void {
    const t = this.catcherTarget(w, true);
    const sb = t?.st.slotBoundsDip;
    if (t && sb) {
      const pt = { x: sb.x + toDip(x), y: sb.y + toDip(y) };
      t.st.lastCursorDip = pt;
      this.fireHover(t.tab.tabKey, x, y, m);
      this.probeHover(w, t.st, pt);
    }
  }

  private flushCatcherIdle(w: WcvZOrderWindow, st: CatcherState): void {
    if (!this.isPressIdle(st) || !this.liveCatcher(w)) {
      return;
    }
    const [leave, hover] = [st.deferredLeave, st.deferredHover];
    [st.deferredLeave, st.deferredHover] = [false, undefined];
    if (leave || !st.lastKnownIsHole) {
      st.lastKnownIsHole = false;
      this.syncCatcher(w);
    } else if (hover) {
      this.applyHoverMove(w, hover.x, hover.y, hover.modifiers);
    }
  }

  private abortProbes(w: WcvZOrderWindow, st: CatcherState): void {
    st.catcherEpoch += 1;
    [st.hoverProbeId, st.queuedHoverDip] = [undefined, undefined];
    [st.deferredHover, st.deferredLeave] = [undefined, false];
    st.latestAppliedProbeId = st.probeSeq;
    this.resetPressChain(st);
    this.cancelActiveDrags(st);
    this.releaseActivePresses(w, st);
    const list = [...st.pendingProbes.values()];
    st.pendingProbes.clear();
    list.forEach((p) => {
      clearTimeout(p.timer);
      p.onSettle(null, false);
    });
  }

  private dropCatcherState(w: WcvZOrderWindow): void {
    const st = this.catcherStates.get(w);
    if (st) {
      this.abortProbes(w, st);
      this.catcherStates.delete(w);
    }
  }

  private eligibleTab(w: WcvZOrderWindow): WcvZOrderEntry | undefined {
    const list = this.registry.getEntriesForWindow(w);
    const e = list.find((x) => this.registry.isClipEntryRevealed(x));
    const b = e?.boundsDip;
    const ok = !e?.htmlFullscreen && (b?.width ?? 0) > 0 && (b?.height ?? 0) > 0;
    return e && ok && !this.isElevationBlocked(e, this.ensureRecord(e))
      ? e
      : undefined;
  }

  private hideCatcher(
    w: WcvZOrderWindow,
    st: CatcherState,
    o?: { noIpc?: boolean; noHover?: boolean; skip?: Set<number> },
  ): void {
    const {
      visibleTabKey: k,
      visibleWebContentsId: id,
      slotBoundsDip: was,
    } = st;
    st.lastKnownIsHole = false;
    st.slotBoundsDip = st.visibleTabKey = st.visibleWebContentsId = undefined;
    this.abortProbes(w, st);
    if (!was) {
      return;
    }
    const catcher = getHubCatcherView(w);
    catcher?.setVisible?.(false);
    if (!o?.noIpc && !catcher?.webContents?.isDestroyed?.()) {
      catcher?.webContents?.send?.(WCV_CATCHER_RESET_DRAG_CHANNEL);
    }
    if (o?.noHover) {
      this.cancelQueuedHover(k);
    }
    const e = k !== undefined ? this.registry.getEntry(k) : undefined;
    if (
      e &&
      e.webContentsId === id &&
      !e.crashed &&
      !e.view.webContents.isDestroyed()
    ) {
      if (!o?.noHover && !e.htmlFullscreen) {
        this.fireHover(e.tabKey, -1, -1, undefined, true);
      }
      if (!o?.skip?.has(e.webContentsId)) {
        const shouldStillDismiss = (): boolean => !this.isCatcherVisible(w);
        const popOpts = { onlyPopupControls: true, shouldStillDismiss };
        dismissNativeGuestPopups(e.view.webContents, popOpts);
      }
    }
  }

  private syncCatcher(
    w: WcvZOrderWindow,
    skip?: Set<number>,
    noHover = false,
  ): void {
    const live = !this.disposed && Boolean(w && !w.isDestroyed?.());
    const catcher = live ? getHubCatcherView(w) : null;
    if (!catcher) {
      if (live) {
        this.dropCatcherState(w);
        for (const e of this.registry.getEntriesForWindow(w)) {
          const r = this.tabStates.get(e.tabKey);
          if (r?.cdpAttached && r.dragInterceptionEnabled) {
            void this.syncDragInterception(e, r);
          }
        }
      }
      return;
    }
    const hasHooks = Boolean(this.ensureCatcherHooks(w));
    const st = this.catcherStates.get(w);
    if (!st) {
      return;
    }
    const tab = hasHooks ? this.eligibleTab(w) : undefined;
    const slot = tab?.boundsDip ? { ...tab.boundsDip } : undefined;
    const same =
      tab?.tabKey === st.visibleTabKey &&
      tab?.webContentsId === st.visibleWebContentsId;
    const nonHole = !st.lastKnownIsHole && !this.hasActiveHoldOrReplay(st);
    if ((st.slotBoundsDip && !same) || !tab || !slot || nonHole) {
      if (st.slotBoundsDip) {
        this.hideCatcher(w, st, { skip });
      }
      return;
    }
    const cv = contentViewOf(w);
    const kids = cv?.children;
    if (cv && Array.isArray(kids)) {
      const idx = kids.indexOf(catcher);
      if (idx < 0) {
        if (st.slotBoundsDip) {
          this.hideCatcher(w, st, { noIpc: true, skip });
        }
        return;
      }
      const rest = kids.filter((c) => c !== catcher);
      const idxOf = (v: unknown): number => (v ? rest.indexOf(v) : -1);
      const top = Math.max(idxOf(getHubClipView(w)), idxOf(getHubView(w)));
      const target = top >= 0 ? top + 1 : rest.length;
      if (idx !== target) {
        placeAt(cv, catcher, target);
      }
    }
    if (catcher.getVisible?.() === false) {
      st.slotBoundsDip = undefined;
    }
    const wasVis = Boolean(st.slotBoundsDip);
    if (!sameRect(st.slotBoundsDip, slot)) {
      st.slotBoundsDip = slot;
      catcher.setBounds?.(slot);
    }
    if (!wasVis) {
      st.visibleTabKey = tab.tabKey;
      st.visibleWebContentsId = tab.webContentsId;
      catcher.setVisible?.(true);
      if (!noHover && !st.externalDrag && st.lastCursorDip) {
        const { x, y } = st.lastCursorDip;
        this.fireHover(tab.tabKey, x - slot.x, y - slot.y);
      }
    }
  }

  isCatcherVisible(win: WcvZOrderWindow): boolean {
    return Boolean(this.catcherStates.get(win)?.slotBoundsDip);
  }

  syncCatcherSlotBounds(win: WcvZOrderWindow): void {
    this.syncCatcher(win);
  }

  handleHubCursorHoleState(
    w: WcvZOrderWindow,
    p: HubCursorHoleStatePayload,
  ): void {
    if (!this.liveCatcher(w) || typeof p?.isHole !== 'boolean') {
      return;
    }
    const pt =
      isNum(p.dipX) && isNum(p.dipY)
        ? { x: toDip(p.dipX), y: toDip(p.dipY) }
        : undefined;
    if (!p.isHole) {
      const st = this.catcherStates.get(w);
      if (st) {
        st.lastCursorDip = pt ?? st.lastCursorDip;
        st.latestAppliedProbeId = st.probeSeq;
        st.lastKnownIsHole = false;
        if (this.canPreemptChain(st)) {
          this.syncNonHole(w, st);
        }
      }
    } else if (pt) {
      this.attachWindowHooks(w);
      let st = this.catcherStates.get(w);
      if (!st) {
        st = makeCatcherState();
        this.catcherStates.set(w, st);
      }
      const wasVis = Boolean(st.slotBoundsDip);
      const noHover = p.reason === 'dragover' || Boolean(st.externalDrag);
      st.lastCursorDip = pt;
      st.latestAppliedProbeId = st.probeSeq;
      st.lastKnownIsHole = true;
      st.deferredLeave = false;
      this.syncCatcher(w, undefined, noHover);
      if (
        !noHover &&
        wasVis &&
        st.slotBoundsDip &&
        st.visibleTabKey &&
        this.isPressIdle(st)
      ) {
        const { x, y } = st.slotBoundsDip;
        this.fireHover(st.visibleTabKey, pt.x - x, pt.y - y);
      }
    }
  }

  handleHubProbePointResponse(
    w: WcvZOrderWindow,
    p: HubProbePointResponsePayload,
  ): void {
    const id = p?.requestId;
    const ok = Number.isInteger(id) && id > 0 && typeof p.isHole === 'boolean';
    const st = ok && this.liveCatcher(w) ? this.catcherStates.get(w) : null;
    const pending = st && id ? st.pendingProbes.get(id) : undefined;
    if (st && pending && id) {
      st.pendingProbes.delete(id);
      clearTimeout(pending.timer);
      pending.onSettle(
        pending.epoch === st.catcherEpoch ? p.isHole : null,
        true,
      );
    }
  }

  private startProbe(
    w: WcvZOrderWindow,
    st: CatcherState,
    pt: DipPoint,
    ms: number,
    onDone: (val: boolean | null) => void,
    onTo: () => boolean | null = () => null,
    isDown = false,
  ): number | null {
    const hub = !this.disposed && st.slotBoundsDip ? this.liveHub(w) : null;
    if (!hub || typeof hub.send !== 'function') {
      return null;
    }
    const [epoch, requestId] = [st.catcherEpoch, ++st.probeSeq];
    let done = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const finish = (val: boolean | null, fromHub = false): void => {
      if (done) {
        return;
      }
      done = true;
      if (timer !== undefined) {
        clearTimeout(timer);
      }
      st.pendingProbes.delete(requestId);
      if (st.hoverProbeId === requestId) {
        st.hoverProbeId = undefined;
      }
      const liveVal = this.isEpochLive(w, st, epoch) ? val : null;
      if (
        fromHub &&
        liveVal !== null &&
        requestId >= st.latestAppliedProbeId
      ) {
        [st.latestAppliedProbeId, st.lastKnownIsHole] = [requestId, liveVal];
      }
      onDone(liveVal);
    };
    timer = setTimeout(() => finish(onTo(), false), ms);
    st.pendingProbes.set(requestId, {
      epoch,
      isDown,
      timer,
      onSettle: finish,
    });
    hub.send(WCV_HUB_PROBE_POINT_CHANNEL, {
      requestId,
      dipX: pt.x,
      dipY: pt.y,
    });
    return requestId;
  }

  private probeHover(w: WcvZOrderWindow, st: CatcherState, pt: DipPoint): void {
    if (st.hoverProbeId !== undefined) {
      st.queuedHoverDip = pt;
      return;
    }
    st.queuedHoverDip = undefined;
    const epoch = st.catcherEpoch;
    const reqId = this.startProbe(
      w,
      st,
      pt,
      HOVER_PROBE_TIMEOUT_MS,
      (v) => {
        if (this.isEpochLive(w, st, epoch)) {
          const next = st.queuedHoverDip;
          st.queuedHoverDip = undefined;
          if (v !== null && !st.lastKnownIsHole && this.canPreemptChain(st)) {
            this.syncNonHole(w, st);
          } else if (
            v !== null &&
            st.lastKnownIsHole &&
            next &&
            st.slotBoundsDip
          ) {
            this.probeHover(w, st, next);
          }
        }
      },
    );
    if (reqId !== null && st.pendingProbes.has(reqId)) {
      st.hoverProbeId = reqId;
    }
  }

  private enqueuePressOp(
    w: WcvZOrderWindow,
    st: CatcherState,
    fn: () => Promise<void> | void,
  ): Promise<void> {
    const gen = st.chainGen;
    st.pendingPressOps += 1;
    const run = (): Promise<void> | void | undefined =>
      st.chainGen === gen ? fn() : undefined;
    const done = (): void => {
      if (st.chainGen === gen) {
        st.pendingPressOps = Math.max(0, st.pendingPressOps - 1);
        this.flushCatcherIdle(w, st);
      }
    };
    const next = st.pointerChain.catch(noop).then(run).catch(noop).then(done);
    st.pointerChain = next;
    return next;
  }

  private catcherTarget(
    w: WcvZOrderWindow,
    sync = false,
  ): { st: CatcherState; tab: WcvZOrderEntry } | null {
    const st = this.liveCatcher(w) ? this.catcherStates.get(w) : undefined;
    if (!st?.slotBoundsDip) {
      return null;
    }
    const tab = this.eligibleTab(w);
    const same =
      tab?.tabKey === st.visibleTabKey &&
      tab?.webContentsId === st.visibleWebContentsId;
    const dirty =
      sync && tab?.boundsDip && !sameRect(st.slotBoundsDip, tab.boundsDip);
    if (!same || dirty) {
      this.syncCatcher(w);
    }
    return same && st.slotBoundsDip && tab ? { st, tab } : null;
  }

  handleCatcherWheel(w: WcvZOrderWindow, p: CatcherWheelPayload): void {
    const ok = hasXY(p) && isNum(p.deltaX) && isNum(p.deltaY);
    const t = ok ? this.catcherTarget(w) : null;
    if (t && !this.hasActiveHoldOrReplay(t.st)) {
      const { x, y, deltaX, deltaY } = p;
      const modifiers = toMods(p.modifiers);
      const ev: WcvCdpMouseParams = {
        type: 'mouseWheel',
        x,
        y,
        deltaX,
        deltaY,
        modifiers,
      };
      void this.dispatchMouseEvent(t.tab.tabKey, ev).catch(noop);
    }
  }

  handleCatcherPinch(w: WcvZOrderWindow, p: CatcherPinchPayload): void {
    const t = hasXY(p) && isNum(p.scaleFactor) ? this.catcherTarget(w) : null;
    if (t && !this.hasActiveHoldOrReplay(t.st)) {
      const g: WcvPinchGestureParams = {
        x: p.x,
        y: p.y,
        scaleFactor: p.scaleFactor,
      };
      void this.synthesizePinchGesture(t.tab.tabKey, g).catch(noop);
    }
  }

  private dragOp(k: string, p: WcvCdpDragParams): Promise<void> {
    return withPressTimeout(this.dispatchDragEvent(k, p));
  }

  private async sendDragEnterOver(
    k: string,
    p: Omit<WcvCdpDragParams, 'type'>,
    isLive: () => boolean,
  ): Promise<boolean> {
    await this.dragOp(k, { ...p, type: 'dragEnter' });
    if (!isLive()) {
      return false;
    }
    await this.dragOp(k, { ...p, type: 'dragOver' });
    return isLive();
  }

  handleCatcherDrag(w: WcvZOrderWindow, p: CatcherDragPayload): Promise<void> {
    const ph = p?.phase;
    const isMove = ph === 'enter' || ph === 'over';
    if (!hasXY(p) || (!isMove && ph !== 'leave' && ph !== 'drop')) {
      return Promise.resolve();
    }
    const t = this.catcherTarget(w, isMove);
    const sb = t?.st.slotBoundsDip;
    if (!t || !sb || this.hasActiveHoldOrReplay(t.st)) {
      const st =
        (ph === 'leave' || ph === 'drop') && this.liveCatcher(w)
          ? this.catcherStates.get(w)
          : undefined;
      if (st?.externalDrag) {
        this.cancelExternalDrag(st);
      }
      return Promise.resolve();
    }
    const { st, tab } = t;
    const pt = { x: sb.x + toDip(p.x), y: sb.y + toDip(p.y) };
    st.lastCursorDip = pt;
    if (isMove) {
      this.probeHover(w, st, pt);
    }
    const epoch = st.catcherEpoch;
    const commitSeq = this.ensureRecord(tab).clipCommitSeq ?? 0;
    const ref: TabRef = {
      tabKey: tab.tabKey,
      webContentsId: tab.webContentsId,
      commitSeq,
    };
    const dropProbe =
      ph === 'drop'
        ? new Promise<boolean | null>((r) => {
            const onTo = (): boolean => Boolean(st.lastKnownIsHole);
            const ms = POINTERDOWN_PROBE_TIMEOUT_MS;
            if (!this.startProbe(w, st, pt, ms, r, onTo, true)) {
              r(null);
            }
          })
        : null;
    const [mods, lpt] = [toMods(p.modifiers), { x: p.x, y: p.y }];
    return this.enqueuePressOp(w, st, async () => {
      if (dropProbe) {
        const isHole = await dropProbe;
        if (isHole === null || !this.isEpochLive(w, st, epoch)) {
          this.cancelExternalDrag(st);
          return;
        }
        if (!isHole) {
          st.lastKnownIsHole = false;
          this.revokeWindowFocusGrants(w);
          this.syncNonHole(w, st);
          return;
        }
      }
      const liveTab = (): WcvZOrderEntry | undefined => {
        const ok = this.isEpochLive(w, st, epoch) && Boolean(st.slotBoundsDip);
        const cur = ok ? this.eligibleTab(w) : undefined;
        const curSeq = cur
          ? (this.tabStates.get(cur.tabKey)?.clipCommitSeq ?? 0)
          : -1;
        return sameTabRef(ref, cur, curSeq) ? cur : undefined;
      };
      const curTab = liveTab();
      if (!curTab) {
        if (!isMove && st.externalDrag) {
          this.cancelExternalDrag(st);
        }
        return;
      }
      const isOk = (): boolean => Boolean(liveTab());
      const prev = st.externalDrag;
      const sameTab = sameTabRef(prev, ref, commitSeq);
      if (isMove) {
        const placeholderData = buildPlaceholderDragData(p);
        st.externalDrag = {
          ...ref,
          lastPt: lpt,
          modifiers: mods,
          placeholderData,
        };
        const dp = { ...lpt, data: placeholderData, modifiers: mods };
        await this.sendDragEnterOver(ref.tabKey, dp, isOk);
      } else if (ph === 'leave') {
        if (sameTab && prev) {
          st.externalDrag = undefined;
          const dp = { ...lpt, data: prev.placeholderData, modifiers: mods };
          await this.dragOp(ref.tabKey, { ...dp, type: 'dragCancel' });
        }
      } else {
        st.externalDrag = undefined;
        this.setUserElevationLatch(curTab, this.ensureRecord(curTab), true);
        curTab.view.webContents.focus?.();
        this.emitZOrderChanged(curTab, 1, 'user-gesture');
        const dropData = buildDropDragData(p);
        const dp = { ...lpt, data: dropData, modifiers: mods };
        const same = Boolean(
          sameTab &&
            prev &&
            prev.lastPt.x === lpt.x &&
            prev.lastPt.y === lpt.y &&
            !dropData.files?.length,
        );
        if (same || (await this.sendDragEnterOver(ref.tabKey, dp, isOk))) {
          await this.dragOp(ref.tabKey, { ...dp, type: 'drop' });
        }
      }
    });
  }

  private activePressTab(
    w: WcvZOrderWindow,
    st: CatcherState,
    id: number,
    press: CatcherPress,
  ): WcvZOrderEntry | undefined {
    const ok =
      this.isEpochLive(w, st, press.epoch) &&
      Boolean(press.accepted && !press.hubReplay) &&
      st.presses.get(id) === press;
    const tab = ok ? this.eligibleTab(w) : undefined;
    const seq = tab ? (this.tabStates.get(tab.tabKey)?.clipCommitSeq ?? 0) : -1;
    return tab?.tabKey === press.tabKey &&
      tab.webContentsId === press.webContentsId &&
      seq === press.commitSeq
      ? tab
      : undefined;
  }

  handleCatcherPointerDown(
    w: WcvZOrderWindow,
    p: CatcherPointerDownPayload,
  ): Promise<void> {
    const ok = hasXY(p) && isPressId(p.pressId) && isDownBtn(p.button);
    const t = ok ? this.catcherTarget(w, true) : null;
    const sb = t?.st.slotBoundsDip;
    if (!t || !sb) {
      return Promise.resolve();
    }
    const { st, tab } = t;
    const origin = { x: sb.x, y: sb.y };
    const pt = { x: origin.x + toDip(p.x), y: origin.y + toDip(p.y) };
    st.lastCursorDip = pt;
    [st.deferredHover, st.deferredLeave] = [undefined, false];
    this.cancelQueuedHover(tab.tabKey);
    const [epoch, commitSeq] = [
      st.catcherEpoch,
      this.ensureRecord(tab).clipCommitSeq ?? 0,
    ];
    const { tabKey, webContentsId } = tab;
    const press: CatcherPress | undefined = isDragBtn(p.button)
      ? {
          epoch,
          tabKey,
          webContentsId,
          commitSeq,
          button: p.button,
          originPt: { x: p.x, y: p.y },
          lastPt: { x: p.x, y: p.y },
          lastModifiers: toMods(p.modifiers),
        }
      : undefined;
    if (press) {
      st.presses.set(p.pressId, press);
    }
    const drop = (): void => {
      if (press && st.presses.get(p.pressId) === press) {
        st.presses.delete(p.pressId);
      }
    };
    const onTo = (): boolean => Boolean(st.lastKnownIsHole);
    const ms = POINTERDOWN_PROBE_TIMEOUT_MS;
    const probe = new Promise<boolean | null>((r) => {
      if (!this.startProbe(w, st, pt, ms, r, onTo, true)) {
        r(null);
      }
    });
    return this.enqueuePressOp(w, st, async () => {
      const isHole = await probe;
      if (isHole === null || !this.isEpochLive(w, st, epoch)) {
        return drop();
      }
      if (!isHole) {
        st.lastKnownIsHole = false;
        this.revokeWindowFocusGrants(w);
        const vis = st.visibleTabKey
          ? this.registry.getEntry(st.visibleTabKey)
          : undefined;
        let skip: Set<number> | undefined;
        if (
          vis &&
          vis.webContentsId === st.visibleWebContentsId &&
          !vis.crashed &&
          !vis.view.webContents.isDestroyed()
        ) {
          const shouldStillDismiss = (): boolean =>
            !this.isCatcherVisible(w) || !st.lastKnownIsHole;
          const opts = { onlyPopupControls: true, shouldStillDismiss };
          dismissNativeGuestPopups(vis.view.webContents, opts);
          skip = new Set([vis.webContentsId]);
        }
        const hub = this.liveHub(w);
        if (!press || st.presses.get(p.pressId) !== press || !hub) {
          drop();
          this.hideCatcher(w, st, { noHover: true, skip });
          hub?.focus?.();
          return;
        }
        press.accepted = true;
        press.hubReplay = { originDip: origin, lastDip: pt, skip };
        hub.focus?.();
        const [mods, clickCount] = [
          toInputMods(p.modifiers),
          toClicks(p.clickCount),
        ];
        const m: Partial<Pick<MouseInputEvent, 'modifiers'>> =
          mods.length > 0 ? { modifiers: mods } : {};
        hub.sendInputEvent?.({
          type: 'mouseDown',
          ...pt,
          button: press.button,
          clickCount,
          ...m,
        });
        return;
      }
      const curTab = st.slotBoundsDip ? this.eligibleTab(w) : undefined;
      const curSeq = curTab
        ? (this.tabStates.get(curTab.tabKey)?.clipCommitSeq ?? 0)
        : -1;
      const same =
        curTab?.tabKey === tabKey &&
        curTab.webContentsId === webContentsId &&
        curSeq === commitSeq;
      if (!curTab || !same) {
        drop();
        this.syncCatcher(w);
      } else if (p.button === 'back' || p.button === 'forward') {
        this.navigateGuestHistory(curTab.tabKey, p.button);
      } else if (press && st.presses.get(p.pressId) === press) {
        press.accepted = true;
        this.setUserElevationLatch(curTab, this.ensureRecord(curTab), true);
        curTab.view.webContents.focus?.();
        this.emitZOrderChanged(curTab, 1, 'user-gesture');
        const [clickCount, modifiers] = [
          toClicks(p.clickCount),
          toMods(p.modifiers),
        ];
        const ev: WcvCdpMouseParams = {
          type: 'mousePressed',
          x: p.x,
          y: p.y,
          button: press.button,
          clickCount,
          modifiers,
        };
        await withPressTimeout(this.dispatchMouseEvent(curTab.tabKey, ev));
      }
    });
  }

  handleCatcherPointerMove(
    w: WcvZOrderWindow,
    p: CatcherPointerMovePayload,
  ): void {
    if (!hasXY(p)) {
      return;
    }
    const st = this.liveCatcher(w) ? this.catcherStates.get(w) : undefined;
    if (p.isDragging) {
      const id = isPressId(p.pressId) ? p.pressId : 0;
      const press =
        st && id && isDragBtn(p.button) ? st.presses.get(id) : undefined;
      if (!st || !press || press.upQueued || press.epoch !== st.catcherEpoch) {
        return;
      }
      const sb = st.slotBoundsDip;
      if (sb) {
        st.lastCursorDip = { x: sb.x + toDip(p.x), y: sb.y + toDip(p.y) };
      }
      const [x, y, rawMods] = [p.x, p.y, p.modifiers];
      void this.enqueuePressOp(w, st, async () => {
        const rep = press.hubReplay;
        if (rep) {
          if (
            this.isEpochLive(w, st, press.epoch) &&
            st.presses.get(id) === press
          ) {
            const [gx, gy] = [
              rep.originDip.x + toDip(x),
              rep.originDip.y + toDip(y),
            ];
            const [movementX, movementY] = [
              gx - rep.lastDip.x,
              gy - rep.lastDip.y,
            ];
            rep.lastDip = { x: gx, y: gy };
            const modifiers: NonNullable<MouseInputEvent['modifiers']> = [
              ...toInputMods(rawMods),
              `${press.button}buttondown`,
            ];
            const ev: MouseInputEvent = {
              type: 'mouseMove',
              x: gx,
              y: gy,
              button: press.button,
              movementX,
              movementY,
              modifiers,
            };
            this.liveHub(w)?.sendInputEvent?.(ev);
          }
          return;
        }
        const curTab = this.activePressTab(w, st, id, press);
        if (curTab) {
          const [modifiers, pt] = [toMods(rawMods), { x, y }];
          [press.lastPt, press.lastModifiers] = [pt, modifiers];
          const isLive = (): boolean =>
            Boolean(
              this.activePressTab(w, st, id, press) && press.interceptedDrag,
            );
          const drag = press.interceptedDrag;
          if (!drag) {
            const ev: WcvCdpMouseParams = {
              type: 'mouseMoved',
              x,
              y,
              button: press.button,
              modifiers,
            };
            await withPressTimeout(
              this.dispatchMouseEvent(curTab.tabKey, ev, {
                allowOutOfBounds: true,
              }),
            );
            const late = press.interceptedDrag;
            const liveTab =
              late && !late.entered
                ? this.activePressTab(w, st, id, press)
                : undefined;
            if (liveTab && late && !late.entered) {
              late.entered = true;
              const dp = { ...pt, data: late.data, modifiers };
              await this.sendDragEnterOver(liveTab.tabKey, dp, isLive);
            }
          } else {
            drag.entered = true;
            const dp = { ...pt, data: drag.data, modifiers };
            await this.sendDragEnterOver(curTab.tabKey, dp, isLive);
          }
        }
      });
    } else if (st?.slotBoundsDip && !this.isPressIdle(st)) {
      st.deferredHover = { x: p.x, y: p.y, modifiers: toMods(p.modifiers) };
      st.deferredLeave = false;
    } else {
      this.applyHoverMove(w, p.x, p.y, toMods(p.modifiers));
    }
  }

  handleCatcherPointerUp(
    w: WcvZOrderWindow,
    p: CatcherPointerUpPayload,
  ): Promise<void> {
    if (!hasXY(p) || !isPressId(p.pressId) || !isDragBtn(p.button)) {
      return Promise.resolve();
    }
    const st = this.liveCatcher(w) ? this.catcherStates.get(w) : undefined;
    const press = st?.presses.get(p.pressId);
    if (!st || !press || press.upQueued || press.epoch !== st.catcherEpoch) {
      return Promise.resolve();
    }
    press.upQueued = true;
    const sb = st.slotBoundsDip;
    if (sb && !p.outOfBounds) {
      st.lastCursorDip = { x: sb.x + toDip(p.x), y: sb.y + toDip(p.y) };
    }
    const [id, x, y, oob, rawMods] = [
      p.pressId,
      p.x,
      p.y,
      p.outOfBounds,
      p.modifiers,
    ];
    const clickCount = toClicks(p.clickCount);
    return this.enqueuePressOp(w, st, async () => {
      const wasLive =
        this.isEpochLive(w, st, press.epoch) &&
        Boolean(press.accepted) &&
        st.presses.get(id) === press;
      const rep = press.hubReplay;
      if (rep) {
        if (st.presses.get(id) === press) {
          st.presses.delete(id);
        }
        if (wasLive) {
          const [gx, gy] = [
            rep.originDip.x + toDip(x),
            rep.originDip.y + toDip(y),
          ];
          rep.lastDip = { x: gx, y: gy };
          const upMods = toInputMods(rawMods);
          const m: Partial<Pick<MouseInputEvent, 'modifiers'>> =
            upMods.length > 0 ? { modifiers: upMods } : {};
          this.liveHub(w)?.sendInputEvent?.({
            type: 'mouseUp',
            x: gx,
            y: gy,
            button: press.button,
            clickCount,
            ...m,
          });
          st.lastKnownIsHole = false;
          [st.deferredHover, st.deferredLeave] = [undefined, false];
          if (!this.hasActiveHoldOrReplay(st)) {
            this.hideCatcher(w, st, { noHover: true, skip: rep.skip });
          }
        }
        return;
      }
      const curTab = wasLive
        ? this.activePressTab(w, st, id, press)
        : undefined;
      const drag = press.interceptedDrag;
      if (st.presses.get(id) === press) {
        st.presses.delete(id);
      }
      if (curTab && drag) {
        const [mods, pt] = [toMods(rawMods), { x, y }];
        [press.lastPt, press.lastModifiers] = [pt, mods];
        const dp = { ...pt, data: drag.data, modifiers: mods };
        const isLive = (): boolean =>
          this.isEpochLive(w, st, press.epoch) &&
          (this.tabStates.get(curTab.tabKey)?.clipCommitSeq ?? 0) ===
            press.commitSeq;
        if (oob) {
          await this.dragOp(curTab.tabKey, { ...dp, type: 'dragCancel' });
        } else if (
          drag.entered ||
          ((drag.entered = true),
          await this.sendDragEnterOver(curTab.tabKey, dp, isLive))
        ) {
          await this.dragOp(curTab.tabKey, { ...dp, type: 'drop' });
        }
        if (
          hasCrossProcessSubframe(curTab) &&
          (press.originPt.x !== pt.x || press.originPt.y !== pt.y) &&
          isLive()
        ) {
          await this.dragOp(curTab.tabKey, {
            ...press.originPt,
            data: drag.data,
            modifiers: mods,
            type: 'dragCancel',
          });
        }
      } else if (curTab) {
        const ev: WcvCdpMouseParams = {
          type: 'mouseReleased',
          x,
          y,
          button: press.button,
          clickCount,
          modifiers: toMods(rawMods),
        };
        await withPressTimeout(
          this.dispatchMouseEvent(curTab.tabKey, ev, {
            allowOutOfBounds: true,
          }),
        );
      }
      if (oob && wasLive && this.isEpochLive(w, st, press.epoch)) {
        st.lastKnownIsHole = false;
        [st.deferredHover, st.deferredLeave] = [undefined, false];
      }
    });
  }

  handleCatcherPointerLeave(w: WcvZOrderWindow): void {
    const st = this.liveCatcher(w) ? this.catcherStates.get(w) : undefined;
    if (st && !this.canPreemptChain(st)) {
      st.deferredLeave = true;
      st.deferredHover = undefined;
    } else if (st) {
      st.lastKnownIsHole = false;
      this.syncNonHole(w, st);
    }
  }

  dispose(): void {
    if (!this.disposed) {
      for (const [w, st] of [...this.catcherStates]) {
        this.hideCatcher(w, st, { noHover: true });
      }
      this.catcherStates.clear();
      this.disposed = true;
      this.tabStates.forEach((rec) => this.clearTabInputQueues(rec));
      [...this.disposers].forEach((fn) => fn());
      this.disposers.clear();
      this.tabStates.clear();
      this.viewportOverrides.clear();
    }
  }

  private ensureCatcherHooks(win: WcvZOrderWindow) {
    if (this.disposed || !win || win.isDestroyed?.()) {
      return null;
    }
    const catcher = getHubCatcherView(win);
    const catcherContents = catcher?.webContents;
    if (!catcher || !catcherContents || catcherContents.isDestroyed?.()) {
      return null;
    }
    if (!this.trackedCatcherContents.has(catcherContents)) {
      this.trackedCatcherContents.add(catcherContents);
      const onCatcherDomReady = (): void => {
        if (
          !this.disposed &&
          !win.isDestroyed?.() &&
          getHubCatcherView(win)?.webContents === catcherContents &&
          !catcherContents.isDestroyed?.()
        ) {
          this.syncWindowCatcherCursor(win, true);
        }
      };
      catcherContents.on?.('dom-ready', onCatcherDomReady);
      let detach: () => void = noop;
      const onGone = (_ev: unknown, d?: unknown): void => {
        if (
          this.disposed ||
          win.isDestroyed?.() ||
          (typeof d === 'object' &&
            d !== null &&
            (d as { reason?: unknown }).reason === 'clean-exit')
        ) {
          return;
        }
        const st = this.catcherStates.get(win);
        if (st) {
          this.hideCatcher(win, st, { noIpc: true });
        }
        if (!getHubCatcherView(win)) {
          this.dropCatcherState(win);
          detach();
          this.reconcileWindow(win, 'catcher-exhausted');
        }
      };
      const syncLive = (): void => {
        if (
          !this.disposed &&
          getHubCatcherView(win)?.webContents === catcherContents
        ) {
          this.syncCatcher(win);
        }
      };
      const onFinish = (): void => {
        syncLive();
        void Promise.resolve()
          .then(noop)
          .then(noop)
          .then(noop)
          .then(syncLive);
      };
      catcherContents.on?.('render-process-gone', onGone);
      catcherContents.on?.('did-finish-load', onFinish);
      const catcherEv = catcherContents as unknown as EventEmitterLike;
      detach = this.onWinClosed(win, () => {
        detachEv(catcherEv, 'dom-ready', onCatcherDomReady);
        detachEv(catcherEv, 'render-process-gone', onGone);
        detachEv(catcherEv, 'did-finish-load', onFinish);
        this.trackedCatcherContents.delete(catcherContents);
        this.windowCatcherCursor.delete(win);
      });
    }
    return catcherContents;
  }

  private findActiveCursorEntry(
    win: WcvZOrderWindow,
  ): WcvZOrderEntry | undefined {
    let active: WcvZOrderEntry | undefined;
    for (const entry of this.registry.getEntriesForWindow(win)) {
      if (this.registry.isClipEntryRevealed(entry)) {
        active = entry;
      } else {
        const rec = this.tabStates.get(entry.tabKey);
        if (rec && rec.webContentsId === entry.webContentsId) {
          rec.guestCssCursor = 'default';
        }
      }
    }
    return active;
  }

  private syncWindowCatcherCursor(win: WcvZOrderWindow, force = false): void {
    const catcherContents = this.ensureCatcherHooks(win);
    if (!catcherContents) {
      return;
    }
    const active = this.findActiveCursorEntry(win);
    const activeRec = active ? this.tabStates.get(active.tabKey) : undefined;
    const desired =
      active && activeRec && activeRec.webContentsId === active.webContentsId
        ? (activeRec.guestCssCursor ?? 'default')
        : 'default';
    const prevSent = this.windowCatcherCursor.get(win) ?? 'default';
    this.windowCatcherCursor.set(win, desired);
    if (force || prevSent !== desired) {
      catcherContents.send?.(WCV_CATCHER_SET_CURSOR_CHANNEL, desired);
    }
  }

  private setUserElevationLatch(
    entry: WcvZOrderEntry | undefined,
    rec: TabStateRecord,
    latched: boolean,
  ): void {
    rec.userElevationLatch = latched;
    if (entry && getHubClipView(entry.parentWindow)) {
      this.syncTabFocusEmulation(entry, rec);
    }
  }

  revokeFocusGrant(tabKey: string): void {
    const rec = this.tabStates.get(tabKey);
    if (rec) {
      this.setUserElevationLatch(this.registry.getEntry(tabKey), rec, false);
    }
  }

  revokeWindowFocusGrants(win: WcvZOrderWindow): void {
    for (const entry of this.registry.getEntriesForWindow(win)) {
      if (entry.htmlFullscreen && entry.visible && !entry.isAgentStepActive) {
        continue;
      }
      const rec = this.tabStates.get(entry.tabKey);
      if (rec) {
        this.setUserElevationLatch(entry, rec, false);
      }
    }
  }

  /** Reconciles `tabKey`'s window and, when elevated at `z=1` with `focusBrowser`, grants and applies guest keyboard focus. */
  elevate(
    tabKey: string,
    reason: WcvZOrderReason = 'pointerdown',
    focusBrowser = true,
  ): WcvZOrderState | null {
    const entry = this.registry.getEntry(tabKey);
    if (!entry || entry.view.webContents.isDestroyed()) {
      return null;
    }
    const rec = this.ensureRecord(entry);
    this.reconcileWindow(entry.parentWindow, reason);
    if (rec.zOrder === 1 && focusBrowser) {
      this.setUserElevationLatch(entry, rec, true);
      entry.view.webContents.focus?.();
    }
    return this.getState(tabKey);
  }

  setDragOverActive(tabKey: string, _active: boolean): WcvZOrderState | null {
    const entry = this.registry.getEntry(tabKey);
    if (!entry || entry.view.webContents.isDestroyed()) {
      return null;
    }
    return this.getState(tabKey);
  }

  /** Revokes `tabKey`'s keyboard focus grant without changing z-order. */
  demote(
    tabKey: string,
    reason: WcvZOrderReason = 'hub-interaction',
  ): WcvZOrderState | null {
    const entry = this.registry.getEntry(tabKey);
    if (!entry) {
      return null;
    }
    if (
      isTransientFullscreenFocusReason(reason) &&
      entry.htmlFullscreen &&
      entry.visible &&
      !entry.isAgentStepActive
    ) {
      return this.getState(tabKey);
    }
    if (entry.htmlFullscreen) {
      this.registry.exitHtmlFullscreen?.(tabKey);
    }
    const rec = this.ensureRecord(entry);
    this.setUserElevationLatch(entry, rec, false);
    return this.getState(tabKey);
  }

  clearWindowElevationBlockers(
    win: WcvZOrderWindow,
    reason: WcvZOrderReason,
  ): void {
    const entriesSnapshot = this.registry
      .getEntriesForWindow(win)
      .map((entry) => ({
        entry,
        wasVisible: Boolean(entry.visible),
        wasAgentActive: Boolean(entry.isAgentStepActive),
      }));
    this.registry.cancelWindowShowRequests?.(win);
    this.windowPortalOpen.set(win, false);
    const catcherSt = this.catcherStates.get(win);
    if (catcherSt) {
      catcherSt.lastKnownIsHole = false;
    }
    for (const { entry, wasVisible, wasAgentActive } of entriesSnapshot) {
      if (entry.htmlFullscreen) {
        this.registry.exitHtmlFullscreen?.(entry.tabKey);
      }
      if (wasVisible && entry.visible) {
        this.registry.hide(entry.tabKey);
      }
      this.registry.setAgentStepActive(entry.tabKey, false);
      if (wasAgentActive) {
        this.resetViewportOverride(entry);
      }
      const rec = this.tabStates.get(entry.tabKey);
      if (rec) {
        rec.overlayActive = false;
        rec.humanPromptOpen = false;
        rec.userElevationLatch = false;
        if (
          wasAgentActive &&
          !wasVisible &&
          !entry.view.webContents.isDestroyed()
        ) {
          dismissNativeGuestPopups(entry.view.webContents, {
            onlyPopupControls: true,
            shouldStillDismiss: () =>
              !entry.isAgentStepActive && rec.zOrder === 0,
          });
        }
      }
    }
    this.reconcileWindow(win, reason);
  }

  setOverlayActive(tabKey: string, active: boolean): WcvZOrderState | null {
    const entry = this.registry.getEntry(tabKey);
    if (!entry) {
      return null;
    }
    const rec = this.ensureRecord(entry);
    rec.overlayActive = Boolean(active);
    this.reconcileWindow(
      entry.parentWindow,
      rec.overlayActive ? 'overlay-active' : 'overlay-inactive',
    );
    return this.getState(tabKey);
  }

  setHumanPromptOpen(tabKey: string, open: boolean): WcvZOrderState | null {
    const entry = this.registry.getEntry(tabKey);
    if (!entry) {
      return null;
    }
    const rec = this.ensureRecord(entry);
    rec.humanPromptOpen = Boolean(open);
    this.reconcileWindow(
      entry.parentWindow,
      rec.humanPromptOpen ? 'human-prompt-open' : 'human-prompt-closed',
    );
    return this.getState(tabKey);
  }

  setPortalOverlayOpen(win: WcvZOrderWindow, open: boolean): void {
    const isOpen = Boolean(open);
    this.windowPortalOpen.set(win, isOpen);
    this.reconcileWindow(
      win,
      isOpen ? 'portal-overlay-open' : 'portal-overlay-closed',
    );
  }

  setAgentControlling(tabKey: string, active: boolean): WcvZOrderState | null {
    const isActive = Boolean(active);
    this.registry.setAgentStepActive(tabKey, isActive);
    const entry = this.registry.getEntry(tabKey);
    if (!entry) {
      return null;
    }
    const rec = this.ensureRecord(entry);
    if (isActive) {
      this.cancelTabActiveDrags(entry);
    }
    this.reconcileWindow(
      entry.parentWindow,
      isActive ? 'agent-controlling' : 'agent-controlling-ended',
    );
    if (isActive) {
      void this.syncDragInterception(entry, rec);
      void this.ensureCdpAttached(entry);
    } else {
      void this.syncFocusEmulation(entry, rec);
      void this.syncDragInterception(entry, rec);
      this.syncMenuShortcutSuppression(entry, rec);
      this.resetViewportOverride(entry);
      dismissNativeGuestPopups(entry.view.webContents, {
        onlyPopupControls: true,
        shouldStillDismiss: () => !entry.isAgentStepActive && rec.zOrder === 0,
      });
    }
    void this.dialogController.syncFallbackContext?.(entry.view.webContents);
    return this.getState(tabKey);
  }

  private async ensureCdpAttached(entry: WcvZOrderEntry): Promise<void> {
    const wc = entry.view.webContents;
    if (!wc || wc.isDestroyed?.()) {
      return;
    }
    const rec = this.ensureRecord(entry);
    const dbg = getCdpDebugger(wc);
    if (!dbg) {
      return;
    }
    if (typeof wc.getURL === 'function' && (wc.getURL() ?? '').trim() === '') {
      return;
    }
    if (!dbg.isAttached?.()) {
      try {
        dbg.attach?.('1.3');
      } catch {
        // Already attached.
      }
    }
    if (!rec.cdpAttached) {
      if (rec.cdpAttachInFlight) {
        await rec.cdpAttachInFlight;
      } else if (typeof dbg.sendCommand !== 'function') {
        rec.cdpAttached = true;
      } else {
        const epoch = rec.cdpAttachEpoch ?? 0;
        let timer: ReturnType<typeof setTimeout> | undefined;
        const sendCommand = dbg.sendCommand;
        const inFlight = (async () => {
          try {
            const sendFailed = Symbol('cdp-page-enable-failed');
            const timedOut = Symbol('cdp-page-enable-timeout');
            const result = await Promise.race([
              Promise.resolve(sendCommand.call(dbg, 'Page.enable')).catch(
                () => sendFailed,
              ),
              new Promise<typeof timedOut>((resolve) => {
                timer = setTimeout(
                  () => resolve(timedOut),
                  this.pageEnableTimeoutMs,
                );
              }),
            ]);
            if (
              result !== sendFailed &&
              result !== timedOut &&
              (rec.cdpAttachEpoch ?? 0) === epoch &&
              !wc.isDestroyed?.()
            ) {
              rec.cdpAttached = true;
            }
          } finally {
            if (timer !== undefined) {
              clearTimeout(timer);
            }
          }
        })();
        const tracked: Promise<void> = inFlight.finally(() => {
          if (rec.cdpAttachInFlight === tracked) {
            rec.cdpAttachInFlight = undefined;
          }
        });
        rec.cdpAttachInFlight = tracked;
        await tracked;
      }
    }
    await this.syncFileChooserInterception(entry, rec);
    await this.syncFocusEmulation(entry, rec);
    await this.syncDragInterception(entry, rec);
  }

  private async syncDragInterception(
    entry: WcvZOrderEntry,
    rec: TabStateRecord,
  ): Promise<void> {
    const want =
      Boolean(getHubCatcherView(entry.parentWindow)) &&
      !entry.isAgentStepActive;
    if (want) {
      this.ensureCatcherHooks(entry.parentWindow);
    }
    if (
      !rec.cdpAttached ||
      entry.view.webContents.isDestroyed() ||
      this.isCrashedClipEntry(entry) ||
      (!want && !rec.dragInterceptionEnabled) ||
      rec.dragInterceptionEnabled === want
    ) {
      return;
    }
    const [prev, seq] = [
      rec.dragInterceptionEnabled,
      (rec.dragInterceptionSeq ?? 0) + 1,
    ];
    [rec.dragInterceptionSeq, rec.dragInterceptionEnabled] = [seq, want];
    try {
      await getCdpDebugger(entry.view.webContents)?.sendCommand?.(
        'Input.setInterceptDrags',
        { enabled: want },
      );
    } catch {
      if (
        rec.webContentsId === entry.webContentsId &&
        rec.cdpAttached &&
        rec.dragInterceptionSeq === seq
      ) {
        rec.dragInterceptionEnabled = prev;
      }
    }
  }

  private shouldSuppressNativePopups(
    entry: WcvZOrderEntry,
    rec: TabStateRecord,
  ): boolean {
    return !entry.visible || rec.zOrder !== 1;
  }

  private async syncFileChooserInterception(
    entry: WcvZOrderEntry,
    rec: TabStateRecord,
  ): Promise<void> {
    if (!rec.cdpAttached || entry.view.webContents.isDestroyed()) {
      return;
    }
    await this.fileChooserController.syncInterception(entry.view.webContents);
  }

  private shouldRetainUserFocusEmulation(
    entry: WcvZOrderEntry,
    rec: TabStateRecord,
  ): boolean {
    return Boolean(
      getHubClipView(entry.parentWindow) &&
        rec.userElevationLatch &&
        !this.isElevationBlocked(entry, rec),
    );
  }

  private syncTabFocusEmulation(
    entry: WcvZOrderEntry,
    rec: TabStateRecord,
  ): void {
    if (entry.view.webContents.isDestroyed() || this.isCrashedClipEntry(entry)) {
      return;
    }
    if (!rec.cdpAttached) {
      if (this.shouldRetainUserFocusEmulation(entry, rec)) {
        void this.ensureCdpAttached(entry);
      }
      return;
    }
    void this.syncFocusEmulation(entry, rec);
  }

  /**
   * Synchronizes Chromium CDP focus emulation (`Emulation.setFocusEmulationEnabled`)
   * on `entry` so background agent input, cold unshown clip tabs, and active
   * user-focused clip tabs maintain guest focus state independent of native OS
   * view focus.
   */
  private async syncFocusEmulation(
    entry: WcvZOrderEntry,
    rec: TabStateRecord,
  ): Promise<void> {
    if (
      !rec.cdpAttached ||
      entry.view.webContents.isDestroyed() ||
      this.isCrashedClipEntry(entry)
    ) {
      return;
    }
    const wantEnabled = Boolean(
      entry.isAgentStepActive ||
        (getHubClipView(entry.parentWindow) && !entry.shownInVisibleClip) ||
        this.shouldRetainUserFocusEmulation(entry, rec),
    );
    if (!wantEnabled && !rec.focusEmulationEnabled) {
      return;
    }
    if (rec.focusEmulationEnabled === wantEnabled) {
      return;
    }
    const prevEnabled = rec.focusEmulationEnabled;
    const seq = (rec.focusEmulationSeq ?? 0) + 1;
    rec.focusEmulationSeq = seq;
    rec.focusEmulationEnabled = wantEnabled;
    try {
      await getCdpDebugger(entry.view.webContents)?.sendCommand?.(
        'Emulation.setFocusEmulationEnabled',
        { enabled: wantEnabled },
      );
    } catch {
      if (
        rec.webContentsId === entry.webContentsId &&
        rec.cdpAttached &&
        rec.focusEmulationSeq === seq
      ) {
        rec.focusEmulationEnabled = prevEnabled;
      }
    }
  }

  /**
   * Synchronizes non-elevated popup protections for `entry`:
   * - While hidden or at `z=0`, injects user-origin CSS rendering `<select>`
   *   menus in-page (`appearance: base-select`) and hiding calendar picker
   *   indicators, enables the isolated-world color/file activation guard, and
   *   intercepts file-chooser dialogs over CDP so background tabs never open
   *   OS-level popups over the active tab or Hub UI.
   * - While visible and elevated at `z=1`, removes those overrides so direct
   *   human interaction uses native OS menus and pickers.
   */
  private async syncPopupSuppression(
    entry: WcvZOrderEntry,
    rec: TabStateRecord,
  ): Promise<void> {
    const wc = entry.view.webContents;
    if (!wc || wc.isDestroyed?.() || this.isCrashedClipEntry(entry)) {
      return;
    }
    const suppress = this.shouldSuppressNativePopups(entry, rec);
    if (suppress) {
      if (
        !rec.popupSuppressionCssKey &&
        !rec.popupSuppressionCssInFlight &&
        typeof wc.insertCSS === 'function'
      ) {
        const gen = (rec.popupCssGeneration ?? 0) + 1;
        rec.popupCssGeneration = gen;
        const tracked: Promise<void> = Promise.resolve(
          wc.insertCSS(NON_ELEVATED_POPUP_SUPPRESSION_CSS, {
            cssOrigin: 'user',
          }),
        )
          .then((key) => {
            if (typeof key !== 'string' || !key) {
              return;
            }
            if (
              wc.isDestroyed?.() ||
              rec.popupCssGeneration !== gen ||
              !this.shouldSuppressNativePopups(entry, rec)
            ) {
              void Promise.resolve(wc.removeInsertedCSS?.(key)).catch(
                () => undefined,
              );
              return;
            }
            rec.popupSuppressionCssKey = key;
          })
          .catch(() => undefined)
          .finally(() => {
            if (rec.popupSuppressionCssInFlight === tracked) {
              rec.popupSuppressionCssInFlight = undefined;
            }
          });
        rec.popupSuppressionCssInFlight = tracked;
      }
    } else {
      rec.popupCssGeneration = (rec.popupCssGeneration ?? 0) + 1;
      const key = rec.popupSuppressionCssKey;
      rec.popupSuppressionCssKey = undefined;
      rec.popupSuppressionCssInFlight = undefined;
      if (key && typeof wc.removeInsertedCSS === 'function') {
        void Promise.resolve(wc.removeInsertedCSS(key)).catch(() => undefined);
      }
    }

    const hasCommittedUrl =
      typeof wc.getURL !== 'function' || (wc.getURL() ?? '').trim() !== '';
    const isLoadingMain =
      typeof wc.isLoadingMainFrame === 'function' &&
      Boolean(wc.isLoadingMainFrame());
    if (!hasCommittedUrl) {
      // No main-frame document has committed yet; `onDocumentReset` will apply
      // the isolated-world popup guard once navigation commits.
    } else if (isLoadingMain) {
      // Electron's `WebContents.executeJavaScriptInIsolatedWorld` defers
      // execution until `did-stop-loading` anyway and registers a separate
      // `did-stop-loading` listener per call. Coalesce main-frame guard updates
      // during an active load into a single post-load sync using the latest
      // desired `suppress` state while still updating already-loaded subframes.
      void Promise.all(this.syncSubframePopupSuppression(wc, suppress));
      if (!rec.isolatedGuardFlushPending) {
        rec.isolatedGuardFlushPending = true;
        const targetWcId = rec.webContentsId;
        void waitForWebContentsCanExecuteJs(wc)
          .then(() => {
            if (rec.webContentsId !== targetWcId) {
              return;
            }
            rec.isolatedGuardFlushPending = false;
            if (!wc.isDestroyed?.() && this.tabStates.get(entry.tabKey) === rec) {
              void this.syncPopupSuppression(entry, rec);
            }
          })
          .catch(() => {
            if (rec.webContentsId === targetWcId) {
              rec.isolatedGuardFlushPending = false;
            }
          });
      }
    } else if (rec.isolatedGuardSuppressed !== suppress) {
      rec.isolatedGuardSuppressed = suppress;
      const script = buildIsolatedWorldPopupGuardScript(
        suppress,
        this.fileChooserController.isFileChooserAllowed(entry.webContentsId),
      );
      const tasks: Array<Promise<unknown>> = [];
      if (typeof wc.executeJavaScriptInIsolatedWorld === 'function') {
        tasks.push(
          Promise.resolve(
            executeJavaScriptInIsolatedWorldWhenReady(
              wc,
              POPUP_GUARD_ISOLATED_WORLD_ID,
              [{ code: script }],
            ),
          ).catch(() => undefined),
        );
      }
      tasks.push(...this.syncSubframePopupSuppression(wc, suppress));
      if (tasks.length > 0) {
        const tracked: Promise<void> = Promise.all(tasks)
          .then(() => undefined)
          .finally(() => {
            if (rec.isolatedGuardInFlight === tracked) {
              rec.isolatedGuardInFlight = undefined;
            }
          });
        rec.isolatedGuardInFlight = tracked;
      }
    }

    await Promise.all([
      rec.popupSuppressionCssInFlight,
      rec.isolatedGuardInFlight,
      this.syncFileChooserInterception(entry, rec),
    ]);
  }

  private syncSubframePopupSuppression(
    wc: WcvZOrderWebContents,
    suppress: boolean,
  ): Array<Promise<void>> {
    const tasks: Array<Promise<void>> = [];
    const frames = Array.isArray(wc.mainFrame?.framesInSubtree)
      ? wc.mainFrame.framesInSubtree
      : null;
    if (!frames) {
      return tasks;
    }
    // Carry the file chooser allow flag so a suppress toggle does not reset it
    // in same-origin subframes.
    const script = buildIsolatedWorldPopupGuardScript(
      suppress,
      this.fileChooserController.isFileChooserAllowed(wc.id),
    );
    for (const frame of frames) {
      if (
        frame &&
        frame !== wc.mainFrame &&
        !frame.isDestroyed?.() &&
        typeof frame.executeJavaScriptInIsolatedWorld === 'function'
      ) {
        tasks.push(
          Promise.resolve(
            frame.executeJavaScriptInIsolatedWorld(
              POPUP_GUARD_ISOLATED_WORLD_ID,
              [{ code: script }],
            ),
          )
            .then(() => undefined)
            .catch(() => undefined),
        );
      }
    }
    return tasks;
  }

  private async ensurePopupSuppressionBeforeInput(
    tabKey: string,
  ): Promise<void> {
    const entry = this.registry.getEntry(tabKey);
    if (!entry || entry.view.webContents.isDestroyed()) {
      return;
    }
    const rec = this.ensureRecord(entry);
    if (this.shouldSuppressNativePopups(entry, rec)) {
      await this.syncPopupSuppression(entry, rec);
    }
  }

  async sendCdp(
    tabKey: string,
    method: string,
    params?: Record<string, unknown>,
  ): Promise<unknown> {
    const entry = this.registry.getEntry(tabKey);
    if (!entry || entry.view.webContents.isDestroyed()) {
      return undefined;
    }
    const wc = entry.view.webContents;
    if (typeof wc.getURL === 'function' && (wc.getURL() ?? '').trim() === '') {
      return undefined;
    }
    await this.ensureCdpAttached(entry);
    if (wc.isDestroyed?.()) {
      return undefined;
    }
    if (
      getHubClipView(entry.parentWindow) ||
      this.viewportOverrides.has(tabKey)
    ) {
      await this.applyViewportOverride(entry);
      if (wc.isDestroyed?.()) {
        return undefined;
      }
    }
    return await getCdpDebugger(wc)?.sendCommand?.(method, params);
  }

  private isClipTabOnProtectedIdp(entry: WcvZOrderEntry): boolean {
    const wc = entry.view.webContents;
    if (!wc || wc.isDestroyed?.()) {
      return false;
    }
    const rec = this.tabStates.get(entry.tabKey);
    const activeRec =
      rec && rec.webContentsId === entry.webContentsId ? rec : undefined;
    if (
      activeRec?.pendingMainFrameUrl &&
      isProtectedViewportOverrideUrl(activeRec.pendingMainFrameUrl)
    ) {
      return true;
    }
    // A non-protected pending URL intentionally falls through to the last
    // committed URL so overrides stay blocked while an IdP document remains
    // live in Blink (including if the pending navigation aborts or redirects back).
    const rawWcUrl = (
      (typeof wc.getURL === 'function' ? wc.getURL() : '') || ''
    ).trim();
    const usableWcUrl =
      !entry.lastLoadError &&
      rawWcUrl &&
      rawWcUrl !== 'about:blank' &&
      !rawWcUrl.startsWith('chrome-error://')
        ? rawWcUrl
        : '';
    const effectiveUrl =
      activeRec?.committedMainFrameUrl ??
      (entry.lastUrl || usableWcUrl || rawWcUrl);
    return isProtectedViewportOverrideUrl(effectiveUrl);
  }

  private isProtectedTabUrl(
    entry: WcvZOrderEntry,
    fallbackUrl: string,
  ): boolean {
    return getHubClipView(entry.parentWindow)
      ? this.isClipTabOnProtectedIdp(entry)
      : isProtectedViewportOverrideUrl(fallbackUrl);
  }

  private resolveClipFallbackSize(entry: WcvZOrderEntry): WcvSize | null {
    if (
      !getHubClipView(entry.parentWindow) ||
      entry.shownInVisibleClip ||
      entry.view.webContents.isDestroyed() ||
      this.isClipTabOnProtectedIdp(entry)
    ) {
      return null;
    }
    const nonEmpty = (r?: Rectangle | WcvSize | null) =>
      r && r.width > 0 && r.height > 0 ? r : undefined;
    const src =
      nonEmpty(entry.appliedBoundsDip) ??
      nonEmpty(entry.boundsDip) ??
      entry.lastNonZeroSize ??
      DEFAULT_HIDDEN_WCV_BOUNDS;
    return { width: src.width, height: src.height };
  }

  /** Clears any caller-supplied viewport override on `entry`; a no-op when none is set. */
  private resetViewportOverride(entry: WcvZOrderEntry): void {
    if (
      this.viewportOverrides.get(entry.tabKey)?.webContentsId !==
      entry.webContentsId
    ) {
      return;
    }
    void this.applyViewportOverride(entry, { caller: null }).catch(
      () => undefined,
    );
  }

  private clearClipProtectedIdpViewportSync(entry: WcvZOrderEntry): void {
    const wc = entry.view.webContents;
    if (
      !getHubClipView(entry.parentWindow) ||
      !wc ||
      wc.isDestroyed?.() ||
      this.isCrashedClipEntry(entry)
    ) {
      return;
    }
    const vpRec = this.viewportOverrides.get(entry.tabKey);
    if (!vpRec || vpRec.webContentsId !== entry.webContentsId) {
      return;
    }
    vpRec.callerDesired = null;
    if (!vpRec.appliedKey && !vpRec.pipeDirty) {
      return;
    }
    const dbg = getCdpDebugger(wc);
    if (!dbg?.isAttached?.() || typeof dbg.sendCommand !== 'function') {
      return;
    }
    vpRec.syncClearSeq = (vpRec.syncClearSeq ?? 0) + 1;
    vpRec.appliedKey = undefined;
    vpRec.pipeDirty = false;
    try {
      const ok = (): boolean =>
        !this.disposed && !wc.isDestroyed?.() && !this.isCrashedClipEntry(entry);
      const cmd = dbg.sendCommand('Emulation.clearDeviceMetricsOverride', {});
      void Promise.resolve(cmd).then(
        () => void (ok() && wc.invalidate?.()),
        noop,
      );
    } catch {
      // Debugger detached concurrently.
    }
  }

  private async applyViewportOverride(
    entry: WcvZOrderEntry,
    update?: { caller?: WcvSize | null },
  ): Promise<void> {
    const wc = entry.view.webContents;
    if (!wc || wc.isDestroyed?.()) {
      return;
    }
    const hasClip = Boolean(getHubClipView(entry.parentWindow));
    if (this.isCrashedClipEntry(entry)) {
      const vp =
        update && 'caller' in update
          ? this.viewportOverrides.get(entry.tabKey)
          : undefined;
      if (vp && vp.webContentsId === entry.webContentsId) {
        vp.callerDesired = update?.caller ?? null;
        if (
          !vp.callerDesired &&
          !vp.fallbackDesired &&
          !vp.appliedKey &&
          !vp.pipeDirty
        ) {
          this.viewportOverrides.delete(entry.tabKey);
        }
      }
      return;
    }
    const nextFallback = this.resolveClipFallbackSize(entry);
    let vpRec = this.viewportOverrides.get(entry.tabKey);
    const nextCaller =
      update && 'caller' in update
        ? (update.caller ?? null)
        : vpRec && vpRec.webContentsId === entry.webContentsId
          ? vpRec.callerDesired
          : null;
    if (!vpRec || vpRec.webContentsId !== entry.webContentsId) {
      if (!nextCaller && !nextFallback) {
        this.viewportOverrides.delete(entry.tabKey);
        return;
      }
      vpRec = {
        webContentsId: entry.webContentsId,
        callerDesired: nextCaller,
        fallbackDesired: nextFallback,
      };
      this.viewportOverrides.set(entry.tabKey, vpRec);
    } else {
      vpRec.callerDesired = nextCaller;
      vpRec.fallbackDesired = nextFallback;
      if (!vpRec.inFlight) {
        const eff = vpRec.callerDesired ?? vpRec.fallbackDesired;
        if (!eff && !vpRec.appliedKey && (!hasClip || !vpRec.pipeDirty)) {
          this.viewportOverrides.delete(entry.tabKey);
          return;
        }
        if (eff && vpRec.appliedKey === `${eff.width}x${eff.height}`) {
          return;
        }
      }
    }

    const prev = vpRec.inFlight ?? Promise.resolve();
    const currentRec = vpRec;
    const tracked: Promise<void> = prev
      .catch(() => undefined)
      .then(async () => {
        if (
          wc.isDestroyed?.() ||
          this.viewportOverrides.get(entry.tabKey) !== currentRec ||
          this.isCrashedClipEntry(entry)
        ) {
          return;
        }
        currentRec.fallbackDesired = this.resolveClipFallbackSize(entry);
        const initialEffective =
          currentRec.callerDesired ?? currentRec.fallbackDesired;
        if (
          typeof wc.getURL === 'function' &&
          (wc.getURL() ?? '').trim() === ''
        ) {
          currentRec.appliedKey = undefined;
          if (hasClip) {
            currentRec.pipeDirty = false;
          }
          if (!initialEffective) {
            this.viewportOverrides.delete(entry.tabKey);
          }
          return;
        }
        if (
          !initialEffective &&
          !currentRec.appliedKey &&
          (!hasClip || !currentRec.pipeDirty)
        ) {
          this.viewportOverrides.delete(entry.tabKey);
          return;
        }
        await this.ensureCdpAttached(entry);
        if (this.isCrashedClipEntry(entry)) {
          return;
        }
        currentRec.fallbackDesired = this.resolveClipFallbackSize(entry);
        const dbg = getCdpDebugger(wc);
        const sendCommand = dbg?.sendCommand;
        if (
          wc.isDestroyed?.() ||
          this.viewportOverrides.get(entry.tabKey) !== currentRec ||
          !dbg ||
          typeof sendCommand !== 'function'
        ) {
          if (
            !(currentRec.callerDesired ?? currentRec.fallbackDesired) &&
            (!hasClip || (!currentRec.appliedKey && !currentRec.pipeDirty))
          ) {
            this.viewportOverrides.delete(entry.tabKey);
          }
          return;
        }
        const liveUrl =
          (typeof wc.getURL === 'function' ? wc.getURL() : '') ||
          entry.lastUrl ||
          '';
        if (currentRec.callerDesired && this.isProtectedTabUrl(entry, liveUrl)) {
          currentRec.callerDesired = null;
        }
        const currentEffective =
          currentRec.callerDesired ?? currentRec.fallbackDesired;
        if (!currentEffective) {
          if (currentRec.appliedKey || (hasClip && currentRec.pipeDirty)) {
            await sendCommand.call(
              dbg,
              'Emulation.clearDeviceMetricsOverride',
              {},
            );
            currentRec.appliedKey = undefined;
            if (hasClip) {
              currentRec.pipeDirty = false;
            }
            if (!wc.isDestroyed?.() && typeof wc.invalidate === 'function') {
              wc.invalidate();
              await new Promise((resolve) =>
                setTimeout(resolve, VIEWPORT_INVALIDATE_SETTLE_MS),
              );
            }
          }
          if (
            !(currentRec.callerDesired ?? currentRec.fallbackDesired) &&
            (!hasClip || (!currentRec.appliedKey && !currentRec.pipeDirty))
          ) {
            this.viewportOverrides.delete(entry.tabKey);
          }
          return;
        }
        const targetKey = `${currentEffective.width}x${currentEffective.height}`;
        if (currentRec.appliedKey === targetKey) {
          return;
        }
        const clearSeqBeforeSet = currentRec.syncClearSeq ?? 0;
        if (hasClip) {
          currentRec.pipeDirty = true;
        }
        try {
          await sendCommand.call(dbg, 'Emulation.setDeviceMetricsOverride', {
            width: currentEffective.width,
            height: currentEffective.height,
            deviceScaleFactor: 0,
            mobile: false,
          });
        } catch (err) {
          if (
            hasClip &&
            (currentRec.syncClearSeq ?? 0) === clearSeqBeforeSet &&
            !currentRec.appliedKey
          ) {
            currentRec.pipeDirty = false;
          }
          throw err;
        }
        if (
          hasClip &&
          (currentRec.syncClearSeq ?? 0) !== clearSeqBeforeSet
        ) {
          currentRec.fallbackDesired = this.resolveClipFallbackSize(entry);
          if (
            !(currentRec.callerDesired ?? currentRec.fallbackDesired) &&
            !currentRec.appliedKey &&
            !currentRec.pipeDirty
          ) {
            this.viewportOverrides.delete(entry.tabKey);
          }
          return;
        }
        currentRec.appliedKey = targetKey;
        if (hasClip) {
          currentRec.pipeDirty = false;
        }
        currentRec.fallbackDesired = this.resolveClipFallbackSize(entry);
        if (!(currentRec.callerDesired ?? currentRec.fallbackDesired)) {
          const clearSeqBeforePostClear = currentRec.syncClearSeq ?? 0;
          await sendCommand.call(
            dbg,
            'Emulation.clearDeviceMetricsOverride',
            {},
          );
          if (
            !hasClip ||
            (currentRec.syncClearSeq ?? 0) === clearSeqBeforePostClear
          ) {
            currentRec.appliedKey = undefined;
            if (hasClip) {
              currentRec.pipeDirty = false;
            }
          }
          if (
            !hasClip ||
            (!(currentRec.callerDesired ?? currentRec.fallbackDesired) &&
              !currentRec.appliedKey &&
              !currentRec.pipeDirty)
          ) {
            this.viewportOverrides.delete(entry.tabKey);
          }
        }
        if (!wc.isDestroyed?.() && typeof wc.invalidate === 'function') {
          wc.invalidate();
          await new Promise((resolve) =>
            setTimeout(resolve, VIEWPORT_INVALIDATE_SETTLE_MS),
          );
        }
      })
      .finally(() => {
        if (currentRec && currentRec.inFlight === tracked) {
          currentRec.inFlight = undefined;
        }
      });
    vpRec.inFlight = tracked;
    await tracked;
  }

  /**
   * Overrides or clears (`null`) the emulated CSS viewport dimensions for a
   * `WebContentsView` tab via `Emulation.setDeviceMetricsOverride`, refusing and
   * clearing overrides on protected Identity Provider origins.
   */
  async setViewportSize(tabKey: string, size: WcvSize | null): Promise<boolean> {
    const entry = this.registry.getEntry(tabKey);
    if (!entry || entry.view.webContents.isDestroyed()) {
      return false;
    }
    this.ensureRecord(entry);
    const wc = entry.view.webContents;
    if (size === null) {
      await this.applyViewportOverride(entry, { caller: null });
      return true;
    }
    if (
      !size ||
      typeof size !== 'object' ||
      typeof size.width !== 'number' ||
      typeof size.height !== 'number' ||
      !Number.isFinite(size.width) ||
      !Number.isFinite(size.height) ||
      Math.round(size.width) < MIN_VIEWPORT_DIMENSION ||
      Math.round(size.height) < MIN_VIEWPORT_DIMENSION
    ) {
      return false;
    }
    const currentUrl =
      (typeof wc.getURL === 'function' ? wc.getURL() : '') ||
      entry.lastUrl ||
      '';
    if (this.isProtectedTabUrl(entry, currentUrl)) {
      await this.applyViewportOverride(entry, { caller: null });
      return false;
    }
    const normalized: WcvSize = {
      width: Math.min(
        MAX_VIEWPORT_DIMENSION,
        Math.max(MIN_VIEWPORT_DIMENSION, Math.round(size.width)),
      ),
      height: Math.min(
        MAX_VIEWPORT_DIMENSION,
        Math.max(MIN_VIEWPORT_DIMENSION, Math.round(size.height)),
      ),
    };
    await this.applyViewportOverride(entry, { caller: normalized });
    return true;
  }

  private clampMousePointToViewport(
    entry: WcvZOrderEntry | undefined,
    rawX: number,
    rawY: number,
  ): DipPoint {
    const vpRec = entry ? this.viewportOverrides.get(entry.tabKey) : undefined;
    const activeOverride =
      vpRec && vpRec.webContentsId === entry?.webContentsId
        ? vpRec.callerDesired
        : null;
    const applied =
      entry && getHubClipView(entry.parentWindow)
        ? entry.appliedBoundsDip
        : undefined;
    const liveBounds =
      activeOverride ??
      (applied && applied.width > 0 && applied.height > 0
        ? applied
        : undefined) ??
      (entry?.boundsDip &&
      entry.boundsDip.width > 0 &&
      entry.boundsDip.height > 0
        ? entry.boundsDip
        : entry?.lastNonZeroSize);
    const width = liveBounds?.width ?? 0;
    const height = liveBounds?.height ?? 0;
    const roundCoord = (v: number): number =>
      typeof v === 'number' && Number.isFinite(v) ? Math.round(v) : 0;
    const rx = roundCoord(rawX);
    const ry = roundCoord(rawY);
    return {
      x: width > 0 ? Math.max(0, Math.min(rx, width - 1)) : Math.max(0, rx),
      y: height > 0 ? Math.max(0, Math.min(ry, height - 1)) : Math.max(0, ry),
    };
  }

  async dispatchMouseEvent(
    tabKey: string,
    params: WcvCdpMouseParams,
    options?: WcvDispatchMouseOptions,
  ): Promise<void> {
    if (params.type === 'mousePressed') {
      await this.ensurePopupSuppressionBeforeInput(tabKey);
    }
    const entry = this.registry.getEntry(tabKey);
    if (!entry || entry.view.webContents.isDestroyed()) {
      return;
    }
    const roundUnclampedCoord = (v: number): number =>
      typeof v === 'number' && Number.isFinite(v) ? Math.round(v) || 0 : 0;
    const allowOutOfBounds =
      Boolean(options?.allowOutOfBounds) &&
      (params.type === 'mouseMoved' || params.type === 'mouseReleased');
    const { x, y } = allowOutOfBounds
      ? {
          x: roundUnclampedCoord(params.x),
          y: roundUnclampedCoord(params.y),
        }
      : this.clampMousePointToViewport(entry, params.x, params.y);
    const rec = this.ensureRecord(entry);
    const wcId = entry.webContentsId;
    rec.cdpMouseInFlight++;
    try {
      await this.sendCdp(tabKey, 'Input.dispatchMouseEvent', {
        button: 'none',
        clickCount: 0,
        ...params,
        x,
        y,
      });
    } finally {
      if (rec.webContentsId === wcId && rec.cdpMouseInFlight > 0) {
        rec.cdpMouseInFlight--;
      }
    }
  }

  async dispatchDragEvent(
    tabKey: string,
    params: WcvCdpDragParams,
  ): Promise<void> {
    const entry = this.registry.getEntry(tabKey);
    if (!entry || entry.crashed || entry.view.webContents.isDestroyed()) {
      return;
    }
    const { x, y } = this.clampMousePointToViewport(entry, params.x, params.y);
    const [rec, wcId] = [this.ensureRecord(entry), entry.webContentsId];
    rec.cdpMouseInFlight++;
    try {
      const modifiers = toMods(params.modifiers);
      const p: Record<string, unknown> = {
        type: params.type,
        x,
        y,
        data: params.data,
        modifiers,
      };
      await this.sendCdp(tabKey, 'Input.dispatchDragEvent', p);
    } finally {
      if (rec.webContentsId === wcId && rec.cdpMouseInFlight > 0) {
        rec.cdpMouseInFlight--;
      }
    }
  }

  synthesizePinchGesture(
    tabKey: string,
    params: WcvPinchGestureParams,
  ): Promise<void> {
    if (
      this.disposed ||
      !params ||
      typeof params !== 'object' ||
      typeof params.scaleFactor !== 'number' ||
      !Number.isFinite(params.scaleFactor) ||
      params.scaleFactor <= 0
    ) {
      return Promise.resolve();
    }
    let scaleFactor = Math.max(
      MIN_PINCH_SCALE_FACTOR,
      Math.min(MAX_PINCH_SCALE_FACTOR, params.scaleFactor),
    );
    if (scaleFactor === 1) {
      return Promise.resolve();
    }
    const entry = this.registry.getEntry(tabKey);
    const wc = entry?.view.webContents;
    if (
      !entry ||
      entry.crashed ||
      !wc ||
      wc.isDestroyed?.() ||
      wc.isCrashed?.() ||
      (typeof wc.getURL === 'function' && (wc.getURL() ?? '').trim() === '')
    ) {
      return Promise.resolve();
    }
    const rec = this.ensureRecord(entry);
    const wcId = entry.webContentsId;
    const clampScale = (s: number): number =>
      Math.max(MIN_PINCH_SCALE_FACTOR, Math.min(MAX_PINCH_SCALE_FACTOR, s));
    if (rec.pinchInFlight) {
      const next = rec.queuedPinch
        ? clampScale(rec.queuedPinch.scaleFactor * scaleFactor)
        : scaleFactor;
      rec.queuedPinch = { x: params.x, y: params.y, scaleFactor: next };
      return rec.pinchInFlight;
    }
    let [pt, zoomGen, timedOut]: [DipPoint, number, boolean] = [
      { x: params.x, y: params.y },
      rec.visualZoomGen ?? 0,
      false,
    ];
    let timer: ReturnType<typeof setTimeout> | undefined;
    const hasEmptyUrl = (): boolean =>
      typeof wc.getURL === 'function' && !(wc.getURL() ?? '').trim();
    const sameTab = (): boolean =>
      this.tabStates.get(tabKey) === rec &&
      this.registry.getEntry(tabKey) === entry &&
      rec.webContentsId === wcId;
    const isDead = (): boolean =>
      Boolean(entry.crashed || wc.isDestroyed?.() || wc.isCrashed?.());
    const isTabAlive = (): boolean =>
      !this.disposed && sameTab() && !isDead() && !hasEmptyUrl();
    const isLive = (): boolean =>
      !timedOut && isTabAlive() && (rec.visualZoomGen ?? 0) === zoomGen;
    const runPinch = async (): Promise<void> => {
      if (
        !rec.visualZoomUnlocked &&
        typeof wc.setVisualZoomLevelLimits === 'function'
      ) {
        let limitsApplied = false;
        try {
          await Promise.resolve(wc.setVisualZoomLevelLimits(1, 4));
          limitsApplied = true;
        } catch {
          limitsApplied = false;
        }
        if (limitsApplied && isLive()) {
          rec.visualZoomUnlocked = true;
        }
      }
      if (!isLive()) {
        return;
      }
      const { x, y } = this.clampMousePointToViewport(entry, pt.x, pt.y);
      const dispatchWithLayoutMetrics = async (): Promise<void> => {
        const metrics = (await this.sendCdp(
          tabKey,
          'Page.getLayoutMetrics',
        ).catch(() => undefined)) as PageLayoutMetricsResult | undefined;
        if (!isLive()) {
          return;
        }
        const rawScale =
          metrics?.cssVisualViewport?.scale ??
          metrics?.visualViewport?.scale ??
          1;
        const pageScale =
          typeof rawScale === 'number' &&
          Number.isFinite(rawScale) &&
          rawScale > 1
            ? rawScale
            : 1;
        let anchorX = pageScale > 1 ? Math.max(0, Math.floor(x / pageScale)) : x;
        let anchorY = pageScale > 1 ? Math.max(0, Math.floor(y / pageScale)) : y;
        if (pageScale > 1) {
          const cssW = metrics?.cssVisualViewport?.clientWidth;
          const cssH = metrics?.cssVisualViewport?.clientHeight;
          if (typeof cssW === 'number' && Number.isFinite(cssW) && cssW >= 1) {
            anchorX = Math.min(anchorX, Math.floor(cssW) - 1);
          }
          if (typeof cssH === 'number' && Number.isFinite(cssH) && cssH >= 1) {
            anchorY = Math.min(anchorY, Math.floor(cssH) - 1);
          }
        }
        await this.sendCdp(tabKey, 'Input.synthesizePinchGesture', {
          x: anchorX,
          y: anchorY,
          scaleFactor,
          relativeSpeed: PINCH_RELATIVE_SPEED_PX_PER_S,
        }).catch(() => undefined);
        if (isLive()) {
          rec.pinchZoomed = !(pageScale <= 1.001 && scaleFactor <= 1);
        }
      };
      if (rec.pinchZoomed) {
        await dispatchWithLayoutMetrics();
        return;
      }
      try {
        await this.sendCdp(tabKey, 'Input.synthesizePinchGesture', {
          x,
          y,
          scaleFactor,
          relativeSpeed: PINCH_RELATIVE_SPEED_PX_PER_S,
        });
        if (isLive() && scaleFactor > 1) {
          rec.pinchZoomed = true;
        }
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err);
        if (isLive() && /position out of bounds/i.test(msg)) {
          await dispatchWithLayoutMetrics();
        }
      }
    };
    const stepOnce = (): Promise<void> =>
      Promise.race([
        runPinch().catch(() => undefined),
        new Promise<void>(
          (r) =>
            void (timer = setTimeout(
              () => ((timedOut = true), r()),
              PINCH_GESTURE_TIMEOUT_MS,
            )),
        ),
      ]).finally(() => {
        if (timer !== undefined) {
          clearTimeout(timer);
        }
      });
    let p: Promise<void> = Promise.resolve();
    p = (async () => {
      try {
        await stepOnce();
        while (rec.pinchInFlight === p) {
          const n: WcvPinchGestureParams | undefined =
            !timedOut && isTabAlive() ? rec.queuedPinch : undefined;
          rec.queuedPinch = undefined;
          if (!n) {
            break;
          }
          if (Math.abs(n.scaleFactor - 1) >= 1e-12) {
            [pt, scaleFactor, zoomGen, timedOut] = [
              n,
              n.scaleFactor,
              rec.visualZoomGen ?? 0,
              false,
            ];
            await stepOnce();
          }
        }
      } finally {
        if (rec.pinchInFlight === p) {
          rec.pinchInFlight = undefined;
        }
      }
    })();
    rec.pinchInFlight = p;
    return p;
  }

  navigateGuestHistory(
    tabKey: string,
    direction: 'back' | 'forward' | (string & {}),
  ): boolean {
    if (direction !== 'back' && direction !== 'forward') {
      return false;
    }
    const entry = this.registry.getEntry(tabKey);
    const wc = entry?.view.webContents;
    if (
      !entry ||
      entry.crashed ||
      !wc ||
      wc.isDestroyed?.() ||
      wc.isCrashed?.()
    ) {
      return false;
    }
    try {
      const history = wc.navigationHistory;
      if (direction === 'back') {
        if (typeof history?.canGoBack === 'function' && history.canGoBack()) {
          history.goBack?.();
          return true;
        }
        return false;
      }
      if (
        typeof history?.canGoForward === 'function' &&
        history.canGoForward()
      ) {
        history.goForward?.();
        return true;
      }
      return false;
    } catch {
      return false;
    }
  }

  async dispatchKeyEvent(tabKey: string, params: WcvCdpKeyParams): Promise<void> {
    if (params.type === 'keyDown' || params.type === 'rawKeyDown') {
      await this.ensurePopupSuppressionBeforeInput(tabKey);
    }
    const entry = this.registry.getEntry(tabKey);
    if (entry && !entry.view.webContents.isDestroyed()) {
      this.syncMenuShortcutSuppression(entry, this.ensureRecord(entry));
    }
    // Strip `nativeVirtualKeyCode` so Chromium's `InputHandler::DispatchKeyEvent`
    // sets `skip_if_unhandled = true` and `os_event = nil`, preventing synthetic
    // CDP keys from reaching AppKit `[[NSApp mainMenu] performKeyEquivalent:]`.
    const { nativeVirtualKeyCode: _ignoredNativeVk, ...sanitized } = params;
    const macCommands = isMacOS()
      ? resolveMacEditingCommands(sanitized)
      : undefined;
    const cdpParams: Record<string, unknown> = macCommands
      ? { ...sanitized, commands: macCommands }
      : sanitized;
    await this.sendCdp(tabKey, 'Input.dispatchKeyEvent', cdpParams);
  }

  async insertText(tabKey: string, text: string): Promise<void> {
    await this.sendCdp(tabKey, 'Input.insertText', { text });
  }

  async capturePage(tabKey: string): Promise<string | null> {
    const entry = this.registry.getEntry(tabKey);
    if (!entry || entry.view.webContents.isDestroyed()) {
      return null;
    }
    const hasClipView = Boolean(getHubClipView(entry.parentWindow));
    if (hasClipView) {
      await this.applyViewportOverride(entry).catch(() => undefined);
      if (entry.view.webContents.isDestroyed()) {
        return null;
      }
    }
    // A parked hidden tab may be covered by a more recently hidden one; move
    // it directly below the curtain and allow one frame for it to repaint.
    this.registry.prepareHiddenCapture(tabKey);
    let image: WcvNativeImageLike | undefined;
    for (let attempt = 0; attempt < 2; attempt++) {
      try {
        image = await entry.view.webContents.capturePage?.();
      } catch {
        image = undefined;
      }
      if (
        (image && !image.isEmpty?.()) ||
        attempt > 0 ||
        entry.view.webContents.isDestroyed()
      ) {
        break;
      }
      await new Promise((resolve) => setTimeout(resolve, CAPTURE_RETRY_MS));
    }
    if (
      (!image || image.isEmpty?.()) &&
      hasClipView &&
      !entry.view.webContents.isDestroyed()
    ) {
      // Never-shown children of a hidden container have no Viz surface until
      // device emulation sizes Blink; off-surface fallback is non-macOS only
      // because macOS GrabViewSnapshot triggers the OS Screen Recording path.
      const halfMs = Math.floor(CDP_CAPTURE_TIMEOUT_MS / 2);
      const attempts: Array<{
        params: Record<string, unknown>;
        budgetMs: number;
      }> = isMacOS()
        ? [{ params: { format: 'png' }, budgetMs: CDP_CAPTURE_TIMEOUT_MS }]
        : [
            { params: { format: 'png' }, budgetMs: halfMs },
            {
              params: { format: 'png', fromSurface: false },
              budgetMs: CDP_CAPTURE_TIMEOUT_MS - halfMs,
            },
          ];
      for (const { params, budgetMs } of attempts) {
        if (entry.view.webContents.isDestroyed()) {
          break;
        }
        let timer: ReturnType<typeof setTimeout> | undefined;
        try {
          const shot = await Promise.race([
            this.sendCdp(tabKey, 'Page.captureScreenshot', params)
              .then((res) =>
                typeof res === 'object' && res !== null
                  ? (res as { data?: unknown })
                  : undefined,
              )
              .catch(() => undefined),
            new Promise<undefined>((resolve) => {
              timer = setTimeout(() => resolve(undefined), budgetMs);
            }),
          ]);
          if (typeof shot?.data === 'string' && shot.data.length > 0) {
            const decoded = nativeImage.createFromBuffer(
              Buffer.from(shot.data, 'base64'),
            );
            if (decoded && !decoded.isEmpty?.()) {
              image = decoded;
              break;
            }
          }
        } catch {
          image = undefined;
        } finally {
          if (timer !== undefined) {
            clearTimeout(timer);
          }
        }
      }
    }
    if (!image || image.isEmpty?.()) {
      return null;
    }
    const size = image.getSize?.();
    let finalImage: WcvNativeImageLike = image;
    if (
      size &&
      typeof image.resize === 'function' &&
      (size.width > 1024 || size.height > 1024)
    ) {
      const resized =
        size.width >= size.height
          ? image.resize({ width: 1024, quality: 'good' })
          : image.resize({ height: 1024, quality: 'good' });
      if (resized && !resized.isEmpty?.()) {
        finalImage = resized;
      }
    }
    return finalImage.toDataURL();
  }

  async navigate(tabKey: string, url: string): Promise<boolean> {
    const entry = this.registry.getEntry(tabKey);
    if (!entry || entry.view.webContents.isDestroyed()) {
      return false;
    }
    if (isExternalOnlyAuthUrl(url)) {
      void this.registry.openExternalUrl(url);
      return false;
    }
    if (!validateWebviewNavigationUrl(url, 'http://127.0.0.1').allowed) {
      return false;
    }
    if (!getHubClipView(entry.parentWindow)) {
      this.registry.noteExternalNavigation(tabKey, url);
      await entry.view.webContents.loadURL?.(url).catch(() => undefined);
      return true;
    }
    const rec = this.ensureRecord(entry);
    const navGen = (rec.clipNavGen ?? 0) + 1;
    rec.clipNavGen = navGen;
    const prevCommitSeq = rec.clipCommitSeq ?? 0;
    this.registry.noteExternalNavigation(tabKey, url);
    rec.pendingMainFrameUrl = url;
    this.resetTabPinchState(rec);
    if (getHubCatcherView(entry.parentWindow)) {
      rec.guestCssCursor = 'default';
      this.syncWindowCatcherCursor(entry.parentWindow);
    }
    if (isProtectedViewportOverrideUrl(url)) {
      this.clearClipProtectedIdpViewportSync(entry);
      void this.applyViewportOverride(entry, { caller: null }).catch(
        () => undefined,
      );
    }
    const wc = entry.view.webContents;
    let loadSucceeded = false;
    try {
      await entry.view.webContents.loadURL?.(url);
      loadSucceeded = true;
      if (
        rec.webContentsId === entry.webContentsId &&
        rec.clipNavGen === navGen &&
        (rec.clipCommitSeq ?? 0) === prevCommitSeq &&
        !entry.lastLoadError
      ) {
        // Covers no-op loadURL mocks where 'did-navigate' did not fire.
        rec.committedMainFrameUrl = url;
        this.bumpClipCommitSeq(entry, rec);
      }
    } catch {
      if (
        rec.webContentsId === entry.webContentsId &&
        rec.clipNavGen === navGen &&
        entry.lastLoadError?.validatedURL
      ) {
        rec.committedMainFrameUrl = entry.lastLoadError.validatedURL;
        this.bumpClipCommitSeq(entry, rec);
      }
    } finally {
      if (
        rec.webContentsId === entry.webContentsId &&
        rec.clipNavGen === navGen &&
        !(
          typeof wc.isLoadingMainFrame === 'function' && wc.isLoadingMainFrame()
        )
      ) {
        rec.pendingMainFrameUrl = undefined;
      }
    }
    if (
      rec.webContentsId === entry.webContentsId &&
      rec.clipNavGen === navGen &&
      !entry.view.webContents.isDestroyed()
    ) {
      this.registry.noteExternalNavigationSettled(tabKey, loadSucceeded);
      await this.applyViewportOverride(entry).catch(() => undefined);
    }
    return true;
  }

  private emitZOrderChanged(
    entry: WcvZOrderEntry,
    zOrder: WcvZOrderLevel,
    reason: WcvZOrderReason,
  ): void {
    if (!entry.hubSender.isDestroyed()) {
      entry.hubSender.send(WCV_Z_ORDER_CHANGED_CHANNEL, {
        tabKey: entry.tabKey,
        webContentsId: entry.webContentsId,
        zOrder,
        reason,
      });
    }
  }

  private attachWindowHooks(win: WcvZOrderWindow): void {
    if (this.disposed) {
      return;
    }
    this.ensureCatcherHooks(win);
    if (this.trackedWindows.has(win)) {
      return;
    }
    this.trackedWindows.add(win);
    const onHubFocus = (): void => {
      if (!win.isDestroyed?.()) {
        this.revokeWindowFocusGrants(win);
      }
    };
    const onHubDidStartNavigation = (
      rawDetails?: unknown,
      _url?: unknown,
      isInPlace?: unknown,
      isMainFrame?: unknown,
    ): void => {
      const details =
        typeof rawDetails === 'object' && rawDetails !== null
          ? (rawDetails as NavigationEventDetails)
          : undefined;
      const mainFrame = (details?.isMainFrame ?? isMainFrame) === true;
      const inPlace = Boolean(
        details?.isSameDocument ?? details?.isInPlace ?? isInPlace,
      );
      if (mainFrame && !inPlace && !win.isDestroyed?.()) {
        this.clearWindowElevationBlockers(win, 'hub-reload');
      }
    };
    const onHubRenderProcessGone = (): void => {
      if (!win.isDestroyed?.()) {
        this.clearWindowElevationBlockers(win, 'hub-render-process-gone');
      }
    };
    const hubContents = getHubWebContents(win) as
      | EventEmitterLike
      | undefined;
    hubContents?.on?.('focus', onHubFocus);
    hubContents?.on?.('did-start-navigation', onHubDidStartNavigation);
    hubContents?.on?.('render-process-gone', onHubRenderProcessGone);
    this.onWinClosed(win, () => {
      hubContents?.off?.('focus', onHubFocus);
      hubContents?.off?.('did-start-navigation', onHubDidStartNavigation);
      hubContents?.off?.('render-process-gone', onHubRenderProcessGone);
      this.dropCatcherState(win);
    });
  }

  private findFocusGrantedVisibleTab(
    win: WcvZOrderWindow,
  ): WcvZOrderEntry | undefined {
    for (const entry of this.registry.getEntriesForWindow(win)) {
      const rec = this.tabStates.get(entry.tabKey);
      if (entry.visible && rec?.userElevationLatch) {
        return entry;
      }
    }
    return undefined;
  }

  private handlePreCommitNavigation(
    entry: WcvZOrderEntry,
    rec: TabStateRecord,
    details?: NavigationEventDetails,
    url?: string,
    isInPlace?: boolean,
    isMainFrame?: boolean,
  ): void {
    const navUrl = (url ?? details?.url ?? '').trim();
    const mainFrame = (details?.isMainFrame ?? isMainFrame) !== false;
    const sameDocument = Boolean(
      details?.isSameDocument ?? details?.isInPlace ?? isInPlace,
    );
    if (!mainFrame || sameDocument) {
      return;
    }
    if (getHubClipView(entry.parentWindow)) {
      if (details?.defaultPrevented) {
        return;
      }
      if (
        getHubCatcherView(entry.parentWindow) &&
        rec.webContentsId === entry.webContentsId
      ) {
        rec.guestCssCursor = 'default';
        this.syncWindowCatcherCursor(entry.parentWindow);
      }
      if (!navUrl || navUrl.startsWith('chrome-error://')) {
        return;
      }
      rec.pendingMainFrameUrl = navUrl;
      if (isProtectedViewportOverrideUrl(navUrl)) {
        this.clearClipProtectedIdpViewportSync(entry);
        void this.applyViewportOverride(entry, { caller: null }).catch(
          () => undefined,
        );
      }
      return;
    }
    if (isProtectedViewportOverrideUrl(navUrl)) {
      this.resetViewportOverride(entry);
    }
  }

  private cancelTabActiveDrags(entry: WcvZOrderEntry): void {
    const st = this.catcherStates.get(entry.parentWindow);
    if (st) {
      if (sameTabRef(st.externalDrag, entry)) {
        this.cancelExternalDrag(st);
      }
      this.cancelInterceptedPresses(st, entry);
    }
  }

  private clearTabDragState(entry: WcvZOrderEntry): void {
    const st = this.catcherStates.get(entry.parentWindow);
    if (st) {
      if (sameTabRef(st.externalDrag, entry)) {
        st.externalDrag = undefined;
      }
      this.cancelInterceptedPresses(st, entry, false);
    }
  }

  private bumpClipCommitSeq(
    entry: WcvZOrderEntry,
    rec: TabStateRecord,
  ): void {
    rec.clipCommitSeq = (rec.clipCommitSeq ?? 0) + 1;
    this.clearTabDragState(entry);
  }

  private handleDragIntercepted(
    entry: WcvZOrderEntry,
    rec: TabStateRecord,
    method: unknown,
    params: unknown,
  ): void {
    const p =
      typeof params === 'object' && params !== null
        ? (params as { data?: unknown })
        : undefined;
    const hasData = Boolean(p?.data && typeof p.data === 'object');
    const ok =
      method === 'Input.dragIntercepted' &&
      hasData &&
      rec.webContentsId === entry.webContentsId &&
      Boolean(getHubCatcherView(entry.parentWindow));
    if (!ok || !p?.data) {
      return;
    }
    const dragData = p.data as WcvCdpDragData;
    const st = this.catcherStates.get(entry.parentWindow);
    const seq = rec.clipCommitSeq ?? 0;
    const hit = st
      ? [...st.presses.values()].find(
          (press) => !press.hubReplay && sameTabRef(press, entry, seq),
        )
      : undefined;
    if (hit) {
      hit.interceptedDrag = { data: dragData, entered: false };
    } else {
      const a: BestEffortDragCancelArgs = {
        e: entry,
        rec,
        pt: { x: 0, y: 0 },
        data: dragData,
      };
      this.emitBestEffortDragCancel(a);
    }
  }

  private attachGuestHooks(
    entry: WcvZOrderEntry,
    rec: TabStateRecord,
  ): void {
    const wc = entry.view.webContents;
    const wcEv = wc as unknown as EventEmitterLike;
    const bag = this.guestHooks.get(wc) ?? { off: [] };
    this.guestHooks.set(wc, bag);
    const bind = <Args extends unknown[]>(
      t: EventEmitterLike | null | undefined,
      ev: string,
      fn: (...args: Args) => void,
    ): void => {
      const g = (...a: unknown[]): void => {
        if (!this.disposed) {
          fn(...(a as Args));
        }
      };
      t?.on?.(ev, g);
      bag.off.push(() => detachEv(t, ev, g));
    };
    const armBag = (): void => {
      if (bag.armed) {
        return;
      }
      bag.armed = true;
      let clean: (dead?: boolean) => void = (_dead?: boolean) => {};
      const onDead = (): void => {
        clean(true);
        if (!this.disposed) {
          bag.after?.();
        }
      };
      clean = (dead = false): void => {
        if (this.disposers.delete(clean)) {
          bag.off.splice(0).forEach((f) => f());
          void (!dead && detachEv(wcEv, 'destroyed', onDead));
        }
      };
      this.disposers.add(clean);
      wcEv.once?.('destroyed', onDead);
    };
    const onNav = (ev: string): void =>
      bind<
        [
          d?: NavigationEventDetails,
          u?: string,
          ip?: boolean,
          mf?: boolean,
        ]
      >(wcEv, ev, (d, u, ip, mf) =>
        this.handlePreCommitNavigation(entry, rec, d, u, ip, mf),
      );
    if (getHubClipView(entry.parentWindow) && !rec.clipNavListenersAttached) {
      rec.clipNavListenersAttached = true;
      const liveWcUrl = (
        (typeof wc.getURL === 'function' ? wc.getURL() : '') || ''
      ).trim();
      const hasUsableLiveUrl =
        !entry.lastLoadError &&
        Boolean(liveWcUrl) &&
        liveWcUrl !== 'about:blank' &&
        !liveWcUrl.startsWith('chrome-error://');
      const seededUrl =
        rec.listenersAttached && hasUsableLiveUrl
          ? liveWcUrl
          : entry.lastUrl || liveWcUrl;
      rec.committedMainFrameUrl = seededUrl || undefined;
      onNav('will-redirect');
      onNav('did-redirect-navigation');
      bind(wcEv, 'did-stop-loading', () => {
        if (
          getHubClipView(entry.parentWindow) &&
          rec.pendingMainFrameUrl !== undefined &&
          !(
            typeof wc.isLoadingMainFrame === 'function' &&
            wc.isLoadingMainFrame()
          )
        ) {
          if (entry.lastLoadError?.validatedURL) {
            rec.committedMainFrameUrl = entry.lastLoadError.validatedURL;
            this.bumpClipCommitSeq(entry, rec);
          }
          rec.pendingMainFrameUrl = undefined;
          void this.applyViewportOverride(entry).catch(() => undefined);
        }
      });
      bind(wcEv, 'render-process-gone', () => {
        if (
          !getHubClipView(entry.parentWindow) ||
          rec.webContentsId !== entry.webContentsId
        ) {
          return;
        }
        this.bumpClipCommitSeq(entry, rec);
        rec.cdpMouseInFlight = 0;
        rec.cdpAttachEpoch = (rec.cdpAttachEpoch ?? 0) + 1;
        rec.cdpAttachInFlight = undefined;
        rec.cdpAttached = false;
        rec.focusEmulationEnabled = undefined;
        rec.focusEmulationSeq = 0;
        [rec.dragInterceptionEnabled, rec.dragInterceptionSeq] = [undefined, 0];
        rec.guestCssCursor = 'default';
        this.resetTabPinchState(rec);
        this.clearTabInputQueues(rec);
        if (getHubCatcherView(entry.parentWindow)) {
          this.syncWindowCatcherCursor(entry.parentWindow);
        }
        rec.popupCssGeneration = (rec.popupCssGeneration ?? 0) + 1;
        rec.popupSuppressionCssKey = undefined;
        rec.popupSuppressionCssInFlight = undefined;
        rec.isolatedGuardSuppressed = undefined;
        const vpRec = this.viewportOverrides.get(entry.tabKey);
        if (vpRec && vpRec.webContentsId === entry.webContentsId) {
          vpRec.appliedKey = undefined;
          vpRec.pipeDirty = false;
        }
      });
    }
    if (
      (getHubClipView(entry.parentWindow) ||
        getHubCatcherView(entry.parentWindow)) &&
      !rec.dragInterceptListenerAttached
    ) {
      rec.dragInterceptListenerAttached = true;
      bind<[_e: unknown, m: unknown, p: unknown]>(
        getCdpDebugger(wc),
        'message',
        (_e, m, p) => this.handleDragIntercepted(entry, rec, m, p),
      );
    }
    if (rec.listenersAttached) {
      armBag();
      return;
    }
    rec.listenersAttached = true;
    const onWc = <Args extends unknown[]>(
      ev: string,
      fn: (...args: Args) => void,
    ): void => bind(wcEv, ev, fn);
    onWc<[_event: unknown, mouse?: { type?: string }]>(
      'before-mouse-event',
      (_event, mouse) => {
        if (
          entry.visible &&
          mouse?.type === 'mouseDown' &&
          rec.cdpMouseInFlight === 0
        ) {
          this.setUserElevationLatch(entry, rec, true);
          this.emitZOrderChanged(entry, 1, 'user-gesture');
        }
      },
    );
    onWc('focus', () => {
      if (!entry.visible) {
        // Agent CDP input can focus a hidden tab. Hand focus back to the
        // focus-granted visible tab, or to the Hub when none holds a grant.
        setImmediate(() => {
          // The window may have closed, or the user may have shown this tab,
          // before this callback runs.
          if (
            this.disposed ||
            entry.parentWindow.isDestroyed?.() ||
            entry.visible
          ) {
            return;
          }
          const owner = this.findFocusGrantedVisibleTab(entry.parentWindow);
          if (owner && !owner.view.webContents.isDestroyed()) {
            // focus() fires the owner's own focus handler, possibly after a
            // forwarded shortcut or wcv:demote has revoked its grant. Mark the
            // hand-back so that event is not bounced to the Hub as ungranted
            // guest focus.
            this.ensureRecord(owner).focusHandBackUntil =
              Date.now() + FOCUS_HAND_BACK_GRACE_MS;
            owner.view.webContents.focus?.();
            return;
          }
          const hubContents = getHubWebContents(entry.parentWindow);
          if (hubContents && !hubContents.isDestroyed?.()) {
            hubContents.focus?.();
          }
        });
        return;
      }
      if (rec.focusHandBackUntil !== undefined) {
        const handedBack = Date.now() <= rec.focusHandBackUntil;
        rec.focusHandBackUntil = undefined;
        if (handedBack) {
          return;
        }
      }
      if (
        !rec.userElevationLatch &&
        rec.zOrder === 1 &&
        rec.cdpMouseInFlight === 0
      ) {
        // Touch and pen taps never reach before-mouse-event, so focus moving
        // into the page on top with no agent input in flight is the user's.
        this.setUserElevationLatch(entry, rec, true);
        this.emitZOrderChanged(entry, 1, 'user-gesture');
        return;
      }
      if (
        !rec.userElevationLatch &&
        !(entry.htmlFullscreen && entry.visible && !entry.isAgentStepActive)
      ) {
        setImmediate(() => {
          if (
            !this.disposed &&
            !rec.userElevationLatch &&
            !(
              entry.htmlFullscreen &&
              entry.visible &&
              !entry.isAgentStepActive
            ) &&
            !entry.parentWindow.isDestroyed?.() &&
            !this.findFocusGrantedVisibleTab(entry.parentWindow)
          ) {
            const hubContents = getHubWebContents(entry.parentWindow);
            if (hubContents && !hubContents.isDestroyed?.()) {
              hubContents.focus?.();
            }
          }
        });
      }
    });
    onWc<[_event: unknown, type?: string]>('cursor-changed', (_event, type) => {
      if (!entry.hubSender.isDestroyed()) {
        entry.hubSender.send(WCV_CURSOR_CHANGED_CHANNEL, {
          tabKey: entry.tabKey,
          webContentsId: entry.webContentsId,
          cursor: type || 'default',
        });
      }
      if (
        getHubCatcherView(entry.parentWindow) &&
        rec.webContentsId === entry.webContentsId &&
        this.registry.isClipEntryRevealed(entry)
      ) {
        rec.guestCssCursor = mapElectronCursorToCss(type);
        this.syncWindowCatcherCursor(entry.parentWindow);
      }
    });
    onWc<[event: { preventDefault(): void }, input: GuestBeforeInput]>(
      'before-input-event',
      (event, input) => {
        if (
          input?.type &&
          input.type !== 'keyDown' &&
          input.type !== 'rawKeyDown'
        ) {
          return;
        }
        if (input?.isComposing) {
          return;
        }
        const keyLower = (input?.key || '').toLowerCase();
        if (keyLower === 'escape' || input?.code === 'Escape') {
          const st = this.catcherStates.get(entry.parentWindow);
          if (st && this.cancelInterceptedPresses(st, entry)) {
            event.preventDefault();
            const cWc = getHubCatcherView(entry.parentWindow)?.webContents;
            if (st.presses.size === 0 && cWc && !cWc.isDestroyed?.()) {
              cWc.send?.(WCV_CATCHER_RESET_DRAG_CHANNEL);
            }
            return;
          }
        }
        const cmdOrCtrl = isMacOS()
          ? Boolean(input?.meta)
          : Boolean(input?.control);
        const hasDisallowedModifier = isMacOS()
          ? Boolean(input?.control || input?.alt)
          : Boolean(input?.alt || input?.meta);
        const hasAltGr = Boolean(
          input?.modifiers?.some((mod) => {
            const m = mod.toLowerCase();
            return m === 'altgr' || m === 'alt';
          }),
        );
        if (
          entry.visible &&
          !entry.isAgentStepActive &&
          cmdOrCtrl &&
          !hasDisallowedModifier &&
          !hasAltGr
        ) {
          const zoomAction = resolveHubZoomShortcutAction(input, keyLower);
          if (zoomAction) {
            event.preventDefault();
            wc.setZoomLevel?.(0);
            void applyHubWindowZoom(entry.parentWindow, zoomAction).catch(
              () => undefined,
            );
            return;
          }
        }
        // Tab cycling uses physical Ctrl+Tab / Ctrl+Shift+Tab across all
        // desktop OSes (including macOS, where cmdOrCtrl requires Meta so
        // Cocoa editing chords such as Ctrl+K remain in the guest).
        const isTabCycleShortcut =
          Boolean(input?.control) &&
          !input?.meta &&
          !input?.alt &&
          !hasAltGr &&
          (keyLower === 'tab' || input?.code === 'Tab');
        const isGlobalShortcut =
          cmdOrCtrl &&
          !hasDisallowedModifier &&
          !hasAltGr &&
          ((!input?.shift && HUB_GLOBAL_SHORTCUT_KEYS.has(keyLower)) ||
            (Boolean(input?.shift) && (keyLower === 'n' || keyLower === 't')));
        if (
          entry.visible &&
          !entry.isAgentStepActive &&
          rec.zOrder === 1 &&
          (isTabCycleShortcut || isGlobalShortcut)
        ) {
          event.preventDefault();
          // Suppress repeat ticks after preventDefault so held chords neither
          // leak into the guest nor re-emit synthetic host keydowns.
          const isAutoRepeat = Boolean(
            input?.isAutoRepeat ||
              input?.modifiers?.some(
                (mod) => mod.toLowerCase() === 'isautorepeat',
              ),
          );
          if (isAutoRepeat) {
            return;
          }
          if (entry.htmlFullscreen) {
            this.registry.exitHtmlFullscreen?.(entry.tabKey);
          }
          this.setUserElevationLatch(entry, rec, false);
          const hubContents = getHubWebContents(entry.parentWindow);
          if (hubContents && !hubContents.isDestroyed?.()) {
            hubContents.focus?.();
          }
          if (!entry.hubSender.isDestroyed()) {
            entry.hubSender.send(WCV_SHORTCUT_FORWARDED_CHANNEL, {
              tabKey: entry.tabKey,
              key: isTabCycleShortcut ? 'Tab' : input.key || '',
              code: isTabCycleShortcut ? input.code || 'Tab' : input.code || '',
              meta: Boolean(input.meta),
              control: Boolean(input.control),
              shift: Boolean(input.shift),
              alt: Boolean(input.alt),
            });
          }
        }
      },
    );
    const onDocumentReset = (): void => {
      rec.popupCssGeneration = (rec.popupCssGeneration ?? 0) + 1;
      rec.popupSuppressionCssKey = undefined;
      rec.popupSuppressionCssInFlight = undefined;
      rec.isolatedGuardSuppressed = undefined;
      void this.syncPopupSuppression(entry, rec);
      const vpRec = this.viewportOverrides.get(entry.tabKey);
      const hasClipView = Boolean(getHubClipView(entry.parentWindow));
      if (
        hasClipView ||
        (vpRec &&
          vpRec.webContentsId === entry.webContentsId &&
          (vpRec.callerDesired || vpRec.fallbackDesired))
      ) {
        const navUrl =
          (typeof wc.getURL === 'function' ? wc.getURL() : '') ||
          entry.lastUrl ||
          '';
        if (vpRec?.callerDesired && this.isProtectedTabUrl(entry, navUrl)) {
          void this.applyViewportOverride(entry, { caller: null }).catch(
            () => undefined,
          );
        } else if (!vpRec?.appliedKey || hasClipView) {
          void this.applyViewportOverride(entry).catch(() => undefined);
        }
      }
      if (
        (entry.isAgentStepActive ||
          this.shouldRetainUserFocusEmulation(entry, rec)) &&
        !rec.cdpAttached
      ) {
        void this.ensureCdpAttached(entry);
      }
    };
    onWc('dom-ready', onDocumentReset);
    onWc<[_event: unknown, url?: unknown]>('did-navigate', (_event, url) => {
      if (rec.webContentsId === entry.webContentsId) {
        this.resetTabPinchState(rec);
      }
      if (getHubClipView(entry.parentWindow)) {
        if (rec.webContentsId === entry.webContentsId) {
          this.bumpClipCommitSeq(entry, rec);
          if (getHubCatcherView(entry.parentWindow)) {
            rec.guestCssCursor = 'default';
            this.syncWindowCatcherCursor(entry.parentWindow);
          }
        }
        const navUrl = typeof url === 'string' ? url.trim() : '';
        if (navUrl.startsWith('chrome-error://')) {
          const errorTarget =
            entry.lastLoadError?.validatedURL ||
            entry.lastUrl ||
            rec.committedMainFrameUrl;
          if (errorTarget !== undefined) {
            rec.committedMainFrameUrl = errorTarget;
          }
        } else {
          const liveUrl = (
            navUrl ||
            (typeof wc.getURL === 'function' ? wc.getURL() : '') ||
            ''
          ).trim();
          if (liveUrl && !liveUrl.startsWith('chrome-error://')) {
            rec.committedMainFrameUrl = liveUrl;
          }
        }
        rec.pendingMainFrameUrl = undefined;
      }
      onDocumentReset();
    });
    // Clear before commit so a protected IdP never paints with an override.
    onNav('did-start-navigation');
    onWc<[_event: unknown, isMainFrame?: boolean]>(
      'did-frame-finish-load',
      (_event, isMainFrame) => {
        if (isMainFrame === false && !wc.isDestroyed?.()) {
          void Promise.all(
            this.syncSubframePopupSuppression(
              wc,
              this.shouldSuppressNativePopups(entry, rec),
            ),
          );
        }
      },
    );
    const dbg = getCdpDebugger(wc);
    this.dialogController.registerGuest(wc, entry.hubSender, () => ({
      tabKey: entry.tabKey,
      isAgentStepActive: entry.isAgentStepActive,
    }));
    bind(dbg, 'detach', () => {
      if (rec.webContentsId === entry.webContentsId) {
        rec.cdpAttachEpoch = (rec.cdpAttachEpoch ?? 0) + 1;
        rec.cdpAttachInFlight = undefined;
        rec.cdpAttached = false;
        rec.focusEmulationEnabled = undefined;
        rec.focusEmulationSeq = 0;
        [rec.dragInterceptionEnabled, rec.dragInterceptionSeq] = [undefined, 0];
        this.clearTabDragState(entry);
        this.clearTabInputQueues(rec);
      }
      const vpRec = this.viewportOverrides.get(entry.tabKey);
      if (vpRec && vpRec.webContentsId === entry.webContentsId) {
        vpRec.appliedKey = undefined;
        if (getHubClipView(entry.parentWindow)) {
          vpRec.pipeDirty = false;
        }
      }
    });
    this.fileChooserController.registerGuest(wc, entry.hubSender, () => ({
      tabKey: entry.tabKey,
      suppressNativePopups: this.shouldSuppressNativePopups(entry, rec),
    }));
    bag.after = () => {
      if (
        this.tabStates.get(entry.tabKey)?.webContentsId ===
          entry.webContentsId &&
        !this.registry.getEntry(entry.tabKey)
      ) {
        this.clearTabInputQueues(this.tabStates.get(entry.tabKey));
        this.tabStates.delete(entry.tabKey);
      }
      if (
        this.viewportOverrides.get(entry.tabKey)?.webContentsId ===
        entry.webContentsId
      ) {
        this.viewportOverrides.delete(entry.tabKey);
      }
      if (
        getHubClipView(entry.parentWindow) &&
        !entry.parentWindow.isDestroyed?.()
      ) {
        this.reconcileWindow(entry.parentWindow, 'guest-destroyed');
      }
    };
    armBag();
  }
}

export const wcvZOrderController = new WcvZOrderController(
  wcvBrowserRegistry,
  guestDialogController,
  { fileChooserController: guestFileChooserController },
);
