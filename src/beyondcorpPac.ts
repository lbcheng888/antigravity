import { app, session } from 'electron';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { isScriptJailSession } from './scriptJail';

/**
 * Chrome Web Store / enterprise force-installed extension IDs for the BeyondCorp
 * proxy extension (production first, followed by development).
 */
export const BEYONDCORP_EXTENSION_IDS: readonly string[] = [
  'aihpiglmnhnhijdnjghpfnlledckkhja',
  'mnhjodcoklifmaholfognbdkgohlcbmg',
];

/** Filename of the bundled PAC script inside the unpacked extension directory. */
export const BEYONDCORP_PAC_FILENAME = 'beyondcorp_bundled.pac.js';

/**
 * Default persistent partition used by the built-in `<webview>` browser before
 * user identity resolves (must match `ANONYMOUS_BROWSER_PARTITION` in AUIT).
 */
export const ANONYMOUS_WEBVIEW_PARTITION = 'persist:hub-browser-anonymous';

const WEBVIEW_PARTITION_DIR_PREFIX = 'hub-browser-';
const PROFILE_DIR_RE = /^(Default|Profile \d+)$/;
const XUP_PROXY_DIRECTIVE_RE = /(?:^|\s|;)(?:HTTPS|PROXY)\s+[^;]*xup\./i;
const XUP_PROXY_ENTRY_RE = /^(?:HTTPS|PROXY)\s+[^;]*xup\./i;

/** Response passed to Electron's `webRequest.onBeforeRequest` callback. */
export type WebviewProxyRequestCallbackResponse = {
  cancel?: boolean;
  redirectURL?: string;
};

/** Listener registered via `session.webRequest.onBeforeRequest`. */
export type WebviewProxyBeforeRequestListener = (
  details: unknown,
  callback: (response: WebviewProxyRequestCallbackResponse) => void,
) => void;

/**
 * Minimal `webRequest` surface required to hold initial guest requests while
 * `session.setProxy` resolves.
 */
export interface WebviewProxyWebRequest {
  onBeforeRequest?(listener: WebviewProxyBeforeRequestListener | null): void;
}

/** Minimal `Session` surface required to configure a `<webview>` PAC proxy. */
export interface WebviewProxySession {
  setProxy(config: { pacScript: string }): Promise<void> | void;
  webRequest?: WebviewProxyWebRequest | null;
}

/** Options accepted by {@link createWebviewProxyConfigurator}. */
export interface WebviewProxyConfiguratorOptions {
  defaultSession?: object | null;
}

/** Function that configures the BeyondCorp PAC proxy on a `<webview>` session. */
export type WebviewSessionProxyConfigurator = (
  targetSession: WebviewProxySession | null | undefined,
) => Promise<void>;

/** Options accepted by {@link preconfigureWebviewPartitionProxies}. */
export interface PreconfigureWebviewPartitionProxiesOptions {
  userDataPath?: string;
  fromPartition?: (
    partition: string,
  ) => WebviewProxySession | null | undefined;
  configureSession?: WebviewSessionProxyConfigurator;
}

function resolveDefaultAppDataPath(): string {
  try {
    return app.getPath('appData');
  } catch {
    return '';
  }
}

function listSubdirectories(dirPath: string): string[] {
  try {
    return fs
      .readdirSync(dirPath, { withFileTypes: true })
      .filter((entry) => entry.isDirectory())
      .map((entry) => entry.name);
  } catch {
    return [];
  }
}

function isNonEmptyRegularFile(filePath: string): boolean {
  try {
    const stat = fs.statSync(filePath);
    return stat.isFile() && stat.size > 0;
  } catch {
    return false;
  }
}

/**
 * Returns candidate Chrome `User Data` directories for the given platform in
 * channel priority order (Stable, Beta, Canary/Unstable).
 */
export function getChromeUserDataDirs(
  platform: string = process.platform,
  appDataPath?: string,
  localAppDataPath: string | undefined = process.env.LOCALAPPDATA,
  homePath: string = os.homedir(),
): string[] {
  switch (platform) {
    case 'darwin': {
      const base = appDataPath ?? resolveDefaultAppDataPath();
      if (!base) {
        return [];
      }
      return [
        path.join(base, 'Google', 'Chrome'),
        path.join(base, 'Google', 'Chrome Beta'),
        path.join(base, 'Google', 'Chrome Canary'),
      ];
    }
    case 'linux': {
      const base = appDataPath ?? resolveDefaultAppDataPath();
      if (!base) {
        return [];
      }
      return [
        path.join(base, 'google-chrome'),
        path.join(base, 'google-chrome-beta'),
        path.join(base, 'google-chrome-unstable'),
      ];
    }
    case 'win32': {
      const base =
        localAppDataPath ||
        appDataPath ||
        (homePath ? path.join(homePath, 'AppData', 'Local') : '');
      if (!base) {
        return [];
      }
      return [
        path.join(base, 'Google', 'Chrome', 'User Data'),
        path.join(base, 'Google', 'Chrome Beta', 'User Data'),
        path.join(base, 'Google', 'Chrome SxS', 'User Data'),
      ];
    }
    default:
      return [];
  }
}

/**
 * Scans candidate Chrome `User Data` directories for the unpacked BeyondCorp
 * extension's bundled PAC file (`beyondcorp_bundled.pac.js`).
 *
 * Inspects `Default` before numerically sorted `Profile N` directories and
 * selects the highest version directory containing a non-empty PAC file.
 * Returns `null` when the extension is absent.
 */
export function findBeyondCorpPacFile(
  userDataDirs?: readonly string[],
): string | null {
  // Phase 1 reads the force-installed Chrome extension's bundled PAC file from
  // the local disk; Phase 2 will transition to native OS-level MDM / Managed
  // Preferences policy keys (ProxyPacUrl, AutoSelectCertificateForUrls).
  const candidateDirs = userDataDirs ?? getChromeUserDataDirs();
  for (const userDataDir of candidateDirs) {
    const profiles = listSubdirectories(userDataDir)
      .filter((name) => PROFILE_DIR_RE.test(name))
      .sort((a, b) => {
        if (a === b) {
          return 0;
        }
        if (a === 'Default') {
          return -1;
        }
        if (b === 'Default') {
          return 1;
        }
        return a.localeCompare(b, undefined, { numeric: true });
      });
    for (const profile of profiles) {
      for (const extId of BEYONDCORP_EXTENSION_IDS) {
        const extDir = path.join(userDataDir, profile, 'Extensions', extId);
        const versions = listSubdirectories(extDir).sort((a, b) =>
          b.localeCompare(a, undefined, { numeric: true }),
        );
        for (const version of versions) {
          const pacPath = path.join(extDir, version, BEYONDCORP_PAC_FILENAME);
          if (isNonEmptyRegularFile(pacPath)) {
            return pacPath;
          }
        }
      }
    }
  }
  return null;
}

/**
 * Wraps `FindProxyForURL` in the given PAC script source so any `xup.` proxy
 * entry is stripped from the directive list, falling back to `'DIRECT'` only
 * when no non-`xup.` directives remain.
 */
export function wrapBeyondCorpPacScript(rawPacJs: string): string {
  // XUP proxy endpoints challenge clients with HTTP 407 Proxy Authentication
  // Required, which relies on the Chrome extension's webRequest.onAuthRequired
  // listener that does not run inside an Electron <webview>. Stripping xup
  // directives (and falling back to DIRECT only if nothing else remains)
  // avoids 407 failures on third-party SaaS domains while preserving any
  // non-xup fallback routes (such as l2-uberproxy and up.gcp).
  return `${rawPacJs}
;(function() {
  if (typeof FindProxyForURL !== 'function') {
    return;
  }
  var originalFindProxyForURL = FindProxyForURL;
  var xupProxyDirectiveRe = ${XUP_PROXY_DIRECTIVE_RE.toString()};
  var xupProxyEntryRe = ${XUP_PROXY_ENTRY_RE.toString()};
  FindProxyForURL = function(url, host) {
    var result = originalFindProxyForURL(url, host);
    if (typeof result !== 'string' || !xupProxyDirectiveRe.test(result)) {
      return result;
    }
    var entries = result.split(';');
    var filtered = [];
    for (var i = 0; i < entries.length; i++) {
      var entry = entries[i].trim();
      if (entry && !xupProxyEntryRe.test(entry)) {
        filtered.push(entry);
      }
    }
    return filtered.length > 0 ? filtered.join('; ') : 'DIRECT';
  };
})();
`;
}

/**
 * Locates the local BeyondCorp PAC file, wraps it with `wrapBeyondCorpPacScript`,
 * and encodes it as a base64 `data:application/x-ns-proxy-autoconfig` URI.
 * Returns `null` if no valid PAC file is found or reading fails.
 */
export function loadBeyondCorpPacDataUri(
  userDataDirs?: readonly string[],
): string | null {
  const pacPath = findBeyondCorpPacFile(userDataDirs);
  if (!pacPath) {
    return null;
  }
  try {
    const rawPacJs = fs.readFileSync(pacPath, 'utf8');
    if (!rawPacJs || !rawPacJs.trim()) {
      return null;
    }
    const wrapped = wrapBeyondCorpPacScript(rawPacJs);
    return `data:application/x-ns-proxy-autoconfig;base64,${Buffer.from(wrapped, 'utf8').toString('base64')}`;
  } catch {
    return null;
  }
}

/**
 * Creates a `<webview>` session proxy configurator that resolves the PAC `data:`
 * URI at most once, skips `session.defaultSession`, holds in-flight initial
 * requests via `webRequest.onBeforeRequest` until `session.setProxy` resolves,
 * and applies `session.setProxy` at most once per `Session` instance.
 */
export function createWebviewProxyConfigurator(
  resolvePacDataUri: () => string | null | undefined = () =>
    loadBeyondCorpPacDataUri(),
  options?: WebviewProxyConfiguratorOptions,
): WebviewSessionProxyConfigurator {
  const configuredSessions = new WeakMap<WebviewProxySession, Promise<void>>();
  let hasResolvedPacDataUri = false;
  let cachedPacDataUri: string | null | undefined = null;

  return (targetSession) => {
    const resolvedDefaultSession =
      options?.defaultSession !== undefined
        ? options.defaultSession
        : session?.defaultSession;
    if (
      !targetSession ||
      (resolvedDefaultSession && targetSession === resolvedDefaultSession) ||
      isScriptJailSession(targetSession)
    ) {
      return Promise.resolve();
    }

    const existingPromise = configuredSessions.get(targetSession);
    if (existingPromise) {
      return existingPromise;
    }

    if (!hasResolvedPacDataUri) {
      hasResolvedPacDataUri = true;
      cachedPacDataUri = resolvePacDataUri();
      if (!cachedPacDataUri) {
        console.info(
          '[BeyondCorpPAC] No local BeyondCorp PAC file found; skipping webview proxy configuration.',
        );
      }
    }

    if (!cachedPacDataUri) {
      return Promise.resolve();
    }

    let releaseHold!: () => void;
    const holdGate = new Promise<void>((resolve) => {
      releaseHold = resolve;
    });

    if (targetSession.webRequest?.onBeforeRequest) {
      targetSession.webRequest.onBeforeRequest((_details, callback) => {
        void holdGate.then(() => {
          callback({});
        });
      });
    }

    const proxyPromise = Promise.resolve(
      targetSession.setProxy({ pacScript: cachedPacDataUri }),
    )
      .catch((err: unknown) => {
        configuredSessions.delete(targetSession);
        console.warn(
          '[BeyondCorpPAC] Failed to configure webview session proxy:',
          err,
        );
      })
      .finally(() => {
        if (targetSession.webRequest?.onBeforeRequest) {
          targetSession.webRequest.onBeforeRequest(null);
        }
        releaseHold();
      });

    configuredSessions.set(targetSession, proxyPromise);
    return proxyPromise;
  };
}

/**
 * Default `<webview>` session proxy configurator used by the main process.
 */
export const configureWebviewSessionProxy = createWebviewProxyConfigurator();

/**
 * Pre-configures the BeyondCorp PAC proxy on `persist:hub-browser-anonymous`
 * and any existing `persist:hub-browser-*` account partitions on disk before
 * any `BrowserWindow` or `<webview>` is created.
 */
export function preconfigureWebviewPartitionProxies<TSession>(options: {
  userDataPath?: string;
  fromPartition: (partition: string) => TSession;
  configureSession: (targetSession: TSession) => Promise<void>;
}): Promise<void>;
export function preconfigureWebviewPartitionProxies(
  options?: PreconfigureWebviewPartitionProxiesOptions,
): Promise<void>;
export async function preconfigureWebviewPartitionProxies(options?: {
  userDataPath?: string;
  fromPartition?: (partition: string) => unknown;
  configureSession?: (targetSession: never) => Promise<void>;
}): Promise<void> {
  const configureSession = (options?.configureSession ??
    configureWebviewSessionProxy) as (
    targetSession: unknown,
  ) => Promise<void>;
  const fromPartition =
    options?.fromPartition ??
    (typeof session?.fromPartition === 'function'
      ? (partition: string) => session.fromPartition(partition)
      : undefined);
  if (!fromPartition) {
    return;
  }

  const partitions = new Set<string>([ANONYMOUS_WEBVIEW_PARTITION]);
  let userDataPath = options?.userDataPath;
  if (userDataPath === undefined) {
    try {
      userDataPath = app.getPath('userData');
    } catch {
      userDataPath = '';
    }
  }

  if (userDataPath) {
    const partitionsDir = path.join(userDataPath, 'Partitions');
    for (const dirName of listSubdirectories(partitionsDir)) {
      if (dirName.startsWith(WEBVIEW_PARTITION_DIR_PREFIX)) {
        partitions.add(`persist:${dirName}`);
      }
    }
  }

  await Promise.all(
    [...partitions].map((partition) =>
      configureSession(fromPartition(partition)),
    ),
  );
}
