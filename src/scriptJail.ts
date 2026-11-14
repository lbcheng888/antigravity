/**
 * Ephemeral, zero-egress Electron Script Jail (`runScriptInJail` / `abortScriptInJail` / `warmScriptJail`).
 *
 * Security & Consumer Contract (for AUIT `CL_0` callers, `scriptJailProtocol === 2`):
 * 1. **Hub-side global FIFO admission & Port-as-admission (`window.addEventListener('message', onPort)`)**:
 *    - Register the `message` listener BEFORE calling `electronNative.runScriptInJail(...)`.
 *    - `runScriptInJail` accepts requests into a main-process global FIFO queue (`QUEUED` state,
 *      bounded at `MAX_QUEUED_JAILS_PER_SENDER = 8` / `MAX_QUEUED_JAILS_GLOBAL = 32`) when the
 *      running caps (`MAX_CONCURRENT_JAILS_PER_SENDER = 4` / `MAX_CONCURRENT_JAILS_GLOBAL = 8`)
 *      are full, and only delivers the `MessagePort` on `agy-script-jail:port` once a jail is
 *      actually assigned (`RUNNING` state).
 *    - The per-script `timeoutMs` clock and `512 MB` RSS watchdog start at jail assignment,
 *      not while waiting in the queue. Callers should start their execution budget, capture
 *      page baselines, and mark active actuation upon receiving the `agy-script-jail-port` event.
 *    - Always verify `event.source === window`, `event.data?.type === 'agy-script-jail-port'`,
 *      `event.data?.jailId === jailId`, and `event.ports[0]` so embedded subframes cannot
 *      spoof the port handoff.
 *    - Generate a fresh random `jailId` (e.g. `crypto.randomUUID()`) per invocation.
 * 2. **Host-side RPC validation (`port.onmessage`)**:
 *    - Stage 1 (`pageFacadeBundle`) executes in the same untrusted Worker realm as Stage 2
 *      (`modelScript`) and is NOT a security boundary (Stage 2 can intercept
 *      `MessagePort.prototype.postMessage` and send arbitrary RPC messages).
 *    - The host handler MUST enforce an explicit per-method allowlist with param schemas
 *      (defined AUIT-side), enforce a per-script operation budget (`maxOps`),
 *      call `electronNative.abortScriptJail(jailId)` on the first fatal/schema/budget error,
 *      and call `port.close()` when the run finishes.
 *    - Host-side first failure wins: classify navigation, domain-gate, and takeover errors
 *      from host-side state rather than trusting `result.errorCode` from the jail.
 */
import {
  app,
  MessageChannelMain,
  type MessagePortMain,
  session,
  type Session,
  type WebContents,
  WebContentsView,
} from 'electron';
import * as crypto from 'crypto';
import * as path from 'path';
import { getHubWebContents, getHubWindows } from './utils';
import { executeJavaScriptWhenReady } from './isolatedWorldExec';

export const SCRIPT_JAIL_PROTOCOL_VERSION = 2;
export const SCRIPT_JAIL_PORT_CHANNEL = 'agy-script-jail:port';
export const SCRIPT_JAIL_PORT_MESSAGE_TYPE = 'agy-script-jail-port';
/** Random per-process non-persistent partition so no `<webview partition="...">` can guess it. */
export const SCRIPT_JAIL_PARTITION = `agy-script-jail-${crypto.randomUUID()}`;
/** Blackhole proxy routing all proxyable traffic to dead loopback port 9 without localhost bypass. */
export const SCRIPT_JAIL_BLACKHOLE_PROXY_RULES = 'http://127.0.0.1:9';
export const SCRIPT_JAIL_BLACKHOLE_PROXY_BYPASS_RULES = '<-loopback>';
// Matches the agent browser's 30 s per-script budget.
export const DEFAULT_SCRIPT_JAIL_TIMEOUT_MS = 30000;
export const MAX_SCRIPT_JAIL_TIMEOUT_MS = 30000;
export const MIN_SCRIPT_JAIL_TIMEOUT_MS = 50;
export const SCRIPT_JAIL_ACQUIRE_TIMEOUT_MS = 10000;
export const MAX_CONCURRENT_JAILS_PER_SENDER = 4;
export const MAX_CONCURRENT_JAILS_GLOBAL = 8;
export const MAX_QUEUED_JAILS_PER_SENDER = 8;
export const MAX_QUEUED_JAILS_GLOBAL = 32;
export const SCRIPT_JAIL_POOL_TARGET_IDLE = 2;
export const MAX_CONSECUTIVE_IDLE_DEATHS = 3;
export const MAX_SCRIPT_JAIL_RSS_KB = 512 * 1024;
export const SCRIPT_JAIL_RSS_POLL_INTERVAL_MS = 200;
export const MAX_SCRIPT_JAIL_RESULT_BYTES = 1024 * 1024;
export const MAX_SCRIPT_JAIL_ERROR_MESSAGE_LENGTH = 4096;
export const SCRIPT_JAIL_PRELOAD_PATH = path.join(
  __dirname,
  'scriptJailPreload.js',
);

export type ScriptJailWorkerErrorCode =
  | 'FACADE_ERROR'
  | 'SYNTAX_ERROR'
  | 'RUNTIME_ERROR'
  | 'SERIALIZATION_ERROR';

export type ScriptJailErrorCode =
  | ScriptJailWorkerErrorCode
  | 'SCRIPT_ABORTED'
  | 'TIMEOUT'
  | 'JAIL_CRASHED'
  | 'RESOURCE_LIMIT'
  | 'INTERNAL_ERROR'
  | 'SANDBOX_UNAVAILABLE'
  | 'INVALID_REQUEST';

export interface ScriptJailSuccessResult {
  ok: true;
  value?: unknown;
}

export interface ScriptJailFailureResult {
  ok: false;
  errorCode: ScriptJailErrorCode;
  errorMessage: string;
}

export type ScriptJailResult =
  | ScriptJailSuccessResult
  | ScriptJailFailureResult;

export interface ScriptJailPoolSnapshot {
  poolActivated: boolean;
  idleWarmJails: number;
  idleWarmWebContentsIds: number[];
  warmingJails: number;
  runningJails: number;
  runningIds: string[];
  queuedJails: number;
  queuedIds: string[];
}

export interface ScriptJailTimeoutConfig {
  timeoutMs?: unknown;
}

export interface ScriptJailRequest {
  jailId: string;
  pageFacadeBundle: string;
  modelScript: string;
  timeoutMs?: number;
}

export interface ScriptJailBootstrapParams {
  pageFacadeBundle: string;
  modelScript: string;
}

export interface ScriptJailNavigationEvent {
  isMainFrame?: boolean;
  isSameDocument?: boolean;
}

export interface ScriptJailSenderFrame {
  readonly parent?: unknown;
}

export interface ScriptJailSenderBase {
  readonly id?: number;
  isDestroyed(): boolean;
  getType(): string;
}

export interface ScriptJailSender extends ScriptJailSenderBase {
  readonly id: number;
  on(event: 'destroyed', listener: () => void): unknown;
  on(event: 'render-process-gone', listener: () => void): unknown;
  on(
    event: 'did-start-navigation',
    listener: (details: ScriptJailNavigationEvent) => void,
  ): unknown;
  removeListener(event: 'destroyed', listener: () => void): unknown;
  removeListener(event: 'render-process-gone', listener: () => void): unknown;
  removeListener(
    event: 'did-start-navigation',
    listener: (details: ScriptJailNavigationEvent) => void,
  ): unknown;
  postMessage(
    channel: string,
    message: unknown,
    transfer?: MessagePortMain[],
  ): void;
}

export interface ScriptJailPreloadScript {
  id: string;
}

export interface ScriptJailSession {
  getPreloadScripts(): ScriptJailPreloadScript[];
  unregisterPreloadScript(id: string): void;
  getPreloads?(): string[];
  setPreloads?(preloads: string[]): void;
  webRequest: {
    onBeforeRequest(
      listener: (
        details: unknown,
        callback: (response: { cancel?: boolean }) => void,
      ) => void,
    ): void;
  };
  setPermissionRequestHandler(
    handler: (
      webContents: unknown,
      permission: string,
      callback: (permissionGranted: boolean) => void,
    ) => void,
  ): void;
  setPermissionCheckHandler(handler: () => boolean): void;
  setProxy(config: {
    mode: 'fixed_servers';
    proxyRules: string;
    proxyBypassRules: string;
  }): Promise<void>;
}

export interface ScriptJailPreventableEvent {
  preventDefault(): void;
}

export interface ScriptJailWebContents {
  setWebRTCIPHandlingPolicy(policy: 'disable_non_proxied_udp'): void;
  setWindowOpenHandler(handler: () => { action: 'deny' }): void;
  on(
    event: 'will-navigate',
    listener: (event: ScriptJailPreventableEvent) => void,
  ): unknown;
  on(
    event: 'will-frame-navigate',
    listener: (event: ScriptJailPreventableEvent) => void,
  ): unknown;
}

interface LegacyPreloadSession {
  getPreloads?: () => string[];
  setPreloads?: (preloads: string[]) => void;
}

type SenderAbortCallback = (result: ScriptJailFailureResult) => void;

interface SenderWatcher {
  sender: ScriptJailSender;
  callbacks: Set<SenderAbortCallback>;
  onDestroyed: () => void;
  onGone: () => void;
  onNavigate: (details: ScriptJailNavigationEvent) => void;
}

interface QueuedJailEntry {
  jailId: string;
  sender: ScriptJailSender;
  senderId: number;
  req: ScriptJailRequest;
  timeoutMs: number;
  resolveRun: (res: ScriptJailResult) => void;
  detachQueuedWatcher: () => void;
  settleQueued: (res: ScriptJailFailureResult) => void;
}

interface ActiveJailEntry {
  senderId: number;
  abort: (res?: ScriptJailFailureResult) => void;
}

interface IdleWarmJailEntry {
  jailView: WebContentsView;
  jailContents: WebContents;
  detachIdleListeners: () => void;
}

interface SpawnedJail {
  jailView: WebContentsView;
  jailContents: WebContents;
  spawnEpoch: number;
}

interface AcquiredJail {
  jailView: WebContentsView;
  jailContents: WebContents;
}

type AcquireRaceResult =
  | { kind: 'acquired'; jail: AcquiredJail }
  | { kind: 'early'; res: ScriptJailResult };

interface RawWorkerOutcome {
  ok?: unknown;
  value?: unknown;
  errorCode?: unknown;
  errorMessage?: unknown;
}

const WORKER_ERROR_CODES: ReadonlySet<string> = new Set([
  'FACADE_ERROR',
  'SYNTAX_ERROR',
  'RUNTIME_ERROR',
  'SERIALIZATION_ERROR',
]);

class JailSpawnError extends Error {}

const queuedJails = new Map<string, QueuedJailEntry>();
const activeJails = new Map<string, ActiveJailEntry>();
const senderWatchers = new Map<ScriptJailSender, SenderWatcher>();
const idleWarmJails: IdleWarmJailEntry[] = [];
const inFlightWarmups = new Set<Promise<void>>();
/**
 * Strong reference anchor for every live detached `WebContentsView` (warming, idle,
 * running, or orphaned acquire). Because jail views are never attached to a window,
 * dropping the JS `WebContentsView` reference while `webContents` is in use allows V8
 * GC to finalize the C++ view and destroy its `webContents` mid-run.
 */
const liveJailViews = new Set<WebContentsView>();
let poolActivated = false;
let consecutiveIdleDeaths = 0;
let preloadEpoch = 0;
let jailSessionInstance: Session | null = null;

/** Returns `true` if `targetSession` is the dedicated script-jail session. */
export function isScriptJailSession(targetSession: unknown): boolean {
  return Boolean(targetSession && targetSession === jailSessionInstance);
}

/** Returns a read-only snapshot of the script-jail pool and admission queue. */
export function getScriptJailPoolSnapshot(): ScriptJailPoolSnapshot {
  const liveWarmIds: number[] = [];
  for (const entry of idleWarmJails) {
    if (!entry.jailContents.isDestroyed()) {
      liveWarmIds.push(entry.jailContents.id);
    }
  }
  return {
    poolActivated,
    idleWarmJails: liveWarmIds.length,
    idleWarmWebContentsIds: liveWarmIds,
    warmingJails: inFlightWarmups.size,
    runningJails: activeJails.size,
    runningIds: [...activeJails.keys()],
    queuedJails: queuedJails.size,
    queuedIds: [...queuedJails.keys()],
  };
}

/** Waits for all in-flight background pool warmup tasks to settle. */
export async function waitForScriptJailPoolIdle(): Promise<ScriptJailPoolSnapshot> {
  while (inFlightWarmups.size > 0) {
    await Promise.allSettled([...inFlightWarmups]);
  }
  return getScriptJailPoolSnapshot();
}

function closeJailView(
  jailView: WebContentsView | undefined,
  jailContents: WebContents | undefined,
): void {
  if (jailView) {
    liveJailViews.delete(jailView);
  }
  if (jailContents && !jailContents.isDestroyed()) {
    jailContents.close();
  }
}

function closeAllIdleWarmJails(): void {
  while (idleWarmJails.length > 0) {
    const entry = idleWarmJails.shift();
    if (!entry) {
      continue;
    }
    entry.detachIdleListeners();
    closeJailView(entry.jailView, entry.jailContents);
  }
}

/** Resets pool and queue state (closing any idle warm jails). Intended for shutdown and unit tests. */
export function resetScriptJailStateForTests(): void {
  poolActivated = false;
  consecutiveIdleDeaths = 0;
  preloadEpoch = 0;
  closeAllIdleWarmJails();
  liveJailViews.clear();
  for (const entry of [...queuedJails.values()]) {
    entry.settleQueued(
      failResult('SCRIPT_ABORTED', 'Script jail state reset'),
    );
  }
  queuedJails.clear();
}

function failResult(
  errorCode: ScriptJailErrorCode,
  errorMessage: string,
): ScriptJailFailureResult {
  return {
    ok: false,
    errorCode,
    errorMessage: errorMessage.slice(0, MAX_SCRIPT_JAIL_ERROR_MESSAGE_LENGTH),
  };
}

export function resolveScriptJailTimeoutMs(req?: unknown): number {
  const candidate =
    req && typeof req === 'object'
      ? (req as ScriptJailTimeoutConfig).timeoutMs
      : undefined;
  if (typeof candidate !== 'number' || !Number.isFinite(candidate)) {
    return DEFAULT_SCRIPT_JAIL_TIMEOUT_MS;
  }
  return Math.min(
    MAX_SCRIPT_JAIL_TIMEOUT_MS,
    Math.max(MIN_SCRIPT_JAIL_TIMEOUT_MS, Math.floor(candidate)),
  );
}

function isValidJailId(rawJailId: unknown): rawJailId is string {
  return (
    typeof rawJailId === 'string' && /^[A-Za-z0-9._:-]{1,128}$/.test(rawJailId)
  );
}

function getRunningCountForSender(senderId: number): number {
  let count = 0;
  for (const entry of activeJails.values()) {
    if (entry.senderId === senderId) {
      count++;
    }
  }
  return count;
}

function getQueuedCountForSender(senderId: number): number {
  let count = 0;
  for (const entry of queuedJails.values()) {
    if (entry.senderId === senderId) {
      count++;
    }
  }
  return count;
}

function registerSenderAbortCallback(
  sender: ScriptJailSender,
  cb: SenderAbortCallback,
): () => void {
  let watcher = senderWatchers.get(sender);
  if (!watcher) {
    const callbacks = new Set<SenderAbortCallback>();
    const fireAll = (res: ScriptJailFailureResult): void => {
      for (const fn of [...callbacks]) {
        fn(res);
      }
    };
    const onDestroyed = (): void =>
      fireAll(
        failResult(
          'SCRIPT_ABORTED',
          'Requesting sender WebContents was destroyed',
        ),
      );
    const onGone = (): void =>
      fireAll(
        failResult(
          'SCRIPT_ABORTED',
          'Requesting sender renderer process exited',
        ),
      );
    const onNavigate = (details: ScriptJailNavigationEvent): void => {
      if (details?.isMainFrame && !details.isSameDocument) {
        fireAll(
          failResult(
            'SCRIPT_ABORTED',
            'Requesting sender WebContents navigated',
          ),
        );
      }
    };
    sender.on('destroyed', onDestroyed);
    sender.on('render-process-gone', onGone);
    sender.on('did-start-navigation', onNavigate);
    watcher = { sender, callbacks, onDestroyed, onGone, onNavigate };
    senderWatchers.set(sender, watcher);
  }
  watcher.callbacks.add(cb);
  let removed = false;
  return () => {
    if (removed) {
      return;
    }
    removed = true;
    const current = senderWatchers.get(sender);
    if (!current) {
      return;
    }
    current.callbacks.delete(cb);
    if (current.callbacks.size === 0) {
      senderWatchers.delete(sender);
      sender.removeListener('destroyed', current.onDestroyed);
      sender.removeListener('render-process-gone', current.onGone);
      sender.removeListener('did-start-navigation', current.onNavigate);
    }
  };
}

/** Verifies `sender` belongs to the main frame of an active Hub `BrowserWindow`. */
export function assertAuthorizedJailSender(
  sender: ScriptJailSenderBase | null | undefined,
  senderFrame?: ScriptJailSenderFrame | null,
): asserts sender is ScriptJailSenderBase {
  if (!sender || sender.isDestroyed()) {
    throw new Error(
      'Unauthorized script-jail sender: WebContents is missing or destroyed',
    );
  }
  if (sender.getType() !== 'window') {
    throw new Error(
      `Unauthorized script-jail sender: expected type "window", got "${sender.getType()}"`,
    );
  }
  if (senderFrame?.parent) {
    throw new Error(
      'Unauthorized script-jail sender: subframe cannot invoke script jail',
    );
  }
  const matchingHubWindow = getHubWindows().find((win) => {
    if (!win || win.isDestroyed()) {
      return false;
    }
    const hubContents = getHubWebContents(win);
    return (
      hubContents === sender ||
      (typeof sender.id === 'number' && hubContents?.id === sender.id)
    );
  });
  if (!matchingHubWindow) {
    throw new Error(
      'Unauthorized script-jail sender: WebContents is not an active Hub BrowserWindow',
    );
  }
}

function scrubAndAssertZeroSessionPreloads(
  jailSession: ScriptJailSession,
): number {
  const modernPreloads = jailSession.getPreloadScripts();
  for (const script of modernPreloads) {
    jailSession.unregisterPreloadScript(script.id);
  }
  const legacySession: LegacyPreloadSession = jailSession;
  const legacyPreloadCount =
    typeof legacySession.getPreloads === 'function'
      ? legacySession.getPreloads().length
      : 0;
  if (
    legacyPreloadCount > 0 &&
    typeof legacySession.setPreloads === 'function'
  ) {
    legacySession.setPreloads([]);
  }
  const remainingModern = jailSession.getPreloadScripts().length;
  const remainingLegacy =
    typeof legacySession.getPreloads === 'function'
      ? legacySession.getPreloads().length
      : 0;
  if (remainingModern !== 0 || remainingLegacy !== 0) {
    throw new Error(
      'Script jail session has unexpected preload scripts registered',
    );
  }
  const removedCount = modernPreloads.length + legacyPreloadCount;
  if (removedCount > 0) {
    preloadEpoch++;
    closeAllIdleWarmJails();
  }
  return removedCount;
}

/**
 * Re-asserts full fail-closed hardening on the script-jail `Session` before each run.
 *
 * Actively strips any session-level preload scripts (such as `electron-log-preload`
 * injected by `electron-log`'s `app.on('session-created')` listener via
 * `registerPreloadScript`, or legacy `setPreloads` entries) so only
 * `SCRIPT_JAIL_PRELOAD_PATH` runs in the jail, then asserts zero session preloads
 * remain (failing closed if removal did not stick). Returns the number of preloads removed.
 */
export async function hardenScriptJailSession(
  jailSession: ScriptJailSession,
): Promise<number> {
  const removedCount = scrubAndAssertZeroSessionPreloads(jailSession);
  jailSession.webRequest.onBeforeRequest((_details, callback) => {
    callback({ cancel: true });
  });
  jailSession.setPermissionRequestHandler((_wc, _permission, callback) => {
    callback(false);
  });
  jailSession.setPermissionCheckHandler(() => false);
  await jailSession.setProxy({
    mode: 'fixed_servers',
    proxyRules: SCRIPT_JAIL_BLACKHOLE_PROXY_RULES,
    proxyBypassRules: SCRIPT_JAIL_BLACKHOLE_PROXY_BYPASS_RULES,
  });
  if (removedCount > 0 && poolActivated) {
    schedulePoolRefill();
  }
  return removedCount;
}

export function hardenScriptJailWebContents(
  jailContents: ScriptJailWebContents,
): void {
  jailContents.setWebRTCIPHandlingPolicy('disable_non_proxied_udp');
  jailContents.setWindowOpenHandler(() => ({ action: 'deny' }));
  jailContents.on('will-navigate', (event) => event.preventDefault());
  jailContents.on('will-frame-navigate', (event) => event.preventDefault());
}

/** Builds the dedicated Worker script that executes Stage 1 and Stage 2 in `DedicatedWorkerGlobalScope`. */
export function buildScriptJailWorkerCode(): string {
  return `(() => {
  'use strict';
  const formatErr = (err, fallback) =>
    err && typeof err === 'object' && typeof err.message === 'string' && err.message
      ? err.message
      : typeof err === 'string' && err
        ? err
        : fallback;

  let initialized = false;
  self.onmessage = (initEvent) => {
    const data = initEvent && initEvent.data;
    const port = initEvent && initEvent.ports && initEvent.ports[0];
    if (!data || data.type !== 'init' || initialized || !port) return;
    initialized = true;

    let nextRpcId = 1;
    let finished = false;
    const pendingRpcs = new Map();

    const finish = (outcome) => {
      if (finished) return;
      finished = true;
      pendingRpcs.clear();
      self.postMessage(outcome);
    };

    port.addEventListener('message', (pEvent) => {
      const msg = pEvent && pEvent.data;
      if (!msg || msg.type !== 'rpc-result' || typeof msg.id !== 'number') return;
      const entry = pendingRpcs.get(msg.id);
      if (!entry) return;
      pendingRpcs.delete(msg.id);
      if (msg.ok) {
        entry.resolve(msg.value);
      } else {
        const errObj = msg.error && typeof msg.error === 'object' ? msg.error : {};
        const errMsg = typeof errObj.message === 'string' && errObj.message ? errObj.message : 'RPC call failed';
        entry.reject(new Error(errMsg));
      }
    });
    port.start();

    const callRpc = (method, params) => {
      if (finished) return Promise.reject(new Error('Script execution has already finished'));
      if (typeof method !== 'string' || !method) {
        return Promise.reject(new TypeError('callRpc: method must be a non-empty string'));
      }
      const id = nextRpcId++;
      const promise = new Promise((resolve, reject) => {
        pendingRpcs.set(id, {resolve, reject});
        port.postMessage({type: 'rpc', id, method, params});
      });
      promise.catch(() => {});
      return promise;
    };

    (async () => {
      let pageFacade;
      try {
        const createPageFacade = (0, eval)(data.pageFacadeBundle);
        if (typeof createPageFacade !== 'function') {
          throw new Error('pageFacadeBundle must evaluate to a createPageFacade function');
        }
        pageFacade = await createPageFacade({callRpc});
        if (!pageFacade || (typeof pageFacade !== 'object' && typeof pageFacade !== 'function')) {
          throw new Error('createPageFacade({callRpc}) must return a valid page facade');
        }
      } catch (facadeErr) {
        finish({ok: false, errorCode: 'FACADE_ERROR', errorMessage: formatErr(facadeErr, 'Failed to initialize pageFacadeBundle')});
        return;
      }

      let modelFn;
      try {
        const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
        modelFn = new AsyncFunction('page', 'console', "'use strict';\\n" + String(data.modelScript || '') + '\\n');
      } catch (syntaxErr) {
        finish({ok: false, errorCode: 'SYNTAX_ERROR', errorMessage: formatErr(syntaxErr, 'Failed to compile modelScript')});
        return;
      }

      try {
        const rawValue = await modelFn(pageFacade, console);
        if (rawValue === undefined) {
          finish({ok: true});
          return;
        }
        let serializedValue;
        try {
          const encoded = JSON.stringify(rawValue);
          if (encoded === undefined) throw new TypeError('script return value is not JSON-serializable');
          serializedValue = JSON.parse(encoded);
        } catch (serErr) {
          finish({
            ok: false,
            errorCode: 'SERIALIZATION_ERROR',
            errorMessage: 'script return value is not JSON-serializable: ' + formatErr(serErr, String(serErr)),
          });
          return;
        }
        finish({ok: true, value: serializedValue});
      } catch (runErr) {
        finish({ok: false, errorCode: 'RUNTIME_ERROR', errorMessage: formatErr(runErr, 'Script execution failed')});
      }
    })();
  };
})()`;
}

/** Builds the main-world bootstrap that spawns the dedicated Worker and enforces result/error size caps. */
export function buildScriptJailBootstrapCode(
  params: ScriptJailBootstrapParams,
): string {
  const serializedParams = JSON.stringify({
    pageFacadeBundle: params.pageFacadeBundle,
    modelScript: params.modelScript,
    portMessageType: SCRIPT_JAIL_PORT_MESSAGE_TYPE,
    maxResultBytes: MAX_SCRIPT_JAIL_RESULT_BYTES,
    maxErrorLength: MAX_SCRIPT_JAIL_ERROR_MESSAGE_LENGTH,
    workerCode: buildScriptJailWorkerCode(),
  });
  return `(() => {
  'use strict';
  const __cfg = ${serializedParams};
  return new Promise((resolveBootstrap) => {
    const blobUrl = URL.createObjectURL(new Blob([__cfg.workerCode], {type: 'text/javascript'}));
    const worker = new Worker(blobUrl);
    let portTransferred = false;

    const cleanup = () => {
      window.removeEventListener('message', onWindowMessage);
      worker.terminate();
      URL.revokeObjectURL(blobUrl);
    };

    worker.onmessage = (event) => {
      cleanup();
      const data = event && event.data;
      if (!data || typeof data !== 'object') {
        resolveBootstrap({ok: false, errorCode: 'RUNTIME_ERROR', errorMessage: 'Invalid Worker outcome'});
        return;
      }
      if (data.ok === true) {
        if (data.value === undefined) {
          resolveBootstrap({ok: true});
          return;
        }
        let encoded;
        try {
          encoded = JSON.stringify(data.value);
        } catch (_err) {
          resolveBootstrap({ok: false, errorCode: 'SERIALIZATION_ERROR', errorMessage: 'Script return value is not JSON-serializable'});
          return;
        }
        if (typeof encoded === 'string' && encoded.length > __cfg.maxResultBytes) {
          resolveBootstrap({
            ok: false,
            errorCode: 'SERIALIZATION_ERROR',
            errorMessage: 'Script return value exceeded maximum size (' + __cfg.maxResultBytes + ' bytes)',
          });
          return;
        }
        resolveBootstrap({ok: true, value: JSON.parse(encoded)});
        return;
      }
      resolveBootstrap({
        ok: false,
        errorCode: typeof data.errorCode === 'string' ? data.errorCode : 'RUNTIME_ERROR',
        errorMessage: String(data.errorMessage || 'Script execution failed').slice(0, __cfg.maxErrorLength),
      });
    };

    worker.onerror = (err) => {
      if (err && typeof err.preventDefault === 'function') err.preventDefault();
      cleanup();
      resolveBootstrap({
        ok: false,
        errorCode: 'RUNTIME_ERROR',
        errorMessage: String((err && err.message) || 'Worker execution failed').slice(0, __cfg.maxErrorLength),
      });
    };

    const onWindowMessage = (event) => {
      const data = event && event.data;
      if (!data || data.type !== __cfg.portMessageType) return;
      const port = event.ports && event.ports[0];
      if (!port || portTransferred) return;
      portTransferred = true;
      window.removeEventListener('message', onWindowMessage);
      worker.postMessage(
        {type: 'init', pageFacadeBundle: __cfg.pageFacadeBundle, modelScript: __cfg.modelScript},
        [port]
      );
    };

    window.addEventListener('message', onWindowMessage);
    window.postMessage({type: 'agy-script-jail-ready'}, '*');
  });
})()`;
}

/**
 * Spawns and initializes a single hardened `WebContentsView` (`about:blank`) for either the
 * warm pool or an on-demand cold start. Closes the view on any initialization failure or timeout.
 */
async function spawnJail(): Promise<SpawnedJail> {
  jailSessionInstance ??= session.fromPartition(SCRIPT_JAIL_PARTITION, {
    cache: false,
  });
  scrubAndAssertZeroSessionPreloads(jailSessionInstance);
  const spawnEpoch = preloadEpoch;
  const jailView = new WebContentsView({
    webPreferences: {
      sandbox: true,
      contextIsolation: true,
      navigateOnDragDrop: false,
      partition: SCRIPT_JAIL_PARTITION,
      preload: SCRIPT_JAIL_PRELOAD_PATH,
    },
  });
  liveJailViews.add(jailView);
  const jailContents = jailView.webContents;
  jailContents.once('destroyed', () => {
    liveJailViews.delete(jailView);
  });
  hardenScriptJailWebContents(jailContents);
  let timer: ReturnType<typeof setTimeout> | undefined;
  let onDead: (() => void) | undefined;
  try {
    await Promise.race([
      jailContents.loadURL('about:blank'),
      new Promise<never>((_, reject) => {
        onDead = () =>
          reject(
            new JailSpawnError(
              'Script jail renderer exited during initialization',
            ),
          );
        jailContents.once('render-process-gone', onDead);
        jailContents.once('destroyed', onDead);
        timer = setTimeout(
          () =>
            reject(
              new JailSpawnError(
                `Script jail initialization timed out after ${SCRIPT_JAIL_ACQUIRE_TIMEOUT_MS}ms`,
              ),
            ),
          SCRIPT_JAIL_ACQUIRE_TIMEOUT_MS,
        );
      }),
    ]);
    if (jailContents.isDestroyed()) {
      throw new JailSpawnError(
        'Script jail WebContents was destroyed during initialization',
      );
    }
    return { jailView, jailContents, spawnEpoch };
  } catch (err: unknown) {
    closeJailView(jailView, jailContents);
    throw err instanceof JailSpawnError
      ? err
      : new JailSpawnError(err instanceof Error ? err.message : String(err));
  } finally {
    if (timer !== undefined) {
      clearTimeout(timer);
    }
    if (onDead) {
      jailContents.removeListener('render-process-gone', onDead);
      jailContents.removeListener('destroyed', onDead);
    }
  }
}

async function createWarmJailEntry(
  attempt = 0,
): Promise<IdleWarmJailEntry | null> {
  try {
    const { jailView, jailContents, spawnEpoch } = await spawnJail();
    if (
      !poolActivated ||
      preloadEpoch !== spawnEpoch ||
      jailContents.isDestroyed()
    ) {
      closeJailView(jailView, jailContents);
      return poolActivated && preloadEpoch !== spawnEpoch && attempt < 2
        ? createWarmJailEntry(attempt + 1)
        : null;
    }
    const onIdleDead = (): void => {
      const idx = idleWarmJails.indexOf(entryRef);
      if (idx !== -1) {
        idleWarmJails.splice(idx, 1);
      }
      entryRef.detachIdleListeners();
      closeJailView(jailView, jailContents);
      consecutiveIdleDeaths++;
      schedulePoolRefill();
    };
    const detachIdleListeners = (): void => {
      jailContents.removeListener('render-process-gone', onIdleDead);
      jailContents.removeListener('destroyed', onIdleDead);
    };
    const entryRef: IdleWarmJailEntry = {
      jailView,
      jailContents,
      detachIdleListeners,
    };
    jailContents.once('render-process-gone', onIdleDead);
    jailContents.once('destroyed', onIdleDead);
    return entryRef;
  } catch {
    return null;
  }
}

function schedulePoolRefill(): void {
  if (
    !poolActivated ||
    consecutiveIdleDeaths > MAX_CONSECUTIVE_IDLE_DEATHS
  ) {
    return;
  }
  while (
    idleWarmJails.length + inFlightWarmups.size <
    SCRIPT_JAIL_POOL_TARGET_IDLE
  ) {
    const taskPromise: Promise<void> = createWarmJailEntry()
      .then((entry) => {
        if (!entry) {
          return;
        }
        if (
          poolActivated &&
          idleWarmJails.length < SCRIPT_JAIL_POOL_TARGET_IDLE &&
          !entry.jailContents.isDestroyed()
        ) {
          idleWarmJails.push(entry);
        } else {
          entry.detachIdleListeners();
          closeJailView(entry.jailView, entry.jailContents);
        }
      })
      .finally(() => {
        inFlightWarmups.delete(taskPromise);
      });
    inFlightWarmups.add(taskPromise);
  }
}

async function ensurePoolReady(): Promise<void> {
  jailSessionInstance ??= session.fromPartition(SCRIPT_JAIL_PARTITION, {
    cache: false,
  });
  await hardenScriptJailSession(jailSessionInstance);
  poolActivated = true;
  consecutiveIdleDeaths = 0;
}

async function acquireJailForRun(): Promise<AcquiredJail> {
  await ensurePoolReady();
  while (idleWarmJails.length > 0) {
    const warm = idleWarmJails.shift();
    if (!warm) {
      continue;
    }
    warm.detachIdleListeners();
    if (!warm.jailContents.isDestroyed()) {
      schedulePoolRefill();
      return { jailView: warm.jailView, jailContents: warm.jailContents };
    }
    liveJailViews.delete(warm.jailView);
  }
  try {
    const { jailView, jailContents } = await spawnJail();
    return { jailView, jailContents };
  } finally {
    schedulePoolRefill();
  }
}

/**
 * Activates and pre-warms the disposable script-jail pool (`SCRIPT_JAIL_POOL_TARGET_IDLE = 2`).
 *
 * The pool is NOT warmed at Hub startup so flag-off users pay zero memory or process cost.
 * Once activated via `warmScriptJail` or the first `runScriptInJail` call, the pool maintains
 * 2 idle pre-warmed jails (with `about:blank` loaded) and refills asynchronously as jails are consumed.
 */
export async function warmScriptJail(
  sender: ScriptJailSenderBase | null | undefined,
  senderFrame?: ScriptJailSenderFrame | null,
): Promise<boolean> {
  assertAuthorizedJailSender(sender, senderFrame);
  if (app.commandLine.hasSwitch('no-sandbox')) {
    return false;
  }
  try {
    await ensurePoolReady();
    schedulePoolRefill();
    await waitForScriptJailPoolIdle();
    return idleWarmJails.length > 0;
  } catch {
    return false;
  }
}

/** Aborts a queued or running script-jail execution owned by `sender`. */
export function abortScriptInJail(
  sender: ScriptJailSenderBase | null | undefined,
  jailId: unknown,
  senderFrame?: ScriptJailSenderFrame | null,
): boolean {
  assertAuthorizedJailSender(sender, senderFrame);
  if (!isValidJailId(jailId)) {
    return false;
  }
  const queued = queuedJails.get(jailId);
  if (queued) {
    if (queued.senderId !== sender.id) {
      return false;
    }
    queued.settleQueued(
      failResult('SCRIPT_ABORTED', 'Script aborted by host'),
    );
    return true;
  }
  const active = activeJails.get(jailId);
  if (!active || active.senderId !== sender.id) {
    return false;
  }
  active.abort(failResult('SCRIPT_ABORTED', 'Script aborted by host'));
  return true;
}

function pumpJailQueue(): void {
  while (activeJails.size < MAX_CONCURRENT_JAILS_GLOBAL) {
    let nextEntry: QueuedJailEntry | undefined;
    for (const entry of queuedJails.values()) {
      if (
        getRunningCountForSender(entry.senderId) <
        MAX_CONCURRENT_JAILS_PER_SENDER
      ) {
        nextEntry = entry;
        break;
      }
    }
    if (!nextEntry) {
      break;
    }
    queuedJails.delete(nextEntry.jailId);
    admitAndRunJailEntry(nextEntry);
  }
}

function admitAndRunJailEntry(entry: QueuedJailEntry): void {
  const { jailId, sender: authorizedSender, req, timeoutMs, resolveRun } = entry;
  let earlyResult: ScriptJailFailureResult | undefined;
  let resolveEarlyResult!: (res: ScriptJailFailureResult) => void;
  const earlyExitPromise = new Promise<ScriptJailFailureResult>((resolve) => {
    resolveEarlyResult = (res) => {
      earlyResult ??= res;
      resolve(earlyResult);
    };
  });
  entry.detachQueuedWatcher();
  const unregisterRunningSenderAbort = registerSenderAbortCallback(
    authorizedSender,
    (res) => resolveEarlyResult(res),
  );
  activeJails.set(jailId, {
    senderId: authorizedSender.id,
    abort: (res) =>
      resolveEarlyResult(
        res ?? failResult('SCRIPT_ABORTED', 'Script aborted by host'),
      ),
  });
  void (async (): Promise<ScriptJailResult> => {
    // Keep jailView anchored in the run closure (in addition to liveJailViews)
    // so V8 GC cannot finalize the detached WebContentsView while webContents is executing.
    let jailView: WebContentsView | undefined;
    let jailContents: WebContents | undefined;
    let port1: MessagePortMain | undefined;
    let port2: MessagePortMain | undefined;
    let timeoutHandle: ReturnType<typeof setTimeout> | undefined;
    let rssWatchdogHandle: ReturnType<typeof setInterval> | undefined;
    try {
      let acquireTimeoutHandle: ReturnType<typeof setTimeout> | undefined;
      const acquirePromise = acquireJailForRun();
      const closeOrphanAcquire = (): void => {
        void acquirePromise.then(
          (j) => {
            closeJailView(j.jailView, j.jailContents);
          },
          () => {},
        );
      };
      try {
        const acquiredOrEarly = await Promise.race<AcquireRaceResult>([
          acquirePromise.then(
            (jail): AcquireRaceResult => ({ kind: 'acquired', jail }),
          ),
          earlyExitPromise.then(
            (res): AcquireRaceResult => ({ kind: 'early', res }),
          ),
          new Promise<never>((_, reject) => {
            acquireTimeoutHandle = setTimeout(
              () =>
                reject(
                  new JailSpawnError(
                    `Script jail acquisition timed out after ${SCRIPT_JAIL_ACQUIRE_TIMEOUT_MS}ms`,
                  ),
                ),
              SCRIPT_JAIL_ACQUIRE_TIMEOUT_MS,
            );
          }),
        ]);
        if (acquiredOrEarly.kind === 'early') {
          closeOrphanAcquire();
          return acquiredOrEarly.res;
        }
        ({ jailView, jailContents } = acquiredOrEarly.jail);
      } catch (acquireErr: unknown) {
        closeOrphanAcquire();
        throw acquireErr;
      } finally {
        if (acquireTimeoutHandle !== undefined) {
          clearTimeout(acquireTimeoutHandle);
        }
      }
      if (earlyResult) {
        return earlyResult;
      }
      // Start execution timeout at jail assignment (not while queued or acquiring a jail).
      timeoutHandle = setTimeout(() => {
        resolveEarlyResult(
          failResult(
            'TIMEOUT',
            `Script execution timed out after ${timeoutMs}ms`,
          ),
        );
      }, timeoutMs);
      ({ port1, port2 } = new MessageChannelMain());
      const bootstrapCode = buildScriptJailBootstrapCode({
        pageFacadeBundle: req.pageFacadeBundle,
        modelScript: req.modelScript,
      });
      jailContents.once('render-process-gone', (_event, details) => {
        resolveEarlyResult(
          failResult(
            'JAIL_CRASHED',
            `Script jail renderer process exited (${details?.reason ?? 'unknown'})`,
          ),
        );
      });
      jailContents.once('destroyed', () => {
        resolveEarlyResult(
          failResult('JAIL_CRASHED', 'Script jail WebContents was destroyed'),
        );
      });
      rssWatchdogHandle = setInterval(() => {
        if (!jailContents || jailContents.isDestroyed()) {
          return;
        }
        const pid = jailContents.getOSProcessId();
        if (typeof pid !== 'number' || pid <= 0) {
          return;
        }
        const rssKB =
          app.getAppMetrics().find((m) => m.pid === pid)?.memory
            ?.workingSetSize ?? 0;
        if (rssKB > MAX_SCRIPT_JAIL_RSS_KB) {
          resolveEarlyResult(
            failResult(
              'RESOURCE_LIMIT',
              `Script jail exceeded memory limit (${MAX_SCRIPT_JAIL_RSS_KB / 1024} MB)`,
            ),
          );
          try {
            process.kill(pid, 'SIGKILL');
          } catch {
            // Ignore if process already exited.
          }
        }
      }, SCRIPT_JAIL_RSS_POLL_INTERVAL_MS);
      const execPromise: Promise<ScriptJailResult> = executeJavaScriptWhenReady(
        jailContents,
        bootstrapCode,
        false,
      ).then(
        (rawOutcome): ScriptJailResult => {
          if (!rawOutcome || typeof rawOutcome !== 'object') {
            return failResult(
              'JAIL_CRASHED',
              'Script jail returned an invalid bootstrap response',
            );
          }
          const outcome = rawOutcome as RawWorkerOutcome;
          if (outcome.ok === true) {
            return outcome.value === undefined
              ? { ok: true }
              : { ok: true, value: outcome.value };
          }
          const mappedErrorCode: ScriptJailErrorCode =
            typeof outcome.errorCode === 'string' &&
            WORKER_ERROR_CODES.has(outcome.errorCode)
              ? (outcome.errorCode as ScriptJailWorkerErrorCode)
              : 'RUNTIME_ERROR';
          return failResult(
            mappedErrorCode,
            typeof outcome.errorMessage === 'string' && outcome.errorMessage
              ? outcome.errorMessage
              : 'Script execution failed',
          );
        },
        (err: unknown): ScriptJailResult =>
          failResult(
            'JAIL_CRASHED',
            err instanceof Error ? err.message : String(err),
          ),
      );
      // Port-as-admission: deliver port2 to the requesting renderer ONLY when
      // a jail has been assigned and started.
      authorizedSender.postMessage(SCRIPT_JAIL_PORT_CHANNEL, { jailId }, [
        port2,
      ]);
      jailContents.postMessage(SCRIPT_JAIL_PORT_CHANNEL, null, [port1]);
      return await Promise.race([execPromise, earlyExitPromise]);
    } catch (err: unknown) {
      if (earlyResult) {
        return earlyResult;
      }
      const code: ScriptJailErrorCode =
        err instanceof JailSpawnError ? 'JAIL_CRASHED' : 'INTERNAL_ERROR';
      return failResult(code, err instanceof Error ? err.message : String(err));
    } finally {
      activeJails.delete(jailId);
      unregisterRunningSenderAbort();
      if (timeoutHandle !== undefined) {
        clearTimeout(timeoutHandle);
      }
      if (rssWatchdogHandle !== undefined) {
        clearInterval(rssWatchdogHandle);
      }
      try {
        port1?.close();
        port2?.close();
        closeJailView(jailView, jailContents);
      } finally {
        // A throwing teardown step must not stall the queue or the warm pool.
        pumpJailQueue();
        schedulePoolRefill();
      }
    }
  })().then(resolveRun, (err: unknown) =>
    resolveRun(
      failResult(
        'INTERNAL_ERROR',
        err instanceof Error ? err.message : String(err),
      ),
    ),
  );
}

/**
 * Enqueues or admits an untrusted agent script for execution inside a single-use,
 * zero-egress `WebContentsView` from the pre-warmed pool (or cold-started on demand).
 */
export async function runScriptInJail(
  sender: ScriptJailSender | null | undefined,
  req: unknown,
  senderFrame?: ScriptJailSenderFrame | null,
): Promise<ScriptJailResult> {
  assertAuthorizedJailSender(sender, senderFrame);
  const authorizedSender = sender;
  if (app.commandLine.hasSwitch('no-sandbox')) {
    return failResult(
      'SANDBOX_UNAVAILABLE',
      'Script jail requires Chromium OS sandboxing (--no-sandbox is active)',
    );
  }
  if (!req || typeof req !== 'object') {
    return failResult('INVALID_REQUEST', 'Request payload must be an object');
  }
  const rawReq = req as Record<string, unknown>;
  if (!isValidJailId(rawReq.jailId)) {
    return failResult(
      'INVALID_REQUEST',
      'jailId must be a valid non-empty identifier',
    );
  }
  const jailId = rawReq.jailId;
  if (activeJails.has(jailId) || queuedJails.has(jailId)) {
    return failResult('INVALID_REQUEST', `Duplicate active jailId: ${jailId}`);
  }
  if (
    typeof rawReq.pageFacadeBundle !== 'string' ||
    rawReq.pageFacadeBundle.trim() === ''
  ) {
    return failResult(
      'INVALID_REQUEST',
      'pageFacadeBundle must be a non-empty string',
    );
  }
  if (typeof rawReq.modelScript !== 'string') {
    return failResult('INVALID_REQUEST', 'modelScript must be a string');
  }
  const senderId = authorizedSender.id;
  const canAdmitImmediately =
    activeJails.size < MAX_CONCURRENT_JAILS_GLOBAL &&
    getRunningCountForSender(senderId) < MAX_CONCURRENT_JAILS_PER_SENDER &&
    getQueuedCountForSender(senderId) === 0;
  if (!canAdmitImmediately) {
    if (getQueuedCountForSender(senderId) >= MAX_QUEUED_JAILS_PER_SENDER) {
      return failResult(
        'RESOURCE_LIMIT',
        `Too many queued script jails for sender (max ${MAX_QUEUED_JAILS_PER_SENDER})`,
      );
    }
    if (queuedJails.size >= MAX_QUEUED_JAILS_GLOBAL) {
      return failResult(
        'RESOURCE_LIMIT',
        `Too many queued script jails globally (max ${MAX_QUEUED_JAILS_GLOBAL})`,
      );
    }
  }
  const timeoutMs = resolveScriptJailTimeoutMs(rawReq);
  const validatedReq: ScriptJailRequest = {
    jailId,
    pageFacadeBundle: rawReq.pageFacadeBundle,
    modelScript: rawReq.modelScript,
    ...(typeof rawReq.timeoutMs === 'number'
      ? { timeoutMs: rawReq.timeoutMs }
      : {}),
  };
  return new Promise<ScriptJailResult>((resolveRun) => {
    const detachQueuedWatcher = registerSenderAbortCallback(
      authorizedSender,
      (res) => entry.settleQueued(res),
    );
    const entry: QueuedJailEntry = {
      jailId,
      sender: authorizedSender,
      senderId,
      req: validatedReq,
      timeoutMs,
      resolveRun,
      detachQueuedWatcher,
      settleQueued: (res) => {
        if (!queuedJails.delete(jailId)) {
          return;
        }
        detachQueuedWatcher();
        resolveRun(res);
      },
    };
    queuedJails.set(jailId, entry);
    pumpJailQueue();
  });
}
