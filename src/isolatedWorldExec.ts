import type { WebSource } from 'electron';

export type WebContentsLifecycleEvent = 'did-stop-loading' | 'destroyed';

/**
 * Minimal `WebContents` lifecycle surface required by
 * {@link waitForWebContentsCanExecuteJs}, satisfied by Electron's `WebContents`
 * as well as lightweight structural mocks.
 */
export interface CanExecuteJsWebContents {
  isDestroyed?(): boolean;
  getURL?(): string | undefined;
  isLoadingMainFrame?(): boolean;
  on?(event: WebContentsLifecycleEvent, listener: () => void): unknown;
  once?(event: WebContentsLifecycleEvent, listener: () => void): unknown;
  off?(event: WebContentsLifecycleEvent, listener: () => void): unknown;
  removeListener?(
    event: WebContentsLifecycleEvent,
    listener: () => void,
  ): unknown;
}

/**
 * `WebContents` surface required by
 * {@link executeJavaScriptInIsolatedWorldWhenReady}.
 */
export interface IsolatedWorldWebContents extends CanExecuteJsWebContents {
  executeJavaScriptInIsolatedWorld(
    worldId: number,
    scripts: WebSource[],
    userGesture?: boolean,
  ): Promise<unknown>;
}

/**
 * `WebContents` surface required by {@link executeJavaScriptWhenReady}.
 */
export interface MainWorldWebContents extends CanExecuteJsWebContents {
  executeJavaScript(code: string, userGesture?: boolean): Promise<unknown>;
}

/**
 * `WebContents` surface accepted by {@link dismissNativeGuestPopups}.
 */
export interface GuestPopupWebContents extends CanExecuteJsWebContents {
  executeJavaScriptInIsolatedWorld?(
    worldId: number,
    scripts: WebSource[],
    userGesture?: boolean,
  ): Promise<unknown>;
}

/**
 * Minimal DOM element surface traversed by
 * {@link runGuestPopupDismissalInDocument}.
 */
export interface PopupDismissalElementLike {
  shadowRoot?: { activeElement?: PopupDismissalElementLike | null } | null;
  contentDocument?: PopupDismissalDocumentLike | null;
  blur?(): void;
  tagName?: string;
  multiple?: boolean;
  size?: number;
  hasAttribute?(qualifiedName: string): boolean;
  list?: unknown;
  type?: string;
}

/**
 * Minimal DOM document surface traversed by
 * {@link runGuestPopupDismissalInDocument}.
 */
export interface PopupDismissalDocumentLike {
  activeElement?: PopupDismissalElementLike | null;
  body?: unknown;
  documentElement?: unknown;
}

/**
 * Options for {@link dismissNativeGuestPopups}.
 */
export interface DismissNativeGuestPopupsOptions {
  worldId?: number;
  onlyPopupControls?: boolean;
  shouldStillDismiss?: () => boolean;
}

type DismissGuard = () => boolean;

interface DismissModeBatch {
  queued: boolean;
  guard?: DismissGuard;
}

interface PendingPopupDismissal {
  worldId: number;
  unconditional: DismissModeBatch;
  popupOnly: DismissModeBatch;
}

const pendingCanExecuteWaiters = new WeakMap<
  CanExecuteJsWebContents,
  Promise<void>
>();

function canListenToLifecycle(wc: CanExecuteJsWebContents): boolean {
  return typeof wc.on === 'function' || typeof wc.once === 'function';
}

function getCommittedUrl(wc: CanExecuteJsWebContents): string {
  return typeof wc.getURL === 'function'
    ? (wc.getURL() ?? '').trim()
    : 'about:blank';
}

function needsCanExecuteWait(wc: CanExecuteJsWebContents): boolean {
  // Match Electron's internal `waitTillCanExecuteJavaScript(wc)` predicate
  // (`Boolean(wc.getURL()) && !wc.isLoadingMainFrame()`): whenever that
  // condition is false, Electron attaches a fresh `wc.once('did-stop-loading')`
  // listener on every `executeJavaScript` / `executeJavaScriptInIsolatedWorld` call.
  const missingUrl = typeof wc.getURL === 'function' && !wc.getURL();
  const loadingMain =
    typeof wc.isLoadingMainFrame === 'function' &&
    Boolean(wc.isLoadingMainFrame());
  return missingUrl || loadingMain;
}

/**
 * Resolves once `wc` can execute main-frame JavaScript without Electron's
 * internal `waitTillCanExecuteJavaScript` registering per-call
 * `did-stop-loading` listeners, or once `wc` is destroyed.
 *
 * Concurrent callers targeting the same `WebContents` share a single
 * `{did-stop-loading, destroyed}` listener pair that symmetrically removes both
 * listeners on settlement so neither `did-stop-loading` nor `destroyed`
 * accumulates listeners across concurrent calls or repeated navigations.
 */
export function waitForWebContentsCanExecuteJs(
  wc: CanExecuteJsWebContents | null | undefined,
): Promise<void> {
  if (!wc || wc.isDestroyed?.() || !needsCanExecuteWait(wc)) {
    return Promise.resolve();
  }
  if (!canListenToLifecycle(wc)) {
    return Promise.resolve();
  }
  const existing = pendingCanExecuteWaiters.get(wc);
  if (existing) {
    return existing;
  }
  const remove = (event: WebContentsLifecycleEvent, fn: () => void): void => {
    if (typeof wc.removeListener === 'function') {
      wc.removeListener(event, fn);
    } else if (typeof wc.off === 'function') {
      wc.off(event, fn);
    }
  };
  const add = (event: WebContentsLifecycleEvent, fn: () => void): void => {
    if (typeof wc.on === 'function') {
      wc.on(event, fn);
    } else if (typeof wc.once === 'function') {
      wc.once(event, fn);
    }
  };
  let settled = false;
  const waiter = new Promise<void>((resolve) => {
    const settle = (): void => {
      if (settled) {
        return;
      }
      settled = true;
      remove('did-stop-loading', settle);
      remove('destroyed', settle);
      pendingCanExecuteWaiters.delete(wc);
      resolve();
    };
    add('did-stop-loading', settle);
    add('destroyed', settle);
    if (wc.isDestroyed?.() || !needsCanExecuteWait(wc)) {
      settle();
    }
  });
  if (!settled) {
    pendingCanExecuteWaiters.set(wc, waiter);
  }
  return waiter;
}

/**
 * Evaluates `scripts` in `worldId` on `wc`, coalescing concurrent waits while
 * `wc` has not yet committed a URL or is loading its main frame onto a single
 * shared `did-stop-loading` listener and rejecting cleanly if `wc` is destroyed
 * before execution can begin.
 */
export async function executeJavaScriptInIsolatedWorldWhenReady(
  wc: IsolatedWorldWebContents,
  worldId: number,
  scripts: WebSource[],
  userGesture?: boolean,
): Promise<unknown> {
  const canListen = canListenToLifecycle(wc);
  while (!wc.isDestroyed?.() && canListen && needsCanExecuteWait(wc)) {
    await waitForWebContentsCanExecuteJs(wc);
  }
  if (wc.isDestroyed?.()) {
    throw new Error(
      'WebContents was destroyed before isolated-world script could execute',
    );
  }
  return userGesture === undefined
    ? wc.executeJavaScriptInIsolatedWorld(worldId, scripts)
    : wc.executeJavaScriptInIsolatedWorld(worldId, scripts, userGesture);
}

/**
 * Evaluates `code` in the main world on `wc`, coalescing concurrent waits while
 * `wc` has not yet committed a URL or is loading its main frame onto the shared
 * `waitForWebContentsCanExecuteJs` listener and rejecting cleanly if `wc` is
 * destroyed before execution can begin.
 */
export async function executeJavaScriptWhenReady(
  wc: MainWorldWebContents,
  code: string,
  userGesture?: boolean,
): Promise<unknown> {
  const canListen = canListenToLifecycle(wc);
  while (!wc.isDestroyed?.() && canListen && needsCanExecuteWait(wc)) {
    await waitForWebContentsCanExecuteJs(wc);
  }
  if (wc.isDestroyed?.()) {
    throw new Error(
      'WebContents was destroyed before main-world script could execute',
    );
  }
  return userGesture === undefined
    ? wc.executeJavaScript(code)
    : wc.executeJavaScript(code, userGesture);
}

/**
 * Isolated V8 world ID used to guard and dismiss native OS/Chromium guest
 * popup widgets (`<datalist>`, `<select>`, date/time/color/file pickers)
 * without touching main-world globals or DOM prototypes.
 */
export const POPUP_GUARD_ISOLATED_WORLD_ID = 9997;

/**
 * Blurs the deepest focused element (piercing open Shadow DOM roots and
 * same-origin `<iframe>` `contentDocument`s) in `doc` when `onlyPopupControls`
 * is `false` (tab hidden, collapsed, or disposed) or when the focused leaf is a
 * native-popup-capable control (single-select `<select>`, `<input list="...">`,
 * or date/time/color/file `<input>`).
 *
 * Plain text inputs (`<input type="text">` without `list`), inline multi-row
 * `<select multiple>` / `<select size="...">` listboxes, `<textarea>`, and
 * `[contenteditable]` editors remain focused when `onlyPopupControls` is `true`
 * so multi-step agent typing (`click` -> `keyboard.type`) and in-page editor
 * state survive across `setAgentControlling(false)` and `z=1 -> z=0` demotions
 * without stealing OS/window focus from `hubWcv`.
 */
export function runGuestPopupDismissalInDocument(
  doc: PopupDismissalDocumentLike | null | undefined,
  onlyPopupControls: boolean,
): boolean {
  if (!doc) {
    return false;
  }
  let leaf: PopupDismissalElementLike | null = doc.activeElement ?? null;
  if (!leaf || leaf === doc.body || leaf === doc.documentElement) {
    return false;
  }
  const seen = new Set<PopupDismissalElementLike>();
  while (leaf && !seen.has(leaf)) {
    seen.add(leaf);
    const shadowActive: PopupDismissalElementLike | null =
      leaf.shadowRoot?.activeElement ?? null;
    if (shadowActive && shadowActive !== leaf) {
      leaf = shadowActive;
      continue;
    }
    let frameDoc: PopupDismissalDocumentLike | null = null;
    try {
      frameDoc = leaf.contentDocument ?? null;
    } catch {
      frameDoc = null;
    }
    const frameActive: PopupDismissalElementLike | null =
      frameDoc?.activeElement ?? null;
    if (
      frameDoc &&
      frameActive &&
      frameActive !== leaf &&
      frameActive !== frameDoc.body &&
      frameActive !== frameDoc.documentElement
    ) {
      leaf = frameActive;
      continue;
    }
    break;
  }
  if (!leaf || typeof leaf.blur !== 'function') {
    return false;
  }
  if (onlyPopupControls) {
    const tag =
      typeof leaf.tagName === 'string' ? leaf.tagName.toUpperCase() : '';
    let isPopupControl =
      tag === 'SELECT' &&
      !leaf.multiple &&
      !(typeof leaf.size === 'number' && leaf.size > 1);
    if (!isPopupControl && tag === 'INPUT') {
      const hasDatalist =
        Boolean(leaf.hasAttribute?.('list')) || Boolean(leaf.list);
      const inputType =
        typeof leaf.type === 'string' ? leaf.type.toLowerCase() : '';
      const isPickerType =
        inputType === 'date' ||
        inputType === 'time' ||
        inputType === 'datetime-local' ||
        inputType === 'month' ||
        inputType === 'week' ||
        inputType === 'color' ||
        inputType === 'file';
      isPopupControl = hasDatalist || isPickerType;
    }
    if (!isPopupControl) {
      return false;
    }
  }
  leaf.blur();
  return true;
}

/** Builds the self-contained isolated-world script invoking `runGuestPopupDismissalInDocument`. */
export function buildGuestPopupDismissalScript(
  onlyPopupControls: boolean,
): string {
  return `(${runGuestPopupDismissalInDocument.toString()})(typeof document !== 'undefined' ? document : null, ${onlyPopupControls ? 'true' : 'false'})`;
}

const pendingPopupDismissals = new WeakMap<
  GuestPopupWebContents,
  PendingPopupDismissal
>();

function mergeDismissGuards(
  prev: DismissGuard | undefined,
  next: DismissGuard | undefined,
): DismissGuard | undefined {
  if (!prev || !next) {
    return undefined;
  }
  if (prev === next) {
    return prev;
  }
  return () => prev() || next();
}

function recordDismissMode(
  mode: DismissModeBatch,
  guard: DismissGuard | undefined,
): void {
  if (!mode.queued) {
    mode.queued = true;
    mode.guard = guard;
  } else {
    mode.guard = mergeDismissGuards(mode.guard, guard);
  }
}

function hasIsolatedWorldExecution(
  wc: GuestPopupWebContents,
): wc is IsolatedWorldWebContents {
  return typeof wc.executeJavaScriptInIsolatedWorld === 'function';
}

function runIsolatedDismissNow(
  wc: GuestPopupWebContents,
  worldId: number,
  onlyPopupControls: boolean,
): void {
  if (!hasIsolatedWorldExecution(wc)) {
    return;
  }
  const script = buildGuestPopupDismissalScript(onlyPopupControls);
  try {
    void executeJavaScriptInIsolatedWorldWhenReady(wc, worldId, [
      { code: script },
    ]).catch(() => undefined);
  } catch {
    // Ignore synchronously torn-down WebContents.
  }
}

/**
 * Dismisses any open native Chromium/Electron popup widget (`<datalist>`
 * `AutofillPopupView`, `<select>`, or date/time/color `PagePopup`) on `wc`
 * (including nested open Shadow DOM roots and same-origin `<iframe>`s) in
 * isolated world `worldId`, coalescing calls while `isLoadingMainFrame()` is
 * true onto the shared `waitForWebContentsCanExecuteJs` listener (`O(1)`
 * listeners) and never stealing focus from `hubWcv`.
 */
export function dismissNativeGuestPopups(
  wc: GuestPopupWebContents | null | undefined,
  options?: DismissNativeGuestPopupsOptions,
): void {
  if (!wc || wc.isDestroyed?.() || !hasIsolatedWorldExecution(wc)) {
    return;
  }
  if (!getCommittedUrl(wc)) {
    return;
  }
  const worldId = options?.worldId ?? POPUP_GUARD_ISOLATED_WORLD_ID;
  const onlyPopupControls = options?.onlyPopupControls ?? false;
  const isLoadingMain =
    typeof wc.isLoadingMainFrame === 'function' &&
    Boolean(wc.isLoadingMainFrame());
  if (!isLoadingMain) {
    const pending = pendingPopupDismissals.get(wc);
    if (pending) {
      if (!onlyPopupControls || !pending.unconditional.queued) {
        pendingPopupDismissals.delete(wc);
      } else {
        pending.popupOnly.queued = false;
      }
    }
    runIsolatedDismissNow(wc, worldId, onlyPopupControls);
    return;
  }
  if (!canListenToLifecycle(wc)) {
    return;
  }
  const existing = pendingPopupDismissals.get(wc);
  const isNewBatch = !existing;
  const batch: PendingPopupDismissal = existing ?? {
    worldId,
    unconditional: { queued: false },
    popupOnly: { queued: false },
  };
  if (existing) {
    batch.worldId = worldId;
  } else {
    pendingPopupDismissals.set(wc, batch);
  }
  recordDismissMode(
    onlyPopupControls ? batch.popupOnly : batch.unconditional,
    options?.shouldStillDismiss,
  );
  if (!isNewBatch) {
    return;
  }
  void (async () => {
    while (
      !wc.isDestroyed?.() &&
      pendingPopupDismissals.get(wc) === batch &&
      typeof wc.isLoadingMainFrame === 'function' &&
      Boolean(wc.isLoadingMainFrame())
    ) {
      await waitForWebContentsCanExecuteJs(wc);
    }
    if (pendingPopupDismissals.get(wc) !== batch) {
      return;
    }
    pendingPopupDismissals.delete(wc);
    if (wc.isDestroyed?.()) {
      return;
    }
    if (!getCommittedUrl(wc)) {
      return;
    }
    const runUnconditional =
      batch.unconditional.queued &&
      (!batch.unconditional.guard || batch.unconditional.guard());
    const runPopupOnly =
      !runUnconditional &&
      batch.popupOnly.queued &&
      (!batch.popupOnly.guard || batch.popupOnly.guard());
    if (!runUnconditional && !runPopupOnly) {
      return;
    }
    runIsolatedDismissNow(wc, batch.worldId, !runUnconditional);
  })().catch(() => {
    if (pendingPopupDismissals.get(wc) === batch) {
      pendingPopupDismissals.delete(wc);
    }
  });
}
