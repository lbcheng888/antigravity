import type { BaseWindow } from 'electron';
import {
  getFocusedHubWindow,
  getHubWebContents,
  getHubWindows,
  type HubWindowInput,
  type HubWindowLike,
} from './utils';

export const ZOOM_LEVEL_KEY = 'zoomLevel';
export const ZOOM_STEP = 0.5;

export type HubZoomAction = 'in' | 'out' | 'reset';

export interface HubZoomStorageManager {
  updateItems?: (
    changes: Record<string, string>,
  ) => Promise<unknown> | unknown;
}

export interface HubZoomShortcutInput {
  key?: string;
  code?: string;
  shift?: boolean;
}

interface PopupWindowFlag {
  __isWebviewPopupWindow?: unknown;
}

let registeredStorageManager: HubZoomStorageManager | null | undefined;

/**
 * Registers the main-process storage updater used to persist Hub window zoom
 * changes initiated from IPC, application menu items, or keyboard shortcuts.
 */
export function setHubZoomStorageManager(
  storageManager?: HubZoomStorageManager | null,
): void {
  registeredStorageManager = storageManager;
}

function isWebviewPopupWindow(win: unknown): boolean {
  return Boolean(
    win &&
      typeof win === 'object' &&
      (win as PopupWindowFlag).__isWebviewPopupWindow,
  );
}

/**
 * Resolves the target top-level Hub window for a menu or IPC zoom action,
 * preferring an explicit live Hub window and falling back to the focused or
 * first open Hub window.
 */
export function resolveTargetHubWindow(
  win?: HubWindowInput,
): BaseWindow | HubWindowLike | undefined {
  if (
    win &&
    !(win as HubWindowLike).isDestroyed?.() &&
    !isWebviewPopupWindow(win)
  ) {
    return win as BaseWindow | HubWindowLike;
  }
  return getFocusedHubWindow() ?? getHubWindows()[0];
}

/**
 * Maps a `Cmd`/`Ctrl`-modified keyboard input payload (after caller modifier
 * validation) to its corresponding Hub zoom action, or `null` when unrelated.
 */
export function resolveHubZoomShortcutAction(
  input: HubZoomShortcutInput,
  keyLower: string = (input.key || '').toLowerCase(),
): HubZoomAction | null {
  const code = input.code || '';
  if (keyLower === '+' || keyLower === '=' || code === 'NumpadAdd') {
    return 'in';
  }
  if (keyLower === '-' || keyLower === '_' || code === 'NumpadSubtract') {
    return 'out';
  }
  if (
    !input.shift &&
    (keyLower === '0' || code === 'Digit0' || code === 'Numpad0')
  ) {
    return 'reset';
  }
  return null;
}

/**
 * Adjusts or resets the zoom level strictly on `win`'s Hub UI `WebContents`
 * (`getHubWebContents(win)`) — never on a focused child `WebContentsView` —
 * and persists `zoomLevel` when a storage updater is registered.
 */
export async function applyHubWindowZoom(
  win: HubWindowInput,
  action: HubZoomAction,
  /** Bounds the new level (e.g. for zoom that remote content asked for). */
  limit?: (level: number) => number,
): Promise<void> {
  if (!win || (win as HubWindowLike).isDestroyed?.()) {
    return;
  }
  const hubContents = getHubWebContents(win);
  if (!hubContents || hubContents.isDestroyed?.()) {
    return;
  }
  let newLevel = 0;
  if (action !== 'reset') {
    const current =
      typeof hubContents.getZoomLevel === 'function'
        ? hubContents.getZoomLevel()
        : 0;
    const baseLevel =
      typeof current === 'number' && Number.isFinite(current) ? current : 0;
    newLevel =
      action === 'in' ? baseLevel + ZOOM_STEP : baseLevel - ZOOM_STEP;
  }
  newLevel = limit ? limit(newLevel) : newLevel;
  hubContents.setZoomLevel?.(newLevel);
  if (typeof registeredStorageManager?.updateItems === 'function') {
    await registeredStorageManager.updateItems({
      [ZOOM_LEVEL_KEY]: String(newLevel),
    });
  }
}
