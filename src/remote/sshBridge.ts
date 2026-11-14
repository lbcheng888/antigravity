/**
 * Main-process side of window.sshRemote. preload.ts declares its API; the
 * toolkit mirrors it in src/features/sshRemote/sshRemoteBridge.ts.
 */
import {
  BrowserWindow,
  ipcMain,
  type IpcMainInvokeEvent,
  type WebContents,
} from 'electron';
import log from 'electron-log/main';
import { isLocalHubTopFrame } from '../utils';
import { isRemoteContents } from './remoteWindowSecurity';
import {
  RemoteError,
  type RecentSshHost,
  type RemoteErrorCode,
} from './types';
import { remoteBackendOf } from './windowBackends';

const SSH_CONNECT_INTENTS = ['newProject'] as const;

export type SshIntent = (typeof SSH_CONNECT_INTENTS)[number];

/** Tells an SSH window that it has an intent to take (ssh:take-intent). */
export const SSH_INTENT_CHANNEL = 'ssh:intent';

/** Same as sshDestination.ts's limit. */
const MAX_HOST_INPUT = 1024;
const MAX_CONFIG_FILE_INPUT = 4096;
const BAD_CONFIG_FILE = 'The SSH config file must be a path of at most 4 KiB.';

export interface SshConnectedEntry {
  host: string;
  configFile?: string;
}

export interface SshState {
  currentHost: string | null;
  connectedHosts: string[];
  connected: SshConnectedEntry[];
}

export interface SshListHostsOptions {
  configFile?: string;
}

export interface SshListHostsResult {
  configHosts: string[];
  recent: RecentSshHost[];
  recentHosts: string[];
  recentConfigFiles: Record<string, string>;
}

export interface SshChooseConfigFileResult {
  configFile?: string;
}

export interface SshConnectBackendOptions {
  intent?: SshIntent;
  configFile?: string;
}

export type SshConnectResult =
  | { ok: true }
  | { ok: false; code: RemoteErrorCode; message: string };

export interface SshProgressPayload {
  requestId: number;
  message: string;
}

/** Main-process backend serving `window.sshRemote` IPC calls. */
export interface SshBridgeBackend {
  getState(sender: WebContents): SshState | Promise<SshState>;
  listHosts(
    options?: SshListHostsOptions,
  ): Promise<SshListHostsResult> | SshListHostsResult;
  chooseConfigFile(
    sender: WebContents,
  ):
    | Promise<SshChooseConfigFileResult | null>
    | SshChooseConfigFileResult
    | null;
  connect(
    sender: WebContents,
    host: string,
    onProgress: (message: string) => void,
    options?: SshConnectBackendOptions,
  ): Promise<SshConnectResult>;
  takeIntent(
    sender: WebContents,
  ): SshIntent | undefined | Promise<SshIntent | undefined>;
  stopServer(sender: WebContents): Promise<void> | void;
  openLocalWindow(): void | Promise<void>;
}

interface ParsedConnectOptions {
  requestId?: number;
  intent?: SshIntent;
}

/** The page's `{configFile}` (null means none); undefined if it isn't one. */
function configFileOption(
  options: unknown,
): { configFile?: string } | undefined {
  const value =
    typeof options === 'object' && options !== null && 'configFile' in options
      ? options.configFile
      : undefined;
  if (value === undefined || value === null) {
    return {};
  }
  return typeof value === 'string' &&
    Buffer.byteLength(value) <= MAX_CONFIG_FILE_INPUT
    ? { configFile: value }
    : undefined;
}

function parseConnectOptions(options: unknown): ParsedConnectOptions {
  if (typeof options !== 'object' || options === null) {
    return {};
  }
  const value = 'intent' in options ? options.intent : undefined;
  const intent = SSH_CONNECT_INTENTS.find((i) => i === value);
  if (value !== undefined && !intent) {
    // A newer toolkit's: connect anyway. The value isn't logged (page input).
    log.warn('[SSH] Ignoring an unknown connect intent.');
  }
  const requestId = 'requestId' in options ? options.requestId : undefined;
  return typeof requestId === 'number' &&
    Number.isSafeInteger(requestId) &&
    requestId > 0
    ? { requestId, intent }
    : { intent };
}

/** Registers the `ssh:*` IPC handlers backed by `backend`. */
export function registerSshBridgeIpc(backend: SshBridgeBackend): void {
  const localOnly = (channel: string, event: IpcMainInvokeEvent): void => {
    if (!isLocalHubTopFrame(event)) {
      throw new Error(`${channel} is only available in local windows`);
    }
  };
  // Any frame of an SSH window may use these: its host serves them all.
  const sshOnly = (channel: string, event: IpcMainInvokeEvent): void => {
    if (!remoteBackendOf(event.sender)) {
      throw new Error(`${channel} is only available in SSH windows`);
    }
  };

  ipcMain.handle('ssh:get-state', (event: IpcMainInvokeEvent) => {
    // Remote contents get only their own host (RemoteManager.getState).
    if (!isRemoteContents(event.sender)) {
      localOnly('ssh:get-state', event);
    }
    return backend.getState(event.sender);
  });

  ipcMain.handle(
    'ssh:list-hosts',
    (event: IpcMainInvokeEvent, options: unknown) => {
      localOnly('ssh:list-hosts', event);
      const file = configFileOption(options);
      if (!file) {
        throw new RemoteError('INVALID_DESTINATION', BAD_CONFIG_FILE);
      }
      return backend.listHosts(file);
    },
  );

  ipcMain.handle('ssh:choose-config-file', (event: IpcMainInvokeEvent) => {
    localOnly('ssh:choose-config-file', event);
    return backend.chooseConfigFile(event.sender);
  });

  ipcMain.handle(
    'ssh:connect',
    async (
      event: IpcMainInvokeEvent,
      host: unknown,
      options: unknown,
    ): Promise<SshConnectResult> => {
      localOnly('ssh:connect', event);
      if (typeof host !== 'string' || host.length > MAX_HOST_INPUT) {
        return {
          ok: false,
          code: 'INVALID_DESTINATION',
          message: 'Enter an SSH host, for example "devbox" or "user@host".',
        };
      }
      const file = configFileOption(options);
      if (!file) {
        return {
          ok: false,
          code: 'INVALID_DESTINATION',
          message: BAD_CONFIG_FILE,
        };
      }
      const { requestId, intent } = parseConnectOptions(options);
      const sender = event.sender;
      const onProgress = (message: string): void => {
        if (requestId !== undefined && !sender.isDestroyed()) {
          const progress: SshProgressPayload = { requestId, message };
          try {
            sender.send('ssh:progress', progress);
          } catch {
            // The window is closing.
          }
        }
      };
      return backend.connect(sender, host, onProgress, { intent, ...file });
    },
  );

  // Keyed by the sender: a window only ever gets its own intent.
  ipcMain.handle('ssh:take-intent', (event: IpcMainInvokeEvent) => {
    sshOnly('ssh:take-intent', event);
    return backend.takeIntent(event.sender);
  });

  ipcMain.handle(
    'ssh:stop-server',
    async (event: IpcMainInvokeEvent): Promise<void> => {
      sshOnly('ssh:stop-server', event);
      await backend.stopServer(event.sender);
    },
  );

  // Remote content mustn't pop up windows while you're elsewhere.
  ipcMain.handle('ssh:open-local-window', (event: IpcMainInvokeEvent): void => {
    sshOnly('ssh:open-local-window', event);
    if (!BrowserWindow.fromWebContents(event.sender)?.isFocused()) {
      throw new Error(
        'ssh:open-local-window is only available while the window is focused',
      );
    }
    backend.openLocalWindow();
  });
}
