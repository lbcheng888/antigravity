import {
  ipcMain,
  type BrowserWindow,
  type IpcMainEvent,
  type IpcMainInvokeEvent,
  type OpenDevToolsOptions,
  type Rectangle,
  type WebContents,
} from 'electron';
import {
  WCV_CATCHER_DRAG_CHANNEL,
  WCV_CATCHER_PINCH_CHANNEL,
  WCV_CATCHER_POINTER_DOWN_CHANNEL,
  WCV_CATCHER_POINTER_LEAVE_CHANNEL,
  WCV_CATCHER_POINTER_MOVE_CHANNEL,
  WCV_CATCHER_POINTER_UP_CHANNEL,
  WCV_CATCHER_WHEEL_CHANNEL,
} from './catcherChannels';
import {
  HUB_SURFACE_COLOR_STORAGE_KEY,
  getHubCatcherView,
  getHubWebContents,
  resolveHubWindowFromCatcherWebContents,
  resolveHubWindowFromWebContents,
  setHubCurtainColor,
} from './utils';
import {
  wcvBrowserRegistry,
  type WcvAcquireRequest,
  type WcvBoundsInput,
  type WcvSize,
  type WcvTabSnapshot,
} from './wcvBrowserRegistry';
import { isRemoteContents } from './remote/remoteWindowSecurity';
import {
  WCV_HUB_CURSOR_HOLE_STATE_CHANNEL,
  WCV_HUB_PROBE_POINT_RESPONSE_CHANNEL,
  wcvZOrderController,
  type CatcherDragPayload,
  type CatcherPinchPayload,
  type CatcherPointerDownPayload,
  type CatcherPointerMovePayload,
  type CatcherPointerUpPayload,
  type CatcherWheelPayload,
  type HubCursorHoleStatePayload,
  type HubProbePointResponsePayload,
  type WcvCdpKeyParams,
  type WcvCdpMouseParams,
  type WcvDispatchMouseOptions,
  type WcvZOrderReason,
  type WcvZOrderState,
  type WcvZOrderWindow,
} from './wcvZOrderController';
import { guestPromptController } from './guestPromptController';
import { isGuestAgentControlled } from './guestAgentControl';
import { guestWebMcpController } from './guestWebMcpController';

export interface WcvIpcSenderFrameLike {
  parent?: unknown;
}

export interface WcvIpcSenderLike {
  id?: number;
  session?: unknown;
  mainFrame?: unknown;
  isDestroyed?: () => boolean;
  getType?: () => string;
  getZoomFactor?: () => number;
}

export interface WcvIpcEventLike {
  sender?: WcvIpcSenderLike | null;
  senderFrame?: WcvIpcSenderFrameLike | null;
}

export interface WcvIpcAcquireRequest extends WcvAcquireRequest {
  webmcpEnabled?: boolean;
}

export type WcvControlAction =
  | 'goBack'
  | 'back'
  | 'goForward'
  | 'forward'
  | 'reload'
  | 'stop'
  | 'openDevTools'
  | 'mute'
  | 'unmute'
  | 'cut'
  | 'copy'
  | 'paste'
  | 'selectAll';

export interface WcvControlNavigationHistory {
  canGoBack?: () => boolean;
  goBack?: () => void;
  canGoForward?: () => boolean;
  goForward?: () => void;
}

export interface WcvControlWebContents {
  id: number;
  isDestroyed(): boolean;
  navigationHistory?: WcvControlNavigationHistory;
  canGoBack?: () => boolean;
  goBack?: () => void;
  canGoForward?: () => boolean;
  goForward?: () => void;
  reload?: () => void;
  stop?: () => void;
  openDevTools?: (options?: OpenDevToolsOptions) => void;
  cut?: () => void;
  copy?: () => void;
  paste?: () => void;
  selectAll?: () => void;
}

export interface WcvIpcEntry {
  parentWindow: BrowserWindow;
  hubSender: { id: number };
  view: { webContents: WcvControlWebContents };
}

export interface WcvIpcRegistry {
  getEntry(tabKey: string): WcvIpcEntry | undefined;
  getEvictedSenderId(tabKey: string): number | undefined;
  withOperationPin<T>(tabKey: string, op: () => Promise<T>): Promise<T>;
  rehydrateEvicted(
    parentWindow: BrowserWindow,
    hubSender: WebContents,
    tabKey: string,
  ): Promise<unknown>;
  acquire(
    parentWindow: BrowserWindow,
    hubSender: WebContents,
    request: WcvAcquireRequest,
  ): Promise<WcvTabSnapshot>;
  show(
    tabKey: string,
    boundsDip: WcvBoundsInput,
    parentWindow?: BrowserWindow,
  ): WcvTabSnapshot | null;
  hasEvicted(tabKey: string, hubSenderId?: number): boolean;
  rehydrateEvictedAndShow(
    parentWindow: BrowserWindow,
    hubSender: WebContents,
    tabKey: string,
    boundsDip: WcvBoundsInput,
  ): Promise<WcvTabSnapshot | null>;
  hide(tabKey: string): void;
  setBounds(tabKey: string, bounds: WcvBoundsInput): void;
  setPinned(tabKey: string, pinned: boolean): void;
  setUserMuted(tabKey: string, muted: boolean): void;
  dispose(tabKey: string): void;
}

export interface WcvIpcZOrderController {
  setHumanPromptOpen(tabKey: string, open: boolean): void;
  reconcileWindow(win: WcvZOrderWindow, reason: WcvZOrderReason): void;
  revokeFocusGrant(tabKey: string): void;
  syncCatcherSlotBounds(win: WcvZOrderWindow): void;
  setAgentControlling(tabKey: string, active: boolean): WcvZOrderState | null;
  getState(tabKey: string): WcvZOrderState | null;
  setOverlayActive(tabKey: string, active: boolean): WcvZOrderState | null;
  setPortalOverlayOpen(win: WcvZOrderWindow, open: boolean): void;
  elevate(
    tabKey: string,
    reason?: WcvZOrderReason,
    focusBrowser?: boolean,
  ): WcvZOrderState | null;
  setDragOverActive(tabKey: string, active: boolean): WcvZOrderState | null;
  demote(tabKey: string, reason?: WcvZOrderReason): WcvZOrderState | null;
  navigate(tabKey: string, url: string): Promise<boolean>;
  dispatchMouseEvent(
    tabKey: string,
    params: WcvCdpMouseParams,
    options?: WcvDispatchMouseOptions,
  ): Promise<void>;
  dispatchKeyEvent(tabKey: string, params: WcvCdpKeyParams): Promise<void>;
  insertText(tabKey: string, text: string): Promise<void>;
  setViewportSize(tabKey: string, size: WcvSize | null): Promise<boolean>;
  capturePage(tabKey: string): Promise<string | null>;
  handleHubCursorHoleState(
    w: WcvZOrderWindow,
    p: HubCursorHoleStatePayload,
  ): void;
  handleHubProbePointResponse(
    w: WcvZOrderWindow,
    p: HubProbePointResponsePayload,
  ): void;
  handleCatcherPointerDown(
    w: WcvZOrderWindow,
    p: CatcherPointerDownPayload,
  ): Promise<void> | void;
  handleCatcherPointerMove(
    w: WcvZOrderWindow,
    p: CatcherPointerMovePayload,
  ): void;
  handleCatcherPointerUp(
    w: WcvZOrderWindow,
    p: CatcherPointerUpPayload,
  ): Promise<void> | void;
  handleCatcherPointerLeave(w: WcvZOrderWindow): void;
  handleCatcherWheel(w: WcvZOrderWindow, p: CatcherWheelPayload): void;
  handleCatcherPinch(w: WcvZOrderWindow, p: CatcherPinchPayload): void;
  handleCatcherDrag(
    w: WcvZOrderWindow,
    p: CatcherDragPayload,
  ): Promise<void> | void;
}

export interface WcvIpcStorageManager {
  updateItems(changes: Record<string, string>): Promise<unknown> | unknown;
}

interface WcvAuthorizedCaller {
  win: BrowserWindow;
  sender: WebContents;
  entry?: WcvIpcEntry;
}

interface CatcherDragBase {
  phase: 'enter' | 'over' | 'leave' | 'drop';
  x: number;
  y: number;
  modifiers: number;
  hasFiles: boolean;
  types: string[];
  files?: unknown;
  items?: unknown;
}

const DOWN_BUTTONS = ['left', 'middle', 'right', 'back', 'forward'] as const;

const isPlainObject = (v: unknown): v is Record<string, unknown> =>
  v !== null && typeof v === 'object' && !Array.isArray(v);
const isFiniteNum = (v: unknown): v is number =>
  typeof v === 'number' && Number.isFinite(v);
const isPositiveInt = (v: unknown): v is number =>
  typeof v === 'number' && Number.isInteger(v) && v > 0;
const isModifiersMask = (v: unknown): v is number =>
  typeof v === 'number' && Number.isInteger(v) && v >= 0 && v <= 15;
const isDragButton = (v: unknown): v is 'left' | 'middle' | 'right' =>
  v === 'left' || v === 'middle' || v === 'right';
const isClickCount = (v: unknown): v is 1 | 2 | 3 =>
  v === 1 || v === 2 || v === 3;
const isDragPhase = (v: unknown): v is 'enter' | 'over' | 'leave' | 'drop' =>
  v === 'enter' || v === 'over' || v === 'leave' || v === 'drop';
const isStr = (v: unknown, max: number): v is string =>
  typeof v === 'string' && v.length > 0 && v.length <= max;
const isList = <T>(
  v: unknown,
  max: number,
  fn: (item: unknown) => item is T,
): v is T[] => Array.isArray(v) && v.length <= max && v.every(fn);
const isDropFile = (f: unknown): f is string =>
  isStr(f, 4096) && !f.includes('\0');
const isDropItem = (it: unknown): it is { mimeType: string; data: string } =>
  isPlainObject(it) &&
  isStr(it.mimeType, 256) &&
  typeof it.data === 'string' &&
  it.data.length <= 1048576;

function isCatcherDragBase(p: unknown): p is CatcherDragBase {
  return (
    isPlainObject(p) &&
    isDragPhase(p.phase) &&
    isFiniteNum(p.x) &&
    isFiniteNum(p.y) &&
    isModifiersMask(p.modifiers) &&
    typeof p.hasFiles === 'boolean' &&
    isList(p.types, 64, (t): t is string => isStr(t, 256))
  );
}

function parseCatcherDragPayload(p: unknown): CatcherDragPayload | null {
  if (!isCatcherDragBase(p)) {
    return null;
  }
  const { phase, x, y, modifiers, hasFiles } = p;
  const base: CatcherDragPayload = {
    phase,
    x,
    y,
    modifiers,
    hasFiles,
    types: [...p.types],
  };
  if (phase !== 'drop') {
    return base;
  }
  const { files, items } = p;
  if (files !== undefined && !isList(files, 256, isDropFile)) {
    return null;
  }
  if (items !== undefined && !isList(items, 64, isDropItem)) {
    return null;
  }
  if (files) {
    base.files = [...files];
  }
  if (items) {
    base.items = items.map(({ mimeType, data }) => ({ mimeType, data }));
  }
  return base;
}

function isHubCursorHoleStatePayload(
  p: unknown,
): p is HubCursorHoleStatePayload {
  return (
    isPlainObject(p) &&
    typeof p.isHole === 'boolean' &&
    isFiniteNum(p.dipX) &&
    isFiniteNum(p.dipY) &&
    (p.reason === undefined ||
      p.reason === 'pointermove' ||
      p.reason === 'stationary-overlay' ||
      p.reason === 'dragover')
  );
}

function isHubProbePointResponsePayload(
  p: unknown,
): p is HubProbePointResponsePayload {
  return (
    isPlainObject(p) &&
    isPositiveInt(p.requestId) &&
    typeof p.isHole === 'boolean'
  );
}

function isCatcherPointerDownPayload(
  p: unknown,
): p is CatcherPointerDownPayload {
  return (
    isPlainObject(p) &&
    isPositiveInt(p.pressId) &&
    isFiniteNum(p.x) &&
    isFiniteNum(p.y) &&
    typeof p.rawButton === 'number' &&
    Number.isInteger(p.rawButton) &&
    p.rawButton >= 0 &&
    p.rawButton <= 4 &&
    DOWN_BUTTONS[p.rawButton] === p.button &&
    isClickCount(p.clickCount) &&
    isModifiersMask(p.modifiers)
  );
}

function isCatcherPointerMovePayload(
  p: unknown,
): p is CatcherPointerMovePayload {
  return (
    isPlainObject(p) &&
    typeof p.isDragging === 'boolean' &&
    isFiniteNum(p.x) &&
    isFiniteNum(p.y) &&
    isModifiersMask(p.modifiers) &&
    (p.isDragging
      ? isPositiveInt(p.pressId) && isDragButton(p.button)
      : p.pressId === null && p.button === 'none')
  );
}

function isCatcherPointerUpPayload(p: unknown): p is CatcherPointerUpPayload {
  return (
    isPlainObject(p) &&
    isPositiveInt(p.pressId) &&
    isFiniteNum(p.x) &&
    isFiniteNum(p.y) &&
    isDragButton(p.button) &&
    isClickCount(p.clickCount) &&
    isModifiersMask(p.modifiers) &&
    typeof p.outOfBounds === 'boolean'
  );
}

function isCatcherWheelPayload(p: unknown): p is CatcherWheelPayload {
  return (
    isPlainObject(p) &&
    isFiniteNum(p.x) &&
    isFiniteNum(p.y) &&
    isFiniteNum(p.deltaX) &&
    isFiniteNum(p.deltaY) &&
    isModifiersMask(p.modifiers)
  );
}

function isCatcherPinchPayload(p: unknown): p is CatcherPinchPayload {
  return (
    isPlainObject(p) &&
    isFiniteNum(p.x) &&
    isFiniteNum(p.y) &&
    isFiniteNum(p.scaleFactor) &&
    p.scaleFactor > 0
  );
}

function isTopFrameWindowSender(
  event: WcvIpcEventLike | null | undefined,
): event is { sender: WcvIpcSenderLike; senderFrame: WcvIpcSenderFrameLike } {
  const s = event?.sender;
  const f = event?.senderFrame;
  const okType = typeof s?.getType !== 'function' || s?.getType?.() === 'window';
  return Boolean(
    s &&
      typeof s === 'object' &&
      !s.isDestroyed?.() &&
      okType &&
      !isRemoteContents(s) &&
      f &&
      typeof f === 'object' &&
      !f.parent &&
      s.mainFrame &&
      f === s.mainFrame,
  );
}

function resolveAuthorizedCatcherWindow(
  event: WcvIpcEventLike | null | undefined,
): BrowserWindow | undefined {
  if (!isTopFrameWindowSender(event)) {
    return undefined;
  }
  const win = resolveHubWindowFromCatcherWebContents(event.sender);
  if (!win || win.isDestroyed?.()) {
    return undefined;
  }
  const catcher = getHubCatcherView(win);
  return catcher?.webContents &&
    !catcher.webContents.isDestroyed?.() &&
    catcher.webContents === event.sender
    ? win
    : undefined;
}

function resolveAuthorizedHubCatcherTargetWindow(
  event: WcvIpcEventLike | null | undefined,
): BrowserWindow | undefined {
  if (!isTopFrameWindowSender(event)) {
    return undefined;
  }
  const win = resolveHubWindowFromWebContents(event.sender);
  if (!win || win.isDestroyed?.() || getHubWebContents(win) !== event.sender) {
    return undefined;
  }
  const catcher = getHubCatcherView(win);
  return catcher?.webContents && !catcher.webContents.isDestroyed?.()
    ? win
    : undefined;
}

export function isAuthorizedHubMainFrameEvent(
  event:
    | IpcMainEvent
    | IpcMainInvokeEvent
    | WcvIpcEventLike
    | null
    | undefined,
): boolean {
  const s = event?.sender;
  const f = event?.senderFrame;
  const okType = typeof s?.getType !== 'function' || s?.getType?.() === 'window';
  if (!s || s.isDestroyed?.() || !okType || !f || f.parent) {
    return false;
  }
  if (s.mainFrame && f !== s.mainFrame) {
    return false;
  }
  if (isRemoteContents(s)) {
    return false;
  }
  const win = resolveHubWindowFromWebContents(s);
  const hub = win ? getHubWebContents(win) : undefined;
  return hub === s || (typeof s.id === 'number' && hub?.id === s.id);
}

function assertAuthorizedWcvCaller(
  event: IpcMainInvokeEvent | IpcMainEvent,
  registry: WcvIpcRegistry,
  tabKey?: string,
): WcvAuthorizedCaller {
  const sender = event?.sender;
  if (
    !sender ||
    sender.isDestroyed?.() ||
    (typeof sender.getType === 'function' && sender.getType() !== 'window') ||
    event?.senderFrame?.parent
  ) {
    throw new Error('Unauthorized WCV IPC caller');
  }
  // Not SSH windows: these tabs use the laptop's browser profiles.
  if (isRemoteContents(sender)) {
    throw new Error('Unauthorized WCV IPC caller: SSH window');
  }
  const win = resolveHubWindowFromWebContents(sender);
  if (!win || getHubWebContents(win) !== sender) {
    throw new Error('Unauthorized WCV IPC window sender');
  }
  if (tabKey !== undefined) {
    const entry = registry.getEntry(tabKey);
    if (entry && entry.hubSender.id !== sender.id) {
      throw new Error(`WCV tab ${tabKey} is not owned by calling renderer`);
    }
    const evictedSenderId = !entry
      ? registry.getEvictedSenderId(tabKey)
      : undefined;
    if (evictedSenderId !== undefined && evictedSenderId !== sender.id) {
      throw new Error(`WCV tab ${tabKey} is not owned by calling renderer`);
    }
    return { win, sender, entry };
  }
  return { win, sender };
}

function resolveHubSenderZoomFactor(event: WcvIpcEventLike): number {
  if (event.sender?.isDestroyed?.()) {
    return 1;
  }
  const zoom = event.sender?.getZoomFactor?.();
  return typeof zoom === 'number' && Number.isFinite(zoom) && zoom > 0
    ? zoom
    : 1;
}

/**
 * Converts a rect the hub renderer measured in its own CSS px into the DIP that
 * `WebContentsView.setBounds` takes. Hub zoom makes one CSS px span
 * `zoomFactor` DIP; a missing or invalid factor is treated as 1.
 */
function hubCssToDip(event: WcvIpcEventLike, boundsCss: Rectangle): Rectangle {
  const zoom = resolveHubSenderZoomFactor(event);
  if (zoom === 1) {
    return boundsCss;
  }
  // Round edges rather than sizes so the right/bottom edge cannot drift 1 DIP
  // from the pane it covers.
  const x = Math.round(boundsCss.x * zoom);
  const y = Math.round(boundsCss.y * zoom);
  return {
    x,
    y,
    width: Math.round((boundsCss.x + boundsCss.width) * zoom) - x,
    height: Math.round((boundsCss.y + boundsCss.height) * zoom) - y,
  };
}

/**
 * Converts `WcvCdpMouseParams` measured in hub renderer CSS pixels into guest
 * DIP when `coordSpace === 'hub-css'`, leaving guest-DIP or untagged payloads
 * untouched.
 */
function hubCssMouseToDip(
  event: WcvIpcEventLike,
  params: WcvCdpMouseParams,
  coordSpace?: string,
): WcvCdpMouseParams {
  if (coordSpace !== 'hub-css' || !params || typeof params !== 'object') {
    return params;
  }
  const zoom = resolveHubSenderZoomFactor(event);
  if (zoom === 1) {
    return params;
  }
  const x =
    typeof params.x === 'number' && Number.isFinite(params.x) && params.x >= 0
      ? Math.round(params.x * zoom)
      : params.x;
  const y =
    typeof params.y === 'number' && Number.isFinite(params.y) && params.y >= 0
      ? Math.round(params.y * zoom)
      : params.y;
  const scaled: WcvCdpMouseParams = { ...params, x, y };
  if (typeof params.deltaX === 'number' && Number.isFinite(params.deltaX)) {
    scaled.deltaX = Math.round(params.deltaX * zoom) || 0;
  }
  if (typeof params.deltaY === 'number' && Number.isFinite(params.deltaY)) {
    scaled.deltaY = Math.round(params.deltaY * zoom) || 0;
  }
  return scaled;
}

/**
 * Authorizes a tab-scoped agent operation and runs it on a live tab. A hidden
 * tab evicted under the hidden-tab count or memory budget keeps only its URL
 * and title, so CDP input and `capturePage` would silently do nothing;
 * recreate it at its saved URL (or wait for a rehydration already loading)
 * first. The tab stays exempt from eviction until `op` settles.
 */
async function runOnLiveAgentTab<T>(
  event: IpcMainInvokeEvent | IpcMainEvent,
  registry: WcvIpcRegistry,
  tabKey: string,
  op: () => Promise<T> | T,
): Promise<T> {
  const { win, sender } = assertAuthorizedWcvCaller(event, registry, tabKey);
  return registry.withOperationPin(tabKey, async () => {
    await registry.rehydrateEvicted(win, sender, tabKey);
    return op();
  });
}

export function registerWcvIpcHandlers(
  registry: WcvIpcRegistry = wcvBrowserRegistry,
  controller: WcvIpcZOrderController = wcvZOrderController,
  storageManager?: WcvIpcStorageManager | null,
): () => void {
  const handleChannels: string[] = [];
  const registerHandle = (
    channel: string,
    listener: (event: IpcMainInvokeEvent, ...args: never[]) => unknown,
  ): void => {
    const handler = listener as (
      event: IpcMainInvokeEvent,
      ...args: unknown[]
    ) => unknown;
    ipcMain.handle(channel, handler);
    handleChannels.push(channel);
  };
  const onListeners: Array<
    [string, (event: IpcMainEvent, ...args: unknown[]) => void]
  > = [];
  const registerOn = (
    channel: string,
    fn: (event: IpcMainEvent, ...args: never[]) => void,
  ): void => {
    const listener = fn as (event: IpcMainEvent, ...args: unknown[]) => void;
    ipcMain.on(channel, listener);
    onListeners.push([channel, listener]);
  };
  guestPromptController.setPromptVisibilityListener((tabKey, open) => {
    controller.setHumanPromptOpen(tabKey, open);
  });
  // Latest `wcv:set-agent-controlling(true)` call per tab. A step start that
  // is still waiting for rehydration applies only if no later call (such as
  // the step end) arrived meanwhile, so it cannot leave the tab
  // agent-controlling after the step.
  let agentControllingCalls = 0;
  const latestAgentControllingCall = new Map<string, number>();

  registerHandle(
    'wcv:acquire',
    (event, req?: WcvIpcAcquireRequest): Promise<WcvTabSnapshot> => {
      const { win, sender } = assertAuthorizedWcvCaller(
        event,
        registry,
        req?.tabKey,
      );
      const { webmcpEnabled, ...rest } = req ?? ({} as WcvIpcAcquireRequest);
      if (typeof webmcpEnabled === 'boolean') {
        if (!isAuthorizedHubMainFrameEvent(event)) {
          throw new Error('Unauthorized wcv:acquire webmcpEnabled sender');
        }
        guestWebMcpController.setEnabled(webmcpEnabled);
      }
      return registry.acquire(win, sender, rest).then((snap) => {
        controller.reconcileWindow(win, 'tab-acquired');
        return snap;
      });
    },
  );
  registerHandle(
    'wcv:show',
    (
      event,
      tabKey: string,
      boundsCss: Rectangle,
    ): WcvTabSnapshot | Promise<WcvTabSnapshot | null> | null => {
      const { win, sender } = assertAuthorizedWcvCaller(
        event,
        registry,
        tabKey,
      );
      const boundsDip = hubCssToDip(event, boundsCss);
      const snap = registry.show(tabKey, boundsDip, win);
      controller.reconcileWindow(win, 'tab-shown');
      if (snap) {
        return snap;
      }
      if (registry.hasEvicted(tabKey, sender.id)) {
        return registry
          .rehydrateEvictedAndShow(win, sender, tabKey, boundsDip)
          .then((rehydrated) => {
            controller.reconcileWindow(win, 'tab-shown');
            return rehydrated;
          });
      }
      return null;
    },
  );
  registerHandle('wcv:hide', (event, tabKey: string): void => {
    const { win } = assertAuthorizedWcvCaller(event, registry, tabKey);
    registry.hide(tabKey);
    controller.revokeFocusGrant(tabKey);
    controller.reconcileWindow(win, 'tab-hidden');
  });
  registerOn(
    'wcv:set-bounds-sync',
    (event, tabKey: string, boundsCss: Rectangle): void => {
      try {
        const { win, entry } = assertAuthorizedWcvCaller(
          event,
          registry,
          tabKey,
        );
        const apply = (): void =>
          registry.setBounds(tabKey, hubCssToDip(event, boundsCss));
        if (!getHubCatcherView(win)) {
          apply();
          return;
        }
        const ok =
          isPlainObject(boundsCss) &&
          [
            boundsCss.x,
            boundsCss.y,
            boundsCss.width,
            boundsCss.height,
          ].every(isFiniteNum);
        if (!ok) {
          return;
        }
        apply();
        if (entry && getHubCatcherView(entry.parentWindow)) {
          controller.syncCatcherSlotBounds(entry.parentWindow);
        }
      } catch {
        // Ignore unauthorized one-way callers without crashing main process.
      }
    },
  );
  registerHandle(
    'wcv:set-theme-color',
    async (event, color: unknown): Promise<boolean> => {
      const { win } = assertAuthorizedWcvCaller(event, registry);
      const normalized = setHubCurtainColor(win, color);
      if (!normalized) {
        return false;
      }
      await Promise.resolve(
        storageManager?.updateItems({
          [HUB_SURFACE_COLOR_STORAGE_KEY]: normalized,
        }),
      ).catch(() => undefined);
      return true;
    },
  );
  registerHandle(
    'wcv:set-pinned',
    (event, tabKey: string, pinned: boolean): void => {
      assertAuthorizedWcvCaller(event, registry, tabKey);
      registry.setPinned(tabKey, pinned);
    },
  );
  registerHandle(
    'wcv:set-agent-controlling',
    async (
      event,
      tabKey: string,
      active: boolean,
    ): Promise<WcvZOrderState | null> => {
      assertAuthorizedWcvCaller(event, registry, tabKey);
      if (!active) {
        latestAgentControllingCall.delete(tabKey);
        return controller.setAgentControlling(tabKey, false);
      }
      const call = ++agentControllingCalls;
      latestAgentControllingCall.set(tabKey, call);
      // Rehydrating at step start also lets the step-active flag protect the
      // tab from eviction for the rest of the step.
      return runOnLiveAgentTab(event, registry, tabKey, () => {
        if (latestAgentControllingCall.get(tabKey) !== call) {
          return controller.getState(tabKey);
        }
        latestAgentControllingCall.delete(tabKey);
        return controller.setAgentControlling(tabKey, true);
      });
    },
  );
  registerHandle(
    'wcv:set-overlay-active',
    (event, tabKey: string, active: boolean): WcvZOrderState | null => {
      assertAuthorizedWcvCaller(event, registry, tabKey);
      return controller.setOverlayActive(tabKey, active);
    },
  );
  registerHandle(
    'wcv:set-portal-overlay-open',
    (event, open: boolean): void => {
      const { win } = assertAuthorizedWcvCaller(event, registry);
      controller.setPortalOverlayOpen(win, open);
    },
  );
  registerHandle(
    'wcv:elevate',
    (
      event,
      tabKey: string,
      reason?: WcvZOrderReason,
      focusBrowser?: boolean,
    ): WcvZOrderState | null => {
      assertAuthorizedWcvCaller(event, registry, tabKey);
      return controller.elevate(tabKey, reason, focusBrowser);
    },
  );
  registerHandle(
    'wcv:set-drag-over',
    (event, tabKey: string, active: boolean): WcvZOrderState | null => {
      assertAuthorizedWcvCaller(event, registry, tabKey);
      return controller.setDragOverActive(tabKey, active);
    },
  );
  registerHandle(
    'wcv:demote',
    (event, tabKey: string, reason?: WcvZOrderReason): WcvZOrderState | null => {
      assertAuthorizedWcvCaller(event, registry, tabKey);
      return controller.demote(tabKey, reason);
    },
  );
  registerHandle(
    'wcv:navigate',
    async (event, tabKey: string, url: string): Promise<boolean> => {
      return runOnLiveAgentTab(event, registry, tabKey, () =>
        controller.navigate(tabKey, url),
      );
    },
  );
  registerHandle(
    'wcv:control',
    (
      event,
      tabKey: string,
      action: WcvControlAction | (string & {}),
    ): void => {
      const { entry } = assertAuthorizedWcvCaller(event, registry, tabKey);
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const wc = entry?.view.webContents;
      if (!wc || wc.isDestroyed()) {
        return;
      }
      const history = wc.navigationHistory;
      switch (action) {
        case 'goBack':
        case 'back':
          if (history?.canGoBack) {
            if (history.canGoBack()) {
              history.goBack?.();
            }
          } else if (wc.canGoBack?.()) {
            wc.goBack?.();
          }
          break;
        case 'goForward':
        case 'forward':
          if (history?.canGoForward) {
            if (history.canGoForward()) {
              history.goForward?.();
            }
          } else if (wc.canGoForward?.()) {
            wc.goForward?.();
          }
          break;
        case 'reload':
          wc.reload?.();
          break;
        case 'stop':
          wc.stop?.();
          break;
        case 'openDevTools':
          // Agent-driven tabs must never expose a DevTools surface.
          if (!isGuestAgentControlled(wc.id)) {
            wc.openDevTools?.({ mode: 'detach' });
          }
          break;
        case 'mute':
          registry.setUserMuted(tabKey, true);
          break;
        case 'unmute':
          registry.setUserMuted(tabKey, false);
          break;
        case 'cut':
          wc.cut?.();
          break;
        case 'copy':
          wc.copy?.();
          break;
        case 'paste':
          wc.paste?.();
          break;
        case 'selectAll':
          wc.selectAll?.();
          break;
      }
    },
  );
  registerHandle(
    'wcv:send-mouse',
    async (
      event,
      tabKey: string,
      params: WcvCdpMouseParams,
      coordSpace?: string,
    ): Promise<void> => {
      return runOnLiveAgentTab(event, registry, tabKey, () =>
        controller.dispatchMouseEvent(
          tabKey,
          hubCssMouseToDip(event, params, coordSpace),
        ),
      );
    },
  );
  registerHandle(
    'wcv:send-key',
    async (
      event,
      tabKey: string,
      params: WcvCdpKeyParams,
    ): Promise<void> => {
      return runOnLiveAgentTab(event, registry, tabKey, () =>
        controller.dispatchKeyEvent(tabKey, params),
      );
    },
  );
  registerHandle(
    'wcv:insert-text',
    async (event, tabKey: string, text: string): Promise<void> => {
      return runOnLiveAgentTab(event, registry, tabKey, () =>
        controller.insertText(tabKey, text),
      );
    },
  );
  registerHandle(
    'wcv:set-viewport-size',
    (event, tabKey: string, size: WcvSize | null): Promise<boolean> => {
      assertAuthorizedWcvCaller(event, registry, tabKey);
      if (
        size !== null &&
        (!size ||
          typeof size !== 'object' ||
          typeof size.width !== 'number' ||
          typeof size.height !== 'number' ||
          !Number.isFinite(size.width) ||
          !Number.isFinite(size.height))
      ) {
        throw new Error('Invalid WCV viewport size');
      }
      return controller.setViewportSize(tabKey, size);
    },
  );
  registerHandle(
    'wcv:capture-page',
    async (event, tabKey: string): Promise<string | null> => {
      return runOnLiveAgentTab(event, registry, tabKey, () =>
        controller.capturePage(tabKey),
      );
    },
  );
  registerHandle('wcv:dispose', (event, tabKey: string): void => {
    const { win } = assertAuthorizedWcvCaller(event, registry, tabKey);
    controller.revokeFocusGrant(tabKey);
    registry.dispose(tabKey);
    controller.reconcileWindow(win, 'disposed');
  });

  registerOn(WCV_HUB_CURSOR_HOLE_STATE_CHANNEL, (event, p: unknown): void => {
    const win = resolveAuthorizedHubCatcherTargetWindow(event);
    if (win && isHubCursorHoleStatePayload(p)) {
      controller.handleHubCursorHoleState(win, p);
    }
  });
  registerOn(
    WCV_HUB_PROBE_POINT_RESPONSE_CHANNEL,
    (event, p: unknown): void => {
      const win = resolveAuthorizedHubCatcherTargetWindow(event);
      if (win && isHubProbePointResponsePayload(p)) {
        controller.handleHubProbePointResponse(win, p);
      }
    },
  );
  registerOn(WCV_CATCHER_POINTER_DOWN_CHANNEL, (event, p: unknown): void => {
    const win = resolveAuthorizedCatcherWindow(event);
    if (win && isCatcherPointerDownPayload(p)) {
      void controller.handleCatcherPointerDown(win, p);
    }
  });
  registerOn(WCV_CATCHER_POINTER_MOVE_CHANNEL, (event, p: unknown): void => {
    const win = resolveAuthorizedCatcherWindow(event);
    if (win && isCatcherPointerMovePayload(p)) {
      controller.handleCatcherPointerMove(win, p);
    }
  });
  registerOn(WCV_CATCHER_POINTER_UP_CHANNEL, (event, p: unknown): void => {
    const win = resolveAuthorizedCatcherWindow(event);
    if (win && isCatcherPointerUpPayload(p)) {
      void controller.handleCatcherPointerUp(win, p);
    }
  });
  registerOn(WCV_CATCHER_POINTER_LEAVE_CHANNEL, (event): void => {
    const win = resolveAuthorizedCatcherWindow(event);
    if (win) {
      controller.handleCatcherPointerLeave(win);
    }
  });
  registerOn(WCV_CATCHER_WHEEL_CHANNEL, (event, p: unknown): void => {
    const win = resolveAuthorizedCatcherWindow(event);
    if (win && isCatcherWheelPayload(p)) {
      controller.handleCatcherWheel(win, p);
    }
  });
  registerOn(WCV_CATCHER_PINCH_CHANNEL, (event, p: unknown): void => {
    const win = resolveAuthorizedCatcherWindow(event);
    if (win && isCatcherPinchPayload(p)) {
      controller.handleCatcherPinch(win, p);
    }
  });
  registerOn(WCV_CATCHER_DRAG_CHANNEL, (event, p: unknown): void => {
    const win = resolveAuthorizedCatcherWindow(event);
    const parsed = win ? parseCatcherDragPayload(p) : null;
    if (win && parsed) {
      void controller.handleCatcherDrag(win, parsed);
    }
  });

  let cleanedUp = false;
  return () => {
    if (cleanedUp) {
      return;
    }
    cleanedUp = true;
    for (const channel of handleChannels) {
      ipcMain.removeHandler(channel);
    }
    for (const [channel, listener] of onListeners) {
      ipcMain.removeListener(channel, listener);
    }
  };
}
