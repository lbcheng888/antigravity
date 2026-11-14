import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  Menu,
  Notification,
  shell,
  systemPreferences,
  webContents,
  type IpcMainEvent,
  type IpcMainInvokeEvent,
  type MenuItemConstructorOptions,
  type OpenDialogOptions,
  type WebContents,
} from 'electron';
import { autoUpdater } from 'electron-updater';
import log from 'electron-log/main';
import * as fs from 'fs/promises';
import * as fsSync from 'fs';
import * as path from 'path';
import { fileURLToPath } from 'url';
import { UpdateState } from './types';
import {
  broadcastState,
  checkForUpdates,
  getLastState,
  type UpdaterState,
} from './updater';
import { getAppDataDir } from './paths';
import { isDebugConsoleEnabled } from './debugConsole';
import { extensionAuthorities } from './customScheme';
import { updateTrayAgentCount } from './tray';
import {
  cancelTrackedWebviewDownload,
  isOwnedBrowserGuestWebContents,
  openTrackedWebviewDownload,
  resolveTrackedWebviewDownload,
  setGuestAgentControlled,
  setGuestDownloadDirectory,
  showTrackedWebviewDownloadInFolder,
} from './webviewBrowser';
import { getIdeInstallPath } from './ideInstall/constants';
import {
  deviceChooserController,
  WEBVIEW_DEVICE_CHOOSER_RESPONSE_CHANNEL,
  WEBVIEW_DEVICE_CHOOSER_SUBSCRIBER_STATE_CHANNEL,
} from './deviceChooser';
import {
  distroToWindowsPath,
  getActiveWslDistro,
  getDistroHome,
  isWslAvailable,
  listWslDistros,
  windowsToDistroPath,
} from './wsl';
import { relaunchWithWslDistro } from './menu';
import {
  abortScriptInJail,
  runScriptInJail,
  warmScriptJail,
} from './scriptJail';
import { openExternalSafely } from './externalUrl';
import {
  getFocusedHubWindow,
  getHubWindows,
  getLocalHubWindows,
  isLocalHubTopFrame,
  resolveHubWindowFromWebContents,
} from './utils';
import {
  applyHubWindowZoom,
  setHubZoomStorageManager,
  type HubZoomAction,
} from './hubZoom';
import {
  wcvBrowserRegistry,
  type WcvGuestPidInfo,
} from './wcvBrowserRegistry';
import {
  isAuthorizedHubMainFrameEvent,
  registerWcvIpcHandlers,
} from './wcvIpcHandlers';
import {
  guestDialogController,
  type DialogPolicy,
} from './guestDialogController';
import {
  guestFileChooserController,
  resolveGuestUploadFiles,
  type AttachStagedFilesRequest,
  type GuestFileChooserDecision,
  type ResolveGuestUploadFilesRequest,
} from './guestFileChooserController';
import {
  guestPromptController,
  WEBVIEW_PROMPT_RESPONSE_CHANNEL,
  WEBVIEW_PROMPT_SUBSCRIBER_STATE_CHANNEL,
} from './guestPromptController';
import { HUMAN_PROMPT_SYNC_CHANNEL } from './webviewPromptPreload';
import { executeJavaScriptInIsolatedWorldWhenReady } from './isolatedWorldExec';
import { guestFrameBridge } from './guestFrameBridge';
import { guestNetworkController } from './guestNetworkController';
import { guestWebMcpController } from './guestWebMcpController';
import { remoteBackendOf } from './remote/windowBackends';
import {
  isRemoteContents,
  openRemoteUrl,
} from './remote/remoteWindowSecurity';
import type { StorageChanges, StorageItems, StorageManager } from './storage';
import type { RemoteSidecarRoutes } from './remote/remoteSidecars';

export type MicAccessStatus =
  | 'not-determined'
  | 'granted'
  | 'denied'
  | 'restricted'
  | 'unknown';

export interface IpcStorageLike {
  getItems(): Promise<StorageItems>;
  updateItems(changes: StorageChanges): Promise<void>;
}

export interface IpcHandlerDeps {
  resolveStorage?: (sender: WebContents) => IpcStorageLike | undefined;
  sidecarRoutesFor?: (sender: WebContents) => RemoteSidecarRoutes | undefined;
}

export interface NativeNotificationOptions {
  title: string;
  body: string;
  silent?: boolean;
  payload?: unknown;
}

export interface TitleBarOverlayOptions {
  color?: string;
  symbolColor?: string;
}

export interface ContextMenuItem {
  id?: string;
  label?: string;
  type?: 'normal' | 'separator' | 'submenu' | 'checkbox' | 'radio';
  checked?: boolean;
  disabled?: boolean;
  accelerator?: string;
  submenu?: ContextMenuItem[];
}

export interface WslDistroSummary {
  name: string;
  isDefault: boolean;
}

export interface WslState {
  available: boolean;
  activeDistro: string | null;
  distros: WslDistroSummary[];
}

function isMainFramePromptCaller(
  event: IpcMainEvent | IpcMainInvokeEvent | null | undefined,
): boolean {
  const s = event?.sender;
  return Boolean(
    s &&
      !s.isDestroyed?.() &&
      !event?.senderFrame?.parent &&
      (!event?.senderFrame || !s.mainFrame || event.senderFrame === s.mainFrame),
  );
}

// Not SSH windows: they have no guests or session preloads to prompt from.
const onHumanPromptSync = (event: IpcMainEvent, rawJson?: unknown): void => {
  if (isRemoteContents(event?.sender)) {
    event.returnValue = null;
    return;
  }
  guestPromptController.handleSyncPrompt(event, rawJson);
};

const onHumanPromptSubscriberState = (
  event: IpcMainEvent,
  subscribed: unknown,
): void => {
  if (isMainFramePromptCaller(event) && !isRemoteContents(event.sender)) {
    guestPromptController.setHostSubscribed(event.sender, Boolean(subscribed));
  }
};

deviceChooserController.setHostResolver((webContentsId) =>
  wcvBrowserRegistry.resolveDeviceChooserHost(webContentsId),
);

function onDeviceChooserSubscriberState(
  event: IpcMainEvent,
  subscribed: unknown,
): void {
  if (isAuthorizedHubMainFrameEvent(event)) {
    deviceChooserController.setHostSubscribed(
      event.sender,
      Boolean(subscribed),
    );
  }
}

/** Hub window that sent an IPC message, if it still exists. */
function windowForSender(
  sender: WebContents | null | undefined,
): BrowserWindow | undefined {
  // A destroyed sender's getters (e.g. hostWebContents) throw.
  if (!sender || sender.isDestroyed?.()) {
    return undefined;
  }
  const win = resolveHubWindowFromWebContents(sender);
  return win && !win.isDestroyed?.() ? win : undefined;
}

/**
 * Registers a channel SSH windows can't use (defense in depth: their preload
 * lacks it). Remote content must not reach the laptop's dialogs, updater,
 * logs (they name local ports), plugin:// map, files, IDE, WSL, built-in
 * browser or script jail.
 */
function localOnly<Args extends unknown[], Result>(
  channel: string,
  handler: (event: IpcMainInvokeEvent, ...args: Args) => Result,
): void {
  ipcMain.handle(channel, (event, ...args) => {
    if (isRemoteContents(event?.sender)) {
      throw new Error(`${channel} is not available in SSH windows`);
    }
    return handler(event, ...(args as Args));
  });
}

/** Longest notification title/body accepted from a page. */
const MAX_NOTIFICATION_TITLE = 200;
const MAX_NOTIFICATION_BODY = 1000;

function truncate(text: unknown, max: number): string {
  const s = typeof text === 'string' ? text : '';
  return s.length > max ? `${s.slice(0, max - 1)}…` : s;
}

function buildContextMenuTemplate(
  items: ContextMenuItem[],
  onSelect: (id: string | undefined) => void,
): MenuItemConstructorOptions[] {
  return items.map((item) => {
    if (item.type === 'separator') {
      return { type: 'separator' };
    }
    if (item.type === 'submenu' || (item.submenu && item.submenu.length > 0)) {
      return {
        id: item.id,
        label: item.label ?? '',
        type: 'submenu',
        enabled: !item.disabled,
        submenu: buildContextMenuTemplate(item.submenu ?? [], onSelect),
      };
    }
    return {
      id: item.id,
      label: item.label ?? '',
      type: item.type ?? 'normal',
      checked: item.checked,
      enabled: !item.disabled,
      accelerator: item.accelerator,
      click: () => {
        onSelect(item.id);
      },
    };
  });
}

const SAFE_UPLOAD_CONVERSATION_ID_RE = /^[A-Za-z0-9._-]+$/;

/**
 * Registers all IPC handlers for the main process.
 */
export function registerIpcHandlers(
  storageManager: StorageManager,
  deps: IpcHandlerDeps = {},
): void {
  setHubZoomStorageManager(storageManager);

  /** For an SSH window its own (never another), else the focused or first. */
  const getTargetHubWindow = (
    sender: WebContents | undefined,
  ): BrowserWindow | undefined =>
    isRemoteContents(sender)
      ? windowForSender(sender)
      : (getFocusedHubWindow() ?? getHubWindows()[0]);

  const storageFor = (sender: WebContents | undefined): IpcStorageLike => {
    const store = sender && deps.resolveStorage?.(sender);
    if (store) {
      return store;
    }
    // Fail closed: other remote contents get no store, never the main one.
    if (isRemoteContents(sender)) {
      throw new Error('Storage is not available in this SSH window');
    }
    return storageManager;
  };

  // Dialog
  // In WSL mode, start the folder dialog in the distro's home directory.
  async function dialogDefaultPath(): Promise<string | undefined> {
    const distro = getActiveWslDistro();
    if (!distro) {
      return undefined;
    }
    try {
      return distroToWindowsPath(await getDistroHome(distro), distro);
    } catch {
      return undefined;
    }
  }

  // In WSL mode, translate a picked Windows path to the distro's view.
  // Returns undefined when the path is unusable (e.g. another distro).
  function mapPickedPath(picked: string): string | undefined {
    const distro = getActiveWslDistro();
    if (!distro) {
      return picked;
    }
    const t = windowsToDistroPath(picked, distro);
    if (t.error) {
      dialog.showErrorBox('Cannot open folder', t.error);
      return undefined;
    }
    if (t.warning) {
      void dialog.showMessageBox({
        type: 'warning',
        message: 'Folder is on the Windows filesystem',
        detail: t.warning,
      });
    }
    return t.path;
  }

  localOnly('dialog:open-workspace', async (): Promise<string | undefined> => {
    const result = await dialog.showOpenDialog({
      properties: ['openDirectory', 'createDirectory'],
      title: 'Open workspace',
      defaultPath: await dialogDefaultPath(),
    });
    if (result.canceled || result.filePaths.length === 0) {
      return undefined;
    }
    return mapPickedPath(result.filePaths[0]!);
  });

  localOnly('dialog:get-default-download-dir', (event): string | null => {
    const sender = event?.sender;
    if (
      !sender ||
      sender.isDestroyed?.() ||
      event?.senderFrame?.parent ||
      (event?.senderFrame &&
        sender.mainFrame &&
        event.senderFrame !== sender.mainFrame) ||
      sender.getType?.() === 'webview' ||
      (typeof sender.id === 'number' &&
        wcvBrowserRegistry.isRegisteredWcv(sender.id))
    ) {
      throw new Error('Unauthorized dialog:get-default-download-dir sender');
    }
    try {
      const downloadsDir = app.getPath('downloads');
      if (typeof downloadsDir !== 'string' || downloadsDir.includes('\0')) {
        return null;
      }
      const trimmed = downloadsDir.trim();
      return trimmed.length > 0 ? trimmed : null;
    } catch {
      return null;
    }
  });

  // Like 'dialog:open-workspace' but allows selecting multiple folders,
  // returning an array of paths (empty if cancelled).
  localOnly('dialog:open-workspaces', async (): Promise<string[]> => {
    const result = await dialog.showOpenDialog({
      properties: ['openDirectory', 'createDirectory', 'multiSelections'],
      title: 'Open workspaces',
      defaultPath: await dialogDefaultPath(),
    });
    if (result.canceled || result.filePaths.length === 0) {
      return [];
    }
    return result.filePaths
      .map(mapPickedPath)
      .filter((p): p is string => p !== undefined);
  });

  // Auto-updater
  localOnly('updater:apply', async (): Promise<void> => {
    broadcastState({ type: UpdateState.Ready });
  });

  localOnly('updater:quit-and-install', (): void => {
    if (!app.isPackaged) {
      console.log(
        '[AutoUpdater] Skipping quitAndInstall (requires a packaged app).',
      );
      return;
    }
    autoUpdater.quitAndInstall();
  });

  localOnly('updater:get-state', (): UpdaterState => {
    return getLastState();
  });

  // Notifications
  ipcMain.handle(
    'notification:send',
    (event, options: NativeNotificationOptions) => {
      // Named by their host: an SSH window can't pose as the app.
      const remote = isRemoteContents(event?.sender);
      const label = remoteBackendOf(event?.sender)?.label ?? 'SSH';
      const remoteTitle = truncate(options?.title, MAX_NOTIFICATION_TITLE);
      const notification = new Notification({
        title: remote
          ? truncate(
              remoteTitle ? `${label}: ${remoteTitle}` : label,
              MAX_NOTIFICATION_TITLE,
            )
          : options.title,
        body: remote
          ? truncate(options?.body, MAX_NOTIFICATION_BODY)
          : options.body,
        silent: options?.silent ?? false,
      });

      notification.on('click', () => {
        // A remote payload goes only to the window that posted it (if open); a
        // local one to the first local window.
        const win = remote
          ? windowForSender(event?.sender)
          : getLocalHubWindows()[0];
        if (win) {
          if (win.isMinimized()) {
            win.restore();
          }
          win.show();
          win.focus();
          if (options?.payload) {
            win.webContents.send('notification:clicked', options.payload);
          }
        }
      });

      notification.show();
    },
  );

  // Note: copied from our desktop AGY implementation:
  // vs/platform/nativeNotification/electron-main/electronNotificationService.ts
  ipcMain.handle('notification:open-system-preferences', async () => {
    if (process.platform === 'darwin') {
      void shell.openExternal(
        'x-apple.systempreferences:com.apple.preference.notifications',
      );
    } else if (process.platform === 'win32') {
      void shell.openExternal('ms-settings:notifications');
    } else if (process.platform === 'linux') {
      const { exec } = await import('child_process');
      const commands = [
        'gnome-control-center notifications',
        'systemsettings kcm_notifications',
        'xfce4-notifyd-config',
        'gnome-control-center',
        'systemsettings',
      ];
      for (const command of commands) {
        try {
          exec(command);
          return; // If one command executes without immediate error, assume success for now
        } catch {
          // Try next
        }
      }
    }
  });

  const readOsMicAccessStatus = (): MicAccessStatus => {
    try {
      return systemPreferences.getMediaAccessStatus('microphone');
    } catch {
      return 'unknown';
    }
  };

  localOnly('mic:get-access-status', (event): MicAccessStatus => {
    if (!isAuthorizedHubMainFrameEvent(event)) {
      return 'unknown';
    }
    if (process.platform === 'darwin' || process.platform === 'win32') {
      return readOsMicAccessStatus();
    }
    return 'unknown';
  });

  let pendingMicAccessRequest: Promise<MicAccessStatus> | null = null;
  localOnly('mic:request-access', async (event): Promise<MicAccessStatus> => {
    if (!isAuthorizedHubMainFrameEvent(event)) {
      return 'unknown';
    }
    if (process.platform === 'darwin') {
      const status = readOsMicAccessStatus();
      if (status !== 'not-determined') {
        return status;
      }
      if (!pendingMicAccessRequest) {
        pendingMicAccessRequest = (async (): Promise<MicAccessStatus> => {
          try {
            const granted =
              await systemPreferences.askForMediaAccess('microphone');
            if (granted) {
              return 'granted';
            }
            const after = readOsMicAccessStatus();
            return after === 'not-determined' ? 'denied' : after;
          } catch {
            return 'unknown';
          } finally {
            pendingMicAccessRequest = null;
          }
        })();
      }
      return pendingMicAccessRequest;
    }
    if (process.platform === 'win32') {
      return readOsMicAccessStatus();
    }
    return 'unknown';
  });

  localOnly('mic:open-system-preferences', async (event): Promise<void> => {
    if (!isAuthorizedHubMainFrameEvent(event)) {
      return;
    }
    if (process.platform === 'darwin') {
      await openExternalSafely(
        'x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone',
        'confirmed',
      );
    } else if (process.platform === 'win32') {
      await openExternalSafely('ms-settings:privacy-microphone', 'confirmed');
    }
  });

  // Storage
  ipcMain.removeAllListeners?.('storage:get-items-sync');
  ipcMain.on('storage:get-items-sync', (event) => {
    event.returnValue = isLocalHubTopFrame(event)
      ? storageManager.getItemsSync()
      : null;
  });

  ipcMain.handle('storage:get-items', async (event) => {
    return storageFor(event?.sender).getItems();
  });

  ipcMain.handle('storage:update-items', async (event, changes: StorageChanges) => {
    await storageFor(event?.sender).updateItems(changes);
  });

  // Logs
  localOnly('logs:electron', async (): Promise<string> => {
    try {
      const logPath = log.transports.file.getFile().path;
      const contents = await fs.readFile(logPath, 'utf-8');
      return contents;
    } catch (err: unknown) {
      return `Failed to read logs: ${String(err)}`;
    }
  });

  // Sidecar extension custom scheme: the laptop's map for local windows. An
  // SSH window's map is its host's LS's list of sidecars, which main reads
  // itself (remoteSidecars.ts): the send only says that the list changed.
  ipcMain.handle(
    'extensions:send-authorities',
    async (event, authorities: Record<string, string>) => {
      if (isRemoteContents(event?.sender)) {
        const backend = remoteBackendOf(event?.sender);
        const routes = backend && deps.sidecarRoutesFor?.(event.sender);
        if (!routes) {
          throw new Error(
            'extensions:send-authorities is not available in this SSH window',
          );
        }
        const error = await routes.refresh();
        if (error) {
          log.warn(
            `[SSH] ${backend!.label}: listing its sidecars failed: ${error}`,
          );
        }
        return;
      }
      extensionAuthorities.clear();
      for (const [key, value] of Object.entries(authorities)) {
        extensionAuthorities.set(key, value);
      }
    },
  );

  // Agent
  ipcMain.handle(
    'agent:update-active-count',
    async (event, count: number) => {
      if (isRemoteContents(event?.sender)) {
        return; // The tray counts local agents (remote ones outlive quitting).
      }
      updateTrayAgentCount(count);
    },
  );

  // Window
  ipcMain.handle(
    'window:set-title-bar-overlay',
    async (event, options: TitleBarOverlayOptions) => {
      const win = getTargetHubWindow(event?.sender);
      if (win && process.platform === 'win32') {
        win.setTitleBarOverlay({
          color: options.color,
          symbolColor: options.symbolColor,
          height: 30,
        });
      }
    },
  );

  ipcMain.handle('window:minimize', async (event) => {
    const win = getTargetHubWindow(event?.sender);
    if (win) {
      win.minimize();
    }
  });

  ipcMain.handle('window:maximize', async (event) => {
    const win = getTargetHubWindow(event?.sender);
    if (win) {
      win.maximize();
    }
  });

  ipcMain.handle('window:unmaximize', async (event) => {
    const win = getTargetHubWindow(event?.sender);
    if (win) {
      win.unmaximize();
    }
  });

  ipcMain.handle('window:is-maximized', async (event) => {
    const win = getTargetHubWindow(event?.sender);
    return win ? win.isMaximized() : false;
  });

  ipcMain.handle('window:close', async (event) => {
    const win = getTargetHubWindow(event?.sender);
    if (win) {
      win.close();
    }
  });

  ipcMain.handle('window:toggle-devtools', async (event) => {
    // An SSH window's top frame too (the other check takes local windows only).
    const allowed = remoteBackendOf(event?.sender)
      ? !!event.senderFrame && event.senderFrame === event.sender.mainFrame
      : isAuthorizedHubMainFrameEvent(event);
    if (!isDebugConsoleEnabled() || !allowed) {
      return;
    }
    const win = getTargetHubWindow(event?.sender);
    if (win) {
      win.webContents.toggleDevTools();
    }
  });

  ipcMain.removeListener?.(
    WEBVIEW_DEVICE_CHOOSER_SUBSCRIBER_STATE_CHANNEL,
    onDeviceChooserSubscriberState,
  );
  ipcMain.on?.(
    WEBVIEW_DEVICE_CHOOSER_SUBSCRIBER_STATE_CHANNEL,
    onDeviceChooserSubscriberState,
  );

  localOnly(
    WEBVIEW_DEVICE_CHOOSER_RESPONSE_CHANNEL,
    async (
      event,
      requestId: unknown,
      resolution: unknown,
    ): Promise<boolean> => {
      if (!isAuthorizedHubMainFrameEvent(event)) {
        throw new Error('Unauthorized webview:device-chooser-response sender');
      }
      if (
        typeof requestId !== 'string' ||
        requestId.length === 0 ||
        !resolution ||
        typeof resolution !== 'object'
      ) {
        return false;
      }
      const { action, deviceId } = resolution as {
        action?: unknown;
        deviceId?: unknown;
      };
      if (action === 'claim' || action === 'unclaimed' || action === 'cancel') {
        return deviceChooserController.respond(event.sender, requestId, {
          action,
        });
      }
      if (
        action === 'select' &&
        typeof deviceId === 'string' &&
        deviceId.length > 0
      ) {
        return deviceChooserController.respond(event.sender, requestId, {
          action: 'select',
          deviceId,
        });
      }
      return false;
    },
  );

  let activeContextMenu: Menu | null = null;
  // An SSH window may close only its own window's menu.
  let activeContextMenuOwner: BrowserWindow | undefined;

  ipcMain.handle(
    'window:show-context-menu',
    async (event, items: ContextMenuItem[]) => {
      const win = getTargetHubWindow(event?.sender);
      if (!win || !Array.isArray(items) || items.length === 0) {
        return null;
      }
      activeContextMenu?.closePopup?.(win);
      return new Promise<string | null | undefined>((resolve) => {
        let settled = false;
        const template = buildContextMenuTemplate(items, (id) => {
          if (!settled) {
            settled = true;
            activeContextMenu = null;
            activeContextMenuOwner = undefined;
            resolve(id);
          }
        });
        const menu = Menu.buildFromTemplate(template);
        activeContextMenu = menu;
        activeContextMenuOwner = win;
        menu.popup({
          window: win,
          callback: () => {
            setImmediate(() => {
              if (activeContextMenu === menu) {
                activeContextMenu = null;
                activeContextMenuOwner = undefined;
              }
              if (!settled) {
                settled = true;
                resolve(null);
              }
            });
          },
        });
      });
    },
  );

  ipcMain.handle('window:close-context-menu', (event) => {
    const win = getTargetHubWindow(event?.sender);
    if (isRemoteContents(event?.sender) && activeContextMenuOwner !== win) {
      return; // Not its own window's menu.
    }
    activeContextMenu?.closePopup?.(win);
    activeContextMenu = null;
    activeContextMenuOwner = undefined;
  });

  // Zoom — main-process source of truth so the level is reliably persisted
  // across restarts on all platforms (works around a Chromium quirk on Windows
  // where renderer-only webFrame.setZoomLevel changes aren't cached).
  // The level is shared and Electron doesn't clamp it: SSH windows stay within
  // Chrome's 25%-500% (factor 1.2^level).
  const MIN_REMOTE_ZOOM_LEVEL = Math.log(0.25) / Math.log(1.2);
  const MAX_REMOTE_ZOOM_LEVEL = Math.log(5) / Math.log(1.2);
  const zoom = (event: IpcMainInvokeEvent, action: HubZoomAction) =>
    applyHubWindowZoom(
      getTargetHubWindow(event?.sender),
      action,
      isRemoteContents(event?.sender)
        ? (level: number) =>
            Math.min(MAX_REMOTE_ZOOM_LEVEL, Math.max(MIN_REMOTE_ZOOM_LEVEL, level))
        : undefined,
    );

  ipcMain.handle('window:zoom-in', (event) => zoom(event, 'in'));
  ipcMain.handle('window:zoom-out', (event) => zoom(event, 'out'));
  ipcMain.handle('window:reset-zoom', (event) => zoom(event, 'reset'));

  // Auto-updater manual check
  localOnly('updater:check-for-updates', (): void => {
    checkForUpdates(true);
  });

  // Safe external shell launch
  ipcMain.handle('shell:open-external', async (event, url: unknown) => {
    if (isRemoteContents(event?.sender)) {
      if (openRemoteUrl(url) !== 'opened') {
        const label = remoteBackendOf(event?.sender)?.label ?? 'an SSH window';
        log.warn(`[SSH] Not opening a link from ${label}: blocked.`);
      }
      return;
    }
    await openExternalSafely(url, 'confirmed');
  });

  localOnly('shell:reveal-in-file-picker', (_event, fileUrl: string): void => {
    const distro = getActiveWslDistro();
    if (distro) {
      const posixPath = fileURLToPath(fileUrl, { windows: false });
      shell.showItemInFolder(distroToWindowsPath(posixPath, distro));
      return;
    }
    shell.showItemInFolder(fileURLToPath(fileUrl));
  });

  localOnly(
    'webview:set-download-dir',
    (
      event,
      webContentsId: unknown,
      downloadDir: unknown,
    ): string | null => {
      if (
        typeof webContentsId !== 'number' ||
        !Number.isInteger(webContentsId) ||
        webContentsId <= 0
      ) {
        throw new Error(`Invalid webContentsId: ${String(webContentsId)}`);
      }
      if (downloadDir !== null && typeof downloadDir !== 'string') {
        throw new Error('downloadDir must be a string or null');
      }
      if (event?.senderFrame?.parent) {
        throw new Error(
          'Unauthorized webview:set-download-dir sender: subframe cannot configure downloads',
        );
      }
      const sender = event?.sender;
      if (!sender || sender.isDestroyed?.()) {
        throw new Error(
          `WebContents ${webContentsId} is not owned by the calling renderer`,
        );
      }
      const wc = webContents.fromId(webContentsId);
      if (!isOwnedBrowserGuestWebContents(wc, sender.id)) {
        throw new Error(
          `WebContents ${webContentsId} is not owned by the calling renderer`,
        );
      }
      return setGuestDownloadDirectory(wc, downloadDir);
    },
  );

  localOnly(
    'webview:set-agent-controlled',
    (event, webContentsId: unknown, active: unknown): boolean => {
      // Same sender/ownership rules as webview:set-download-dir, but reject
      // with `false` instead of throwing.
      if (
        typeof webContentsId !== 'number' ||
        !Number.isInteger(webContentsId) ||
        webContentsId <= 0 ||
        typeof active !== 'boolean'
      ) {
        return false;
      }
      if (event?.senderFrame?.parent) {
        return false;
      }
      const sender = event?.sender;
      if (!sender || sender.isDestroyed?.()) {
        return false;
      }
      const wc = webContents.fromId(webContentsId);
      if (!wc || !isOwnedBrowserGuestWebContents(wc, sender.id)) {
        return false;
      }
      return setGuestAgentControlled(wc, active);
    },
  );

  const assertTopLevelSenderId = (
    event: IpcMainInvokeEvent,
    channel: string,
  ): number => {
    if (event?.senderFrame?.parent) {
      throw new Error(
        `Unauthorized ${channel} sender: subframe cannot invoke ${channel}`,
      );
    }
    const sender = event?.sender;
    if (
      !sender ||
      sender.isDestroyed?.() ||
      typeof sender.id !== 'number' ||
      !Number.isInteger(sender.id)
    ) {
      throw new Error(`Unauthorized ${channel} sender`);
    }
    return sender.id;
  };

  localOnly(
    'webview:open-download',
    async (event, downloadId: unknown): Promise<boolean> => {
      const senderId = assertTopLevelSenderId(event, 'webview:open-download');
      if (typeof downloadId !== 'string') {
        throw new Error('downloadId must be a string');
      }
      return openTrackedWebviewDownload(senderId, downloadId);
    },
  );

  localOnly(
    'webview:show-download-in-folder',
    (event, downloadId: unknown): boolean => {
      const senderId = assertTopLevelSenderId(
        event,
        'webview:show-download-in-folder',
      );
      if (typeof downloadId !== 'string') {
        throw new Error('downloadId must be a string');
      }
      return showTrackedWebviewDownloadInFolder(senderId, downloadId);
    },
  );

  localOnly(
    'webview:cancel-download',
    (event, downloadId: unknown): boolean => {
      const senderId = assertTopLevelSenderId(event, 'webview:cancel-download');
      if (typeof downloadId !== 'string') {
        throw new Error('downloadId must be a string');
      }
      return cancelTrackedWebviewDownload(senderId, downloadId);
    },
  );

  localOnly(
    'webview:resolve-download',
    (event, downloadId: unknown, approve: unknown): boolean => {
      // Approving resumes a paused agent download, so reject (return false)
      // anything other than the owning host's main frame with valid args.
      let senderId: number;
      try {
        senderId = assertTopLevelSenderId(event, 'webview:resolve-download');
      } catch {
        return false;
      }
      if (typeof downloadId !== 'string' || typeof approve !== 'boolean') {
        return false;
      }
      return resolveTrackedWebviewDownload(senderId, downloadId, approve);
    },
  );

  localOnly(
    'dialog:select-download-dir',
    async (event, defaultPath: unknown): Promise<string | null> => {
      assertTopLevelSenderId(event, 'dialog:select-download-dir');
      let resolvedDefaultPath: string | undefined;
      if (
        typeof defaultPath === 'string' &&
        defaultPath.trim().length > 0 &&
        !defaultPath.includes('\0')
      ) {
        resolvedDefaultPath = defaultPath.trim();
      } else {
        try {
          resolvedDefaultPath = app.getPath('downloads');
        } catch {
          resolvedDefaultPath = undefined;
        }
      }
      const options: OpenDialogOptions = {
        properties: ['openDirectory', 'createDirectory'],
        title: 'Select Download Folder',
        defaultPath: resolvedDefaultPath,
      };
      const owner = BrowserWindow.fromWebContents(event.sender);
      const result = owner
        ? await dialog.showOpenDialog(owner, options)
        : await dialog.showOpenDialog(options);
      if (result.canceled || result.filePaths.length === 0) {
        return null;
      }
      const picked = result.filePaths[0];
      if (
        typeof picked !== 'string' ||
        picked.includes('\0') ||
        !path.isAbsolute(picked)
      ) {
        return null;
      }
      try {
        return (await fs.stat(picked)).isDirectory() ? picked : null;
      } catch {
        return null;
      }
    },
  );

  // IDE installation check
  localOnly('ide:is-installed', async (): Promise<boolean> => {
    try {
      // Check standard installation path (works even if the app has never been launched).
      await fs.stat(getIdeInstallPath());
      return true;
    } catch {
      return false;
    }
  });

  // Per-guest renderer working-set memory in bytes for memory-budgeted LRU eviction.
  localOnly(
    'webview:get-memory-usage',
    async (
      event,
      webContentsIds: unknown,
    ): Promise<Record<number, number>> => {
      const sender = event?.sender;
      if (
        !sender ||
        sender.isDestroyed() ||
        event?.senderFrame?.parent ||
        (event?.senderFrame &&
          sender.mainFrame &&
          event.senderFrame !== sender.mainFrame) ||
        !Array.isArray(webContentsIds) ||
        webContentsIds.length === 0
      ) {
        return {};
      }
      const MAX_QUERIED_GUESTS = 64;
      const seenIds = new Set<number>();
      const ownedGuests: WcvGuestPidInfo[] = [];
      for (const rawId of webContentsIds) {
        if (seenIds.size >= MAX_QUERIED_GUESTS) {
          break;
        }
        if (
          typeof rawId !== 'number' ||
          !Number.isInteger(rawId) ||
          rawId <= 0 ||
          seenIds.has(rawId)
        ) {
          continue;
        }
        seenIds.add(rawId);
        const wc = webContents.fromId(rawId);
        const isWcv = wcvBrowserRegistry.isRegisteredWcv(rawId);
        if (
          !wc ||
          wc.isDestroyed() ||
          (wc.getType() !== 'webview' && !isWcv)
        ) {
          continue;
        }
        const isOwned = isWcv
          ? wcvBrowserRegistry.isOwnedBySender(rawId, sender.id)
          : Boolean(
              sender.id !== undefined && wc.hostWebContents?.id === sender.id,
            );
        if (!isOwned) {
          continue;
        }
        let pid: number | undefined;
        try {
          pid = wc.getOSProcessId?.();
        } catch {
          pid = undefined;
        }
        ownedGuests.push({ webContentsId: rawId, pid });
      }
      if (ownedGuests.length === 0) {
        return {};
      }
      const bytesByWcId =
        wcvBrowserRegistry.computeGuestMemoryBytes(ownedGuests);
      const out: Record<number, number> = {};
      for (const [id, bytes] of bytesByWcId.entries()) {
        out[id] = bytes;
      }
      return out;
    },
  );

  // WSL environment switching (Windows only). Lets the web UI (e.g. the
  // Settings page) discover WSL distros and relaunch the app into/out of one.
  localOnly('wsl:get-state', async (): Promise<WslState> => {
    if (!isWslAvailable()) {
      return { available: false, activeDistro: null, distros: [] };
    }
    try {
      const distros = await listWslDistros();
      return {
        available: true,
        activeDistro: getActiveWslDistro() ?? null,
        distros: distros.map((d) => ({ name: d.name, isDefault: d.isDefault })),
      };
    } catch (err: unknown) {
      log.error('wsl:get-state failed:', err);
      return { available: false, activeDistro: null, distros: [] };
    }
  });

  // Relaunches into the given distro, or locally when `distro` is empty.
  localOnly(
    'wsl:connect',
    (_event, distro: string | null | undefined): void => {
      relaunchWithWslDistro(distro || '');
    },
  );

  // Execute JavaScript inside an Electron <webview> guest's isolated world.
  // Only the renderer that hosts the <webview> may target it, and worldId is
  // bounded to [MIN, MAX] so it can never hit the main (0) or preload worlds
  // and stays well below Chromium's fatal ISOLATED_WORLD_ID_MAX CHECK.
  const MIN_ISOLATED_WORLD_ID = 1000;
  const MAX_ISOLATED_WORLD_ID = 100000;
  localOnly(
    'webview:execute-js-isolated-world',
    async (
      event,
      webContentsId: unknown,
      worldId: unknown,
      code: unknown,
    ): Promise<unknown> => {
      if (
        typeof webContentsId !== 'number' ||
        !Number.isInteger(webContentsId) ||
        webContentsId <= 0
      ) {
        throw new Error(`Invalid webContentsId: ${String(webContentsId)}`);
      }
      if (
        typeof worldId !== 'number' ||
        !Number.isInteger(worldId) ||
        worldId < MIN_ISOLATED_WORLD_ID ||
        worldId > MAX_ISOLATED_WORLD_ID
      ) {
        throw new Error(
          `Invalid isolated worldId: ${String(worldId)} (must be in [${MIN_ISOLATED_WORLD_ID}, ${MAX_ISOLATED_WORLD_ID}])`,
        );
      }
      if (typeof code !== 'string') {
        throw new Error('code must be a string');
      }
      const wc = webContents.fromId(webContentsId);
      const isWcv = wcvBrowserRegistry.isRegisteredWcv(webContentsId);
      if (!wc || wc.isDestroyed() || (wc.getType() !== 'webview' && !isWcv)) {
        throw new Error(`Invalid or non-webview webContents id: ${webContentsId}`);
      }
      const sender = event?.sender;
      const isOwned = isWcv
        ? wcvBrowserRegistry.isOwnedBySender(webContentsId, sender?.id)
        : wc.hostWebContents?.id === sender?.id;
      if (!sender || sender.isDestroyed() || !isOwned) {
        throw new Error(
          `WebContents ${webContentsId} is not owned by the calling renderer`,
        );
      }
      if (
        !event?.senderFrame ||
        event.senderFrame !== sender.mainFrame ||
        event.senderFrame.parent
      ) {
        throw new Error(
          'webview:execute-js-isolated-world must be called from the main frame',
        );
      }
      return executeJavaScriptInIsolatedWorldWhenReady(
        wc,
        worldId,
        [{ code }],
        false,
      );
    },
  );

  const resolveOwnedGuestWebContents = (
    event: IpcMainInvokeEvent,
    webContentsId: unknown,
  ): { wc: WebContents; sender: WebContents } => {
    if (
      typeof webContentsId !== 'number' ||
      !Number.isInteger(webContentsId) ||
      webContentsId <= 0
    ) {
      throw new Error(`Invalid webContentsId: ${String(webContentsId)}`);
    }
    const wc = webContents.fromId(webContentsId);
    const isWcv = wcvBrowserRegistry.isRegisteredWcv(webContentsId);
    if (!wc || wc.isDestroyed() || (wc.getType() !== 'webview' && !isWcv)) {
      throw new Error(`Invalid or non-webview webContents id: ${webContentsId}`);
    }
    const sender = event?.sender;
    const isOwned = isWcv
      ? wcvBrowserRegistry.isOwnedBySender(webContentsId, sender?.id)
      : wc.hostWebContents?.id === sender?.id;
    if (!sender || sender.isDestroyed() || !isOwned) {
      throw new Error(
        `WebContents ${webContentsId} is not owned by the calling renderer`,
      );
    }
    return { wc, sender };
  };

  localOnly('webview:get-network-requests', (event, webContentsId: unknown) => {
    const { wc, sender } = resolveOwnedGuestWebContents(event, webContentsId);
    if (
      !event?.senderFrame ||
      (sender.mainFrame && event.senderFrame !== sender.mainFrame) ||
      event.senderFrame.parent
    ) {
      throw new Error(
        'webview:get-network-requests must be called from the main frame',
      );
    }
    return guestNetworkController.getRequests(wc.id);
  });

  localOnly(
    'webview:clear-network-requests',
    (event, webContentsId: unknown): boolean => {
      const { wc, sender } = resolveOwnedGuestWebContents(event, webContentsId);
      if (
        !event?.senderFrame ||
        (sender.mainFrame && event.senderFrame !== sender.mainFrame) ||
        event.senderFrame.parent
      ) {
        throw new Error(
          'webview:clear-network-requests must be called from the main frame',
        );
      }
      return guestNetworkController.clearRequests(wc.id);
    },
  );

  const guestOf = (
    e: IpcMainInvokeEvent,
    id: unknown,
  ): { wc: WebContents; sender: WebContents } => {
    const { wc, sender } = resolveOwnedGuestWebContents(e, id);
    const f = e?.senderFrame;
    if (!f || f.parent || (sender.mainFrame && f !== sender.mainFrame)) {
      throw new Error('WebMCP IPC must be called from the main frame');
    }
    return { wc, sender };
  };

  localOnly('webview:webmcp-set-enabled', (event, enabled: unknown): boolean => {
    if (!isAuthorizedHubMainFrameEvent(event) || typeof enabled !== 'boolean') {
      throw new Error('Invalid webview:webmcp-set-enabled call');
    }
    guestWebMcpController.setEnabled(enabled);
    return guestWebMcpController.isEnabled();
  });

  localOnly(
    'webview:webmcp-list-tools',
    (event, webContentsId: unknown, origin: unknown) =>
      guestWebMcpController.listTools(
        guestOf(event, webContentsId).wc,
        origin,
      ),
  );

  localOnly(
    'webview:webmcp-call-tool',
    (event, webContentsId: unknown, origin: unknown, params: unknown) => {
      const { wc, sender } = guestOf(event, webContentsId);
      return guestWebMcpController.callTool(wc, sender, origin, params);
    },
  );

  localOnly(
    'webview:webmcp-cancel-tool-call',
    (event, webContentsId: number, callId: unknown): boolean =>
      guestWebMcpController.cancelToolCall(
        webContentsId,
        callId,
        guestOf(event, webContentsId).sender.id,
      ),
  );

  localOnly(
    'webview:set-dialog-policy',
    async (
      event,
      webContentsId: unknown,
      policy: Record<string, unknown> | null | undefined,
    ): Promise<boolean> => {
      const { wc, sender } = resolveOwnedGuestWebContents(event, webContentsId);
      const safePolicy: DialogPolicy = {
        enabled: Boolean(policy?.enabled),
      };
      if (
        policy?.action === 'accept' ||
        policy?.action === 'dismiss' ||
        policy?.action === 'manual'
      ) {
        safePolicy.action = policy.action;
      }
      if (typeof policy?.promptText === 'string') {
        safePolicy.promptText = policy.promptText;
      }
      if (typeof policy?.tabKey === 'string') {
        safePolicy.tabKey = policy.tabKey;
      }
      if (
        policy?.fallbackAction === 'accept' ||
        policy?.fallbackAction === 'dismiss'
      ) {
        safePolicy.fallbackAction = policy.fallbackAction;
      }
      if (typeof policy?.fallbackPromptText === 'string') {
        safePolicy.fallbackPromptText = policy.fallbackPromptText;
      }
      return guestDialogController.setPolicy(wc, sender, safePolicy);
    },
  );

  localOnly(
    'webview:handle-javascript-dialog',
    async (
      event,
      webContentsId: unknown,
      decision: Record<string, unknown> | null | undefined,
    ): Promise<boolean> => {
      const { wc } = resolveOwnedGuestWebContents(event, webContentsId);
      return guestDialogController.handleDialog(wc, {
        ...(typeof decision?.dialogId === 'string'
          ? { dialogId: decision.dialogId }
          : {}),
        accept: Boolean(decision?.accept),
        ...(typeof decision?.promptText === 'string'
          ? { promptText: decision.promptText }
          : {}),
      });
    },
  );

  ipcMain.removeListener?.(HUMAN_PROMPT_SYNC_CHANNEL, onHumanPromptSync);
  ipcMain.on?.(HUMAN_PROMPT_SYNC_CHANNEL, onHumanPromptSync);
  ipcMain.removeListener?.(
    WEBVIEW_PROMPT_SUBSCRIBER_STATE_CHANNEL,
    onHumanPromptSubscriberState,
  );
  ipcMain.on?.(
    WEBVIEW_PROMPT_SUBSCRIBER_STATE_CHANNEL,
    onHumanPromptSubscriberState,
  );

  localOnly(
    WEBVIEW_PROMPT_RESPONSE_CHANNEL,
    (
      event,
      promptId: string | null | undefined,
      value: unknown,
    ): boolean => {
      if (!isMainFramePromptCaller(event)) {
        throw new Error('webview:prompt-response must be called from the main frame');
      }
      return guestPromptController.respondPrompt(
        event.sender.id,
        promptId,
        typeof value === 'string' ? value : null,
      );
    },
  );

  const assertMainFrameSender = (
    event: IpcMainInvokeEvent,
    channel: string,
  ): void => {
    const sender = event?.sender;
    if (
      !sender ||
      sender.isDestroyed() ||
      !event?.senderFrame ||
      event.senderFrame !== sender.mainFrame ||
      event.senderFrame.parent
    ) {
      throw new Error(`${channel} must be called from the main frame`);
    }
  };

  // The renderer only names a conversation; the uploads root itself is always
  // derived here from the app data dir the Language Server was started with.
  const pinUploadsRoot = (conversationId: unknown): string => {
    if (
      typeof conversationId !== 'string' ||
      !SAFE_UPLOAD_CONVERSATION_ID_RE.test(conversationId) ||
      /^\.+$/.test(conversationId)
    ) {
      return '';
    }
    let appDataDir = getAppDataDir();
    try {
      appDataDir = fsSync.realpathSync(appDataDir);
    } catch {
      // Upload validation reports the missing directory.
    }
    return path.join(
      appDataDir,
      'brain',
      conversationId,
      'browser_sandbox',
      'uploads',
    );
  };

  const resolveMainFrameOwnedGuest = (
    event: IpcMainInvokeEvent,
    webContentsId: unknown,
    channel: string,
  ): { wc: WebContents; sender: WebContents } => {
    const { wc, sender } = resolveOwnedGuestWebContents(event, webContentsId);
    assertMainFrameSender(event, channel);
    return { wc, sender };
  };

  localOnly(
    'webview:set-file-chooser-policy',
    async (
      event,
      webContentsId: unknown,
      policy: Record<string, unknown> | null | undefined,
    ): Promise<boolean> => {
      const { wc, sender } = resolveMainFrameOwnedGuest(
        event,
        webContentsId,
        'webview:set-file-chooser-policy',
      );
      return guestFileChooserController.setPolicy(wc, sender, {
        enabled: Boolean(policy?.enabled),
        ...(typeof policy?.tabKey === 'string' ? { tabKey: policy.tabKey } : {}),
      });
    },
  );

  localOnly(
    'webview:handle-file-chooser',
    async (
      event,
      webContentsId: unknown,
      decision: GuestFileChooserDecision | null | undefined,
    ) => {
      const { wc } = resolveMainFrameOwnedGuest(
        event,
        webContentsId,
        'webview:handle-file-chooser',
      );
      const pinned: GuestFileChooserDecision = {
        ...(decision ?? { chooserId: '' }),
      };
      if (Array.isArray(pinned.files) && pinned.files.length > 0) {
        pinned.allowedUploadsDir = pinUploadsRoot(pinned.expectedConversationId);
      }
      return guestFileChooserController.handleFileChooser(wc, pinned);
    },
  );

  localOnly(
    'webview:set-input-files',
    async (
      event,
      webContentsId: unknown,
      request: AttachStagedFilesRequest,
    ) => {
      const { wc } = resolveMainFrameOwnedGuest(
        event,
        webContentsId,
        'webview:set-input-files',
      );
      return guestFileChooserController.setInputFiles(wc, {
        ...request,
        allowedUploadsDir: pinUploadsRoot(request?.expectedConversationId),
      });
    },
  );

  localOnly(
    'webview:resolve-upload-files',
    (event, req: ResolveGuestUploadFilesRequest | null | undefined) => {
      assertMainFrameSender(event, 'webview:resolve-upload-files');
      return resolveGuestUploadFiles(
        req && {
          ...req,
          uploadsRoot: pinUploadsRoot(req.expectedConversationId),
        },
      );
    },
  );

  const assertMainFrameGuestCaller = (
    event: IpcMainInvokeEvent,
    webContentsId: unknown,
    channel: string,
  ): WebContents =>
    resolveMainFrameOwnedGuest(event, webContentsId, channel).wc;

  localOnly('webview:list-frames', async (event, wcId: unknown) =>
    guestFrameBridge.listFrames(
      assertMainFrameGuestCaller(event, wcId, 'webview:list-frames'),
    ),
  );

  localOnly(
    'webview:execute-js-frame-isolated-world',
    async (
      event,
      wcId: unknown,
      frameId: unknown,
      code: unknown,
      expectedOrigin: unknown,
    ): Promise<unknown> => {
      const channel = 'webview:execute-js-frame-isolated-world';
      const wc = assertMainFrameGuestCaller(event, wcId, channel);
      return guestFrameBridge.executeInFrameIsolatedWorld(
        wc,
        frameId,
        code,
        expectedOrigin,
      );
    },
  );

  // Execute an untrusted agent script inside a single-use, zero-egress
  // Electron script jail and transfer the paired MessagePort to the caller.
  localOnly('script-jail:run', async (event, req: unknown) => {
    return runScriptInJail(event?.sender, req, event?.senderFrame);
  });

  localOnly('script-jail:abort', (event, jailId: unknown): boolean => {
    return abortScriptInJail(event?.sender, jailId, event?.senderFrame);
  });

  localOnly('script-jail:warm', async (event): Promise<boolean> => {
    return warmScriptJail(event?.sender, event?.senderFrame);
  });

  registerWcvIpcHandlers(undefined, undefined, storageManager);
}
