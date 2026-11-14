import { ipcRenderer, webUtils } from 'electron';

export const WCV_CATCHER_POINTER_DOWN_CHANNEL = 'wcv:catcher-pointer-down';
export const WCV_CATCHER_POINTER_MOVE_CHANNEL = 'wcv:catcher-pointer-move';
export const WCV_CATCHER_POINTER_UP_CHANNEL = 'wcv:catcher-pointer-up';
export const WCV_CATCHER_POINTER_LEAVE_CHANNEL = 'wcv:catcher-pointer-leave';
export const WCV_CATCHER_WHEEL_CHANNEL = 'wcv:catcher-wheel';
export const WCV_CATCHER_PINCH_CHANNEL = 'wcv:catcher-pinch';
export const WCV_CATCHER_RESET_DRAG_CHANNEL = 'wcv:catcher-reset-drag';
export const WCV_CATCHER_SET_CURSOR_CHANNEL = 'wcv:catcher-set-cursor';
export const WCV_CATCHER_DRAG_CHANNEL = 'wcv:catcher-drag';

export type CatcherDownButton = 'left' | 'middle' | 'right' | 'back' | 'forward';
export type CatcherDragButton = 'left' | 'middle' | 'right';
export type CatcherDragPhase = 'enter' | 'over' | 'leave' | 'drop';

export interface MultiClickTrackerState {
  lastTimeMs: number;
  lastX: number;
  lastY: number;
  lastButton: number;
  count: number;
}

export interface CatcherModifierEvent {
  altKey?: boolean;
  ctrlKey?: boolean;
  metaKey?: boolean;
  shiftKey?: boolean;
}

export interface CatcherPointerDownPayload {
  pressId: number;
  x: number;
  y: number;
  button: CatcherDownButton;
  rawButton: number;
  clickCount: number;
  modifiers: number;
}

export interface CatcherPointerMovePayload {
  pressId: number | null;
  isDragging: boolean;
  x: number;
  y: number;
  button: CatcherDragButton | 'none';
  modifiers: number;
}

export interface CatcherPointerUpPayload {
  pressId: number;
  x: number;
  y: number;
  button: CatcherDragButton;
  clickCount: number;
  modifiers: number;
  outOfBounds: boolean;
}

export interface CatcherWheelPayload {
  x: number;
  y: number;
  deltaX: number;
  deltaY: number;
  modifiers: number;
}

export interface CatcherPinchPayload {
  x: number;
  y: number;
  scaleFactor: number;
}

export interface CatcherDragDropItem {
  mimeType: string;
  data: string;
}

export interface CatcherDragMoveOrLeavePayload {
  phase: 'enter' | 'over' | 'leave';
  x: number;
  y: number;
  modifiers: number;
  types: string[];
  hasFiles: boolean;
}

export interface CatcherDragDropPayload {
  phase: 'drop';
  x: number;
  y: number;
  modifiers: number;
  types: string[];
  hasFiles: boolean;
  files: string[];
  items: CatcherDragDropItem[];
}

export type CatcherDragPayload =
  | CatcherDragMoveOrLeavePayload
  | CatcherDragDropPayload;

export type CatcherIpcPayload =
  | CatcherPointerDownPayload
  | CatcherPointerMovePayload
  | CatcherPointerUpPayload
  | CatcherWheelPayload
  | CatcherPinchPayload
  | CatcherDragPayload;

export type CatcherIpcListener = (...args: unknown[]) => void;

export interface CatcherIpc {
  send: (channel: string, payload?: unknown) => void;
  on?: (channel: string, listener: CatcherIpcListener) => unknown;
  removeListener?: (channel: string, listener: CatcherIpcListener) => unknown;
}

export interface CatcherCaptureElement {
  readonly style?: { cursor: string } | null;
  setPointerCapture?: (pointerId: number) => void;
  releasePointerCapture?: (pointerId: number) => void;
}

export interface CatcherEventTarget {
  addEventListener?: (
    type: string,
    listener: EventListener,
    options?: boolean | AddEventListenerOptions,
  ) => void;
  removeEventListener?: (
    type: string,
    listener: EventListener,
    options?: boolean | EventListenerOptions,
  ) => void;
}

export interface CatcherDocumentTarget extends CatcherEventTarget {
  readonly documentElement?: CatcherCaptureElement | null;
  readonly body?: CatcherCaptureElement | null;
}

export interface CatcherWindowTarget extends CatcherEventTarget {
  readonly innerWidth?: number;
  readonly innerHeight?: number;
  readonly document?: CatcherDocumentTarget | null;
  addEventListener: (
    type: string,
    listener: EventListener,
    options?: boolean | AddEventListenerOptions,
  ) => void;
}

interface ActiveDragState {
  pressId: number;
  pointerId: number | null;
  capEl: CatcherCaptureElement | null;
  button: CatcherDragButton;
  clickCount: number;
  x: number;
  y: number;
}

interface CatcherDragBase {
  x: number;
  y: number;
  modifiers: number;
  types: string[];
  hasFiles: boolean;
}

type CatcherEventEntry = [
  string,
  EventListener,
  boolean | AddEventListenerOptions,
];

const MULTI_CLICK_MAX_INTERVAL_MS = 500;
const MULTI_CLICK_MAX_DISTANCE_DIP = 5;
const WHEEL_OPTS: AddEventListenerOptions = { capture: true, passive: false };
const SUPPRESSED = ['mousedown', 'mouseup', 'auxclick', 'contextmenu'] as const;
const BUTTONS: readonly CatcherDownButton[] = [
  'left',
  'middle',
  'right',
  'back',
  'forward',
];

const resolveFilePath = (file: File): string =>
  typeof webUtils?.getPathForFile === 'function'
    ? webUtils.getPathForFile(file)
    : '';

const safeStr = (fn: () => unknown): string | null => {
  try {
    const v = fn();
    return typeof v === 'string' ? v : null;
  } catch {
    return null;
  }
};

/** Creates an initialized multi-click state record with zero prior presses. */
export function createMultiClickTrackerState(): MultiClickTrackerState {
  return { lastTimeMs: -Infinity, lastX: 0, lastY: 0, lastButton: -1, count: 0 };
}

/** Updates `state` and returns the 1-to-3 multi-click count within `500 ms` and `5 DIP` on `button`. */
export function nextMultiClickCount(
  state: MultiClickTrackerState,
  nowMs: number,
  x: number,
  y: number,
  button: number,
): number {
  const dt = nowMs - state.lastTimeMs;
  const ok =
    state.count > 0 &&
    button === state.lastButton &&
    dt >= 0 &&
    dt <= MULTI_CLICK_MAX_INTERVAL_MS &&
    Math.hypot(x - state.lastX, y - state.lastY) <=
      MULTI_CLICK_MAX_DISTANCE_DIP;
  state.count = ok ? (state.count % 3) + 1 : 1;
  Object.assign(state, {
    lastTimeMs: nowMs,
    lastX: x,
    lastY: y,
    lastButton: button,
  });
  return state.count;
}

/** Encodes DOM modifier flags into the CDP mouse bitmask (`Alt=1 | Ctrl=2 | Meta=4 | Shift=8`). */
export function toCatcherMouseModifiers(ev: CatcherModifierEvent): number {
  const { altKey: a, ctrlKey: c, metaKey: m, shiftKey: s } = ev;
  return (a ? 1 : 0) | (c ? 2 : 0) | (m ? 4 : 0) | (s ? 8 : 0);
}

/** Normalizes wheel `deltaY` by `deltaMode` and maps `ctrlKey` wheel ticks into a pinch scale factor clamped to `[1 / 1.2, 1.2]`. */
export function computePinchScaleFactor(
  deltaY: number,
  deltaMode = 0,
): number {
  const raw = typeof deltaY === 'number' && Number.isFinite(deltaY) ? deltaY : 0;
  const d = deltaMode === 1 ? raw * 40 : deltaMode === 2 ? raw * 800 : raw;
  return Math.max(1 / 1.2, Math.min(1.2, Math.exp(-d * 0.01)));
}

function mapRawMouseButton(rawButton: number): CatcherDownButton | null {
  return Number.isInteger(rawButton) && rawButton >= 0 && rawButton <= 4
    ? BUTTONS[rawButton]
    : null;
}

const installedTargets = new WeakSet<object>();

/** Attaches pointer, pointer-capture drag, wheel/pinch, and DOM drag IPC listeners to `g`, returning an idempotent teardown callback. */
export function installCatcherInputBridge(
  g: CatcherWindowTarget | null | undefined,
  ipc: CatcherIpc | null | undefined,
  now: () => number = () => Date.now(),
  getPathForFile: (file: File) => string = resolveFilePath,
): () => void {
  if (
    !g ||
    typeof g !== 'object' ||
    typeof g.addEventListener !== 'function' ||
    typeof ipc?.send !== 'function' ||
    installedTargets.has(g)
  ) {
    return () => {};
  }
  installedTargets.add(g);

  const clicks = createMultiClickTrackerState();
  let nextPressId = 0;
  let activeDrag: ActiveDragState | null = null;
  let lastDragKey: string | null = null;

  const emit = (channel: string, payload: CatcherIpcPayload): void =>
    ipc.send(channel, payload);
  const toCoord = (v: unknown, fallback = 0): number =>
    typeof v === 'number' && Number.isFinite(v) ? v || 0 : fallback;
  const isOut = (x: number, y: number): boolean => {
    const w = toCoord(g.innerWidth);
    const h = toCoord(g.innerHeight);
    return w <= 0 || h <= 0
      ? x < 0 || y < 0
      : x < 0 || y < 0 || x >= w || y >= h;
  };
  const isForeign = (ev: Partial<PointerEvent>): boolean =>
    activeDrag !== null &&
    activeDrag.pointerId !== null &&
    typeof ev.pointerId === 'number' &&
    ev.pointerId !== activeDrag.pointerId;

  const clearDrag = (rel: boolean): ActiveDragState | null => {
    const prev = activeDrag;
    activeDrag = null;
    if (prev && rel && prev.pointerId !== null) {
      try {
        prev.capEl?.releasePointerCapture?.(prev.pointerId);
      } catch {
        // Pointer capture may already have been revoked by Chromium.
      }
    }
    return prev;
  };

  const emitUp = (
    x: number,
    y: number,
    modifiers: number,
    out: boolean,
    rel = true,
  ): void => {
    const prev = clearDrag(rel);
    if (prev) {
      const { pressId, button, clickCount } = prev;
      const payload = { pressId, x, y, button, clickCount, modifiers };
      emit(WCV_CATCHER_POINTER_UP_CHANNEL, { ...payload, outOfBounds: out });
    }
  };

  const releaseEv = (
    ev: Partial<PointerEvent>,
    forceOut: boolean,
    rel: boolean,
  ): void => {
    if (activeDrag !== null) {
      const x = toCoord(ev.clientX, activeDrag.x);
      const y = toCoord(ev.clientY, activeDrag.y);
      emitUp(x, y, toCatcherMouseModifiers(ev), forceOut || isOut(x, y), rel);
    }
  };

  const onResetDrag = (): void => {
    lastDragKey = null;
    clearDrag(true);
  };

  const applyCursor = (cursor: string): void => {
    if (g.document?.documentElement?.style) {
      g.document.documentElement.style.cursor = cursor;
    }
    if (g.document?.body?.style) {
      g.document.body.style.cursor = cursor;
    }
  };

  const onSetCursor = (_ev?: unknown, rawCursor?: unknown): void => {
    const next =
      typeof rawCursor === 'string' && rawCursor.trim()
        ? rawCursor.trim()
        : 'default';
    applyCursor(next);
  };

  const onPointerDown = (ev: Partial<PointerEvent>): void => {
    ev.preventDefault?.();
    if (isForeign(ev)) {
      return;
    }
    const rawButton = typeof ev.button === 'number' ? ev.button : 0;
    const button = mapRawMouseButton(rawButton);
    if (!button) {
      return;
    }
    const x = toCoord(ev.clientX);
    const y = toCoord(ev.clientY);
    const modifiers = toCatcherMouseModifiers(ev);
    // Chorded buttons on an already-depressed mouse fire `pointermove`, never
    // `pointerdown` (W3C Pointer Events §10), so a new `pointerdown` while
    // `activeDrag` is non-null means the prior release was missed.
    if (activeDrag !== null) {
      emitUp(x, y, modifiers, isOut(x, y));
    }
    const pressId = ++nextPressId;
    if (button === 'back' || button === 'forward') {
      clicks.count = 0;
      const payload: CatcherPointerDownPayload = {
        pressId,
        x,
        y,
        button,
        rawButton,
        clickCount: 1,
        modifiers,
      };
      emit(WCV_CATCHER_POINTER_DOWN_CHANNEL, payload);
      return;
    }
    const clickCount = nextMultiClickCount(clicks, now(), x, y, rawButton);
    const pointerId = typeof ev.pointerId === 'number' ? ev.pointerId : null;
    const doc = pointerId === null ? null : g.document;
    const capEl = doc?.documentElement ?? doc?.body ?? null;
    if (capEl && pointerId !== null) {
      try {
        capEl.setPointerCapture?.(pointerId);
      } catch {
        // Synthetic events may carry a pointerId without an active OS pointer.
      }
    }
    activeDrag = { pressId, pointerId, capEl, button, clickCount, x, y };
    const payload: CatcherPointerDownPayload = {
      pressId,
      x,
      y,
      button,
      rawButton,
      clickCount,
      modifiers,
    };
    emit(WCV_CATCHER_POINTER_DOWN_CHANNEL, payload);
  };

  const onPointerMove = (ev: Partial<PointerEvent>): void => {
    if (isForeign(ev)) {
      return;
    }
    const x = toCoord(ev.clientX, activeDrag ? activeDrag.x : 0);
    const y = toCoord(ev.clientY, activeDrag ? activeDrag.y : 0);
    const modifiers = toCatcherMouseModifiers(ev);
    if (activeDrag !== null && ev.buttons === 0) {
      const out = isOut(x, y);
      emitUp(x, y, modifiers, out);
      if (out) {
        return;
      }
    }
    if (activeDrag !== null) {
      activeDrag.x = x;
      activeDrag.y = y;
      const d = Math.hypot(x - clicks.lastX, y - clicks.lastY);
      if (d > MULTI_CLICK_MAX_DISTANCE_DIP) {
        clicks.count = 0;
      }
    }
    const isDragging = activeDrag !== null;
    const pressId = activeDrag ? activeDrag.pressId : null;
    const button: CatcherDragButton | 'none' = activeDrag
      ? activeDrag.button
      : 'none';
    const payload: CatcherPointerMovePayload = {
      pressId,
      isDragging,
      x,
      y,
      button,
      modifiers,
    };
    emit(WCV_CATCHER_POINTER_MOVE_CHANNEL, payload);
  };

  const finishPointerDrag = (ev: Partial<PointerEvent>): void => {
    if (isForeign(ev)) {
      return;
    }
    if (ev.button === 3 || ev.button === 4) {
      // Chromium fires `pointerup` only when all held buttons on the shared
      // mouse `pointerId` reach `buttons === 0`, so prevent default history
      // navigation without aborting release of a chorded primary drag.
      ev.preventDefault?.();
    }
    releaseEv(ev, ev.type === 'pointercancel', true);
  };

  const onLostCapture = (ev: Partial<PointerEvent>): void => {
    if (!isForeign(ev)) {
      releaseEv(ev, true, false);
    }
  };

  const onPointerLeave = (): void => {
    if (activeDrag === null) {
      ipc.send(WCV_CATCHER_POINTER_LEAVE_CHANNEL);
    }
  };

  const onWheel = (ev: Partial<WheelEvent>): void => {
    if (ev.isTrusted === false) {
      return;
    }
    const x = toCoord(ev.clientX);
    const y = toCoord(ev.clientY);
    const deltaY = toCoord(ev.deltaY);
    if (ev.ctrlKey) {
      ev.preventDefault?.();
      const scaleFactor = computePinchScaleFactor(deltaY, ev.deltaMode);
      emit(WCV_CATCHER_PINCH_CHANNEL, { x, y, scaleFactor });
      return;
    }
    const deltaX = toCoord(ev.deltaX);
    const modifiers = toCatcherMouseModifiers(ev);
    emit(WCV_CATCHER_WHEEL_CHANNEL, { x, y, deltaX, deltaY, modifiers });
  };

  const readDrag = (ev: Partial<DragEvent>): CatcherDragBase => {
    const dt = ev.dataTransfer;
    const types = Array.from(dt?.types ?? []).filter(
      (t) => typeof t === 'string' && t.length > 0,
    );
    const hasFiles =
      types.includes('Files') ||
      Array.from(dt?.items ?? []).some((it) => it?.kind === 'file') ||
      Array.from(dt?.files ?? []).length > 0;
    const [x, y] = [toCoord(ev.clientX), toCoord(ev.clientY)];
    return { x, y, modifiers: toCatcherMouseModifiers(ev), types, hasFiles };
  };

  const onDrag =
    (phase: 'enter' | 'over') =>
    (ev: Partial<DragEvent>): void => {
      if (ev.isTrusted !== false) {
        ev.preventDefault?.();
        const b = readDrag(ev);
        const k = `${phase}:${b.x}:${b.y}:${b.modifiers}:${b.hasFiles ? 1 : 0}:${b.types.join('\0')}`;
        if (phase !== 'over' || k !== lastDragKey) {
          lastDragKey = k;
          emit(WCV_CATCHER_DRAG_CHANNEL, { phase, ...b });
        }
      }
    };

  const onDragLeave = (ev: Partial<DragEvent>): void => {
    const out = ev.relatedTarget === null || ev.relatedTarget === undefined;
    if (ev.isTrusted !== false && out) {
      lastDragKey = null;
      emit(WCV_CATCHER_DRAG_CHANNEL, { phase: 'leave', ...readDrag(ev) });
    }
  };

  const onDrop = (ev: Partial<DragEvent>): void => {
    if (ev.isTrusted === false) {
      return;
    }
    ev.preventDefault?.();
    lastDragKey = null;
    const [dt, b] = [ev.dataTransfer, readDrag(ev)];
    const files = Array.from(dt?.files ?? [])
      .map((f) => safeStr(() => getPathForFile(f)) ?? '')
      .filter(Boolean);
    const items: CatcherDragDropItem[] = [];
    const getData = dt?.getData;
    if (typeof getData === 'function') {
      for (const mimeType of b.types.filter((t) => t !== 'Files')) {
        const data = safeStr(() => getData.call(dt, mimeType));
        if (data !== null) {
          items.push({ mimeType, data });
        }
      }
    }
    const hasFiles = b.hasFiles || files.length > 0;
    const payload: CatcherDragDropPayload = {
      phase: 'drop',
      ...b,
      hasFiles,
      files,
      items,
    };
    emit(WCV_CATCHER_DRAG_CHANNEL, payload);
  };

  const noDefault = (ev: Partial<Event>): void => void ev.preventDefault?.();
  const winEvents: CatcherEventEntry[] = [
    ['pointerdown', onPointerDown, true],
    ['pointermove', onPointerMove, true],
    ['pointerup', finishPointerDrag, true],
    ['pointercancel', finishPointerDrag, true],
    ['lostpointercapture', onLostCapture, true],
    ['wheel', onWheel, WHEEL_OPTS],
    ['dragenter', onDrag('enter'), true],
    ['dragover', onDrag('over'), true],
    ['dragleave', onDragLeave, true],
    ['drop', onDrop, true],
    ...SUPPRESSED.map((t): CatcherEventEntry => [t, noDefault, true]),
  ];

  // Omit `window` `'blur'`: focusing the guest shortly after `pointerdown`
  // blurs the catcher window mid-click while OS pointer capture stays active.
  for (const [t, fn, opts] of winEvents) {
    g.addEventListener(t, fn, opts);
  }

  // Target-phase `pointerleave` on `document` works at `document_start` before
  // `documentElement` exists and ignores internal `<body>` crossings.
  const docTarget: CatcherEventTarget =
    typeof g.document?.addEventListener === 'function' ? g.document : g;
  docTarget.addEventListener?.('pointerleave', onPointerLeave);

  ipc.on?.(WCV_CATCHER_RESET_DRAG_CHANNEL, onResetDrag);
  ipc.on?.(WCV_CATCHER_SET_CURSOR_CHANNEL, onSetCursor);

  let tornDown = false;
  return (): void => {
    if (tornDown) {
      return;
    }
    tornDown = true;
    lastDragKey = null;
    clearDrag(true);
    ipc.removeListener?.(WCV_CATCHER_RESET_DRAG_CHANNEL, onResetDrag);
    ipc.removeListener?.(WCV_CATCHER_SET_CURSOR_CHANNEL, onSetCursor);
    applyCursor('');
    for (const [t, fn, opts] of winEvents) {
      g.removeEventListener?.(t, fn, opts);
    }
    docTarget.removeEventListener?.('pointerleave', onPointerLeave);
    installedTargets.delete(g);
  };
}

const defaultWin: CatcherWindowTarget | null = globalThis.window ?? null;
if (
  typeof defaultWin?.addEventListener === 'function' &&
  typeof ipcRenderer?.send === 'function'
) {
  const bind = (fn: CatcherIpc['on']): CatcherIpc['on'] =>
    typeof fn === 'function' ? fn.bind(ipcRenderer) : undefined;
  installCatcherInputBridge(defaultWin, {
    send: ipcRenderer.send.bind(ipcRenderer),
    on: bind(ipcRenderer.on),
    removeListener: bind(ipcRenderer.removeListener),
  });
}
