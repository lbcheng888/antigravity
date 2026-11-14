import {
  autoUpdater,
  type AppUpdater,
  type ProgressInfo,
  type UpdateDownloadedEvent,
  type UpdateInfo,
} from 'electron-updater';
import { app, dialog, Menu, type MessageBoxOptions } from 'electron';
import * as path from 'path';
import { UpdateState } from './types';
import { spawn } from 'child_process';
import {
  SettingKey,
  type SettingSubscription,
} from './services/settingsService';
import {
  getFocusedHubWindow,
  getHubWebContents,
  getHubWindows,
} from './utils';

export enum MenuUpdateStep {
  CheckForUpdates = 'Check for Updates',
  CheckingForUpdates = 'Checking for Updates...',
  DownloadingUpdate = 'Downloading Update...',
  RestartToUpdate = 'Restart to Update',
}

export interface UpdaterState {
  type: UpdateState;
  update?: {
    version: string;
  };
}

export interface HostUpdateStatus {
  currentVersion: string;
  latestVersion: string;
  updateAvailable: boolean;
}

export interface UpdaterSettingsService {
  getSetting(key: SettingKey): Promise<boolean>;
  onSettingChanged(
    key: SettingKey,
    listener: (value: boolean) => void,
  ): SettingSubscription | void;
}

/**
 * Typed event map for `autoUpdater` (`AppUpdater` extends `EventEmitter` at
 * runtime, though its `tiny-typed-emitter` base lacks `.d.ts` declarations).
 */
interface AutoUpdaterEventMap {
  'checking-for-update': () => void;
  'update-available': (info: UpdateInfo) => void;
  'update-not-available': (info: UpdateInfo) => void;
  'download-progress': (info: ProgressInfo) => void;
  'update-downloaded': (info: UpdateDownloadedEvent) => void;
  error: (error: Error, message?: string) => void;
}

interface AutoUpdaterEmitter extends AppUpdater {
  on<K extends keyof AutoUpdaterEventMap>(
    event: K,
    listener: AutoUpdaterEventMap[K],
  ): this;
}

export const updateActions: Record<MenuUpdateStep, (() => void) | undefined> = {
  [MenuUpdateStep.CheckForUpdates]: () => checkForUpdates(true),
  [MenuUpdateStep.CheckingForUpdates]: undefined,
  [MenuUpdateStep.DownloadingUpdate]: undefined,
  [MenuUpdateStep.RestartToUpdate]: () => quitAndInstall(),
};

// True if the last call to check for updates was from a user click in the menu.
let isManualCheck = false;

// How long to wait after app start before first update check (ms)
const INITIAL_CHECK_DELAY_MS = 10000; // 10 seconds

// How often to re-check for updates after the initial check (ms)
const CHECK_INTERVAL_MS = 60 * 60 * 1000; // 1 hour

/**
 * States in which a newer version is known to exist. Anything else (idle,
 * checking for updates) means we have nothing newer to offer.
 */
const UPDATE_PENDING_STATES: Set<UpdateState> = new Set([
  UpdateState.AvailableForDownload,
  UpdateState.Downloading,
  UpdateState.Ready,
]);

let updaterInitialized = false;
let periodicCheckInterval: ReturnType<typeof setInterval> | undefined;

function startAutoUpdateChecks(): void {
  if (periodicCheckInterval) {
    return;
  }
  console.log('[AutoUpdater] Starting auto update checks');
  checkForUpdates();
  periodicCheckInterval = setInterval(checkForUpdates, CHECK_INTERVAL_MS);
}

function stopAutoUpdateChecks(): void {
  console.log('[AutoUpdater] Stopping auto update checks');
  if (periodicCheckInterval) {
    clearInterval(periodicCheckInterval);
    periodicCheckInterval = undefined;
  }
}

export function setAutoUpdateChecking(enabled: boolean): void {
  if (!updaterInitialized) {
    return;
  }
  if (enabled) {
    startAutoUpdateChecks();
  } else {
    stopAutoUpdateChecks();
  }
}

// The last update state broadcast to renderers.
let lastState: UpdaterState = { type: UpdateState.Idle };

/** Broadcast a state change to every open Hub window. */
export function broadcastState(state: UpdaterState): void {
  lastState = state;
  for (const win of getHubWindows()) {
    const hubContents = getHubWebContents(win);
    if (hubContents && !hubContents.isDestroyed?.()) {
      hubContents.send('updater:state-changed', state);
    }
  }
}

/**
 * Returns the last update state broadcast to renderers.
 */
export function getLastState(): UpdaterState {
  return lastState;
}

/**
 * Updates the state of the menu item based on the current step of the updater.
 */
function updateMenuState(step: MenuUpdateStep): void {
  const menu = Menu.getApplicationMenu();
  if (menu) {
    const item = menu.getMenuItemById('check-for-updates');
    if (item) {
      item.label = step;
      item.enabled = updateActions[step] !== undefined;
    }
  }
}

/**
 * Initializes the auto-updater and registers IPC handlers.
 * Call once after the first window is created.
 *
 * The updater will:
 * 1. Wait INITIAL_CHECK_DELAY_MS ms, then check for updates.
 * 2. Re-check every CHECK_INTERVAL_MS ms.
 * 3. Download updates automatically in the background.
 * 4. Broadcast state to the renderer so AppUpdateButton can display progress.
 */
export function initAutoUpdater(
  isHeadless: boolean,
  settingsService: UpdaterSettingsService,
): void {
  // In dev mode (npm start), electron-updater skips checks because the app
  // isn't packaged. Force it to use the dev config file instead.
  if (!app.isPackaged) {
    autoUpdater.forceDevUpdateConfig = true;
    autoUpdater.updateConfigPath = path.join(
      app.getAppPath(),
      'dev-app-update.yml',
    );
  }

  // Set the channel based on architecture and OS.
  // On Windows, we need to explicitly append '-win' to match the artifact name.
  // On macOS and linux, Electron automatically appends the OS to the channel name.
  if (process.platform === 'win32') {
    autoUpdater.channel = `latest-${process.arch}-win`;
  } else {
    autoUpdater.channel = `latest-${process.arch}`;
  }

  autoUpdater.requestHeaders = { 'x-app-version': app.getVersion() };
  autoUpdater.autoDownload = true;
  autoUpdater.autoInstallOnAppQuit = app.isPackaged;

  // Auto-updater event handlers → broadcast to renderer
  const updater = autoUpdater as AutoUpdaterEmitter;
  updater.on('checking-for-update', () => {
    console.log('[AutoUpdater] Checking for update…');
    broadcastState({ type: UpdateState.CheckingForUpdates });
    updateMenuState(MenuUpdateStep.CheckingForUpdates);
  });

  updater.on('update-available', (info: UpdateInfo) => {
    console.log(`[AutoUpdater] Update available: ${info.version}`);
    broadcastState({
      type: UpdateState.AvailableForDownload,
      update: { version: info.version },
    });
    updateMenuState(MenuUpdateStep.DownloadingUpdate);
    isManualCheck = false;
  });

  updater.on('update-not-available', (info: UpdateInfo) => {
    console.log(`[AutoUpdater] Up to date (${info.version})`);
    broadcastState({ type: UpdateState.Idle });
    updateMenuState(MenuUpdateStep.CheckForUpdates);

    if (isManualCheck && !isHeadless) {
      const win = getFocusedHubWindow() ?? getHubWindows()[0];
      const options: MessageBoxOptions = {
        type: 'info',
        title: 'Check for Updates',
        message: 'No updates available',
        buttons: ['OK'],
      };
      if (win) {
        void dialog.showMessageBox(win, options);
      } else {
        void dialog.showMessageBox(options);
      }
    }
    isManualCheck = false;
  });

  updater.on('download-progress', () => {
    broadcastState({ type: UpdateState.Downloading });
    updateMenuState(MenuUpdateStep.DownloadingUpdate);
  });

  updater.on('update-downloaded', (info: UpdateDownloadedEvent) => {
    console.log(`[AutoUpdater] Update downloaded: ${info.version}`);
    if (isHeadless) {
      // Proceed to auto install in headless mode
      if (app.isPackaged) {
        if (process.platform === 'linux') {
          const downloadedFilePath: string | undefined = info.downloadedFile;
          headlessQuitAndInstall(downloadedFilePath);
        } else {
          autoUpdater.quitAndInstall();
        }
      } else {
        console.log(
          '[AutoUpdater] Headless mode: Skipping quitAndInstall (not packaged).',
        );
      }
      return;
    }

    broadcastState({
      type: UpdateState.Ready,
      update: { version: info.version },
    });
    updateMenuState(MenuUpdateStep.RestartToUpdate);
  });

  updater.on('error', (err: Error) => {
    console.error('[AutoUpdater] Error:', err.message);
    broadcastState({ type: UpdateState.Idle });
    updateMenuState(MenuUpdateStep.CheckForUpdates);
    isManualCheck = false;
  });

  // Schedule periodic checks if needed
  updaterInitialized = true;
  setTimeout(async () => {
    const autoCheckEnabled = await settingsService.getSetting(
      SettingKey.AUTO_CHECK_FOR_UPDATES,
    );
    setAutoUpdateChecking(autoCheckEnabled);
  }, INITIAL_CHECK_DELAY_MS);

  // Cancel or restart periodic checks on setting change
  settingsService.onSettingChanged(
    SettingKey.AUTO_CHECK_FOR_UPDATES,
    (enabled: boolean) => {
      setAutoUpdateChecking(enabled);
    },
  );
}

export function checkForUpdates(isManual = false): void {
  isManualCheck = isManual;
  autoUpdater.checkForUpdates().catch((err: Error) => {
    console.error('[AutoUpdater] Failed to check for updates:', err.message);
  });
}

export function quitAndInstall(): void {
  autoUpdater.quitAndInstall();
}

/**
 * Builds the update status served to the language server over the loopback
 * host bridge server.
 *
 * This reads the cached state only — it never triggers a network check, so
 * polling it is cheap.
 *
 * Before the first check completes the cached state is 'idle' and no version
 * is known. In that case we report `latestVersion` as the running version
 * (rather than an empty string) so consumers can compare the two fields
 * without special-casing, together with `updateAvailable: false`.
 */
export function getHostUpdateStatus(): HostUpdateStatus {
  const currentVersion = app.getVersion();
  const state = getLastState();
  const knownVersion = state.update?.version;
  const updateAvailable =
    UPDATE_PENDING_STATES.has(state.type) && knownVersion !== undefined;
  return {
    currentVersion,
    latestVersion: updateAvailable ? knownVersion! : currentVersion,
    updateAvailable,
  };
}

/**
 * Applies an update on behalf of the language server, mirroring what the
 * renderer bridge used to do.
 *
 * Returns true when the request moved an update forward: restarting into a
 * downloaded update, letting an in-flight download finish, or kicking off a
 * check (autoDownload is enabled, so a found update downloads by itself).
 * Returns false only when there is genuinely nothing we can do.
 */
export function applyHostUpdate(): boolean {
  const state = getLastState();
  switch (state.type) {
    case UpdateState.Ready:
      // quitAndInstall is a no-op (and logs a warning) in an unpackaged dev
      // build, so don't claim we accepted it.
      if (!app.isPackaged) {
        console.log(
          '[AutoUpdater] Skipping quitAndInstall (requires a packaged app).',
        );
        return false;
      }
      quitAndInstall();
      return true;
    case UpdateState.AvailableForDownload:
    case UpdateState.Downloading:
      // Already downloading; it installs once the download completes.
      console.log('[AutoUpdater] Update already in progress.');
      return true;
    case UpdateState.CheckingForUpdates:
      // A check is in flight — starting another would race it.
      return true;
    default:
      // Nothing known yet. Kick off a check so an update can proceed.
      checkForUpdates();
      return true;
  }
}

/**
 * Electron native quitAndInstall doesn't relaunch the app with command line arguments.
 * This function waits for the app process to quit, manually replaces the executable with
 * the downloaded update, and then relaunches it with the right headless flags.
 */
function headlessQuitAndInstall(downloadedFilePath?: string): void {
  console.log('[AutoUpdater] Headless mode: Scheduling post-quit restart.');
  try {
    const currentPid = process.pid;
    const appPath = process.env.APPIMAGE || process.execPath;
    const args = [
      '--ozone-platform=headless',
      '--headless',
      '--disable-gpu',
      '--no-sandbox',
    ];
    let script = '';
    if (downloadedFilePath) {
      console.log(
        `[AutoUpdater] Will manually replace ${appPath} with ${downloadedFilePath}`,
      );
      script = `
        while kill -0 ${currentPid} 2>/dev/null; do sleep 0.5; done
        cp -f "${downloadedFilePath}" "${appPath}"
        chmod +x "${appPath}"
        "${appPath}" ${args.join(' ')}
      `;
    } else {
      console.warn(
        '[AutoUpdater] No downloaded file path found, relaunching without update.',
      );
      script = `
        while kill -0 ${currentPid} 2>/dev/null; do sleep 0.5; done
        sleep 3
        "${appPath}" ${args.join(' ')}
      `;
    }
    const child = spawn('sh', ['-c', script], {
      detached: true,
      stdio: 'ignore',
      env: { ...process.env, ELECTRON_OZONE_PLATFORM_HINT: 'headless' },
    });
    child.unref();
  } catch (e: unknown) {
    console.error('[AutoUpdater] Failed to schedule restart:', e);
  }
  app.quit();
}
