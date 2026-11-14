/** One SshConnection and one window per SSH host. */
import {
  app,
  type BaseWindow,
  BrowserWindow,
  dialog,
  type MessageBoxOptions,
  net,
  session,
  type WebContents,
} from 'electron';
import log from 'electron-log/main';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { openExternalSafely } from '../externalUrl';
import { electronNodeBinary } from '../nodeWrapper';
import { getAppDataDirName } from '../paths';
import {
  createWindow,
  getLastFocusedLocalHubWindow,
  isLightTheme,
  resolveHubWindowFromWebContents,
  type WindowStorageManager,
} from '../utils';
import {
  ConnectionInputs,
  type ConnectionInputsApp,
} from './connectionInputs';
import {
  type DisconnectedPage,
  showDisconnectedPage,
} from './disconnectedPage';
import {
  installRemoteSidecarProtocol,
  type RemoteSidecarRoutes,
} from './remoteSidecars';
import {
  hardenRemoteSession,
  hardenRemoteWindow,
  isRemoteContents,
  remoteNavigationPolicy,
} from './remoteWindowSecurity';
import {
  SSH_LS_REMOTE_PATH_ENV,
  SSH_LS_VERSION_ENV,
} from './serverArtifacts';
import {
  type AskpassAnswer,
  type AskpassPrompt,
  canPrompt,
} from './sshAskpass';
import {
  SSH_INTENT_CHANNEL,
  type SshBridgeBackend,
  type SshChooseConfigFileResult,
  type SshConnectBackendOptions,
  type SshConnectResult,
  type SshIntent,
  type SshListHostsOptions,
  type SshListHostsResult,
  type SshState,
} from './sshBridge';
import {
  canonicalSshConfigFile,
  listSshConfigHosts,
  resolveSshConfigFile,
} from './sshConfigHosts';
import {
  SshConnection,
  type SshSession,
  toRemoteError,
} from './sshConnection';
import { hostIdFor, parseSshDestination } from './sshDestination';
import {
  codeAndLogs,
  detailOf,
  explanationOf,
  userMessage,
} from './sshErrorText';
import { askOnWindow, destroyPromptWindows } from './sshPrompts';
import {
  RecentSshHosts,
  type RecentSshHostsStorage,
} from './sshRecentHosts';
import { openTransport, routeSessionThroughSsh } from './transport';
import { RemoteError, sanitizeRemoteText } from './types';
import {
  registerRemoteWindow,
  remoteBackendOf,
  remoteBackendOfWindow,
  windowsForHost,
} from './windowBackends';

const DEV_OVERRIDE_ENVS = [
  'ANTIGRAVITY_SSH_BIN',
  SSH_LS_VERSION_ENV,
  SSH_LS_REMOTE_PATH_ENV,
];

/** 781 s of back-off; ~20 min if every attempt hits ssh's 15 s timeout. */
const MAX_RECONNECT_ATTEMPTS = 30;
/** Doubles after each automatic reconnect, up to the max. */
const FIRST_RECONNECT_DELAY_MS = 1000;
const MAX_RECONNECT_DELAY_MS = 30000;
/** A session up this long gives the next drop a new reconnect budget. */
const STABLE_SESSION_MS = 60000;
const MAX_PAGE_DETAIL_CHARS = 300;
/** The pinned shape of an organization sign-in's provider_name. */
const WORKFORCE_PROVIDER_RE =
  /^locations\/global\/workforcePools\/([^/]+)\/providers\/([^/]+)$/;

export interface RemoteManagerDeps extends ConnectionInputsApp {
  mainStorage: RecentSshHostsStorage & WindowStorageManager;
  openLocalWindow: () => void;
}

interface ConnectAttempt {
  session: Promise<SshSession>;
  attachOnly: boolean;
  promptParents?: BaseWindow[];
}

interface HostEntry {
  hostId: string;
  destination: string;
  configFile: string | undefined;
  label: string;
  statusListeners: Set<(text: string) => void>;
  attempt?: ConnectAttempt;
  connection?: SshConnection;
  session?: SshSession;
  reconnectTimer?: ReturnType<typeof setTimeout>;
  reconnectAttempts: number;
  connectedAt: number;
  disposed: boolean;
  stopPhase?: 'asking' | 'stopping';
  blockedRequests: number;
}

interface ConnectAndOpenWindowOptions {
  onStatus?: (text: string) => void;
  configFile?: string;
  promptParent?: BaseWindow;
}

interface ConnectEntryOptions {
  onStatus?: (text: string) => void;
  attachOnly?: boolean;
  promptParent?: BaseWindow;
}

export class RemoteManager implements SshBridgeBackend {
  private readonly hosts = new Map<string, HostEntry>();
  /** Teardowns in flight, for disposeAll(). */
  private readonly disposals = new Set<Promise<void>>();
  /** Windows showing a sheet from showSheet(). */
  private readonly openSheets = new WeakSet<BaseWindow>();
  /** Each window's prompts, one at a time; it never holds an answer. */
  private readonly promptChains = new WeakMap<BaseWindow, Promise<void>>();
  /** Prompt parents that started to close: no prompt goes there any more. */
  private readonly closingWindows = new WeakSet<BaseWindow>();
  /** Each window's latest intent from connect(), until its page takes it. */
  private readonly pendingIntents = new WeakMap<WebContents, SshIntent>();
  private readonly recentHosts: RecentSshHosts;
  private readonly inputs: ConnectionInputs;

  constructor(private readonly deps: RemoteManagerDeps) {
    this.recentHosts = new RecentSshHosts(deps.mainStorage);
    this.inputs = new ConnectionInputs(deps);
    for (const name of DEV_OVERRIDE_ENVS) {
      if (process.env[name]) {
        log.warn(`[SSH] ${name} is set; using it for SSH connections.`);
      }
    }
  }

  /** The plugin:// routes of `sender`'s session, while it's up. */
  sidecarRoutesFor(sender: WebContents): RemoteSidecarRoutes | undefined {
    const own = remoteBackendOf(sender);
    const connection = own && this.hosts.get(own.hostId)?.connection;
    return connection?.sidecarFetch ? connection.sidecars : undefined;
  }

  getState(sender: WebContents): SshState {
    // Remote-served content learns only whether its own host is connected,
    // and no laptop paths.
    const own = remoteBackendOf(sender);
    const remote = isRemoteContents(sender);
    const connected = [...this.hosts.values()]
      .filter((e) => !e.disposed && e.session)
      .filter((e) => !remote || e.hostId === own?.hostId);
    return {
      currentHost: own?.label ?? null,
      connectedHosts: connected.map((e) => e.label),
      connected: connected.map((e) =>
        remote || e.configFile === undefined
          ? { host: e.label }
          : { host: e.label, configFile: e.configFile },
      ),
    };
  }

  async listHosts({
    configFile,
  }: SshListHostsOptions = {}): Promise<SshListHostsResult> {
    // A chosen file replaces the default ones, as with ssh -F.
    const found =
      configFile === undefined
        ? listSshConfigHosts()
        : resolveSshConfigFile(configFile).then((configPath) =>
            configPath === undefined
              ? listSshConfigHosts()
              : listSshConfigHosts({ configPath, systemConfigPath: null }),
          );
    const [configHosts, { recent, recentHosts, recentConfigFiles }] =
      await Promise.all([found, this.recentHosts.list()]);
    return { configHosts, recent, recentHosts, recentConfigFiles };
  }

  async connect(
    sender: WebContents,
    host: string,
    onProgress: (message: string) => void,
    { intent, configFile }: SshConnectBackendOptions = {},
  ): Promise<SshConnectResult> {
    const label = host.trim();
    try {
      const win = await this.connectAndOpenWindow(host, {
        onStatus: onProgress,
        configFile:
          configFile === undefined
            ? undefined
            : await resolveSshConfigFile(configFile),
        promptParent: resolveHubWindowFromWebContents(sender),
      });
      if (intent) {
        // Kept across reloads: the toolkit may subscribe only after sign-in.
        this.pendingIntents.set(win.webContents, intent);
        if (!win.webContents.isLoadingMainFrame()) {
          win.webContents.send(SSH_INTENT_CHANNEL);
        }
      }
      return { ok: true };
    } catch (err: unknown) {
      const remoteErr = toRemoteError(err);
      this.logError(remoteErr, label, 'SSH connection failed');
      return {
        ok: false,
        code: remoteErr.code,
        message: userMessage(remoteErr, label),
      };
    }
  }

  /** A file picker on `sender`'s window, in ~/.ssh: see SshBridgeBackend. */
  async chooseConfigFile(
    sender: WebContents,
  ): Promise<SshChooseConfigFileResult | null> {
    const parent = resolveHubWindowFromWebContents(sender);
    if (!parent) {
      return null;
    }
    const sshDir = path.join(os.homedir(), '.ssh');
    const { canceled, filePaths } = await dialog.showOpenDialog(parent, {
      title: 'Choose an SSH config file',
      defaultPath: (await fs.promises.stat(sshDir).catch(() => undefined))
        ? sshDir
        : os.homedir(),
      properties: ['openFile', 'showHiddenFiles'],
    });
    const [picked] = filePaths;
    if (canceled || picked === undefined) {
      return null;
    }
    // listHosts() and connect() check it.
    const configFile = await canonicalSshConfigFile(picked);
    return configFile === undefined ? {} : { configFile };
  }

  async stopServer(sender: WebContents): Promise<void> {
    const win = BrowserWindow.fromWebContents(sender);
    if (win) {
      await this.confirmAndStopServer(win);
    }
  }

  openLocalWindow(): void {
    const win = getLastFocusedLocalHubWindow();
    if (!win) {
      this.deps.openLocalWindow();
      return;
    }
    if (win.isMinimized()) {
      win.restore();
    }
    win.show();
    win.focus();
  }

  /** For --ssh-host: connects, or explains the failure in a sheet on `parent`. */
  async openFromCommandLine(host: string, parent: BaseWindow): Promise<void> {
    const label = host.trim();
    try {
      await this.connectAndOpenWindow(host, {
        onStatus: (text) => log.info(`[SSH] ${text}`),
        promptParent: parent,
      });
    } catch (err: unknown) {
      const remoteErr = toRemoteError(err);
      if (remoteErr.code === 'CANCELLED') {
        return; // E.g. quitting.
      }
      this.logError(remoteErr, label, 'SSH connection failed');
      const detail = detailOf(remoteErr, label);
      await this.showSheet(parent, {
        type: 'error',
        title: 'SSH connection failed',
        message: explanationOf(remoteErr, label),
        detail: [detail, codeAndLogs(remoteErr)].filter(Boolean).join('\n'),
      });
    }
  }

  takeIntent(sender: WebContents): SshIntent | undefined {
    const intent = this.pendingIntents.get(sender);
    this.pendingIntents.delete(sender);
    return intent;
  }

  /** Connects to `input`, then focuses the host's window or opens one. */
  private async connectAndOpenWindow(
    input: string,
    { onStatus, configFile, promptParent }: ConnectAndOpenWindowOptions = {},
  ): Promise<BrowserWindow> {
    const entry = this.entryFor(input, configFile);
    let sshSession: SshSession;
    try {
      sshSession = await this.connectEntry(entry, { onStatus, promptParent });
    } catch (err: unknown) {
      // Forget a host that never connected, so its next spelling starts over.
      if (
        !entry.attempt &&
        !entry.session &&
        windowsForHost(entry.hostId).length === 0
      ) {
        void this.disposeHost(entry);
      }
      throw err;
    }
    if (entry.disposed) {
      throw new RemoteError('CANCELLED', 'Connection cancelled.');
    }
    const [existing] = windowsForHost(entry.hostId);
    if (existing) {
      if (existing.isMinimized()) {
        existing.restore();
      }
      existing.show();
      existing.focus();
      return existing;
    }
    return this.openWindow(entry, sshSession);
  }

  /** The host's session: connects, or joins the connect in flight. */
  private async connectEntry(
    entry: HostEntry,
    { onStatus, attachOnly = false, promptParent }: ConnectEntryOptions = {},
  ): Promise<SshSession> {
    if (entry.session) {
      return entry.session;
    }
    if (onStatus) {
      entry.statusListeners.add(onStatus);
    }
    try {
      const attempt = this.joinAttempt(
        entry,
        attachOnly,
        canPrompt() ? promptParent : undefined,
      );
      try {
        return await attempt.session;
      } catch (err: unknown) {
        // It failed, but an attempt that may start or ask took over.
        if (entry.attempt && entry.attempt !== attempt) {
          return await this.connectEntry(entry, { attachOnly, promptParent });
        }
        throw err;
      }
    } finally {
      if (onStatus) {
        entry.statusListeners.delete(onStatus);
      }
    }
  }

  /**
   * The connect in flight, or a new one: a full connect extends an attach,
   * and one that can prompt extends one that can't.
   */
  private joinAttempt(
    entry: HostEntry,
    attachOnly: boolean,
    promptParent: BaseWindow | undefined,
  ): ConnectAttempt {
    const current = entry.attempt;
    if (
      current &&
      (attachOnly || !current.attachOnly) &&
      (!promptParent || current.promptParents)
    ) {
      if (promptParent) {
        // The run's askOn reads this array: prompts move to the newest caller.
        current.promptParents?.push(promptParent);
      }
      return current;
    }
    const promptParents = promptParent && [promptParent];
    const session = current
      ? this.afterAttempt(entry, current, attachOnly, promptParents)
      : this.openConnection(entry, attachOnly, promptParents);
    const attempt: ConnectAttempt = {
      session: session.finally(() => {
        if (entry.attempt === attempt) {
          entry.attempt = undefined;
        }
      }),
      attachOnly,
      promptParents,
    };
    entry.attempt = attempt;
    return attempt;
  }

  /**
   * Waits for `current`; connects again if it found no server and this may
   * start one, or if ssh needed to ask and only this can.
   */
  private async afterAttempt(
    entry: HostEntry,
    current: ConnectAttempt,
    attachOnly: boolean,
    promptParents: BaseWindow[] | undefined,
  ): Promise<SshSession> {
    try {
      return await current.session;
    } catch (err: unknown) {
      const { code } = toRemoteError(err);
      const start = code === 'LS_STOPPED' && current.attachOnly && !attachOnly;
      const ask =
        promptParents &&
        !current.promptParents &&
        (code === 'AUTH_FAILED' || code === 'HOSTKEY_REJECTED');
      if (!start && !ask) {
        throw err;
      }
      return await this.openConnection(entry, attachOnly, promptParents);
    }
  }

  private async openConnection(
    entry: HostEntry,
    attachOnly: boolean,
    promptParents: BaseWindow[] | undefined,
  ): Promise<SshSession> {
    // In parallel: on the first connect each can take seconds.
    const [loginEnv, artifacts] = await Promise.all([
      this.inputs.loginShellEnv(),
      this.inputs.serverFor(attachOnly),
    ]);
    if (entry.disposed) {
      throw new RemoteError('CANCELLED', 'Connection cancelled.');
    }
    const conn = new SshConnection({
      destination: entry.destination,
      configFile: entry.configFile,
      label: entry.label,
      server: artifacts,
      attachOnly,
      askpass: promptParents && {
        nodeBinary: electronNodeBinary(),
        ask: (prompt) => this.askOn(promptParents, entry.label, prompt),
      },
      userDataDir: app.getPath('userData'),
      appDataDirName: getAppDataDirName(),
      createTransport: (socksPort) => openTransport(entry.hostId, socksPort),
      openExternal: (url) => openExternalSafely(url, 'unconfirmed'),
      confirmOrganizationSignIn: (provider, signal) =>
        this.confirmOrganizationSignIn(entry, provider, signal),
      env: { ...process.env, ...loginEnv },
      fetch: (url, init) => net.fetch(url, init),
      log: (line) => log.info(line),
    });
    // dispose() removes these listeners.
    conn.on('status', (text) => {
      for (const listener of entry.statusListeners) {
        listener(text);
      }
    });
    // No window, no dialog: an app-modal one would block every window.
    conn.on('signInError', (err) => {
      const [win] = windowsForHost(entry.hostId);
      if (win) {
        this.reportError(err, entry.label, 'Sign-in failed', win);
      } else {
        this.logError(err, entry.label, 'Sign-in failed');
      }
    });
    conn.on('disconnected', (err) => this.onDisconnected(entry, conn, err));
    entry.connection = conn;
    // A drop before the session is up fails this connect with its error.
    let dropped: RemoteError | undefined;
    conn.once('disconnected', (err) => (dropped = err));
    try {
      const sshSession = await conn.connect();
      if (entry.disposed) {
        throw new RemoteError('CANCELLED', 'Connection cancelled.');
      }
      // Before any window loads: each connection has a new SOCKS port.
      await routeSessionThroughSsh(
        session.fromPartition(partitionFor(entry.hostId)),
        sshSession.socksPort,
      );
      if (entry.disposed || entry.connection !== conn) {
        throw new RemoteError('CANCELLED', 'Connection cancelled.');
      }
      if (dropped) {
        throw dropped;
      }
      this.onConnected(entry, sshSession);
      return sshSession;
    } catch (err: unknown) {
      if (entry.connection === conn) {
        entry.connection = undefined;
      }
      this.trackDisposal(conn.dispose());
      throw err;
    }
  }

  private onConnected(entry: HostEntry, sshSession: SshSession): void {
    entry.session = sshSession;
    entry.connectedAt = Date.now();
    clearTimeout(entry.reconnectTimer); // E.g. connected from a local window.
    log.info(`[SSH] Connected to ${entry.label} at ${sshSession.url}`);
    const running = sshSession.discovery.lsVersion;
    if (running !== sshSession.expectedVersion) {
      // Kept: replacing it would stop its agents.
      log.info(
        `[SSH] ${entry.label} runs Antigravity server ${sanitizeRemoteText(running, 64)}; this app uses ${sshSession.expectedVersion}.`,
      );
    }
    this.recentHosts.remember(entry.label, entry.configFile);
    for (const win of windowsForHost(entry.hostId)) {
      // E.g. ERR_ABORTED: the window closed or navigated during the load.
      win
        .loadURL(sshSession.url)
        .catch((err: unknown) =>
          log.warn(`[SSH] Could not load ${entry.label} in its window:`, err),
        );
    }
  }

  private openWindow(entry: HostEntry, sshSession: SshSession): BrowserWindow {
    const partition = partitionFor(entry.hostId);
    // Windows outlive entries (Reconnect reuses them): look the host up by id.
    const { hostId, label, configFile } = entry;
    const current = (): HostEntry | undefined => {
      const e = this.hosts.get(hostId);
      return e && !e.disposed ? e : undefined;
    };
    const allowedOrigin = (): string | undefined => {
      const e = current();
      return e?.session ? new URL(e.session.url).origin : undefined;
    };
    let win: BrowserWindow | undefined;
    try {
      const ses = session.fromPartition(partition);
      hardenRemoteSession(ses, {
        allowedOrigin,
        onBlocked: (url) => this.logBlockedRequest(current() ?? entry, url),
      });
      // Sidecar UIs: the host's current session routes and fetches them.
      installRemoteSidecarProtocol(ses, () => current()?.connection);
      const created = createWindow(sshSession.url, this.deps.mainStorage, {
        cascade: true,
        partition,
        titleSuffix: ` (SSH: ${label})`,
        additionalArguments: ['--agy-backend=ssh'],
        builtInBrowser: false,
        navigationPolicy: remoteNavigationPolicy(allowedOrigin, (action) => {
          if (action === 'reconnect') {
            this.reconnect(hostId, label, configFile, created).catch(
              (err: unknown) =>
                log.error(`[SSH] Could not reconnect to ${label}:`, err),
            );
          } else {
            created.close();
          }
        }),
        // A copy would be an unregistered window on the remote's URL.
        onNewWindow: () => this.deps.openLocalWindow(),
      });
      win = created;
      // Registered first: whatever fails next, it never passes for local.
      registerRemoteWindow(created, { kind: 'ssh', hostId, label });
      hardenRemoteWindow(created);
    } catch (err: unknown) {
      log.error(`[SSH] Could not open a window on ${label}:`, err);
      if (win && !win.isDestroyed()) {
        win.destroy();
      }
      // Nothing else would close a connection without windows.
      if (windowsForHost(hostId).length === 0) {
        void this.disposeHost(entry);
      }
      throw err;
    }
    win.on('closed', () => {
      const e = current();
      if (e && windowsForHost(hostId).length === 0) {
        void this.disposeHost(e);
      }
    });
    log.info(`[SSH] Opened a window on ${label}.`);
    return win;
  }

  /** The Disconnected page's Reconnect button, in `win`. */
  private async reconnect(
    hostId: string,
    label: string,
    configFile: string | undefined,
    win: BrowserWindow,
  ): Promise<void> {
    if (windowsForHost(hostId).length === 0) {
      return;
    }
    const entry = this.hosts.get(hostId) ?? this.entryFor(label, configFile);
    if (entry.session) {
      return; // Connected meanwhile (e.g. from a local window).
    }
    this.showPage(entry, { state: 'connecting' });
    try {
      // onConnected reloads the host's windows.
      await this.connectEntry(entry, { promptParent: win });
    } catch (err: unknown) {
      const remoteErr = toRemoteError(err);
      // Cancelled by a teardown, not at a prompt.
      if (
        entry.disposed ||
        (remoteErr.code === 'CANCELLED' && !remoteErr.userCancelled)
      ) {
        return;
      }
      this.logError(remoteErr, entry.label, 'Reconnect failed');
      this.showPage(entry, {
        state: 'disconnected',
        detail: userMessage(remoteErr, entry.label),
      });
    }
  }

  /** Like Reconnect, but it only attaches, and retries network failures. */
  private reconnectLater(entry: HostEntry): void {
    clearTimeout(entry.reconnectTimer);
    const delay = Math.min(
      FIRST_RECONNECT_DELAY_MS * 2 ** entry.reconnectAttempts,
      MAX_RECONNECT_DELAY_MS,
    );
    entry.reconnectAttempts++;
    entry.reconnectTimer = setTimeout(() => {
      this.connectEntry(entry, { attachOnly: true }).catch((err: unknown) => {
        const remoteErr = toRemoteError(err);
        // Cancelled by a teardown: the host was disposed (window closed, Stop,
        // or quit). A prompt's Cancel, in a connect it joined, shows.
        if (
          entry.disposed ||
          (remoteErr.code === 'CANCELLED' && !remoteErr.userCancelled)
        ) {
          return;
        }
        this.logError(remoteErr, entry.label, 'Reconnect failed');
        if (
          remoteErr.retryable &&
          entry.reconnectAttempts < MAX_RECONNECT_ATTEMPTS
        ) {
          this.reconnectLater(entry);
        } else {
          this.showPage(entry, {
            state: 'disconnected',
            detail: userMessage(remoteErr, entry.label),
          });
        }
      });
    }, delay);
  }

  private onDisconnected(
    entry: HostEntry,
    conn: SshConnection,
    err: RemoteError,
  ): void {
    // Before the session is up, the connect in flight reports the drop.
    if (entry.connection !== conn || entry.disposed || !entry.session) {
      return;
    }
    log.warn(
      `[SSH] Lost connection to ${entry.label}: ${sanitizeRemoteText(err.message)}`,
    );
    entry.connection = undefined;
    entry.session = undefined;
    this.trackDisposal(conn.dispose());
    if (entry.stopPhase === 'stopping' && err.code === 'LS_STOPPED') {
      return; // The stop worked: confirmAndStopServer closes the windows.
    }
    if (windowsForHost(entry.hostId).length === 0) {
      void this.disposeHost(entry);
      return;
    }
    // A host that keeps dropping right after connecting gets no new budget.
    if (Date.now() - entry.connectedAt >= STABLE_SESSION_MS) {
      entry.reconnectAttempts = 0;
    }
    // Attach only: a server that went away may have been stopped on purpose.
    if (err.retryable && entry.reconnectAttempts < MAX_RECONNECT_ATTEMPTS) {
      this.showPage(entry, { state: 'connecting' });
      this.reconnectLater(entry);
      return;
    }
    this.showPage(entry, {
      state: 'disconnected',
      detail: sanitizeRemoteText(err.message, MAX_PAGE_DETAIL_CHARS),
    });
  }

  private showPage(
    entry: HostEntry,
    page: Omit<DisconnectedPage, 'label' | 'dark'>,
  ): void {
    for (const win of windowsForHost(entry.hostId)) {
      showDisconnectedPage(win, {
        ...page,
        label: entry.label,
        dark: !isLightTheme(),
      });
    }
  }

  /** Confirms, stops the host's server and agents, and closes its windows. */
  private async confirmAndStopServer(win: BrowserWindow): Promise<void> {
    const info = remoteBackendOfWindow(win);
    const entry = info && this.hosts.get(info.hostId);
    if (!entry || entry.disposed || entry.stopPhase) {
      return;
    }
    // Checked after each await: a drop replaces it and takes its own path.
    const conn = entry.connection;
    if (!conn || !entry.session) {
      await this.tellNotConnected(win, entry.label);
      return;
    }
    entry.stopPhase = 'asking';
    let response: number;
    try {
      ({ response } = await dialog.showMessageBox(win, {
        type: 'warning',
        buttons: ['Stop Server', 'Cancel'],
        defaultId: 1,
        cancelId: 1,
        title: 'Stop SSH server',
        message: `Stop the Antigravity server on ${entry.label}?`,
        detail:
          'Agents running on this host will stop and its window will close. ' +
          'The server starts again the next time you connect.',
        // A drop closes it as Cancel.
        signal: conn.closedSignal,
      }));
    } finally {
      entry.stopPhase = undefined;
    }
    if (response !== 0 || entry.disposed) {
      return;
    }
    if (entry.connection !== conn) {
      await this.tellNotConnected(win, entry.label);
      return;
    }
    entry.stopPhase = 'stopping';
    try {
      await conn.stopRemoteServer();
    } catch (err: unknown) {
      // After a drop, its own path already reconnects or shows the page.
      if (!entry.disposed && entry.connection === conn) {
        this.reportError(
          err,
          entry.label,
          'Could not stop the SSH server',
          win,
        );
      }
      return;
    } finally {
      entry.stopPhase = undefined;
    }
    log.info(`[SSH] Stopped the remote server on ${entry.label}.`);
    const windows = windowsForHost(entry.hostId);
    // Dispose first, so nothing shows the Disconnected page for it.
    await this.disposeHost(entry);
    for (const w of windows) {
      if (!w.isDestroyed()) {
        w.close();
      }
    }
  }

  private tellNotConnected(win: BaseWindow, label: string): Promise<void> {
    return this.showSheet(win, {
      type: 'info',
      title: 'Stop SSH server',
      message: `Not connected to ${label}`,
      detail: 'Reconnect first, then stop its server.',
    });
  }

  /** Logs `err`, and shows it in a sheet on `parent` unless it's CANCELLED. */
  private reportError(
    err: unknown,
    label: string,
    title: string,
    parent: BaseWindow,
  ): void {
    const remoteErr = toRemoteError(err);
    if (remoteErr.code === 'CANCELLED') {
      return;
    }
    this.logError(remoteErr, label, title);
    void this.showSheet(parent, {
      type: 'error',
      title,
      message: detailOf(remoteErr, label),
      detail: codeAndLogs(remoteErr),
    });
  }

  /** One sheet at a time per window: a host can send errors endlessly. */
  private async showSheet(
    parent: BaseWindow,
    options: MessageBoxOptions,
  ): Promise<void> {
    if (parent.isDestroyed() || this.openSheets.has(parent)) {
      return;
    }
    this.openSheets.add(parent);
    try {
      await dialog.showMessageBox(parent, options);
    } catch (err: unknown) {
      log.warn(`[SSH] Could not show "${options.title}":`, err);
    } finally {
      this.openSheets.delete(parent);
    }
  }

  /**
   * ssh's prompt on the newest open window of `parents` as its turn comes,
   * after that window's earlier ones; Cancel if none is open.
   */
  private askOn(
    parents: BaseWindow[],
    label: string,
    prompt: AskpassPrompt,
  ): Promise<AskpassAnswer> {
    const newest = (): BaseWindow | undefined =>
      [...parents]
        .reverse()
        .find((w) => !w.isDestroyed() && !this.closingWindows.has(w));
    const parent = newest();
    if (!parent) {
      return Promise.resolve({ kind: 'cancel' });
    }
    const closing = (): void => {
      this.closingWindows.add(parent);
    };
    parent.once('close', closing);
    // Picked again at its turn (the window may be closing or gone, or a
    // caller joined); a prompt that moves frees the window's slot first.
    const shown = (this.promptChains.get(parent) ?? Promise.resolve()).then(
      () => (newest() === parent ? askOnWindow(parent, label, prompt) : null),
    );
    const done = (): void => undefined;
    this.promptChains.set(parent, shown.then(done, done));
    return shown
      .then((answer) => answer ?? this.askOn(parents, label, prompt))
      .finally(() => parent.off('close', closing));
  }

  private logError(err: RemoteError, label: string, title: string): void {
    log.error(
      `[SSH] ${label}: ${title}: ${err.code} ${sanitizeRemoteText(err.message)}`,
    );
  }

  /** Unlike Google's, its IdP may sign in without a click, for the host. */
  private async confirmOrganizationSignIn(
    entry: HostEntry,
    provider: string, // Pinned by SshConnection to a workforce provider.
    signal: AbortSignal,
  ): Promise<boolean> {
    const [parent] = windowsForHost(entry.hostId);
    // No window, no question: an app-modal one would block every window.
    if (entry.disposed || !parent) {
      return false;
    }
    const names = WORKFORCE_PROVIDER_RE.exec(provider);
    const { response } = await dialog.showMessageBox(parent, {
      type: 'question',
      buttons: ['Sign In', 'Cancel'],
      // The host decides when this shows: a Return meant for it mustn't accept.
      defaultId: 1,
      cancelId: 1,
      title: 'Sign in',
      message: `Sign in to your organization for ${entry.label}?`,
      detail:
        'Continue only if you just chose to sign in with your organization ' +
        `in this window. ${entry.label} will get access to your ` +
        "organization's Google Cloud account.\n" +
        (names
          ? `Workforce pool: ${names[1]}  Provider: ${names[2]}`
          : `Identity provider: ${provider}`),
      // Closes with its connection: a later yes would open nothing.
      signal,
    });
    return response === 0;
  }

  private logBlockedRequest(entry: HostEntry, url: string): void {
    const n = ++entry.blockedRequests;
    if (n <= 20 || n % 100 === 0) {
      let where = url;
      try {
        const u = new URL(url);
        where = `${u.protocol}//${u.host}`;
      } catch {
        // Keep the raw (sanitized) text.
      }
      log.warn(
        `[SSH] Blocked a request from the ${entry.label} window to ${sanitizeRemoteText(where, 200)} (${n} so far).`,
      );
    }
  }

  private entryFor(input: string, configFile?: string): HostEntry {
    const { destination } = parseSshDestination(input);
    const label = input.trim();
    const hostId = hostIdFor(destination, configFile);
    let entry = this.hosts.get(hostId);
    if (!entry || entry.disposed) {
      entry = {
        hostId,
        destination,
        configFile,
        label,
        statusListeners: new Set(),
        reconnectAttempts: 0,
        connectedAt: 0,
        disposed: false,
        blockedRequests: 0,
      };
      this.hosts.set(hostId, entry);
    }
    return entry;
  }

  /**
   * Marks every host closed (window closes and drops then do nothing) and
   * signals ssh, then waits for it. On quit, call it before destroying windows.
   */
  async disposeAll(): Promise<void> {
    // First: main.ts destroys every window next, parents first, and a modal
    // child destroyed after its parent crashes Electron.
    destroyPromptWindows();
    for (const entry of [...this.hosts.values()]) {
      void this.disposeHost(entry);
    }
    await Promise.allSettled([...this.disposals]);
  }

  private disposeHost(entry: HostEntry): Promise<void> {
    entry.disposed = true;
    clearTimeout(entry.reconnectTimer);
    if (this.hosts.get(entry.hostId) === entry) {
      this.hosts.delete(entry.hostId);
    }
    const connection = entry.connection;
    entry.connection = undefined;
    entry.session = undefined;
    const disposing = connection?.dispose() ?? Promise.resolve();
    this.trackDisposal(disposing);
    return disposing;
  }

  private trackDisposal(disposing: Promise<void>): void {
    this.disposals.add(disposing);
    const forget = (): void => {
      this.disposals.delete(disposing);
    };
    // Not finally(): it re-rejects.
    void disposing.then(forget, forget);
  }
}

function partitionFor(hostId: string): string {
  return `persist:ssh-${hostId}`;
}
