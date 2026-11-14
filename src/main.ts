import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  Menu,
  session,
  type MessageBoxOptions,
} from 'electron';
import log from 'electron-log/main';
import { registerIpcHandlers } from './ipcHandlers';
import { registerCableWebAuthnIpcHandlers } from './cablev2';
import { wcvBrowserRegistry } from './wcvBrowserRegistry';
import * as fs from 'fs';
import * as crypto from 'crypto';
import * as readline from 'readline';
import {
  createWindow,
  getFocusedHubWindow,
  getHubWebContents,
  getHubWindows,
  getLocalHubWindows,
  setShowQuitConfirmation,
  showOrCreateWindow,
  showQuitConfirmation,
} from './utils';
import {
  getLsCL,
  getLsPort,
  getLsProcess,
  killLanguageServer,
  LS_BINARY,
  prefetchShellEnv,
  setupLocalCertTrust,
  startAndMonitorLanguageServer,
  type LanguageServerHandle,
  type WslLanguageServerOptions,
} from './languageServer';
import {
  applyHostUpdate,
  getHostUpdateStatus,
  initAutoUpdater,
} from './updater';
import { startHostBridgeServer, type HostBridgeServer } from './hostBridgeServer';
import { DYNAMIC_PORT, WINDOW_ORIGIN } from './constants';
import { createTray, insertTrayMenuItem } from './tray';
import { StorageManager } from './storage';
import { configureSessionDeviceChooser } from './deviceChooser';
import {
  getActivePortFilePath,
  getAppStoragePath,
  getLsLogPath,
  tightenActivePortPermissions,
} from './paths';
import {
  setupApplicationMenu,
  wslConnectMenuTemplate,
  wslReopenLocallyTemplate,
} from './menu';
import {
  registerCustomSchemeHandlers,
  registerCustomSchemes,
} from './customScheme';
import {
  DEFAULTS,
  SettingKey,
  SettingsService,
} from './services/settingsService';
import { maybeShowIdeInstallWizard } from './ideInstall';
import { createProvisionSplash, type ProvisionSplash } from './provisionSplash';
import * as path from 'path';
import {
  ensureServerInstalled,
  listWslDistros,
  persistWslDistro,
  readPersistedWslDistro,
  setActiveWslDistro,
  WSL_STATE_FILE,
} from './wsl';
import {
  configureWebviewSessionProxy,
  preconfigureWebviewPartitionProxies,
} from './beyondcorpPac';
import { isScriptJailSession } from './scriptJail';
import {
  attachWebviewWebAuthnUserActivationTracking,
  configurePlatformWebAuthn,
  configureSessionWebAuthn,
} from './webauthn';
import { configureSessionPromptPreload } from './guestPromptController';
import { configureWebviewBrowserContents } from './webviewBrowser';
import { flushTrackedWindowState } from './windowState';
import { closeAllBrowserSessions } from './browserEngine/browserEngine';
import {
  isRemoteContents,
  isRemoteSession,
} from './remote/remoteWindowSecurity';
import { HostStores, mainStoreScope } from './remote/hostStores';
import { RemoteManager } from './remote/remoteManager';
import { registerSshBridgeIpc } from './remote/sshBridge';

const gotTheLock = app.requestSingleInstanceLock();
if (!gotTheLock) {
  app.quit();
  process.exit(0);
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------
let storageManager!: StorageManager;
let settingsService!: SettingsService;
let hasStartedMainApplication = false;
let isQuitting = false;
// Loopback HTTP server the language server uses to read update status and
// request an update. Kept at module scope so it is not garbage collected.
let hostBridgeServer: HostBridgeServer | undefined;
let remoteManager: RemoteManager | undefined;

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------
// Driven by ELECTRON_OZONE_PLATFORM_HINT=headless env var.
// This single env var both prevents GTK from crashing (Electron 33+)
// and tells our code to skip createWindow().
const HEADLESS = process.env.ELECTRON_OZONE_PLATFORM_HINT === 'headless';
// When set, skip LS startup and load this URL directly (for dev iteration).
const DEV_URL = process.env.DEV_URL;
const IS_INSIDERS = app.getName().toLowerCase().includes('insiders');

// WSL distro to provision and launch the LS in (Windows only, '' = local).
// Precedence: --wsl-distro switch (empty value forces local) >
// ANTIGRAVITY_WSL_DISTRO env var > distro persisted from the last session.
function resolveStartupWslDistro(): string {
  if (process.platform !== 'win32') {
    return '';
  }
  if (app.commandLine.hasSwitch('wsl-distro')) {
    return app.commandLine.getSwitchValue('wsl-distro');
  }
  return (
    process.env.ANTIGRAVITY_WSL_DISTRO ||
    readPersistedWslDistro(path.join(app.getPath('userData'), WSL_STATE_FILE))
  );
}

let WSL_DISTRO: string = resolveStartupWslDistro();

if (!DEV_URL) {
  prefetchShellEnv();
}

/** True if `distro` is currently registered in WSL (false on wsl.exe errors). */
async function wslDistroExists(distro: string): Promise<boolean> {
  try {
    return (await listWslDistros()).some((d) => d.name === distro);
  } catch (err: unknown) {
    console.error('[WSL] Failed to list distros:', err);
    return false;
  }
}

function getErrorMessage(err: unknown): string {
  if (err instanceof Error) {
    return err.message;
  }
  if (typeof err === 'object' && err !== null && 'message' in err) {
    return String((err as { message: unknown }).message);
  }
  return String(err);
}

if (HEADLESS) {
  app.commandLine.appendSwitch('ozone-platform', 'headless');
  app.commandLine.appendSwitch('headless');
  app.commandLine.appendSwitch('disable-gpu');
  app.commandLine.appendSwitch('no-sandbox');
}
if (!app.commandLine.hasSwitch('remote-debugging-port')) {
  app.commandLine.appendSwitch('remote-debugging-port', '0');
}
app.commandLine.appendSwitch('disable-blink-features', 'AutomationControlled');
app.commandLine.appendSwitch('disable-features', 'FedCm');
// Hidden built-in browser tabs stay attached under an opaque curtain view
// (see WcvBrowserRegistry). Without these switches Chromium treats them as
// occluded, stops producing frames, and capturePage() returns nothing. They are
// unconditional because the renderer may switch to the WebContentsView browser
// at any time, after these startup-only switches can no longer be changed.
// `disable-features` keeps only its last value, so merge with any earlier one.
const disabledFeatures = app.commandLine.getSwitchValue('disable-features');
app.commandLine.appendSwitch(
  'disable-features',
  [disabledFeatures, 'CalculateNativeWinOcclusion'].filter(Boolean).join(','),
);
app.commandLine.appendSwitch('disable-backgrounding-occluded-windows');

// ---------------------------------------------------------------------------
// Application Lifecycle
// ---------------------------------------------------------------------------
let pendingDeepLink: string | null = null;

function handleDeepLink(url: string): void {
  // Deep links can carry auth codes: never to an SSH window.
  const wins = getLocalHubWindows();
  // This block handles deep links when windows are already open.
  if (wins.length > 0) {
    if (wins[0].isMinimized()) {
      wins[0].restore();
    }
    wins[0].show();
    wins[0].focus();
    app.focus({ steal: true });
    const hubContents = getHubWebContents(wins[0]);
    if (
      !hubContents ||
      hubContents.isDestroyed?.() ||
      hubContents.isLoadingMainFrame?.()
    ) {
      // The window is still loading (e.g. it was just created by
      // `second-instance`), so the renderer has not subscribed to 'deep-link'
      // yet and the IPC message would be dropped. Store the link instead; the
      // renderer reads it via 'deep-link:get-stored' once it mounts.
      pendingDeepLink = url;
    } else {
      hubContents.send('deep-link', url);
    }
  } else {
    pendingDeepLink = url;
    // No local window (maybe only SSH windows): open one to take the link.
    if (!HEADLESS && hasStartedMainApplication) {
      createWindow(
        DEV_URL ?? `${WINDOW_ORIGIN}:${getLsPort()}/`,
        storageManager,
      );
      app.focus({ steal: true });
    }
  }
}

const PROTOCOL = app
  .getName()
  .toLowerCase()
  .replace(/[^a-z0-9]+/g, '-')
  .replace(/^-|-$/g, '');

// When running unpackaged (`npm start` / `electron .`), process.execPath is
// the stock Electron binary, so the app path must be registered too or the OS
// would launch bare Electron for deep links. Only Windows uses these args.
const protocolClientArgs: [string?, string[]?] =
  process.defaultApp && process.argv.length >= 2
    ? [process.execPath, [path.resolve(process.argv[1])]]
    : [];
if (!app.isDefaultProtocolClient(PROTOCOL, ...protocolClientArgs)) {
  app.setAsDefaultProtocolClient(PROTOCOL, ...protocolClientArgs);
}

app.on('second-instance', (_event, commandLine) => {
  if (!HEADLESS && hasStartedMainApplication) {
    showOrCreateWindow(getLsPort());
    app.focus({ steal: true });
  }
  const url = commandLine.find((arg) => arg.startsWith(`${PROTOCOL}://`));
  if (url) {
    handleDeepLink(url);
  } else if (hasStartedMainApplication) {
    // Not before: the IDE install wizard may be open.
    openSshHostFromArgs(commandLine);
  }
});

const SSH_HOST_SWITCH = '--ssh-host=';

function sshHostFromArgs(argv: readonly string[]): string | undefined {
  const arg = argv.find((a) => a.startsWith(SSH_HOST_SWITCH));
  // '||': an empty value means unset.
  return arg?.slice(SSH_HOST_SWITCH.length).trim() || undefined;
}

/** Opens `--ssh-host`'s window; a failure shows on the local window. */
function openSshHostFromArgs(argv: readonly string[]): void {
  // Deep links take precedence over --ssh-host (both on cold start and
  // second-instance) so protocol launches cannot carry an SSH destination.
  if (argv.some((a) => a.startsWith(`${PROTOCOL}://`))) {
    return;
  }
  const host = sshHostFromArgs(argv);
  if (!host || isQuitting || !remoteManager) {
    return;
  }
  if (HEADLESS) {
    console.log('[SSH] Ignoring --ssh-host: this Hub is headless.');
    return;
  }
  // Opened at startup or by the second launch.
  const [parent] = getLocalHubWindows();
  if (parent) {
    void remoteManager.openFromCommandLine(host, parent);
  }
}

registerCustomSchemes();

app.on('open-url', (event, url) => {
  event.preventDefault();
  handleDeepLink(url);
});

app.on('session-created', (targetSession) => {
  if (isScriptJailSession(targetSession)) {
    return;
  }
  configureSessionWebAuthn(targetSession);
  configureSessionPromptPreload(targetSession);
});

app.on('web-contents-created', (_event, contents) => {
  // Not SSH sessions: the setup below would undo their hardening (e.g. re-add
  // the session preloads it removed).
  if (
    isScriptJailSession(contents.session) ||
    isRemoteSession(contents.session)
  ) {
    return;
  }
  configureSessionDeviceChooser(contents.session);
  configureSessionWebAuthn(contents.session);
  configureSessionPromptPreload(contents.session);
  if (contents.getType() !== 'webview') {
    return;
  }
  attachWebviewWebAuthnUserActivationTracking(contents);
  configureWebviewBrowserContents(contents);
  void configureWebviewSessionProxy(contents.session);
});

/**
 * App entry point. Runs once Electron has finished initializing.
 * Validates the LS binary, frees the port if needed, spawns the LS,
 * and opens the initial browser window.
 */
app
  .whenReady()
  .then(async () => {
    // `electron-log` v5.4.3 runtime reads `includeFutureSession` while its
    // `.d.ts` declares `includeFutureSessions`; pass both safely.
    type LogInitOptions = NonNullable<Parameters<typeof log.initialize>[0]> & {
      includeFutureSession?: boolean;
    };
    // Initialize electron-log on the default window session only so browser
    // <webview> partitions do not inherit the electron-log-preload script.
    log.initialize({
      includeFutureSession: false,
      includeFutureSessions: false,
    } as LogInitOptions);
    Object.assign(console, log.functions);
    tightenActivePortPermissions(
      app.getPath('userData'),
      getActivePortFilePath(),
    );
    const storagePath = getAppStoragePath();
    // SSH windows hear only this store's app settings (hosts have their own).
    storageManager = new StorageManager(storagePath, DEFAULTS, mainStoreScope);
    settingsService = new SettingsService(storageManager);
    // Handle deep link URL from command line arguments (All platforms)
    const deepLinkFromArg = process.argv.find((arg) =>
      arg.startsWith(`${PROTOCOL}://`),
    );
    if (deepLinkFromArg) {
      console.log('Launched with deep link:', deepLinkFromArg);
      pendingDeepLink = deepLinkFromArg;
    }
    remoteManager = new RemoteManager({
      mainStorage: storageManager,
      appVersion: app.getVersion(),
      isInsiders: IS_INSIDERS,
      openLocalWindow: () => {
        createWindow(
          DEV_URL ?? `${WINDOW_ORIGIN}:${getLsPort()}/`,
          storageManager,
          { cascade: true },
        );
      },
    });
    registerSshBridgeIpc(remoteManager);
    // Register IPC handlers. SSH windows get their host's store.
    const hostStores = new HostStores(app.getPath('userData'), storageManager);
    registerIpcHandlers(storageManager, {
      resolveStorage: (sender) => hostStores.forSender(sender),
      sidecarRoutesFor: (sender) => remoteManager?.sidecarRoutesFor(sender),
    });
    ipcMain.handle('deep-link:get-stored', (event) => {
      if (isRemoteContents(event?.sender)) {
        return null; // Deep links are for local windows only.
      }
      const link = pendingDeepLink;
      pendingDeepLink = null; // Clear after read
      return link;
    });
    // Handle requests coming from custom schemes
    registerCustomSchemeHandlers();
    // Set About panel options with LS CL. Not awaited: `--stamp` runs the LS
    // binary, which would otherwise delay the real LS spawn.
    app.setAboutPanelOptions({ applicationVersion: app.getVersion() });
    void getLsCL().then((cl) => {
      if (cl) {
        app.setAboutPanelOptions({
          applicationVersion: app.getVersion(),
          version: cl,
        });
      }
    });
    // Pre-onboarding: check if we should offer to re-install the IDE.
    // This runs before the LS starts so we can show a standalone wizard.
    if (!HEADLESS) {
      await maybeShowIdeInstallWizard(storageManager);
    }
    // Pre-configure the BeyondCorp PAC proxy on known <webview> partitions
    // before any BrowserWindow or <webview> is created so the first shortlink
    // navigation never races an in-flight setProxy call.
    await preconfigureWebviewPartitionProxies();
    // Configure macOS caBLEv2 WebAuthn IPC handlers and attach the WebAuthn
    // account chooser to the default session before any BrowserWindow or
    // <webview> is created.
    configurePlatformWebAuthn({
      registerCableIpc: () =>
        registerCableWebAuthnIpcHandlers({
          resolveWcvTab: (id) =>
            wcvBrowserRegistry.resolveDeviceChooserHost(id),
        }),
    });
    configureSessionWebAuthn(session.defaultSession);
    if (DEV_URL) {
      console.log('Starting in dev mode with URL:', DEV_URL);
      createWindow(DEV_URL, storageManager);
      hasStartedMainApplication = true;
      openSshHostFromArgs(process.argv);
      return;
    }
    if (!WSL_DISTRO && !fs.existsSync(LS_BINARY)) {
      const msg = `language_server binary not found at:\n${LS_BINARY}\n\nPlease build set a valid location.`;
      if (HEADLESS) {
        console.error('ERROR:', msg);
      } else {
        await dialog.showErrorBox('Binary not found', msg);
      }
      app.quit();
      return;
    }
    // In WSL mode, make sure the matching server version is installed in the
    // distro before launching.
    let wslOptions: WslLanguageServerOptions | undefined;
    if (WSL_DISTRO && !(await wslDistroExists(WSL_DISTRO))) {
      // The distro was removed since the last session (e.g. `wsl
      // --unregister`). Open locally instead of failing provisioning.
      console.warn(
        `[WSL] Distro "${WSL_DISTRO}" is not installed; opening locally instead.`,
      );
      persistWslDistro(path.join(app.getPath('userData'), WSL_STATE_FILE), '');
      if (!HEADLESS) {
        void dialog.showMessageBox({
          type: 'warning',
          title: 'WSL distro not found',
          message: `The WSL distro "${WSL_DISTRO}" is no longer installed.`,
          detail: 'Antigravity opened on Windows instead.',
        });
      }
      WSL_DISTRO = '';
    }
    if (WSL_DISTRO) {
      setActiveWslDistro(WSL_DISTRO);
      // Created lazily on the first status update, so it only appears when
      // provisioning actually has work to do (first launch per version).
      let splash: ProvisionSplash | undefined;
      try {
        const binaryPath = await ensureServerInstalled(
          WSL_DISTRO,
          app.getVersion(),
          IS_INSIDERS,
          (status) => {
            if (!HEADLESS) {
              splash ??= createProvisionSplash(WSL_DISTRO);
              splash.setStatus(status);
            }
          },
        );
        wslOptions = { distro: WSL_DISTRO, binaryPath };
      } catch (err: unknown) {
        const msg = `Failed to install the server into WSL distro "${WSL_DISTRO}":\n${getErrorMessage(err)}`;
        persistWslDistro(
          path.join(app.getPath('userData'), WSL_STATE_FILE),
          '',
        );
        if (HEADLESS) {
          console.error('ERROR:', msg);
        } else {
          await dialog.showErrorBox('WSL setup failed', msg);
        }
        app.quit();
        return;
      } finally {
        splash?.close();
      }
    }
    const csrf = crypto.randomUUID();
    console.log(`Starting app (v${app.getVersion()}) with dynamic port…`);
    // Start the host bridge server before the LS so we can hand it the URL
    // and token. This lives in the main process (not the renderer) so it stays
    // up in headless mode and after every window has been closed. It uses its
    // own token — the renderer's CSRF token must not unlock ApplyUpdate.
    try {
      hostBridgeServer = await startHostBridgeServer({
        getUpdateStatus: getHostUpdateStatus,
        applyUpdate: applyHostUpdate,
      });
      console.log(`Host bridge server listening on ${hostBridgeServer.url}`);
    } catch (err: unknown) {
      // Non-fatal: the app is still usable, the LS just can't drive updates.
      console.error('Failed to start host bridge server:', getErrorMessage(err));
    }
    let handle: LanguageServerHandle;
    const targetPort = Number(process.env.JETSKI_LS_PORT) || DYNAMIC_PORT;
    try {
      handle = await startAndMonitorLanguageServer(targetPort, csrf, {
        headless: HEADLESS,
        wsl: wslOptions,
        hostBridgeUrl: hostBridgeServer?.url,
        hostBridgeToken: hostBridgeServer?.token,
        onPortChanged: (newPort) => {
          const newUrl = `${WINDOW_ORIGIN}:${newPort}/`;
          console.log(
            `[Auto-Restart] Port changed! Reloading all windows with URL: ${newUrl}`,
          );
          // Apply cert trust
          setupLocalCertTrust();
          if (!HEADLESS) {
            const windows = getLocalHubWindows();
            for (const win of windows) {
              void win.loadURL(newUrl);
            }
          }
        },
      });
    } catch (err: unknown) {
      const msg = getErrorMessage(err);
      if (HEADLESS) {
        console.error('Startup failed:', msg);
      } else {
        await dialog.showErrorBox('Startup failed', msg);
      }
      app.quit();
      return;
    }
    const url = `${WINDOW_ORIGIN}:${handle.port}/`;
    console.log('\n' + '='.repeat(60));
    console.log(`  Local:       ${url}`);
    console.log(`  LS Logs:     ${getLsLogPath()}`);
    console.log(`  Electron Logs: ${log.transports.file.getFile().path}`);
    console.log('='.repeat(60) + '\n');
    if (HEADLESS) {
      // In headless mode, forward stdin to the Language Server to allow interaction via terminal.
      const rl = readline.createInterface({
        input: process.stdin,
        output: process.stdout,
      });
      rl.on('line', (line) => {
        const lsProc = getLsProcess();
        if (lsProc && lsProc.stdin) {
          lsProc.stdin.write(line + '\n');
          console.log('-> Forwarded input to Language Server.');
        } else {
          console.log('Language Server process is not running.');
        }
      });
    }
    // Initial window — opened once after the LS has successfully started.
    if (!HEADLESS) {
      setupApplicationMenu(url);
      createWindow(url, storageManager);
      if (app.dock) {
        const dockMenu = Menu.buildFromTemplate([
          {
            label: 'New Window',
            click: () => createWindow(url, storageManager, { cascade: true }),
          },
        ]);
        app.dock.setMenu(dockMenu);
      }
      createTray(
        [
          {
            id: 'running-agents',
            label: 'No agents running',
            enabled: false,
          },
          { type: 'separator' },
          {
            label: `Open ${app.getName()}`,
            click: () => showOrCreateWindow(getLsPort()),
          },
          {
            label: 'Quit',
            click: () => {
              // Triggers 'before-quit' to run graceful cleanup without confirmation.
              app.quit();
            },
          },
        ],
        () => {
          showOrCreateWindow(getLsPort());
          app.focus({ steal: true });
        },
      );
      // The app menu bar isn't reachable on Windows (hidden title bar), so
      // the tray menu also gets the "Connect to WSL" entry.
      void wslConnectMenuTemplate().then((wslItem) => {
        if (wslItem) {
          insertTrayMenuItem(2, wslItem);
          const reopen = wslReopenLocallyTemplate();
          if (reopen) {
            insertTrayMenuItem(3, reopen);
          }
        }
      });
    }
    openSshHostFromArgs(process.argv);
    // Start checking for app updates.
    initAutoUpdater(HEADLESS, settingsService);
    hasStartedMainApplication = true;
  })
  .catch(() => {
    hasStartedMainApplication = true;
  });

/**
 * Fired when all windows have been closed.
 * On macOS the app (and LS) stay alive so the user can re-open via the tray.
 * On all other platforms, shut down the LS and quit.
 */
app.on('window-all-closed', async () => {
  if (isQuitting) {
    return;
  }
  if (!hasStartedMainApplication) {
    return;
  }
  const runInBackground = await settingsService.getSetting(
    SettingKey.RUN_IN_BACKGROUND,
  );
  if (!runInBackground) {
    // Triggers 'before-quit' to run graceful cleanup without confirmation.
    app.quit();
  }
});

/** Shuts down the host bridge server, if it was started. */
async function closeHostBridgeServer(): Promise<void> {
  if (!hostBridgeServer) {
    return;
  }
  try {
    await hostBridgeServer.close();
  } catch (err: unknown) {
    console.error('Failed to close host bridge server:', err);
  }
  hostBridgeServer = undefined;
}

/**
 * Fired just before the app quits (e.g. Cmd+Q on macOS, or after
 * window-all-closed on non-macOS). Ensures the LS is terminated even if
 * window-all-closed didn't handle it (e.g. on macOS quit via menu).
 */
app.on('before-quit', async (event) => {
  if (isQuitting) {
    return;
  }
  if (!showQuitConfirmation) {
    event.preventDefault();
    isQuitting = true;
    // Persist the focused/open Hub window bounds and full-screen/maximized state
    // before `win.destroy()`, which bypasses the normal `'close'` event.
    flushTrackedWindowState(getFocusedHubWindow());
    // Before destroying the windows (see disposeAll).
    const sshClosed =
      remoteManager?.disposeAll().catch((err: unknown) => {
        console.error('Failed to close SSH connections:', err);
      }) ?? Promise.resolve();
    // Destroy all windows to terminate renderers and release keep-alive sockets
    const windows = [
      ...new Set([...getHubWindows(), ...BrowserWindow.getAllWindows()]),
    ];
    for (const win of windows) {
      win.destroy();
    }
    // Close all active connections and kill the language server in parallel
    await Promise.all([
      session.defaultSession.closeAllConnections().catch((err: unknown) => {
        console.error('Failed to close session connections:', err);
      }),
      killLanguageServer(),
      closeAllBrowserSessions().catch((err: unknown) => {
        console.error('Failed to close browser sessions:', err);
      }),
      sshClosed,
    ]);
    // The host bridge server comes down last, in reverse order of startup: the
    // language server is its only client, so closing it first would just drop
    // in-flight requests and log errors on the way out.
    await closeHostBridgeServer();
    app.quit();
    return;
  }
  // Show a confirmation dialog before quitting
  event.preventDefault();
  const hubWindows = getHubWindows();
  const focusedWindow =
    getFocusedHubWindow() ?? BrowserWindow.getFocusedWindow();
  const win =
    (focusedWindow && hubWindows.includes(focusedWindow)
      ? focusedWindow
      : undefined) || hubWindows[0];
  const options: MessageBoxOptions = {
    type: 'question',
    buttons: ['Cancel', 'Quit'],
    defaultId: 1,
    cancelId: 0,
    title: 'Confirm Quit',
    message: 'Are you sure you want to quit?',
    detail: 'There may be agents or background tasks running.',
  };
  setShowQuitConfirmation(false);
  if (win) {
    void dialog.showMessageBox(win, options).then((result) => {
      if (result.response === 1) {
        // Quit - this will retrigger 'before-quit'
        app.quit();
      }
    });
  }
});

/**
 * Fired when the app is re-activated (e.g. clicking the dock icon on macOS).
 * Re-opens a window if none are currently open.
 */
app.on('activate', () => {
  // On Mac, re-open a window when the user clicks the dock
  // icon and no windows are open.
  if (!HEADLESS && getHubWindows().length === 0) {
    const url = DEV_URL ?? `${WINDOW_ORIGIN}:${getLsPort()}/`;
    createWindow(url, storageManager);
  }
});
