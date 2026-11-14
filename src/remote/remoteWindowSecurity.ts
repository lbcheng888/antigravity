/**
 * Hardening for SSH windows. The remote host serves their content, so a
 * compromised remote account, tampered install or toolkit XSS must stay on
 * the remote: the page must not reach the laptop's loopback or private network
 * (notably the local language server, whose CORS policy trusts any loopback
 * origin), hand other schemes or local addresses to the OS, leave its origin,
 * attach <webview> guests, get device, media or clipboard-read permissions or
 * a client certificate, run session-wide preloads, or veto unloading.
 * Sidecar UIs (`plugin:`) are the one other origin: their handler fetches the
 * remote's loopback through the same SOCKS proxy (remoteSidecars.ts).
 *
 * TODO(nathanjuan): Narrow the local language server's CORS policy too.
 */

import { app, type Session } from 'electron';
import { isSameOrigin, openExternalSafely } from '../externalUrl';
import {
  disconnectedPageAction,
  isDisconnectedPageLink,
  type DisconnectedPageAction,
  type DisconnectedPageContents,
} from './disconnectedPage';
import { isLocalNetworkHost, isRemoteOpenableUrl } from './localNetwork';
import { trustLoopbackCertificate } from './transport';
import { remoteBackendOf, type WebContentsLike } from './windowBackends';

export interface RemoteContentsLike extends WebContentsLike {
  session?: unknown;
  isDestroyed?: () => boolean;
}

export interface RemotePreloadScript {
  id: string;
}

export interface RemotePreloadSession {
  getPreloadScripts?: () => ReadonlyArray<RemotePreloadScript>;
  unregisterPreloadScript?: (id: string) => void;
}

export interface RemoteSessionPolicy {
  allowedOrigin: () => string | undefined;
  onBlocked?: (url: string) => void;
}

export interface RemoteBeforeRequestDetails {
  url: string;
}

export interface RemoteBeforeRequestResponse {
  cancel: boolean;
}

export interface RemoteWebRequest {
  onBeforeRequest(
    listener: (
      details: RemoteBeforeRequestDetails,
      callback: (response: RemoteBeforeRequestResponse) => void,
    ) => void,
  ): void;
}

export interface PreventableEventLike {
  preventDefault(): void;
}

export interface RemoteHardenedSession extends RemotePreloadSession {
  webRequest: RemoteWebRequest;
  setCertificateVerifyProc?: (
    proc: (
      request: { hostname: string },
      callback: (verificationResult: number) => void,
    ) => void,
  ) => void;
  setPermissionRequestHandler(
    handler: (
      webContents: unknown,
      permission: string,
      callback: (permissionGranted: boolean) => void,
    ) => void,
  ): void;
  setPermissionCheckHandler(
    handler: (webContents: unknown, permission: string) => boolean,
  ): void;
  setDevicePermissionHandler(handler: () => boolean): void;
  on(
    event: 'select-hid-device',
    listener: (
      event: PreventableEventLike,
      details: unknown,
      callback: () => void,
    ) => void,
  ): unknown;
  on(
    event: 'select-serial-port',
    listener: (
      event: PreventableEventLike,
      portList: unknown,
      webContents: unknown,
      callback: (portId: string) => void,
    ) => void,
  ): unknown;
  on(
    event: 'select-usb-device',
    listener: (
      event: PreventableEventLike,
      details: unknown,
      callback: () => void,
    ) => void,
  ): unknown;
}

export type RemoteUrlOpenResult = 'opened' | 'blocked';

export interface RemoteNavigationPolicy {
  allowsNavigation: (url: string) => boolean;
  openOutside: (url: string, contents: DisconnectedPageContents) => void;
}

export interface RemoteHardenedWebContents {
  isDestroyed?: () => boolean;
  on(
    event: 'will-attach-webview',
    listener: (event: PreventableEventLike) => void,
  ): unknown;
  on(
    event: 'will-prevent-unload',
    listener: (event: PreventableEventLike) => void,
  ): unknown;
  on(
    event: 'select-bluetooth-device',
    listener: (
      event: PreventableEventLike,
      devices: unknown,
      callback: (deviceId: string) => void,
    ) => void,
  ): unknown;
  setWebRTCIPHandlingPolicy(policy: 'disable_non_proxied_udp'): void;
}

export interface RemoteHardenedWindow {
  isDestroyed?: () => boolean;
  webContents: RemoteHardenedWebContents;
}

/**
 * Whether a request in an SSH window's session must be blocked: local
 * addresses other than `allowedOrigin` (the window's own; undefined while
 * disconnected), which are the remote's other services or network, and all
 * cleartext, since the host check sees only literals and a DNS name can
 * resolve to a local address.
 */
export function shouldBlockRemoteRequest(
  rawUrl: string,
  allowedOrigin?: string,
): boolean {
  let url: URL;
  try {
    url = new URL(rawUrl);
  } catch {
    return true;
  }
  switch (url.protocol) {
    case 'data:':
    case 'blob:':
    case 'about:':
    case 'devtools:':
      return false;
    case 'plugin:':
      // The session's own handler (remoteSidecars.ts, installed before its
      // window exists): it fetches only the host's loopback, through the
      // host's SOCKS proxy. A partition's handlers are its own: nothing falls
      // through to the default session's (local) plugin:// map.
      return false;
    case 'https:':
    case 'wss:':
      break;
    default:
      return true;
  }
  if (!isLocalNetworkHost(url.hostname)) {
    return false;
  }
  if (!allowedOrigin) {
    return true;
  }
  let allowed: URL;
  try {
    allowed = new URL(allowedOrigin);
  } catch {
    return true;
  }
  const protocol = url.protocol === 'wss:' ? 'https:' : url.protocol;
  return !(
    protocol === allowed.protocol &&
    url.hostname === allowed.hostname &&
    url.port === allowed.port
  );
}

/** Permissions remote pages get without asking; everything else is denied. */
const ALWAYS_ALLOWED_PERMISSIONS: ReadonlySet<string> = new Set([
  'clipboard-sanitized-write',
  'fullscreen',
]);

const hardenedSessions = new WeakMap<object, RemoteSessionPolicy>();

/** Whether `ses` belongs to an SSH window (see hardenRemoteSession). */
export function isRemoteSession(ses: unknown): boolean {
  return typeof ses === 'object' && ses !== null && hardenedSessions.has(ses);
}

/** An SSH window, or anything else on an SSH session: every check uses this. */
export function isRemoteContents(
  contents: RemoteContentsLike | null | undefined,
): boolean {
  if (!contents) {
    return false;
  }
  // Fail closed: a destroyed contents' registration is gone.
  if (contents.isDestroyed?.()) {
    return true;
  }
  return !!remoteBackendOf(contents) || isRemoteSession(contents.session);
}

/**
 * Removes the session-wide preloads, which would run in its pages
 * ('session-created' listeners add them, e.g. electron-log's; legacy
 * setPreloads entries are listed too). Throws unless none remain.
 */
export function removeSessionPreloads(ses: RemotePreloadSession): void {
  if (
    typeof ses.getPreloadScripts !== 'function' ||
    typeof ses.unregisterPreloadScript !== 'function'
  ) {
    throw new Error('Cannot check the preload scripts of a session');
  }
  for (const script of ses.getPreloadScripts()) {
    ses.unregisterPreloadScript(script.id);
  }
  if (ses.getPreloadScripts().length > 0) {
    throw new Error('Could not remove the preload scripts of a session');
  }
}

/**
 * Hardens an SSH window's session (its proxy is set per connection, in
 * transport.ts). May be called again; the latest policy wins.
 */
export function hardenRemoteSession(
  ses: RemoteHardenedSession,
  policy: RemoteSessionPolicy,
): void {
  removeSessionPreloads(ses);
  refuseRemoteClientCertificates();
  const already = hardenedSessions.has(ses);
  hardenedSessions.set(ses, policy);
  if (already) {
    return;
  }
  const current = (): RemoteSessionPolicy =>
    hardenedSessions.get(ses) ?? policy;
  ses.webRequest.onBeforeRequest((details, callback) => {
    const p = current();
    let cancel = true;
    try {
      cancel = shouldBlockRemoteRequest(details.url, p.allowedOrigin());
    } catch {
      // Fail closed.
    }
    if (cancel) {
      try {
        p.onBlocked?.(details.url);
      } catch {
        // Only a log: the request must still get its verdict.
      }
    }
    callback({ cancel });
  });
  trustLoopbackCertificate(ses as Session);
  ses.setPermissionRequestHandler((_contents, permission, callback) => {
    callback(ALWAYS_ALLOWED_PERMISSIONS.has(permission));
  });
  ses.setPermissionCheckHandler((_contents, permission) =>
    ALWAYS_ALLOWED_PERMISSIONS.has(permission),
  );
  // No USB/HID/serial devices (Electron would otherwise ask the app).
  ses.setDevicePermissionHandler(() => false);
  ses.on('select-hid-device', (event, _details, callback) => {
    event.preventDefault();
    callback();
  });
  ses.on('select-serial-port', (event, _ports, _contents, callback) => {
    event.preventDefault();
    callback('');
  });
  ses.on('select-usb-device', (event, _details, callback) => {
    event.preventDefault();
    callback();
  });
}

let clientCertificatePolicyInstalled = false;

// SSH sessions send no client certificate: Electron would pick the store's
// first (e.g. a corp device certificate).
function refuseRemoteClientCertificates(): void {
  if (clientCertificatePolicyInstalled) {
    return;
  }
  clientCertificatePolicyInstalled = true;
  app.on(
    'select-client-certificate',
    (event, contents, _url, _list, callback) => {
      // Without a WebContents (service workers) the session is unknown: keep
      // the default, which local ones may need.
      if (isRemoteContents(contents)) {
        event.preventDefault();
        callback();
      }
    },
  );
}

/**
 * Opens a URL from an SSH window in the OS browser if it is a web URL off the
 * local network: the browser would send the user's cookies to local services.
 */
export function openRemoteUrl(url: unknown): RemoteUrlOpenResult {
  if (typeof url !== 'string' || !isRemoteOpenableUrl(url)) {
    return 'blocked';
  }
  void openExternalSafely(url, 'unconfirmed');
  return 'opened';
}

/**
 * An SSH window stays on its origin; other URLs go to openRemoteUrl, except
 * the Disconnected page's buttons (`onPageAction`, while it shows that page).
 */
export function remoteNavigationPolicy(
  allowedOrigin: () => string | undefined,
  onPageAction?: (action: DisconnectedPageAction) => void,
): RemoteNavigationPolicy {
  return {
    allowsNavigation: (url: string): boolean => {
      const origin = allowedOrigin();
      return !!origin && isSameOrigin(url, origin);
    },
    openOutside: (url: string, contents: DisconnectedPageContents): void => {
      if (isDisconnectedPageLink(url)) {
        const action = disconnectedPageAction(contents, url);
        if (action) {
          onPageAction?.(action);
        }
        return;
      }
      openRemoteUrl(url);
    },
  };
}

/** Hardens an SSH window (its navigation policy is set at creation). */
export function hardenRemoteWindow(win: RemoteHardenedWindow): void {
  // A window that is gone shows nothing (and its webContents throws).
  if (win.isDestroyed?.() || win.webContents?.isDestroyed?.()) {
    return;
  }
  const contents = win.webContents;
  contents.on('will-attach-webview', (event) => {
    event.preventDefault();
  });
  // beforeunload must not keep the window open, or off the Disconnected page.
  contents.on('will-prevent-unload', (event) => {
    event.preventDefault();
  });
  // Bluetooth pickers auto-select the first device when unhandled.
  contents.on('select-bluetooth-device', (event, _devices, callback) => {
    event.preventDefault();
    callback('');
  });
  // No UDP, which the SOCKS proxy can't carry (TCP goes through it). Chromium
  // still resolves TURN host names locally: a DNS lookup, no connection.
  contents.setWebRTCIPHandlingPolicy('disable_non_proxied_udp');
}
