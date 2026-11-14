/**
 * Preload script — runs in every BrowserWindow before the page loads.
 * Exposes a minimal, secure API via contextBridge so the renderer can
 * communicate with the main-process auto-updater without nodeIntegration.
 */
import {
  contextBridge,
  ipcRenderer,
  webFrame,
  webUtils,
  type IpcRendererEvent,
} from 'electron';

export interface UpdaterState {
  type: string;
  update?: {
    version: string;
  };
}

export interface UpdaterAPI {
  onStateChanged: (callback: (state: UpdaterState) => void) => () => void;
  applyUpdate: () => Promise<void>;
  quitAndInstall: () => Promise<void>;
  checkForUpdates: () => Promise<void>;
  getState: () => Promise<UpdaterState>;
}

export interface DialogAPI {
  showOpenDialog: () => Promise<string | undefined>;
  showOpenMultipleFolderDialog: () => Promise<string[]>;
}

export interface NativeNotificationOptions {
  title: string;
  body: string;
  silent?: boolean;
  payload?: unknown;
}

export interface NotificationAPI {
  send: (options: NativeNotificationOptions) => Promise<void>;
  openSystemPreferences: () => Promise<void>;
  onClicked: (callback: (payload: unknown) => void) => () => void;
}

export type StorageItems = Record<string, string>;
export type StorageChanges = Record<string, string | null>;

export interface StorageAPI {
  getItems: () => Promise<StorageItems>;
  updateItems: (changes: StorageChanges) => Promise<void>;
  onChanged: (callback: (changes: StorageChanges) => void) => () => void;
}

export interface LogsAPI {
  getElectronLogs: () => Promise<string>;
}

export interface ExtensionsAPI {
  sendAuthorities: (authoritiesMap: Record<string, string>) => Promise<void>;
}

export interface DeepLinkAPI {
  onDeepLink: (callback: (url: string) => void) => () => void;
  getStoredDeepLink: () => Promise<string | null>;
}

export interface AgentAPI {
  updateActiveAgentCount: (count: number) => Promise<void>;
}

export interface WcvBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface WcvViewportSize {
  width: number;
  height: number;
}

export interface WcvAcquireRequest {
  tabKey: string;
  partition: string;
  initialUrl?: string;
  title?: string;
  webmcpEnabled?: boolean;
  [key: string]: unknown;
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

export interface WcvBrowserAPI {
  acquire: (req: WcvAcquireRequest) => Promise<unknown>;
  show: (tabKey: string, boundsCss: WcvBounds) => Promise<unknown>;
  hide: (tabKey: string) => Promise<void>;
  setBounds: (tabKey: string, boundsCss: WcvBounds) => Promise<void>;
  setThemeColor: (color: string) => Promise<boolean>;
  setPinned: (tabKey: string, pinned: boolean) => Promise<void>;
  setAgentControlling: (tabKey: string, active: boolean) => Promise<unknown>;
  setOverlayActive: (tabKey: string, active: boolean) => Promise<unknown>;
  setPortalOverlayOpen: (open: boolean) => Promise<void>;
  elevate: (
    tabKey: string,
    reason?: string,
    focusBrowser?: boolean,
  ) => Promise<unknown>;
  setDragOver: (tabKey: string, active: boolean) => Promise<unknown>;
  demote: (tabKey: string, reason?: string) => Promise<unknown>;
  navigate: (tabKey: string, url: string) => Promise<unknown>;
  control: (
    tabKey: string,
    action: WcvControlAction | (string & {}),
  ) => Promise<void>;
  sendMouse: (
    tabKey: string,
    params: Record<string, unknown>,
    coordSpace?: string,
  ) => Promise<unknown>;
  sendKey: (
    tabKey: string,
    params: Record<string, unknown>,
  ) => Promise<unknown>;
  insertText: (tabKey: string, text: string) => Promise<unknown>;
  setViewportSize: (
    tabKey: string,
    size: WcvViewportSize | null,
  ) => Promise<boolean>;
  capturePage: (tabKey: string) => Promise<unknown>;
  dispose: (tabKey: string) => Promise<void>;
  onGuestEvent: (cb: (payload: unknown) => void) => () => void;
  onZOrderChanged: (cb: (payload: unknown) => void) => () => void;
  onShortcutForwarded: (cb: (payload: unknown) => void) => () => void;
  onCursorChanged: (cb: (payload: unknown) => void) => () => void;
}

export interface TitleBarOverlayOptions {
  color?: string;
  symbolColor?: string;
}

export type DeviceChooserResolution =
  | { action: 'claim' | 'unclaimed' | 'cancel' }
  | { action: 'select'; deviceId: string };

export interface ContextMenuItem {
  id?: string;
  label?: string;
  type?: 'normal' | 'separator' | 'submenu' | 'checkbox' | 'radio';
  checked?: boolean;
  disabled?: boolean;
  accelerator?: string;
  submenu?: ContextMenuItem[];
}

export interface WebviewSecurityState {
  webContentsId: number;
  seq: number;
  url: string;
  level: string;
  [key: string]: unknown;
}

export interface ElectronNativeAPI {
  getZoomLevel: () => number;
  getPathForFile: (file: File) => string;
  setTitleBarOverlay: (options: TitleBarOverlayOptions) => Promise<void>;
  minimize: () => Promise<void>;
  maximize: () => Promise<void>;
  unmaximize: () => Promise<void>;
  isMaximized: () => Promise<boolean>;
  close: () => Promise<void>;
  toggleDevTools: () => Promise<void>;
  zoomIn: () => void;
  zoomOut: () => void;
  resetZoom: () => void;
  openExternal: (url: string) => Promise<void>;
  revealInFilePicker: (path: string) => Promise<void>;
  onWebviewDeviceChooserEvent: (
    callback: (payload: unknown) => void,
  ) => () => void;
  respondWebviewDeviceChooser: (
    requestId: string,
    resolution: DeviceChooserResolution,
  ) => Promise<boolean>;
  getWebviewMemoryUsage: (
    webContentsIds: number[],
  ) => Promise<Record<number, number>>;
  getDefaultDownloadDirectory: () => Promise<string | null>;
  showContextMenu: (items: ContextMenuItem[]) => Promise<string | null>;
  closeContextMenu: () => Promise<void>;
  executeJavaScriptInIsolatedWorld: (
    wcId: number,
    worldId: number,
    code: string,
  ) => Promise<unknown>;
  scriptJailProtocol: number;
  listWebviewFrames: (wcId: number) => Promise<unknown>;
  executeJavaScriptInFrameIsolatedWorld: (
    wcId: number,
    frameId: string,
    code: string,
    expectedOrigin?: string,
  ) => Promise<unknown>;
  runScriptInJail: (req: Record<string, unknown>) => Promise<unknown>;
  abortScriptJail: (jailId: string) => Promise<unknown>;
  warmScriptJail: () => Promise<boolean>;
  subscribeWebviewSecurityState: (
    webContentsId: number,
    callback: (state: WebviewSecurityState) => void,
  ) => () => void;
  allowWebviewCertificate: (
    webContentsId: number,
    url: string,
  ) => Promise<unknown>;
  clearWebviewSiteData: (
    webContentsId: number,
    origin?: string,
  ) => Promise<unknown>;
  onWebviewOpenNewTab: (
    callback: (url: string, webContentsId: number) => void,
  ) => () => void;
  setWebviewDownloadDirectory: (
    webContentsId: number,
    downloadDir: string | null,
  ) => Promise<unknown>;
  setWebviewAgentControlled: (
    webContentsId: number,
    active: boolean,
  ) => Promise<boolean>;
  openWebviewDownload: (downloadId: string) => Promise<unknown>;
  showWebviewDownloadInFolder: (downloadId: string) => Promise<unknown>;
  cancelWebviewDownload: (downloadId: string) => Promise<unknown>;
  resolveWebviewDownload: (
    downloadId: string,
    approve: boolean,
  ) => Promise<boolean>;
  selectDownloadDirectory: (defaultPath?: string) => Promise<string | null>;
  onWebviewFaviconUpdated: (callback: (payload: unknown) => void) => () => void;
  onWebviewDownloadProgress: (
    callback: (payload: unknown) => void,
  ) => () => void;
  setWebviewDialogPolicy: (
    webContentsId: number,
    policy: Record<string, unknown>,
  ) => Promise<unknown>;
  handleWebviewDialog: (
    webContentsId: number,
    decision: Record<string, unknown>,
  ) => Promise<boolean>;
  onWebviewDialogOpening: (callback: (payload: unknown) => void) => () => void;
  getWebviewNetworkRequests: (webContentsId: number) => Promise<unknown>;
  clearWebviewNetworkRequests: (webContentsId: number) => Promise<unknown>;
  onWebviewNetworkRequest: (
    callback: (payload: unknown) => void,
  ) => () => void;
  onWebviewPromptRequest: (callback: (payload: unknown) => void) => () => void;
  respondWebviewPrompt: (
    promptId: string,
    value: string | null,
  ) => Promise<boolean>;
  setWebviewFileChooserPolicy: (
    webContentsId: number,
    policy: Record<string, unknown>,
  ) => Promise<unknown>;
  handleWebviewFileChooser: (
    webContentsId: number,
    decision: Record<string, unknown>,
  ) => Promise<unknown>;
  setWebviewInputFiles: (
    webContentsId: number,
    request: Record<string, unknown>,
  ) => Promise<unknown>;
  resolveWebviewUploadFiles: (
    request: Record<string, unknown>,
  ) => Promise<unknown>;
  onWebviewFileChooserOpened: (
    callback: (payload: unknown) => void,
  ) => () => void;
  setWebviewWebMcpEnabled: (enabled: boolean) => Promise<boolean>;
  listWebviewWebMcpTools: (
    webContentsId: number,
    origin: string,
  ) => Promise<unknown>;
  callWebviewWebMcpTool: (
    webContentsId: number,
    origin: string,
    params: Record<string, unknown>,
  ) => Promise<unknown>;
  cancelWebviewWebMcpToolCall: (
    webContentsId: number,
    callId: string,
  ) => Promise<boolean>;
  wcvBrowser: WcvBrowserAPI;
}

export type RemoteNativeAPI = Pick<
  ElectronNativeAPI,
  | 'getZoomLevel'
  | 'setTitleBarOverlay'
  | 'minimize'
  | 'maximize'
  | 'unmaximize'
  | 'isMaximized'
  | 'close'
  | 'toggleDevTools'
  | 'zoomIn'
  | 'zoomOut'
  | 'resetZoom'
  | 'openExternal'
  | 'showContextMenu'
  | 'closeContextMenu'
  | 'revealInFilePicker'
>;

export interface IdeAPI {
  isInstalled: () => Promise<boolean>;
}

export interface WslDistroInfo {
  name: string;
  isDefault: boolean;
}

export interface WslState {
  available: boolean;
  activeDistro: string | null;
  distros: WslDistroInfo[];
}

export interface WslAPI {
  getState: () => Promise<WslState>;
  connect: (distro?: string) => Promise<void>;
}

export type MicAccessStatus =
  | 'not-determined'
  | 'granted'
  | 'denied'
  | 'restricted'
  | 'unknown';

export interface DesktopMicAPI {
  getAccessStatus: () => Promise<MicAccessStatus>;
  requestAccess: () => Promise<MicAccessStatus>;
  openSystemPreferences: () => Promise<void>;
}

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
  configFile?: string | null;
}

export type SshIntent = 'newProject';

export interface SshConnectOptions {
  onProgress?: (message: string) => void;
  intent?: SshIntent | string;
  configFile?: string | null;
}

export type SshConnectResult =
  | { ok: true }
  | { ok: false; code: string; message: string };

export interface LocalSshRemoteAPI {
  kind: 'local';
  getState: () => Promise<SshState>;
  listHosts: (options?: SshListHostsOptions) => Promise<unknown>;
  chooseConfigFile: () => Promise<{ configFile?: string } | null>;
  connect: (
    host: string,
    options?: SshConnectOptions,
  ) => Promise<SshConnectResult>;
}

export interface RemoteSshRemoteAPI {
  kind: 'ssh';
  getState: () => Promise<SshState>;
  stopServer: () => Promise<void>;
  onIntent: (callback: (intent: SshIntent) => void) => () => void;
  openLocalWindow: () => Promise<void>;
}

export type SshRemoteAPI = LocalSshRemoteAPI | RemoteSshRemoteAPI;

interface ScriptJailPortPayload {
  jailId?: unknown;
}

interface SshProgressPayload {
  requestId?: unknown;
  message?: unknown;
}

interface PreloadStyleDeclaration {
  getPropertyValue?: (property: string) => string;
}

interface PreloadMutationObserver {
  observe: (target: Element, options?: MutationObserverInit) => void;
}

interface PreloadStorageLike {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
  removeItem?: (key: string) => void;
}

interface PreloadGlobal {
  document?: {
    documentElement?: Element | null;
    addEventListener?: (type: string, listener: () => void) => void;
  };
  getComputedStyle?: (elt: Element) => PreloadStyleDeclaration;
  MutationObserver?: new (callback: () => void) => PreloadMutationObserver;
  addEventListener?: (type: string, listener: () => void) => void;
  location?: {
    hostname?: string;
    protocol?: string;
  };
  localStorage?: Partial<PreloadStorageLike>;
  top?: unknown;
  self?: unknown;
}

function subscribeIpcChannel<T>(
  channel: string,
  callback: (payload: T) => void,
): () => void {
  const handler = (_event: IpcRendererEvent, payload: T): void => {
    callback(payload);
  };
  ipcRenderer.on(channel, handler);
  // Return unsubscribe function
  return () => {
    ipcRenderer.removeListener(channel, handler);
  };
}

function subscribeRefCountedIpcChannel<T>(
  eventChannel: string,
  stateChannel: string,
  updateCount: (delta: 1 | -1) => number,
  callback: (payload: T) => void,
): () => void {
  if (updateCount(1) === 1) {
    ipcRenderer.send(stateChannel, true);
  }
  const unsubscribe = subscribeIpcChannel(eventChannel, callback);
  let active = true;
  return () => {
    if (!active) {
      return;
    }
    active = false;
    unsubscribe();
    if (updateCount(-1) === 0) {
      ipcRenderer.send(stateChannel, false);
    }
  };
}

const updaterAPI: UpdaterAPI = {
  onStateChanged: (callback) =>
    subscribeIpcChannel('updater:state-changed', callback),
  applyUpdate: () => ipcRenderer.invoke('updater:apply'),
  quitAndInstall: () => ipcRenderer.invoke('updater:quit-and-install'),
  checkForUpdates: () => ipcRenderer.invoke('updater:check-for-updates'),
  getState: () => ipcRenderer.invoke('updater:get-state'),
};

let deviceChooserSubscriberCount = 0;

const dialogAPI: DialogAPI = {
  showOpenDialog: () => ipcRenderer.invoke('dialog:open-workspace'),
  showOpenMultipleFolderDialog: () => ipcRenderer.invoke('dialog:open-workspaces'),
};

const notificationAPI: NotificationAPI = {
  send: (options) => ipcRenderer.invoke('notification:send', options),
  openSystemPreferences: () =>
    ipcRenderer.invoke('notification:open-system-preferences'),
  onClicked: (callback) =>
    subscribeIpcChannel('notification:clicked', callback),
};

const storageAPI: StorageAPI = {
  getItems: () => ipcRenderer.invoke('storage:get-items'),
  updateItems: (changes) => ipcRenderer.invoke('storage:update-items', changes),
  onChanged: (callback) => subscribeIpcChannel('storage:changed', callback),
};

const logsAPI: LogsAPI = {
  getElectronLogs: () => ipcRenderer.invoke('logs:electron'),
};

const extensionsAPI: ExtensionsAPI = {
  sendAuthorities: (authoritiesMap) =>
    ipcRenderer.invoke('extensions:send-authorities', authoritiesMap),
};

const deepLinkAPI: DeepLinkAPI = {
  onDeepLink: (callback) => subscribeIpcChannel('deep-link', callback),
  getStoredDeepLink: () => ipcRenderer.invoke('deep-link:get-stored'),
};

const agentAPI: AgentAPI = {
  updateActiveAgentCount: (count) =>
    ipcRenderer.invoke('agent:update-active-count', count),
};

const wcvBrowserAPI: WcvBrowserAPI = {
  acquire: (req) => ipcRenderer.invoke('wcv:acquire', req),
  show: (tabKey, boundsCss) => ipcRenderer.invoke('wcv:show', tabKey, boundsCss),
  hide: (tabKey) => ipcRenderer.invoke('wcv:hide', tabKey),
  setBounds: (tabKey, boundsCss) => {
    ipcRenderer.send('wcv:set-bounds-sync', tabKey, boundsCss);
    return Promise.resolve();
  },
  setThemeColor: (color) => ipcRenderer.invoke('wcv:set-theme-color', color),
  setPinned: (tabKey, pinned) =>
    ipcRenderer.invoke('wcv:set-pinned', tabKey, pinned),
  setAgentControlling: (tabKey, active) =>
    ipcRenderer.invoke('wcv:set-agent-controlling', tabKey, active),
  setOverlayActive: (tabKey, active) =>
    ipcRenderer.invoke('wcv:set-overlay-active', tabKey, active),
  setPortalOverlayOpen: (open) =>
    ipcRenderer.invoke('wcv:set-portal-overlay-open', open),
  elevate: (tabKey, reason, focusBrowser) =>
    ipcRenderer.invoke('wcv:elevate', tabKey, reason, focusBrowser),
  setDragOver: (tabKey, active) =>
    ipcRenderer.invoke('wcv:set-drag-over', tabKey, active),
  demote: (tabKey, reason) => ipcRenderer.invoke('wcv:demote', tabKey, reason),
  navigate: (tabKey, url) => ipcRenderer.invoke('wcv:navigate', tabKey, url),
  control: (tabKey, action) =>
    ipcRenderer.invoke('wcv:control', tabKey, action),
  sendMouse: (tabKey, params, coordSpace) =>
    coordSpace !== undefined
      ? ipcRenderer.invoke('wcv:send-mouse', tabKey, params, coordSpace)
      : ipcRenderer.invoke('wcv:send-mouse', tabKey, params),
  sendKey: (tabKey, params) =>
    ipcRenderer.invoke('wcv:send-key', tabKey, params),
  insertText: (tabKey, text) =>
    ipcRenderer.invoke('wcv:insert-text', tabKey, text),
  setViewportSize: (tabKey, size) =>
    ipcRenderer.invoke('wcv:set-viewport-size', tabKey, size),
  capturePage: (tabKey) => ipcRenderer.invoke('wcv:capture-page', tabKey),
  dispose: (tabKey) => ipcRenderer.invoke('wcv:dispose', tabKey),
  onGuestEvent: (cb) => subscribeIpcChannel('wcv:guest-event', cb),
  onZOrderChanged: (cb) => subscribeIpcChannel('wcv:z-order-changed', cb),
  onShortcutForwarded: (cb) =>
    subscribeIpcChannel('wcv:shortcut-forwarded', cb),
  onCursorChanged: (cb) => subscribeIpcChannel('wcv:cursor-changed', cb),
};

let promptRequestSubscriberCount = 0;

const electronNativeAPI: ElectronNativeAPI = {
  getZoomLevel: () => webFrame.getZoomFactor(),
  getPathForFile: (file) => webUtils.getPathForFile(file),
  setTitleBarOverlay: (options) =>
    ipcRenderer.invoke('window:set-title-bar-overlay', options),
  minimize: () => ipcRenderer.invoke('window:minimize'),
  maximize: () => ipcRenderer.invoke('window:maximize'),
  unmaximize: () => ipcRenderer.invoke('window:unmaximize'),
  isMaximized: () => ipcRenderer.invoke('window:is-maximized'),
  close: () => ipcRenderer.invoke('window:close'),
  toggleDevTools: () => ipcRenderer.invoke('window:toggle-devtools'),
  zoomIn: () => {
    void ipcRenderer.invoke('window:zoom-in');
  },
  zoomOut: () => {
    void ipcRenderer.invoke('window:zoom-out');
  },
  resetZoom: () => {
    void ipcRenderer.invoke('window:reset-zoom');
  },
  openExternal: (url) => ipcRenderer.invoke('shell:open-external', url),
  revealInFilePicker: (path) =>
    ipcRenderer.invoke('shell:reveal-in-file-picker', path),
  onWebviewDeviceChooserEvent: (callback) =>
    subscribeRefCountedIpcChannel(
      'webview:device-chooser-event',
      'webview:device-chooser-subscriber-state',
      (delta) =>
        (deviceChooserSubscriberCount = Math.max(
          0,
          deviceChooserSubscriberCount + delta,
        )),
      callback,
    ),
  respondWebviewDeviceChooser: (requestId, resolution) =>
    ipcRenderer.invoke('webview:device-chooser-response', requestId, resolution),
  getWebviewMemoryUsage: (webContentsIds) =>
    ipcRenderer.invoke('webview:get-memory-usage', webContentsIds),
  getDefaultDownloadDirectory: () =>
    ipcRenderer.invoke('dialog:get-default-download-dir'),
  showContextMenu: (items) =>
    ipcRenderer.invoke('window:show-context-menu', items),
  closeContextMenu: () => ipcRenderer.invoke('window:close-context-menu'),
  executeJavaScriptInIsolatedWorld: (wcId, worldId, code) =>
    ipcRenderer.invoke(
      'webview:execute-js-isolated-world',
      wcId,
      worldId,
      code,
    ),
  scriptJailProtocol: 2,
  listWebviewFrames: (wcId) => ipcRenderer.invoke('webview:list-frames', wcId),
  executeJavaScriptInFrameIsolatedWorld: (wcId, frameId, code, expectedOrigin) =>
    ipcRenderer.invoke(
      'webview:execute-js-frame-isolated-world',
      wcId,
      frameId,
      code,
      expectedOrigin,
    ),
  runScriptInJail: (req) => ipcRenderer.invoke('script-jail:run', req),
  abortScriptJail: (jailId) => ipcRenderer.invoke('script-jail:abort', jailId),
  warmScriptJail: () => ipcRenderer.invoke('script-jail:warm'),
  subscribeWebviewSecurityState: (webContentsId, callback) => {
    // The push channel and the initial fetch can race; `seq` orders them.
    let latestSeq = 0;
    let subscribed = true;
    const deliver = (state: WebviewSecurityState | null | undefined): void => {
      if (
        !subscribed ||
        !state ||
        state.webContentsId !== webContentsId ||
        state.seq <= latestSeq
      ) {
        return;
      }
      latestSeq = state.seq;
      callback(state);
    };
    const handler = (
      _event: IpcRendererEvent,
      state: WebviewSecurityState | null | undefined,
    ): void => {
      deliver(state);
    };
    ipcRenderer.on('webview:security-state-changed', handler);
    void ipcRenderer
      .invoke('webview:get-security-state', webContentsId)
      .then(deliver, () => undefined);
    return () => {
      subscribed = false;
      ipcRenderer.removeListener('webview:security-state-changed', handler);
    };
  },
  allowWebviewCertificate: (webContentsId, url) =>
    ipcRenderer.invoke('webview:allow-certificate', webContentsId, url),
  clearWebviewSiteData: (webContentsId, origin) =>
    ipcRenderer.invoke('webview:clear-site-data', webContentsId, origin),
  onWebviewOpenNewTab: (callback) => {
    const handler = (
      _event: IpcRendererEvent,
      url: string,
      webContentsId: number,
    ): void => {
      callback(url, webContentsId);
    };
    ipcRenderer.on('webview:open-new-tab', handler);
    return () => {
      ipcRenderer.removeListener('webview:open-new-tab', handler);
    };
  },
  setWebviewDownloadDirectory: (webContentsId, downloadDir) =>
    ipcRenderer.invoke('webview:set-download-dir', webContentsId, downloadDir),
  setWebviewAgentControlled: (webContentsId, active) =>
    ipcRenderer.invoke('webview:set-agent-controlled', webContentsId, active),
  openWebviewDownload: (downloadId) =>
    ipcRenderer.invoke('webview:open-download', downloadId),
  showWebviewDownloadInFolder: (downloadId) =>
    ipcRenderer.invoke('webview:show-download-in-folder', downloadId),
  cancelWebviewDownload: (downloadId) =>
    ipcRenderer.invoke('webview:cancel-download', downloadId),
  resolveWebviewDownload: (downloadId, approve) =>
    ipcRenderer.invoke('webview:resolve-download', downloadId, approve),
  selectDownloadDirectory: (defaultPath) =>
    ipcRenderer.invoke('dialog:select-download-dir', defaultPath),
  onWebviewFaviconUpdated: (callback) =>
    subscribeIpcChannel('webview:favicon-updated', callback),
  onWebviewDownloadProgress: (callback) =>
    subscribeIpcChannel('webview:download-progress', callback),
  setWebviewDialogPolicy: (webContentsId, policy) =>
    ipcRenderer.invoke('webview:set-dialog-policy', webContentsId, policy),
  handleWebviewDialog: (webContentsId, decision) =>
    ipcRenderer.invoke('webview:handle-javascript-dialog', webContentsId, decision),
  onWebviewDialogOpening: (callback) =>
    subscribeIpcChannel('webview:javascript-dialog-opening', callback),
  getWebviewNetworkRequests: (webContentsId) =>
    ipcRenderer.invoke('webview:get-network-requests', webContentsId),
  clearWebviewNetworkRequests: (webContentsId) =>
    ipcRenderer.invoke('webview:clear-network-requests', webContentsId),
  onWebviewNetworkRequest: (callback) =>
    subscribeIpcChannel('webview:network-request-updated', callback),
  onWebviewPromptRequest: (callback) =>
    subscribeRefCountedIpcChannel(
      'webview:prompt-request',
      'webview:prompt-subscriber-state',
      (delta) =>
        (promptRequestSubscriberCount = Math.max(
          0,
          promptRequestSubscriberCount + delta,
        )),
      callback,
    ),
  respondWebviewPrompt: (promptId, value) =>
    ipcRenderer.invoke('webview:prompt-response', promptId, value),
  setWebviewFileChooserPolicy: (webContentsId, policy) =>
    ipcRenderer.invoke('webview:set-file-chooser-policy', webContentsId, policy),
  handleWebviewFileChooser: (webContentsId, decision) =>
    ipcRenderer.invoke('webview:handle-file-chooser', webContentsId, decision),
  setWebviewInputFiles: (webContentsId, request) =>
    ipcRenderer.invoke('webview:set-input-files', webContentsId, request),
  resolveWebviewUploadFiles: (request) =>
    ipcRenderer.invoke('webview:resolve-upload-files', request),
  onWebviewFileChooserOpened: (callback) =>
    subscribeIpcChannel('webview:file-chooser-opened', callback),
  setWebviewWebMcpEnabled: (enabled) =>
    ipcRenderer.invoke('webview:webmcp-set-enabled', enabled),
  listWebviewWebMcpTools: (webContentsId, origin) =>
    ipcRenderer.invoke('webview:webmcp-list-tools', webContentsId, origin),
  callWebviewWebMcpTool: (webContentsId, origin, params) =>
    ipcRenderer.invoke('webview:webmcp-call-tool', webContentsId, origin, params),
  cancelWebviewWebMcpToolCall: (webContentsId, callId) =>
    ipcRenderer.invoke('webview:webmcp-cancel-tool-call', webContentsId, callId),
  wcvBrowser: wcvBrowserAPI,
};

// SSH windows (a switch in additionalArguments): the LS and its paths are
// remote, so laptop file APIs are hidden and the toolkit uses remote ones.
const BACKEND_SWITCH = '--agy-backend=';
const isRemoteWindow =
  process.argv
    .find((a) => a.startsWith(BACKEND_SWITCH))
    ?.slice(BACKEND_SWITCH.length) === 'ssh';

// SSH windows have no script jail.
if (!isRemoteWindow) {
  ipcRenderer.on(
    'agy-script-jail:port',
    (event: IpcRendererEvent, payload?: ScriptJailPortPayload): void => {
      window.postMessage(
        {
          type: 'agy-script-jail-port',
          jailId: payload?.jailId,
        },
        '*',
        event.ports,
      );
    },
  );
}

const ideAPI: IdeAPI = {
  isInstalled: () => ipcRenderer.invoke('ide:is-installed'),
};

const wslAPI: WslAPI = {
  getState: () => ipcRenderer.invoke('wsl:get-state'),
  connect: (distro) => ipcRenderer.invoke('wsl:connect', distro),
};

const desktopMicAPI: DesktopMicAPI = {
  getAccessStatus: () => ipcRenderer.invoke('mic:get-access-status'),
  requestAccess: () => ipcRenderer.invoke('mic:request-access'),
  openSystemPreferences: () =>
    ipcRenderer.invoke('mic:open-system-preferences'),
};

function createLocalSshRemoteAPI(): LocalSshRemoteAPI {
  const progressCallbacks = new Map<number, (message: string) => void>();
  // A random start: a connect from before a reload can't match a new call.
  let lastRequestId = Math.floor(Math.random() * 2 ** 30);

  ipcRenderer.on(
    'ssh:progress',
    (_event: IpcRendererEvent, progress?: SshProgressPayload): void => {
      const callback =
        typeof progress?.requestId === 'number'
          ? progressCallbacks.get(progress.requestId)
          : undefined;
      if (callback && typeof progress?.message === 'string') {
        try {
          callback(progress.message);
        } catch {
          // The page's callback failed; the connect goes on.
        }
      }
    },
  );

  return {
    kind: 'local',
    getState: () => ipcRenderer.invoke('ssh:get-state'),
    listHosts: (options) =>
      ipcRenderer.invoke('ssh:list-hosts', {
        configFile: options?.configFile,
      }),
    chooseConfigFile: () => ipcRenderer.invoke('ssh:choose-config-file'),
    connect: async (host, options) => {
      const requestId = ++lastRequestId;
      const onProgress = options?.onProgress;
      if (typeof onProgress === 'function') {
        progressCallbacks.set(requestId, onProgress);
      }
      try {
        return await ipcRenderer.invoke('ssh:connect', host, {
          requestId,
          intent: options?.intent,
          configFile: options?.configFile,
        });
      } catch {
        // The main process refused the call: still a result, not a rejection.
        return {
          ok: false,
          code: 'UNKNOWN',
          message: "Couldn't connect to the SSH host. See the app log.",
        };
      } finally {
        progressCallbacks.delete(requestId);
      }
    },
  };
}

function createRemoteSshRemoteAPI(): RemoteSshRemoteAPI {
  const intentCallbacks = new Set<(intent: SshIntent) => void>();
  const takeIntent = async (): Promise<void> => {
    let intent: unknown;
    try {
      intent = await ipcRenderer.invoke('ssh:take-intent');
    } catch {
      // The page is going away, or its window is no longer an SSH window.
      return;
    }
    if (intent !== 'newProject') {
      return;
    }
    for (const callback of [...intentCallbacks]) {
      try {
        callback(intent);
      } catch {
        // The page's callback failed.
      }
    }
  };

  ipcRenderer.on('ssh:intent', () => {
    if (intentCallbacks.size > 0) {
      void takeIntent();
    }
  });

  return {
    kind: 'ssh',
    getState: () => ipcRenderer.invoke('ssh:get-state'),
    stopServer: () => ipcRenderer.invoke('ssh:stop-server'),
    onIntent: (callback) => {
      if (typeof callback !== 'function') {
        return () => {};
      }
      intentCallbacks.add(callback);
      void takeIntent();
      return () => {
        intentCallbacks.delete(callback);
      };
    },
    openLocalWindow: async () => {
      try {
        await ipcRenderer.invoke('ssh:open-local-window');
      } catch {
        // Refused, e.g. while this window isn't focused.
      }
    },
  };
}

contextBridge.exposeInMainWorld('nativeNotifications', notificationAPI);
contextBridge.exposeInMainWorld('nativeStorage', storageAPI);
contextBridge.exposeInMainWorld('agent', agentAPI);
// Sidecar UIs: the toolkit maps them to plugin:// only where this exists. A
// local window's send sets the laptop's map; an SSH window's only refreshes
// its host's, which main builds from the host's server (remoteSidecars.ts).
contextBridge.exposeInMainWorld('extensions', extensionsAPI);

if (isRemoteWindow) {
  // No laptop-only APIs (updater, logs, deep links with auth codes; main
  // refuses them too). Of electronNative, no built-in browser,
  // script jail or getPathForFile: dropped files get uploaded
  // instead of passing laptop paths to the remote agent. Remote paths can't
  // be shown in Finder either.
  // The toolkit uses window.sshRemote only where window.electronNative exists.
  const remoteNative: RemoteNativeAPI = {
    getZoomLevel: electronNativeAPI.getZoomLevel,
    setTitleBarOverlay: electronNativeAPI.setTitleBarOverlay,
    minimize: electronNativeAPI.minimize,
    maximize: electronNativeAPI.maximize,
    unmaximize: electronNativeAPI.unmaximize,
    isMaximized: electronNativeAPI.isMaximized,
    close: electronNativeAPI.close,
    toggleDevTools: electronNativeAPI.toggleDevTools,
    zoomIn: electronNativeAPI.zoomIn,
    zoomOut: electronNativeAPI.zoomOut,
    resetZoom: electronNativeAPI.resetZoom,
    openExternal: electronNativeAPI.openExternal,
    showContextMenu: electronNativeAPI.showContextMenu,
    closeContextMenu: electronNativeAPI.closeContextMenu,
    revealInFilePicker: () => Promise.resolve(),
  };
  contextBridge.exposeInMainWorld('electronNative', remoteNative);
  contextBridge.exposeInMainWorld('sshRemote', createRemoteSshRemoteAPI());
} else {
  contextBridge.exposeInMainWorld('electronUpdater', updaterAPI);
  contextBridge.exposeInMainWorld('dialog', dialogAPI);
  contextBridge.exposeInMainWorld('logs', logsAPI);
  contextBridge.exposeInMainWorld('deepLink', deepLinkAPI);
  contextBridge.exposeInMainWorld('electronNative', electronNativeAPI);
  contextBridge.exposeInMainWorld('ide', ideAPI);
  contextBridge.exposeInMainWorld('wsl', wslAPI);
  contextBridge.exposeInMainWorld('desktopMic', desktopMicAPI);
  contextBridge.exposeInMainWorld('sshRemote', createLocalSshRemoteAPI());
}

const HEX6_RE = /^#[0-9a-fA-F]{6}$/;
const HEX3_RE = /^#([0-9a-fA-F])([0-9a-fA-F])([0-9a-fA-F])$/;
const RGB_RE =
  /^rgba?\(\s*(\d{1,3})\s*(?:,|\s)\s*(\d{1,3})\s*(?:,|\s)\s*(\d{1,3})(?:\s*[,/]\s*([\d.]+%?))?\s*\)$/i;
const CSS_VAR_RE = /^var\(\s*(--[^,\s)]+)(?:\s*,\s*(.+))?\)$/;

function parseCssColorToHex6(raw: string): string | undefined {
  const s = raw.trim();
  if (!s) {
    return undefined;
  }
  if (HEX6_RE.test(s)) {
    return s.toUpperCase();
  }
  const h3 = HEX3_RE.exec(s);
  if (h3) {
    return `#${h3[1]}${h3[1]}${h3[2]}${h3[2]}${h3[3]}${h3[3]}`.toUpperCase();
  }
  const rgb = RGB_RE.exec(s);
  if (!rgb) {
    return undefined;
  }
  if (rgb[4] !== undefined) {
    const a = rgb[4].endsWith('%')
      ? Number(rgb[4].slice(0, -1)) / 100
      : Number(rgb[4]);
    if (!Number.isFinite(a) || a < 1) {
      return undefined;
    }
  }
  const h2 = (v: string): string =>
    Math.max(0, Math.min(255, Math.round(Number(v))))
      .toString(16)
      .padStart(2, '0');
  return `#${h2(rgb[1])}${h2(rgb[2])}${h2(rgb[3])}`.toUpperCase();
}

// TODO(yushis): Push `wcvBrowser.setThemeColor` directly from toolkit `themeProvider`
// and remove this interim `:root` `--background` MutationObserver bridge.
function installHubSurfaceThemeAutoSync(): void {
  const g = globalThis as unknown as PreloadGlobal;
  if (
    !g.document ||
    typeof g.getComputedStyle !== 'function' ||
    (g.top && g.self && g.top !== g.self)
  ) {
    return;
  }

  let lastSyncedHex = '';
  let observedRoot: Element | null = null;
  const syncNow = (): void => {
    const rootEl = g.document?.documentElement;
    if (!rootEl) {
      return;
    }
    if (observedRoot !== rootEl && typeof g.MutationObserver === 'function') {
      observedRoot = rootEl;
      new g.MutationObserver(syncNow).observe(rootEl, {
        attributes: true,
        attributeFilter: ['style', 'class', 'data-theme'],
      });
    }
    const style = g.getComputedStyle?.(rootEl);
    let cur = style?.getPropertyValue?.('--background')?.trim() ?? '';
    for (let depth = 0; depth < 3 && cur; depth++) {
      const hex = parseCssColorToHex6(cur);
      if (hex) {
        cur = hex;
        break;
      }
      const m = CSS_VAR_RE.exec(cur);
      if (!m) {
        break;
      }
      cur = style?.getPropertyValue?.(m[1])?.trim() || m[2]?.trim() || '';
    }
    const hex = parseCssColorToHex6(cur);
    if (hex && hex !== lastSyncedHex) {
      lastSyncedHex = hex;
      void Promise.resolve(ipcRenderer.invoke('wcv:set-theme-color', hex)).catch(
        () => undefined,
      );
    }
  };

  syncNow();
  g.document.addEventListener?.('DOMContentLoaded', syncNow);
  g.addEventListener?.('load', syncNow);
}

const FLAG_KEY = 'jetski.developer.customFlagOverrides';
const MIG_KEY = `${FLAG_KEY}.migrated`;
const WRITE_EV = 'agy-flag-overrides-write';

const parseFlags = (v: unknown): string | null => {
  try {
    const p: unknown = typeof v === 'string' ? JSON.parse(v) : null;
    return p && typeof p === 'object' && !Array.isArray(p) ? String(v) : null;
  } catch {
    return null;
  }
};

/** Hydrates and mirrors `jetski.developer.customFlagOverrides` via `app_storage.json` across ephemeral LS ports. */
function installCustomFlagOverridesSync(): void {
  const g = globalThis as unknown as PreloadGlobal;
  const h = g.location?.hostname;
  const okHost =
    !g.location ||
    (/^https?:$/.test(g.location.protocol ?? '') &&
      (h === '127.0.0.1' || h === 'localhost'));
  if (!okHost || (g.top && g.self && g.top !== g.self)) {
    return;
  }

  // Own writes not yet acknowledged; their broadcasts are echoed back before
  // the reply, so any broadcast seen meanwhile is superseded by our write.
  let pending = 0;
  const persist = (v: string | null): void => {
    pending++;
    void Promise.resolve(
      ipcRenderer.invoke('storage:update-items', {
        [FLAG_KEY]: v,
        [MIG_KEY]: 'true',
      }),
    )
      .catch(() => {})
      .finally(() => pending--);
  };

  const runMain = (js: string): void =>
    void Promise.resolve(webFrame.executeJavaScript?.(js)).catch(() => {});

  let s: PreloadStorageLike;
  let last: string | null = null;
  let suppress = false;
  try {
    if (!g.localStorage?.getItem || !g.localStorage.setItem) {
      return;
    }
    s = g.localStorage as PreloadStorageLike;
    const m: unknown = ipcRenderer.sendSync?.('storage:get-items-sync');
    if (!m || typeof m !== 'object') {
      // Untrusted sender or IPC failure: never treat as a fresh install.
      return;
    }
    const kv = m as Record<string, unknown>;
    const migrated = MIG_KEY in kv || FLAG_KEY in kv;
    const disk = parseFlags(kv[FLAG_KEY]);
    const localRaw = s.getItem(FLAG_KEY);
    const local = parseFlags(localRaw);
    last = migrated ? disk : local;
    if (disk !== null && localRaw !== disk) {
      s.setItem(FLAG_KEY, disk);
    } else if (!migrated && local !== null) {
      persist(local);
    } else if (migrated && disk === null && localRaw !== null) {
      s.removeItem?.(FLAG_KEY);
    }
  } catch {
    return;
  }

  const syncOut = (): void => {
    try {
      const raw = suppress ? undefined : s.getItem(FLAG_KEY);
      const next = raw ? parseFlags(raw) : null;
      const valid = raw === null || next !== null;
      if (raw !== undefined && valid && next !== last) {
        last = next;
        persist(next);
      }
    } catch {
      // Ignore storage access errors during unload.
    }
  };

  // Not `storage`: that fires when another window on the same origin wrote
  // the shared localStorage, and that window persists its own change.
  [WRITE_EV, 'pagehide', 'beforeunload'].forEach((ev) =>
    g.addEventListener?.(ev, syncOut),
  );
  g.addEventListener?.('visibilitychange', syncOut);

  const kJson = JSON.stringify(FLAG_KEY);
  runMain(
    `(()=>{const P=window.Storage?.prototype,L=window.localStorage,K=${kJson},E=${JSON.stringify(WRITE_EV)};if(!P||!L||P.__agyFlagHook)return;Object.defineProperty(P,'__agyFlagHook',{value:1});const n=()=>{window.dispatchEvent(new Event(E))},si=P.setItem,ri=P.removeItem,cl=P.clear;P.setItem=function(k,v){si.call(this,k,v);if(this===L&&String(k)===K)n()};P.removeItem=function(k){ri.call(this,k);if(this===L&&String(k)===K)n()};P.clear=function(){cl.call(this);if(this===L)n()}})()`,
  );

  ipcRenderer.on(
    'storage:changed',
    (
      _e: IpcRendererEvent,
      changes: Record<string, unknown> | null | undefined,
    ): void => {
      const raw =
        changes && FLAG_KEY in changes ? changes[FLAG_KEY] : undefined;
      const next = raw === null ? null : parseFlags(raw);
      if (raw === undefined || (raw !== null && next === null)) {
        return;
      }
      try {
        const cur = s.getItem(FLAG_KEY);
        if (cur === next) {
          last = next;
          return;
        }
        // A local value not yet persisted (or a newer write from another window
        // sharing this origin) wins over a possibly stale broadcast.
        if (pending > 0 || cur !== last) {
          return;
        }
        last = next;
        suppress = true;
        if (next === null) {
          s.removeItem?.(FLAG_KEY);
        } else {
          s.setItem(FLAG_KEY, next);
        }
        runMain(
          `window.dispatchEvent(new StorageEvent('storage',{key:${kJson},newValue:${JSON.stringify(next)},storageArea:window.localStorage}))`,
        );
      } catch {
        // Ignore teardown write failures.
      } finally {
        suppress = false;
      }
    },
  );
}

// Not SSH windows: no built-in browser tabs; flag overrides are the laptop's.
if (!isRemoteWindow) {
  installHubSurfaceThemeAutoSync();
  installCustomFlagOverridesSync();
}
