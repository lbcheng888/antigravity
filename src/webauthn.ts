import {
  BrowserWindow,
  dialog,
  session,
  type MessageBoxOptions,
  type MessageBoxReturnValue,
  type PreloadScriptRegistration,
  type WebContents,
  type WebFrameMain,
} from 'electron';
import * as path from 'path';
import {
  recordWebviewUserActivation,
  registerCableWebAuthnIpcHandlers,
} from './cablev2';
import {
  getFocusedHubWindow,
  getHubWindows,
  resolveHubWindowFromWebContents,
  type ResolvableWebContents,
  type ResolvableWebContentsInput,
} from './utils';

/** Absolute path to the compiled `<webview>` WebAuthn frame preload script. */
export const WEBVIEW_WEBAUTHN_PRELOAD_PATH = path.join(
  __dirname,
  'webviewWebAuthnPreload.js',
);

const ELECTRON_LOG_PRELOAD_ID = 'electron-log-preload';
const ELECTRON_LOG_PRELOAD_FILE_RE = /(?:^|[\\/])electron-log-preload\.js$/i;

/** Minimal preload script metadata inspected when stripping `electron-log`. */
export interface WebAuthnPreloadScript {
  id: string;
  filePath?: string;
}

/** Minimal account fields formatted for the passkey chooser dialog. */
export interface WebAuthnAccountLabelInput {
  name?: string;
  displayName?: string;
}

/** Minimal discoverable WebAuthn credential entry from Electron's account prompt. */
export interface WebAuthnAccountLike extends WebAuthnAccountLabelInput {
  credentialId?: string;
  userHandle?: string;
}

/** Minimal `WebFrameMain` shape carrying an owning `webContents`. */
export interface WebAuthnFrameLike {
  webContents?: ResolvableWebContentsInput;
}

/** Frame values accepted when resolving the window for a WebAuthn prompt. */
export type WebAuthnFrameInput =
  | WebFrameMain
  | WebAuthnFrameLike
  | Record<string, unknown>
  | null
  | undefined;

/** Minimal `select-webauthn-account` event details surface. */
export interface WebAuthnSelectAccountDetails {
  relyingPartyId?: string;
  accounts?: readonly WebAuthnAccountLike[];
  frame?: WebAuthnFrameInput;
}

/** Minimal Electron `Session` surface configured for WebAuthn support. */
export interface WebAuthnSessionLike {
  on?(
    event: 'select-webauthn-account',
    listener: (
      event: unknown,
      details: WebAuthnSelectAccountDetails,
      callback: (credentialId?: string | null) => void,
    ) => void,
  ): unknown;
  getPreloadScripts?: () => ReadonlyArray<WebAuthnPreloadScript>;
  unregisterPreloadScript?: (id: string) => void;
  getPreloads?: () => readonly string[];
  setPreloads?: (preloads: string[]) => void;
  registerPreloadScript?: (script: PreloadScriptRegistration) => unknown;
}

/** Options accepted by {@link configurePlatformWebAuthn}. */
export interface ConfigurePlatformWebAuthnOptions {
  platform?: NodeJS.Platform | string;
  registerCableIpc?: () => void;
}

/** Minimal Electron `InputEvent` surface checked for user activation. */
export interface WebAuthnInputEventLike {
  type?: string;
}

/** Minimal `WebContents` surface tracked for `<webview>` user activation. */
export interface WebAuthnUserActivationContents {
  id: number;
  on?(event: 'before-input-event', listener: () => void): unknown;
  on?(
    event: 'input-event',
    listener: (event: unknown, inputEvent?: unknown) => void,
  ): unknown;
}

/** Minimal parent window surface used for the WebAuthn account chooser dialog. */
export interface WebAuthnWindowLike {
  isDestroyed?: () => boolean;
}

/** Options accepted by {@link createWebAuthnSessionConfigurator}. */
export interface CreateWebAuthnSessionConfiguratorOptions {
  defaultSession?: object | null;
  isHeadless?: () => boolean;
  getParentWindow?: () => WebAuthnWindowLike | null | undefined;
  showMessageBox?: (
    win: WebAuthnWindowLike,
    boxOptions: MessageBoxOptions,
  ) => Promise<Pick<MessageBoxReturnValue, 'response'>>;
}

/** Function returned by {@link createWebAuthnSessionConfigurator}. */
export type WebAuthnSessionConfigurator = (
  targetSession: WebAuthnSessionLike | null | undefined,
) => void;

/**
 * Removes any `electron-log` renderer preload script from a non-default
 * `<webview>` session so `<webview>` frames do not expose `window.__electronLog`.
 */
export function stripElectronLogPreloadFromSession(
  targetSession: WebAuthnSessionLike | null | undefined,
): void {
  if (!targetSession) {
    return;
  }
  if (
    typeof targetSession.getPreloadScripts === 'function' &&
    typeof targetSession.unregisterPreloadScript === 'function'
  ) {
    for (const script of targetSession.getPreloadScripts()) {
      if (
        script.id === ELECTRON_LOG_PRELOAD_ID ||
        ELECTRON_LOG_PRELOAD_FILE_RE.test(script.filePath ?? '')
      ) {
        targetSession.unregisterPreloadScript(script.id);
      }
    }
  } else if (
    typeof targetSession.getPreloads === 'function' &&
    typeof targetSession.setPreloads === 'function'
  ) {
    const current = targetSession.getPreloads();
    const filtered = current.filter(
      (filePath) => !ELECTRON_LOG_PRELOAD_FILE_RE.test(filePath),
    );
    if (filtered.length !== current.length) {
      targetSession.setPreloads(filtered);
    }
  }
}

/**
 * Configures macOS `caBLEv2` Hybrid Transport IPC handlers for WebAuthn
 * requests (`navigator.credentials.get`). No-op on non-macOS platforms.
 *
 * Note: macOS Touch ID / Secure Enclave (`app.configureWebAuthn`) is not
 * enabled because `keychain-access-groups` is a restricted Apple entitlement
 * enforced by AMFI against an embedded `.provisionprofile`, which terminates
 * Developer-ID-signed desktop distributions that do not bundle a provisioning
 * profile.
 */
export function configurePlatformWebAuthn(
  options?: ConfigurePlatformWebAuthnOptions,
): void {
  const platform = options?.platform ?? process.platform;
  if (platform !== 'darwin') {
    return;
  }
  const registerCableIpc =
    options?.registerCableIpc ?? registerCableWebAuthnIpcHandlers;
  registerCableIpc();
}

/**
 * Attaches user-activation listeners (`before-input-event` and `input-event`)
 * to a `<webview>` guest `WebContents` so main-process `caBLEv2` IPC handlers
 * can enforce recent user interaction before presenting the passkey QR modal.
 */
export function attachWebviewWebAuthnUserActivationTracking(
  contents: WebAuthnUserActivationContents | null | undefined,
): void {
  if (!contents || typeof contents.on !== 'function') {
    return;
  }
  contents.on('before-input-event', () => {
    recordWebviewUserActivation(contents.id);
  });
  contents.on('input-event', (_event, inputEvent) => {
    const type = (inputEvent as WebAuthnInputEventLike | null | undefined)
      ?.type;
    if (
      !type ||
      type === 'mouseDown' ||
      type === 'mouseUp' ||
      type === 'rawKeyDown' ||
      type === 'keyDown' ||
      type === 'char' ||
      type === 'gestureTap'
    ) {
      recordWebviewUserActivation(contents.id);
    }
  });
}

/**
 * Formats a human-readable label for a discoverable WebAuthn credential in the
 * account selection dialog.
 */
export function formatWebAuthnAccountLabel(
  account: WebAuthnAccountLabelInput,
  index: number,
): string {
  const name = account.name?.trim();
  const displayName = account.displayName?.trim();
  if (displayName && name && displayName !== name) {
    return `${displayName} (${name})`;
  }
  if (name) {
    return name;
  }
  if (displayName) {
    return displayName;
  }
  return `Account ${index + 1}`;
}

/**
 * Resolves the owning `BrowserWindow` for a `select-webauthn-account` event's
 * originating `WebFrameMain`, traversing `<webview>` `hostWebContents` when
 * the request originates inside an embedded `<webview>`.
 */
function resolveWindowFromWebAuthnFrame(
  frame: WebAuthnFrameInput,
): BrowserWindow | null {
  if (!frame || typeof frame !== 'object') {
    return null;
  }
  const frameContents = (frame as WebAuthnFrameLike).webContents;
  if (!frameContents) {
    return null;
  }
  const resolved = resolveHubWindowFromWebContents(frameContents);
  if (resolved) {
    return resolved;
  }
  const hostContents =
    (frameContents as ResolvableWebContents).hostWebContents ?? frameContents;
  if (typeof BrowserWindow?.fromWebContents === 'function') {
    return (
      BrowserWindow.fromWebContents(hostContents as WebContents) ??
      BrowserWindow.fromWebContents(frameContents as WebContents) ??
      null
    );
  }
  return null;
}

/**
 * Creates an idempotent `Session` configurator that registers a
 * `'select-webauthn-account'` listener and (for non-default `<webview>`
 * sessions) registers the frame WebAuthn preload script at most once per
 * `Session` instance.
 */
export function createWebAuthnSessionConfigurator(
  options?: CreateWebAuthnSessionConfiguratorOptions,
): WebAuthnSessionConfigurator {
  const configuredSessions = new WeakSet<object>();
  const isHeadless =
    options?.isHeadless ??
    (() => process.env.ELECTRON_OZONE_PLATFORM_HINT === 'headless');
  const getParentWindow =
    options?.getParentWindow ??
    (() => {
      return getFocusedHubWindow() ?? getHubWindows()[0] ?? null;
    });
  const showMessageBox =
    options?.showMessageBox ??
    ((win, boxOptions) =>
      dialog.showMessageBox(win as BrowserWindow, boxOptions));

  return (targetSession) => {
    if (!targetSession || typeof targetSession.on !== 'function') {
      return;
    }
    const resolvedDefaultSession =
      options?.defaultSession !== undefined
        ? options.defaultSession
        : session?.defaultSession;
    if (resolvedDefaultSession !== targetSession) {
      stripElectronLogPreloadFromSession(targetSession);
    }
    if (configuredSessions.has(targetSession)) {
      return;
    }
    configuredSessions.add(targetSession);
    if (
      resolvedDefaultSession !== targetSession &&
      typeof targetSession.registerPreloadScript === 'function'
    ) {
      try {
        targetSession.registerPreloadScript({
          type: 'frame',
          filePath: WEBVIEW_WEBAUTHN_PRELOAD_PATH,
        });
      } catch (err) {
        console.warn(
          '[WebAuthn] Failed to register <webview> WebAuthn preload script:',
          err,
        );
      }
    }
    targetSession.on(
      'select-webauthn-account',
      (_event, details, callback) => {
        // Electron leaves the WebAuthn assertion pending until `callback` is
        // invoked and cancels with NotAllowedError if no listener responds.
        let settled = false;
        const respondOnce = (credentialId?: string | null): void => {
          if (settled) {
            return;
          }
          settled = true;
          callback(credentialId ?? null);
        };
        void (async () => {
          let selectedCredentialId: string | null | undefined = null;
          try {
            const accounts = details?.accounts ?? [];
            if (accounts.length === 0) {
              selectedCredentialId = null;
              return;
            }
            if (accounts.length === 1) {
              selectedCredentialId = accounts[0].credentialId;
              return;
            }
            if (isHeadless()) {
              selectedCredentialId = accounts[0].credentialId;
              return;
            }
            const win =
              resolveWindowFromWebAuthnFrame(details?.frame) ??
              getParentWindow();
            if (!win || win.isDestroyed?.()) {
              selectedCredentialId = null;
              return;
            }
            const accountLabels = accounts.map((acc, idx) =>
              formatWebAuthnAccountLabel(acc, idx),
            );
            const cancelIndex = accountLabels.length;
            const rpLabel = details.relyingPartyId || 'this site';
            const result = await showMessageBox(win, {
              type: 'question',
              title: 'Choose a Passkey',
              message: `Sign in to ${rpLabel}`,
              detail: 'Select a passkey account to continue:',
              buttons: [...accountLabels, 'Cancel'],
              defaultId: 0,
              cancelId: cancelIndex,
            });
            if (result.response >= 0 && result.response < accounts.length) {
              selectedCredentialId = accounts[result.response].credentialId;
            } else {
              selectedCredentialId = null;
            }
          } catch (err) {
            console.warn('[WebAuthn] Failed to select WebAuthn account:', err);
            selectedCredentialId = null;
          } finally {
            respondOnce(selectedCredentialId);
          }
        })();
      },
    );
  };
}

/**
 * Default `Session` WebAuthn configurator used by the main process.
 */
export const configureSessionWebAuthn: WebAuthnSessionConfigurator =
  createWebAuthnSessionConfigurator();
