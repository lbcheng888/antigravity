import { screen, type Rectangle } from 'electron';
import * as fs from 'fs';
import * as path from 'path';
import { getWindowStatePath } from './paths';

export const DEFAULT_WINDOW_WIDTH = 1400;
export const DEFAULT_WINDOW_HEIGHT = 900;
export const MIN_WINDOW_WIDTH = 500;
export const MIN_WINDOW_HEIGHT = 400;

const MIN_VISIBLE_PX = 64;
const CASCADE_OFFSET_PX = 28;
const SAVE_DEBOUNCE_MS = 250;

export interface WindowBounds {
  x?: number;
  y?: number;
  width: number;
  height: number;
}

export interface WindowState extends WindowBounds {
  isMaximized: boolean;
  isFullScreen: boolean;
}

export interface DisplayLike {
  id?: number;
  bounds: Rectangle;
  workArea?: Rectangle;
}

export interface TrackableWindow {
  isDestroyed?(): boolean;
  isMinimized?(): boolean;
  isMaximized?(): boolean;
  isFullScreen?(): boolean;
  getBounds?(): Partial<Rectangle> | undefined;
  on?(event: 'resize', listener: () => void): unknown;
  on?(event: 'move', listener: () => void): unknown;
  on?(event: 'maximize', listener: () => void): unknown;
  on?(event: 'unmaximize', listener: () => void): unknown;
  on?(event: 'enter-full-screen', listener: () => void): unknown;
  on?(event: 'leave-full-screen', listener: () => void): unknown;
  on?(event: 'close', listener: () => void): unknown;
  on?(event: 'closed', listener: () => void): unknown;
  off?(event: 'resize', listener: () => void): unknown;
  off?(event: 'move', listener: () => void): unknown;
  off?(event: 'maximize', listener: () => void): unknown;
  off?(event: 'unmaximize', listener: () => void): unknown;
  off?(event: 'enter-full-screen', listener: () => void): unknown;
  off?(event: 'leave-full-screen', listener: () => void): unknown;
  off?(event: 'close', listener: () => void): unknown;
  off?(event: 'closed', listener: () => void): unknown;
}

export interface ResolveInitialWindowStateOptions {
  stateFilePath?: string;
  cascadeFromWindow?: TrackableWindow | null;
}

interface WindowTracker {
  win: TrackableWindow;
  clearTimer: () => void;
  captureAndSave: () => void;
  dispose: () => void;
}

const activeTrackers = new Map<TrackableWindow, WindowTracker>();

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value);
}

function isValidRectangle(rect: unknown): rect is Rectangle {
  if (!rect || typeof rect !== 'object') {
    return false;
  }
  const r = rect as Record<string, unknown>;
  return (
    isFiniteNumber(r.x) &&
    isFiniteNumber(r.y) &&
    isFiniteNumber(r.width) &&
    r.width > 0 &&
    isFiniteNumber(r.height) &&
    r.height > 0
  );
}

function getUsableArea(display: DisplayLike): Rectangle {
  if (isValidRectangle(display.workArea)) {
    return display.workArea;
  }
  return display.bounds;
}

function clampDimension(
  value: number,
  minValue: number,
  maxAvailable: number,
): number {
  const rounded = Math.max(minValue, Math.round(value));
  if (maxAvailable >= minValue) {
    return Math.min(rounded, Math.round(maxAvailable));
  }
  return rounded;
}

function findBestMatchingDisplay(
  rect: Rectangle,
  displays: readonly DisplayLike[],
): DisplayLike | undefined {
  let bestDisplay: DisplayLike | undefined;
  let bestArea = 0;
  for (const display of displays) {
    const area = getUsableArea(display);
    if (!isValidRectangle(area)) {
      continue;
    }
    const overlapWidth =
      Math.min(rect.x + rect.width, area.x + area.width) -
      Math.max(rect.x, area.x);
    const overlapHeight =
      Math.min(rect.y + rect.height, area.y + area.height) -
      Math.max(rect.y, area.y);
    if (overlapWidth >= MIN_VISIBLE_PX && overlapHeight >= MIN_VISIBLE_PX) {
      const overlapArea = overlapWidth * overlapHeight;
      if (overlapArea > bestArea) {
        bestArea = overlapArea;
        bestDisplay = display;
      }
    }
  }
  return bestDisplay;
}

/**
 * Validates and normalizes raw parsed JSON into a safe `WindowState`.
 */
export function parseWindowState(raw: unknown): WindowState {
  if (!raw || typeof raw !== 'object') {
    return {
      width: DEFAULT_WINDOW_WIDTH,
      height: DEFAULT_WINDOW_HEIGHT,
      isMaximized: false,
      isFullScreen: false,
    };
  }
  const record = raw as Record<string, unknown>;
  const width =
    isFiniteNumber(record.width) && record.width >= MIN_WINDOW_WIDTH
      ? Math.round(record.width)
      : DEFAULT_WINDOW_WIDTH;
  const height =
    isFiniteNumber(record.height) && record.height >= MIN_WINDOW_HEIGHT
      ? Math.round(record.height)
      : DEFAULT_WINDOW_HEIGHT;
  const hasCoordinates = isFiniteNumber(record.x) && isFiniteNumber(record.y);
  return {
    ...(hasCoordinates
      ? {
          x: Math.round(record.x as number),
          y: Math.round(record.y as number),
        }
      : {}),
    width,
    height,
    isMaximized: Boolean(record.isMaximized),
    isFullScreen: Boolean(record.isFullScreen),
  };
}

/**
 * Reads persisted window state from disk, returning defaults if missing or invalid.
 */
export function readPersistedWindowState(
  stateFilePath: string = getWindowStatePath(),
): WindowState {
  try {
    if (!fs.existsSync(stateFilePath)) {
      return parseWindowState(undefined);
    }
    const content = fs.readFileSync(stateFilePath, 'utf-8');
    if (!content || !content.trim()) {
      return parseWindowState(undefined);
    }
    return parseWindowState(JSON.parse(content));
  } catch {
    return parseWindowState(undefined);
  }
}

/**
 * Writes window state to disk on a best-effort basis without throwing.
 */
export function persistWindowState(
  state: Partial<WindowState>,
  stateFilePath: string = getWindowStatePath(),
): void {
  try {
    const normalized = parseWindowState(state);
    const dir = path.dirname(stateFilePath);
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true });
    }
    fs.writeFileSync(stateFilePath, JSON.stringify(normalized, null, 2), 'utf-8');
  } catch (err: unknown) {
    console.error('Failed to persist window state:', err);
  }
}

/**
 * Ensures `state` fits on one of the currently connected `displays`.
 * If the saved coordinates lie off-screen (e.g. an external monitor was
 * disconnected), falls back to default centered positioning on the primary
 * display while preserving `isMaximized` / `isFullScreen`.
 */
export function resolveWindowStateForDisplays(
  state: Partial<WindowState>,
  displays: readonly DisplayLike[],
  primaryDisplay?: DisplayLike,
): WindowState {
  const normalized = parseWindowState(state);
  if (displays.length === 0) {
    return normalized;
  }
  const fallbackDisplay = primaryDisplay ?? displays[0];
  const fallbackArea = getUsableArea(fallbackDisplay);
  const buildFallbackState = (): WindowState => ({
    width: isValidRectangle(fallbackArea)
      ? clampDimension(normalized.width, MIN_WINDOW_WIDTH, fallbackArea.width)
      : normalized.width,
    height: isValidRectangle(fallbackArea)
      ? clampDimension(
          normalized.height,
          MIN_WINDOW_HEIGHT,
          fallbackArea.height,
        )
      : normalized.height,
    isMaximized: normalized.isMaximized,
    isFullScreen: normalized.isFullScreen,
  });
  if (normalized.x === undefined || normalized.y === undefined) {
    return buildFallbackState();
  }
  const matchedDisplay = findBestMatchingDisplay(
    {
      x: normalized.x,
      y: normalized.y,
      width: normalized.width,
      height: normalized.height,
    },
    displays,
  );
  if (!matchedDisplay) {
    return buildFallbackState();
  }
  const area = getUsableArea(matchedDisplay);
  const width = clampDimension(normalized.width, MIN_WINDOW_WIDTH, area.width);
  const height = clampDimension(
    normalized.height,
    MIN_WINDOW_HEIGHT,
    area.height,
  );
  const minX = area.x + MIN_VISIBLE_PX - width;
  const maxX = area.x + area.width - MIN_VISIBLE_PX;
  const x = Math.max(minX, Math.min(normalized.x, maxX));
  // Keep the top edge inside `workArea` so the hidden titlebar drag region
  // remains reachable beneath any OS menu bar or top panel.
  const maxY = Math.max(area.y, area.y + area.height - MIN_VISIBLE_PX);
  const y = Math.max(area.y, Math.min(normalized.y, maxY));
  return {
    x,
    y,
    width,
    height,
    isMaximized: normalized.isMaximized,
    isFullScreen: normalized.isFullScreen,
  };
}

/**
 * Offsets `baseState` slightly when opening an additional window while another
 * Hub window is already open, wrapping within the display's usable area.
 */
export function cascadeWindowState(
  baseState: WindowBounds,
  displays: readonly DisplayLike[],
): WindowState {
  const width = baseState.width;
  const height = baseState.height;
  if (baseState.x === undefined || baseState.y === undefined) {
    return {
      width,
      height,
      isMaximized: false,
      isFullScreen: false,
    };
  }
  let nextX = baseState.x + CASCADE_OFFSET_PX;
  let nextY = baseState.y + CASCADE_OFFSET_PX;
  const matched = findBestMatchingDisplay(
    { x: baseState.x, y: baseState.y, width, height },
    displays,
  );
  if (matched) {
    const area = getUsableArea(matched);
    if (
      nextX + width > area.x + area.width ||
      nextY + height > area.y + area.height
    ) {
      nextX = area.x + CASCADE_OFFSET_PX;
      nextY = area.y + CASCADE_OFFSET_PX;
    }
    nextX = Math.max(area.x, Math.min(nextX, area.x + area.width - width));
    nextY = Math.max(area.y, Math.min(nextY, area.y + area.height - height));
  }
  return {
    x: nextX,
    y: nextY,
    width,
    height,
    isMaximized: false,
    isFullScreen: false,
  };
}

function getConnectedDisplays(): {
  displays: DisplayLike[];
  primaryDisplay: DisplayLike | undefined;
} {
  try {
    const displays =
      typeof screen?.getAllDisplays === 'function'
        ? screen.getAllDisplays()
        : [];
    const primaryDisplay =
      typeof screen?.getPrimaryDisplay === 'function'
        ? screen.getPrimaryDisplay()
        : displays[0];
    return { displays, primaryDisplay };
  } catch {
    return { displays: [], primaryDisplay: undefined };
  }
}

/**
 * Resolves the initial bounds and state to apply to a newly created Hub window.
 */
export function resolveInitialWindowState(
  options?: ResolveInitialWindowStateOptions,
): WindowState {
  const persisted = readPersistedWindowState(options?.stateFilePath);
  const { displays, primaryDisplay } = getConnectedDisplays();
  const resolved = resolveWindowStateForDisplays(
    persisted,
    displays,
    primaryDisplay,
  );
  const refWin = options?.cascadeFromWindow;
  if (!refWin || refWin.isDestroyed?.()) {
    return resolved;
  }
  let baseForCascade: WindowBounds = resolved;
  if (
    !refWin.isFullScreen?.() &&
    !refWin.isMaximized?.() &&
    !refWin.isMinimized?.()
  ) {
    const refBounds = refWin.getBounds?.();
    if (isValidRectangle(refBounds)) {
      baseForCascade = {
        x: Math.round(refBounds.x),
        y: Math.round(refBounds.y),
        width: clampDimension(
          refBounds.width,
          MIN_WINDOW_WIDTH,
          Number.MAX_SAFE_INTEGER,
        ),
        height: clampDimension(
          refBounds.height,
          MIN_WINDOW_HEIGHT,
          Number.MAX_SAFE_INTEGER,
        ),
      };
    }
  }
  return cascadeWindowState(baseForCascade, displays);
}

/**
 * Attaches listeners to `win` so moves, resizes, maximize, and full-screen
 * transitions are persisted to `window_state.json`.
 */
export function trackWindowState(
  win: TrackableWindow,
  initialState: WindowBounds &
    Partial<Pick<WindowState, 'isMaximized' | 'isFullScreen'>>,
  stateFilePath: string = getWindowStatePath(),
): void {
  activeTrackers.get(win)?.dispose();
  const state: WindowState = {
    ...(initialState.x !== undefined && initialState.y !== undefined
      ? { x: initialState.x, y: initialState.y }
      : {}),
    width: initialState.width,
    height: initialState.height,
    isMaximized: Boolean(initialState.isMaximized),
    isFullScreen: Boolean(initialState.isFullScreen),
  };
  if (
    (state.x === undefined || state.y === undefined) &&
    !win.isDestroyed?.() &&
    !win.isMinimized?.() &&
    !win.isMaximized?.() &&
    !win.isFullScreen?.()
  ) {
    const seeded = win.getBounds?.();
    if (isValidRectangle(seeded)) {
      state.x = Math.round(seeded.x);
      state.y = Math.round(seeded.y);
      state.width = Math.max(MIN_WINDOW_WIDTH, Math.round(seeded.width));
      state.height = Math.max(MIN_WINDOW_HEIGHT, Math.round(seeded.height));
    }
  }
  let debounceTimer: ReturnType<typeof setTimeout> | undefined;
  const clearTimer = (): void => {
    if (debounceTimer !== undefined) {
      clearTimeout(debounceTimer);
      debounceTimer = undefined;
    }
  };
  const captureCurrentState = (): boolean => {
    if (win.isDestroyed?.() || win.isMinimized?.()) {
      return false;
    }
    const isFullScreen = Boolean(win.isFullScreen?.());
    const isMaximized = isFullScreen
      ? Boolean(state.isMaximized)
      : Boolean(win.isMaximized?.());
    state.isFullScreen = isFullScreen;
    state.isMaximized = isMaximized;
    if (!isFullScreen && !isMaximized) {
      const bounds = win.getBounds?.();
      if (isValidRectangle(bounds)) {
        state.x = Math.round(bounds.x);
        state.y = Math.round(bounds.y);
        state.width = Math.max(MIN_WINDOW_WIDTH, Math.round(bounds.width));
        state.height = Math.max(MIN_WINDOW_HEIGHT, Math.round(bounds.height));
      }
      return true;
    }
    // When maximized or full-screen, keep the normal width/height intact, but
    // ensure `(x, y)` sits on the display currently hosting the window so
    // reopening restores on the same monitor.
    const currentBounds = win.getBounds?.();
    if (
      isValidRectangle(currentBounds) &&
      typeof screen?.getDisplayMatching === 'function'
    ) {
      try {
        const currentDisplay = screen.getDisplayMatching(currentBounds);
        const area = getUsableArea(currentDisplay);
        if (isValidRectangle(area)) {
          const priorDisplay =
            state.x !== undefined && state.y !== undefined
              ? screen.getDisplayMatching({
                  x: state.x,
                  y: state.y,
                  width: state.width,
                  height: state.height,
                })
              : undefined;
          if (!priorDisplay || priorDisplay.id !== currentDisplay.id) {
            state.width = clampDimension(
              state.width,
              MIN_WINDOW_WIDTH,
              area.width,
            );
            state.height = clampDimension(
              state.height,
              MIN_WINDOW_HEIGHT,
              area.height,
            );
            state.x = Math.round(area.x + (area.width - state.width) / 2);
            state.y = Math.round(area.y + (area.height - state.height) / 2);
          }
        }
      } catch {
        // Ignore screen lookup errors in partial environments.
      }
    }
    return true;
  };
  const captureAndSave = (): void => {
    clearTimer();
    if (captureCurrentState()) {
      persistWindowState(state, stateFilePath);
    }
  };
  const scheduleSave = (): void => {
    clearTimer();
    debounceTimer = setTimeout(captureAndSave, SAVE_DEBOUNCE_MS);
  };
  win.on?.('resize', scheduleSave);
  win.on?.('move', scheduleSave);
  win.on?.('maximize', scheduleSave);
  win.on?.('unmaximize', scheduleSave);
  win.on?.('enter-full-screen', scheduleSave);
  win.on?.('leave-full-screen', scheduleSave);
  const handleClose = (): void => {
    captureAndSave();
  };
  const dispose = (): void => {
    clearTimer();
    win.off?.('resize', scheduleSave);
    win.off?.('move', scheduleSave);
    win.off?.('maximize', scheduleSave);
    win.off?.('unmaximize', scheduleSave);
    win.off?.('enter-full-screen', scheduleSave);
    win.off?.('leave-full-screen', scheduleSave);
    win.off?.('close', handleClose);
    win.off?.('closed', dispose);
    if (activeTrackers.get(win) === tracker) {
      activeTrackers.delete(win);
    }
  };
  const tracker: WindowTracker = {
    win,
    clearTimer,
    captureAndSave,
    dispose,
  };
  activeTrackers.set(win, tracker);
  win.on?.('close', handleClose);
  win.on?.('closed', dispose);
}

/**
 * Synchronously flushes window state before app quit destroys open windows
 * (since `win.destroy()` skips the `'close'` event).
 */
export function flushTrackedWindowState(
  preferredWindow?: TrackableWindow | null,
): void {
  let targetTracker: WindowTracker | undefined;
  for (const tracker of activeTrackers.values()) {
    tracker.clearTimer();
    if (!tracker.win.isDestroyed?.()) {
      targetTracker = tracker;
    }
  }
  if (preferredWindow && !preferredWindow.isDestroyed?.()) {
    const preferredTracker = activeTrackers.get(preferredWindow);
    if (preferredTracker) {
      targetTracker = preferredTracker;
    }
  }
  targetTracker?.captureAndSave();
}
