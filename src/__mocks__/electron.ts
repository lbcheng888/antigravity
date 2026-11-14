/**
 * Shared Electron mock for all test files.
 *
 * This file is automatically used by Vitest when a test calls
 * `vi.mock('electron')` without a factory argument, because it lives
 * in the `__mocks__` directory.
 *
 * Individual tests can still override specific properties on these
 * mock objects in their `beforeEach` or `vi.hoisted` blocks.
 */
import { vi, type MockFn } from 'vitest';

export interface MockBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface MockSize {
  width: number;
  height: number;
}

export interface MockCatchChain {
  catch: MockFn;
}

export interface MockReadyPromise {
  cb?: unknown;
  then: MockFn<[cb: unknown], MockCatchChain>;
}

export interface MockContainerView {
  children: unknown[];
  addChildView: MockFn<[view: unknown, index?: number], void>;
  removeChildView: MockFn<[view: unknown], void>;
  setBounds: MockFn<[next: MockBounds], void>;
  getBounds: MockFn<[], MockBounds>;
  setVisible: MockFn<[next: boolean], void>;
  getVisible: MockFn<[], boolean>;
  setBackgroundColor: MockFn;
}

export interface MockWindowWebContents {
  id: number;
  isDestroyed: MockFn<[], boolean>;
  getType: MockFn<[], string>;
  getURL: MockFn<[], string>;
  send: MockFn;
  postMessage: MockFn;
  on: MockFn;
  off: MockFn;
  once: MockFn;
  removeListener: MockFn;
  setWindowOpenHandler: MockFn;
  getZoomLevel: MockFn<[], number>;
  setZoomLevel: MockFn;
  getZoomFactor: MockFn<[], number>;
  setVisualZoomLevelLimits: MockFn<unknown[], Promise<void>>;
  isLoadingMainFrame: MockFn<[], boolean>;
}

export interface MockBrowserWindowInstance {
  loadURL: MockFn;
  once: MockFn;
  close: MockFn;
  show: MockFn;
  focus: MockFn;
  setTitle: MockFn;
  isMinimized: MockFn<[], boolean>;
  isMaximized: MockFn<[], boolean>;
  isFullScreen: MockFn<[], boolean>;
  maximize: MockFn;
  unmaximize: MockFn;
  setFullScreen: MockFn;
  restore: MockFn;
  on: MockFn;
  off: MockFn;
  isDestroyed: MockFn<[], boolean>;
  getBounds: MockFn<[], MockBounds>;
  getNormalBounds: MockFn<[], MockBounds>;
  getContentSize: MockFn<[], [number, number]>;
  setBackgroundColor: MockFn;
  contentView: MockContainerView;
  webContents: MockWindowWebContents;
}

export interface MockWebContentsViewWebContents {
  id: number;
  loadURL: MockFn<unknown[], Promise<void>>;
  reload: MockFn;
  insertCSS: MockFn<unknown[], Promise<string>>;
  removeInsertedCSS: MockFn<unknown[], Promise<void>>;
  executeJavaScript: MockFn<unknown[], Promise<{ ok: boolean }>>;
  postMessage: MockFn;
  send: MockFn;
  on: MockFn;
  off: MockFn;
  once: MockFn;
  removeListener: MockFn;
  getType: MockFn<[], string>;
  getURL: MockFn<[], string>;
  setWebRTCIPHandlingPolicy: MockFn;
  setWindowOpenHandler: MockFn;
  setIgnoreMenuShortcuts: MockFn;
  setZoomMode: MockFn;
  getZoomLevel: MockFn<[], number>;
  setZoomLevel: MockFn;
  getZoomFactor: MockFn<[], number>;
  setZoomFactor: MockFn<[next: number], void>;
  setVisualZoomLevelLimits: MockFn<unknown[], Promise<void>>;
  toggleDevTools: MockFn;
  getOSProcessId: MockFn<[], number>;
  isDestroyed: MockFn<[], boolean>;
  isLoadingMainFrame: MockFn<[], boolean>;
  close: MockFn;
  destroy: MockFn;
}

export interface MockWebContentsView {
  webContents: MockWebContentsViewWebContents;
  setBounds: MockFn<[next: MockBounds], void>;
  getBounds: MockFn<[], MockBounds>;
  setVisible: MockFn<[next: boolean], void>;
  getVisible: MockFn<[], boolean>;
  setBackgroundColor: MockFn;
}

export interface MockMessagePortMain {
  postMessage: MockFn;
  close: MockFn;
  on: MockFn;
  once: MockFn;
  start: MockFn;
}

export interface MockMessageChannelMain {
  port1: MockMessagePortMain;
  port2: MockMessagePortMain;
}

export interface MockOpenDialogResult {
  canceled: boolean;
  filePaths: string[];
}

export interface MockNativeImage {
  setTemplateImage: MockFn;
}

export interface MockEmptyNativeImage {
  isEmpty: MockFn<[], boolean>;
}

export interface MockTray {
  setToolTip: MockFn;
  setContextMenu: MockFn;
  on: MockFn;
  destroy: MockFn;
}

export interface MockMenuSubmenu {
  insert: MockFn;
}

export interface MockMenuItemEntry {
  label: string;
  submenu?: MockMenuSubmenu;
}

export interface MockMenuBuiltInstance {
  items: MockMenuItemEntry[];
  getMenuItemById: MockFn<[id: string], MockMenuItemEntry | null>;
  popup: MockFn;
}

interface MockMenuSelf {
  items: unknown[];
  append: (item: unknown) => void;
}

export interface MockNotificationInstance {
  show: MockFn;
  on: MockFn;
}

export interface MockPreloadScriptRegistration {
  id?: string;
  filePath: string;
  type: string;
}

export interface MockPreloadScript {
  id: string;
  filePath: string;
  type: string;
}

export interface MockPartitionSession {
  on: MockFn;
  setProxy: MockFn<unknown[], Promise<void>>;
  getPreloadScripts: MockFn<[], MockPreloadScript[]>;
  registerPreloadScript: MockFn<[script: MockPreloadScriptRegistration], string>;
  unregisterPreloadScript: MockFn<[id: string], void>;
  getPreloads: MockFn<[], string[]>;
  setPreloads: MockFn<[preloads: string[]], void>;
  setPermissionRequestHandler: MockFn;
  setPermissionCheckHandler: MockFn;
  webRequest: {
    onBeforeRequest: MockFn;
  };
  protocol: {
    handle: MockFn;
  };
}

export interface MockDisplay {
  id: number;
  bounds: MockBounds;
  workArea: MockBounds;
  workAreaSize: MockSize;
  scaleFactor: number;
}

export const app = {
  whenReady: vi.fn<[], MockReadyPromise>().mockReturnValue({
    then: vi.fn<[cb: unknown], MockCatchChain>().mockImplementation(function (
      this: unknown,
      cb: unknown,
    ): MockCatchChain {
      (this as MockReadyPromise).cb = cb;
      return { catch: vi.fn() };
    }),
  }),
  on: vi.fn(),
  quit: vi.fn(),
  relaunch: vi.fn(),
  isPackaged: true,
  getAppPath: vi.fn<[], string>().mockReturnValue('/mock/path'),
  getPath: vi.fn<[name: string], string>().mockReturnValue('/mock/user/data'),
  getVersion: vi.fn<[], string>().mockReturnValue('1.0.0'),
  getName: vi.fn<[], string>().mockReturnValue('App'),
  commandLine: {
    appendSwitch: vi.fn(),
    getSwitchValue: vi.fn<[switchName: string], string>().mockReturnValue(''),
    hasSwitch: vi.fn<[switchName: string], boolean>().mockReturnValue(false),
  },
  dock: {
    show: vi.fn(),
    hide: vi.fn(),
    setIcon: vi.fn(),
    setMenu: vi.fn(),
  },
  requestSingleInstanceLock: vi.fn<[], boolean>().mockReturnValue(true),
  isDefaultProtocolClient: vi.fn<unknown[], boolean>().mockReturnValue(false),
  setAsDefaultProtocolClient: vi.fn<unknown[], boolean>().mockReturnValue(true),
  isReady: vi.fn<[], boolean>().mockReturnValue(true),
  focus: vi.fn(),
  getAppMetrics: vi.fn<[], unknown[]>().mockReturnValue([]),
};

function createMockContainerView(): MockContainerView {
  const children: unknown[] = [];
  let visible = true;
  let bounds: MockBounds = { x: 0, y: 0, width: 0, height: 0 };
  return {
    children,
    addChildView: vi.fn((view: unknown, index?: number): void => {
      const existing = children.indexOf(view);
      if (existing >= 0) {
        children.splice(existing, 1);
      }
      if (typeof index === 'number' && index >= 0 && index <= children.length) {
        children.splice(index, 0, view);
      } else {
        children.push(view);
      }
    }),
    removeChildView: vi.fn((view: unknown): void => {
      const idx = children.indexOf(view);
      if (idx >= 0) {
        children.splice(idx, 1);
      }
    }),
    setBounds: vi.fn((next: MockBounds): void => {
      bounds = { ...next };
    }),
    getBounds: vi.fn((): MockBounds => ({ ...bounds })),
    setVisible: vi.fn((next: boolean): void => {
      visible = Boolean(next);
    }),
    getVisible: vi.fn((): boolean => visible),
    setBackgroundColor: vi.fn(),
  };
}

const _mockBrowserWindowInstance: MockBrowserWindowInstance = {
  loadURL: vi.fn(),
  once: vi.fn(),
  close: vi.fn(),
  show: vi.fn(),
  focus: vi.fn(),
  setTitle: vi.fn(),
  isMinimized: vi.fn<[], boolean>().mockReturnValue(false),
  isMaximized: vi.fn<[], boolean>().mockReturnValue(false),
  isFullScreen: vi.fn<[], boolean>().mockReturnValue(false),
  maximize: vi.fn(),
  unmaximize: vi.fn(),
  setFullScreen: vi.fn(),
  restore: vi.fn(),
  on: vi.fn(),
  off: vi.fn(),
  isDestroyed: vi.fn<[], boolean>().mockReturnValue(false),
  getBounds: vi
    .fn<[], MockBounds>()
    .mockReturnValue({ x: 100, y: 100, width: 1400, height: 900 }),
  getNormalBounds: vi
    .fn<[], MockBounds>()
    .mockReturnValue({ x: 100, y: 100, width: 1400, height: 900 }),
  getContentSize: vi.fn<[], [number, number]>().mockReturnValue([1000, 800]),
  setBackgroundColor: vi.fn(),
  contentView: createMockContainerView(),
  webContents: {
    id: 1,
    isDestroyed: vi.fn<[], boolean>().mockReturnValue(false),
    getType: vi.fn<[], string>().mockReturnValue('window'),
    getURL: vi.fn<[], string>().mockReturnValue('https://127.0.0.1:1234/'),
    send: vi.fn(),
    postMessage: vi.fn(),
    on: vi.fn(),
    off: vi.fn(),
    once: vi.fn(),
    removeListener: vi.fn(),
    setWindowOpenHandler: vi.fn(),
    getZoomLevel: vi.fn<[], number>().mockReturnValue(0),
    setZoomLevel: vi.fn(),
    getZoomFactor: vi.fn<[], number>().mockReturnValue(1),
    setVisualZoomLevelLimits: vi.fn<unknown[], Promise<void>>().mockResolvedValue(undefined),
    isLoadingMainFrame: vi.fn<[], boolean>().mockReturnValue(false),
  },
};

export const BaseWindow = Object.assign(
  vi.fn<unknown[], MockBrowserWindowInstance>().mockImplementation(
    function (): MockBrowserWindowInstance {
      return {
        ..._mockBrowserWindowInstance,
        contentView: createMockContainerView(),
      };
    },
  ),
  {
    getAllWindows: vi.fn<[], MockBrowserWindowInstance[]>().mockReturnValue([]),
    getFocusedWindow: vi.fn<[], MockBrowserWindowInstance | null>().mockReturnValue(null),
  },
);

export const BrowserWindow = Object.assign(
  vi.fn<unknown[], MockBrowserWindowInstance>().mockImplementation(
    function (): MockBrowserWindowInstance {
      return _mockBrowserWindowInstance;
    },
  ),
  {
    getAllWindows: vi
      .fn<[], MockBrowserWindowInstance[]>()
      .mockReturnValue([_mockBrowserWindowInstance]),
    getFocusedWindow: vi
      .fn<[], MockBrowserWindowInstance | null>()
      .mockReturnValue(_mockBrowserWindowInstance),
    fromWebContents: vi
      .fn<[webContents: unknown], MockBrowserWindowInstance | null>()
      .mockReturnValue(null),
  },
);

export const View = vi
  .fn<unknown[], MockContainerView>()
  .mockImplementation(function (): MockContainerView {
    return createMockContainerView();
  });

let _nextMockWebContentsViewId = 10000;

export const WebContentsView = vi
  .fn<unknown[], MockWebContentsView>()
  .mockImplementation(function (): MockWebContentsView {
    const id = _nextMockWebContentsViewId++;
    let visible = true;
    let zoomFactor = 1;
    let bounds: MockBounds = { x: 0, y: 0, width: 0, height: 0 };
    return {
      webContents: {
        id,
        loadURL: vi.fn<unknown[], Promise<void>>().mockResolvedValue(undefined),
        reload: vi.fn(),
        insertCSS: vi.fn<unknown[], Promise<string>>().mockResolvedValue('mock-css-key'),
        removeInsertedCSS: vi.fn<unknown[], Promise<void>>().mockResolvedValue(undefined),
        executeJavaScript: vi
          .fn<unknown[], Promise<{ ok: boolean }>>()
          .mockResolvedValue({ ok: true }),
        postMessage: vi.fn(),
        send: vi.fn(),
        on: vi.fn(),
        off: vi.fn(),
        once: vi.fn(),
        removeListener: vi.fn(),
        getType: vi.fn<[], string>().mockReturnValue('window'),
        getURL: vi.fn<[], string>().mockReturnValue('https://127.0.0.1:1234/'),
        setWebRTCIPHandlingPolicy: vi.fn(),
        setWindowOpenHandler: vi.fn(),
        setIgnoreMenuShortcuts: vi.fn(),
        setZoomMode: vi.fn(),
        getZoomLevel: vi.fn<[], number>().mockReturnValue(0),
        setZoomLevel: vi.fn(),
        getZoomFactor: vi.fn((): number => zoomFactor),
        setZoomFactor: vi.fn((next: number): void => {
          zoomFactor = next;
        }),
        setVisualZoomLevelLimits: vi.fn<unknown[], Promise<void>>().mockResolvedValue(undefined),
        toggleDevTools: vi.fn(),
        getOSProcessId: vi.fn<[], number>().mockReturnValue(12345),
        isDestroyed: vi.fn<[], boolean>().mockReturnValue(false),
        isLoadingMainFrame: vi.fn<[], boolean>().mockReturnValue(false),
        close: vi.fn(),
        destroy: vi.fn(),
      },
      setBounds: vi.fn((next: MockBounds): void => {
        bounds = { ...next };
      }),
      getBounds: vi.fn((): MockBounds => ({ ...bounds })),
      setVisible: vi.fn((next: boolean): void => {
        visible = Boolean(next);
      }),
      getVisible: vi.fn((): boolean => visible),
      setBackgroundColor: vi.fn(),
    };
  });

export const MessageChannelMain = vi
  .fn<unknown[], MockMessageChannelMain>()
  .mockImplementation(function (): MockMessageChannelMain {
    return {
      port1: {
        postMessage: vi.fn(),
        close: vi.fn(),
        on: vi.fn(),
        once: vi.fn(),
        start: vi.fn(),
      },
      port2: {
        postMessage: vi.fn(),
        close: vi.fn(),
        on: vi.fn(),
        once: vi.fn(),
        start: vi.fn(),
      },
    };
  });

export const dialog = {
  showErrorBox: vi.fn(),
  showMessageBox: vi.fn(),
  showOpenDialog: vi
    .fn<unknown[], Promise<MockOpenDialogResult>>()
    .mockResolvedValue({ canceled: true, filePaths: [] }),
};

export const ipcMain = {
  handle: vi.fn(),
  removeHandler: vi.fn(),
  on: vi.fn(),
  removeListener: vi.fn(),
};

export const protocol = {
  registerSchemesAsPrivileged: vi.fn(),
  handle: vi.fn(),
};

const _mockNativeImage: MockNativeImage = {
  setTemplateImage: vi.fn(),
};

export const nativeImage = {
  createFromPath: vi.fn<[path: string], MockNativeImage>().mockReturnValue(_mockNativeImage),
  createFromBuffer: vi.fn<[buffer: unknown], MockEmptyNativeImage>().mockReturnValue({
    isEmpty: vi.fn<[], boolean>().mockReturnValue(true),
  }),
};

export const Tray = vi
  .fn<unknown[], MockTray>()
  .mockImplementation(function (): MockTray {
    return {
      setToolTip: vi.fn(),
      setContextMenu: vi.fn(),
      on: vi.fn(),
      destroy: vi.fn(),
    };
  });

export const MenuItem = vi
  .fn<[options?: unknown], void>()
  .mockImplementation(function (this: unknown, options?: unknown): void {
    Object.assign(this as object, options);
  });

const _mockMenuInstance: MockMenuBuiltInstance = {
  items: [
    {
      label: 'File',
      submenu: {
        insert: vi.fn(),
      },
    },
  ],
  getMenuItemById: vi.fn<[id: string], MockMenuItemEntry | null>().mockReturnValue({
    label: '',
  }),
  popup: vi.fn(),
};

export const Menu = Object.assign(
  vi.fn<unknown[], void>().mockImplementation(function (this: unknown): void {
    const self = this as MockMenuSelf;
    self.items = [];
    self.append = (item: unknown): void => {
      self.items.push(item);
    };
  }),
  {
    buildFromTemplate: vi
      .fn<unknown[], MockMenuBuiltInstance>()
      .mockReturnValue(_mockMenuInstance),
    getApplicationMenu: vi
      .fn<[], MockMenuBuiltInstance | null>()
      .mockReturnValue(_mockMenuInstance),
    setApplicationMenu: vi.fn(),
  },
);

const _mockNotificationInstance: MockNotificationInstance = {
  show: vi.fn(),
  on: vi.fn(),
};

export const Notification = Object.assign(
  vi.fn<unknown[], MockNotificationInstance>().mockImplementation(
    function (): MockNotificationInstance {
      return _mockNotificationInstance;
    },
  ),
  {
    // Static method
    isSupported: vi.fn<[], boolean>().mockReturnValue(true),
    // Expose the mock instance for assertions
    _mockInstance: _mockNotificationInstance,
  },
);

export const shell = {
  openExternal: vi.fn<unknown[], Promise<void>>().mockResolvedValue(undefined),
  openPath: vi.fn<unknown[], Promise<string>>().mockResolvedValue(''),
  showItemInFolder: vi.fn(),
};

export const systemPreferences = {
  getMediaAccessStatus: vi.fn<[mediaType: string], string>().mockReturnValue('granted'),
  askForMediaAccess: vi.fn<[mediaType: string], Promise<boolean>>().mockResolvedValue(true),
};

export const contextBridge = {
  exposeInMainWorld: vi.fn(),
};

export const ipcRenderer = {
  invoke: vi.fn(),
  send: vi.fn(),
  on: vi.fn(),
  removeListener: vi.fn(),
  sendSync: vi.fn(),
};

export const session = {
  defaultSession: {
    on: vi.fn(),
    setProxy: vi.fn<unknown[], Promise<void>>().mockResolvedValue(undefined),
    closeAllConnections: vi.fn<unknown[], Promise<void>>().mockResolvedValue(undefined),
    setCertificateVerifyProc: vi.fn(),
  },
  fromPartition: vi
    .fn<[partition: string], MockPartitionSession>()
    .mockImplementation((): MockPartitionSession => {
      let preloadScripts: MockPreloadScript[] = [];
      let legacyPreloads: string[] = [];
      return {
        on: vi.fn(),
        setProxy: vi.fn<unknown[], Promise<void>>().mockResolvedValue(undefined),
        getPreloadScripts: vi.fn((): MockPreloadScript[] => [...preloadScripts]),
        registerPreloadScript: vi.fn(
          (script: MockPreloadScriptRegistration): string => {
            const id = script.id ?? `preload-${preloadScripts.length + 1}`;
            preloadScripts.push({
              id,
              filePath: script.filePath,
              type: script.type,
            });
            return id;
          },
        ),
        unregisterPreloadScript: vi.fn((id: string): void => {
          preloadScripts = preloadScripts.filter((s) => s.id !== id);
        }),
        getPreloads: vi.fn((): string[] => [...legacyPreloads]),
        setPreloads: vi.fn((preloads: string[]): void => {
          legacyPreloads = [...preloads];
        }),
        setPermissionRequestHandler: vi.fn(),
        setPermissionCheckHandler: vi.fn(),
        webRequest: {
          onBeforeRequest: vi.fn(),
        },
        protocol: {
          handle: vi.fn(),
        },
      };
    }),
};

export const webContents = {
  fromId: vi.fn(),
};

export const webFrame = {
  executeJavaScript: vi.fn<unknown[], Promise<void>>().mockResolvedValue(undefined),
  getZoomFactor: vi.fn<[], number>().mockReturnValue(1),
};

const _mockDisplay: MockDisplay = {
  id: 1,
  bounds: { x: 0, y: 0, width: 1920, height: 1080 },
  workArea: { x: 0, y: 0, width: 1920, height: 1050 },
  workAreaSize: { width: 1920, height: 1050 },
  scaleFactor: 1,
};

export const screen = {
  getAllDisplays: vi.fn<[], MockDisplay[]>().mockReturnValue([_mockDisplay]),
  getPrimaryDisplay: vi.fn<[], MockDisplay>().mockReturnValue(_mockDisplay),
  getDisplayMatching: vi.fn<[rect: MockBounds], MockDisplay>().mockReturnValue(_mockDisplay),
};

export const webUtils = {
  getPathForFile: vi.fn<[file: unknown], string>().mockReturnValue('/mock/file/path'),
};
