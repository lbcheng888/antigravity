import { ipcRenderer, webFrame } from 'electron';

/** Synchronous guest-to-main IPC channel for human `window.prompt()` requests. */
export const HUMAN_PROMPT_SYNC_CHANNEL = 'webview:human-prompt-sync';

/** Symbol key registering the human fallback on an armed `GuestDialogController` prompt wrapper. */
export const SET_PROMPT_FALLBACK_SYMBOL_KEY = '__agy_set_prompt_fallback__';

const PROTECTED_IDP_EXACT_HOSTS: readonly string[] = [
  'accounts.google.com',
  'myaccount.google.com',
  'accounts.youtube.com',
  'gds.google.com',
  'login.microsoftonline.com',
  'login.live.com',
  'appleid.apple.com',
];

const PROTECTED_IDP_WILDCARD_SUFFIXES: readonly string[] = [
  '.okta.com',
  '.oktapreview.com',
  '.okta-emea.com',
  '.auth0.com',
];

const PROTECTED_IDP_PUBLIC_HOSTS: readonly string[] = [
  'developer.okta.com',
  'docs.okta.com',
  'help.okta.com',
  'support.okta.com',
  'status.okta.com',
  'docs.auth0.com',
  'developer.auth0.com',
  'help.auth0.com',
  'support.auth0.com',
  'community.auth0.com',
  'status.auth0.com',
];

interface InspectableWindowLike {
  parent?: unknown;
  top?: unknown;
  location?: { href?: unknown } | null;
  origin?: unknown;
}

/** Minimal message event shape exchanged between the main and isolated worlds. */
export interface PromptBridgeMessageEvent {
  data?: unknown;
  stopImmediatePropagation?: () => void;
}

/** Window surface required by {@link runWebviewPromptMainWorldBootstrap}. */
export interface MainWorldPromptWindow {
  Object?: typeof Object;
  Function?: typeof Function;
  String?: typeof String;
  MessageEvent?: new (
    type: string,
    eventInitDict?: { data?: unknown },
  ) => unknown;
  addEventListener(
    type: string,
    listener: (ev: PromptBridgeMessageEvent) => void,
    options?: boolean | AddEventListenerOptions,
  ): void;
  removeEventListener(
    type: string,
    listener: (ev: PromptBridgeMessageEvent) => void,
    options?: boolean | EventListenerOptions,
  ): void;
  dispatchEvent(event: unknown): boolean;
  prompt?: unknown;
}

/** Minimal `webFrame` surface required by {@link installWebviewPromptBridge}. */
export interface WebviewPromptFrame {
  executeJavaScript(
    code: string,
    userGesture?: boolean,
  ): Promise<unknown> | unknown;
}

/** Minimal window surface required by {@link installWebviewPromptBridge}. */
export interface WebviewPromptWindow {
  parent?: unknown;
  top?: unknown;
  location?: { href?: unknown } | null;
  origin?: unknown;
  addEventListener(
    type: string,
    listener: (event: PromptBridgeMessageEvent) => void,
    options?: boolean | AddEventListenerOptions,
  ): void;
  dispatchEvent(event: unknown): boolean;
}

/** Options accepted by {@link installWebviewPromptBridge}. */
export interface InstallWebviewPromptBridgeOptions {
  frame?: WebviewPromptFrame | null;
  win?: WebviewPromptWindow | null;
  sendSyncPrompt?: (rawPayload: string) => unknown;
  createResponseEvent?: (type: string, data: string) => unknown;
}

function extractNormalizedHostname(rawUrl?: string | null): string {
  let current = (rawUrl || '').trim();
  for (let depth = 0; depth < 8; depth++) {
    if (!current || current === 'null' || /^about:/i.test(current)) {
      return '';
    }
    try {
      const outer = new URL(current);
      const parsed =
        outer.protocol === 'blob:' ? new URL(outer.pathname) : outer;
      const host = parsed.hostname
        .replace(/^\[|\]$/g, '')
        .toLowerCase()
        .replace(/\.+$/, '')
        .replace(/^www\./, '');
      if (
        parsed.protocol === 'https:' &&
        host === 'uberproxy-pen-redirect.corp.google.com' &&
        parsed.pathname === '/uberproxy/pen'
      ) {
        const target = parsed.searchParams.get('url');
        if (target && /^https?:\/\//i.test(target) && target !== current) {
          current = target;
          continue;
        }
      }
      return host;
    } catch {
      return '';
    }
  }
  return '';
}

/**
 * Returns `true` when `rawUrl` resolves to a protected Identity Provider host
 * where main-world script injection must not run.
 */
export function isProtectedIdentityProviderUrl(
  rawUrl?: string | null,
): boolean {
  const host = extractNormalizedHostname(rawUrl);
  if (
    !host ||
    PROTECTED_IDP_PUBLIC_HOSTS.some(
      (p) => host === p || host.endsWith(`.${p}`),
    )
  ) {
    return false;
  }
  return (
    PROTECTED_IDP_EXACT_HOSTS.some(
      (d) => host === d || host.endsWith(`.${d}`),
    ) || PROTECTED_IDP_WILDCARD_SUFFIXES.some((s) => host.endsWith(s))
  );
}

function isProtectedIdentityProviderWindow(win: unknown): boolean {
  const readProtectedField = (getter: () => unknown): boolean => {
    try {
      const val = getter();
      return typeof val === 'string' && isProtectedIdentityProviderUrl(val);
    } catch {
      return false;
    }
  };
  const frameWin = win as InspectableWindowLike | null | undefined;
  for (const candidate of [frameWin, frameWin?.parent, frameWin?.top]) {
    if (!candidate || typeof candidate !== 'object') {
      continue;
    }
    const w = candidate as InspectableWindowLike;
    if (
      readProtectedField(() => w.location?.href) ||
      readProtectedField(() => w.origin)
    ) {
      return true;
    }
  }
  return false;
}

/**
 * Self-contained main-world bootstrap replacing Electron's unsupported
 * `window.prompt` stub with a synchronous human prompt bridge while leaving
 * native `window.alert` and `window.confirm` untouched.
 */
export function runWebviewPromptMainWorldBootstrap(
  winRoot: MainWorldPromptWindow | null | undefined,
  requestEventType: string,
  responseEventType: string,
  setFallbackSymbolKey: string,
): void {
  if (!winRoot || !winRoot.MessageEvent) {
    return;
  }
  const setFallbackSym = Symbol.for(setFallbackSymbolKey);
  const defProp = (winRoot.Object || Object).defineProperty;
  const fnBind = (winRoot.Function || Function).prototype.bind;
  const Str = winRoot.String || String;
  const toJson = JSON.stringify;
  const parseJson = JSON.parse;
  const MsgEv = winRoot.MessageEvent;
  const addEL = winRoot.addEventListener.bind(winRoot);
  const removeEL = winRoot.removeEventListener.bind(winRoot);
  const dispatch = winRoot.dispatchEvent.bind(winRoot);

  function prompt(message?: unknown, defaultValue?: unknown): string | null {
    const msg =
      arguments.length > 0 && message !== undefined ? Str(message) : '';
    const def =
      arguments.length > 1 && defaultValue !== undefined
        ? Str(defaultValue)
        : '';
    let result: string | null = null;
    const onResponse = (ev?: PromptBridgeMessageEvent | null): void => {
      ev?.stopImmediatePropagation?.();
      try {
        const rawData = ev?.data;
        const parsed: unknown = parseJson(
          typeof rawData === 'string' ? rawData : 'null',
        );
        result = typeof parsed === 'string' ? parsed : null;
      } catch {
        result = null;
      }
    };
    addEL(responseEventType, onResponse, { capture: true, once: true });
    try {
      dispatch(
        new MsgEv(requestEventType, {
          data: toJson({ message: msg, defaultValue: def }),
        }),
      );
    } finally {
      removeEL(responseEventType, onResponse, { capture: true });
    }
    return result;
  }

  const nonConstructible = (
    ...args: Parameters<typeof prompt>
  ): string | null => prompt(...args);
  const nativePrompt = defProp(fnBind.call(nonConstructible, null), 'name', {
    value: 'prompt',
    configurable: true,
  }) as unknown;

  const registerFallback = (
    winRoot.prompt as Record<symbol, unknown> | null | undefined
  )?.[setFallbackSym];
  if (typeof registerFallback === 'function') {
    (registerFallback as (fallback: unknown) => void)(nativePrompt);
    return;
  }

  defProp(winRoot, 'prompt', {
    value: nativePrompt,
    writable: true,
    enumerable: true,
    configurable: true,
  });
}

const installedPromptTargets = new WeakSet<object>();

/** Installs the isolated-world synchronous `window.prompt()` relay in a guest frame. */
export function installWebviewPromptBridge(
  options?: InstallWebviewPromptBridgeOptions,
): boolean {
  const frame: WebviewPromptFrame | null | undefined =
    options?.frame !== undefined ? options.frame : webFrame;
  const win: WebviewPromptWindow | null =
    options?.win !== undefined
      ? options.win
      : ((globalThis as unknown as { window?: WebviewPromptWindow | null })
          .window ?? null);
  if (
    !frame ||
    typeof frame.executeJavaScript !== 'function' ||
    !win ||
    typeof win.addEventListener !== 'function' ||
    typeof win.dispatchEvent !== 'function' ||
    installedPromptTargets.has(win) ||
    isProtectedIdentityProviderWindow(win)
  ) {
    return false;
  }
  installedPromptTargets.add(win);

  const rand = (): string =>
    Math.random().toString(36).slice(2) + Date.now().toString(36);
  const reqType = `__agy_hpr_${rand()}`;
  const resType = `__agy_hps_${rand()}`;

  const sendSync =
    options?.sendSyncPrompt ??
    ((raw: string): unknown =>
      ipcRenderer.sendSync(HUMAN_PROMPT_SYNC_CHANNEL, raw));
  const createRes =
    options?.createResponseEvent ??
    ((type: string, data: string): MessageEvent =>
      new MessageEvent(type, { data }));

  win.addEventListener(
    reqType,
    (event: PromptBridgeMessageEvent) => {
      event.stopImmediatePropagation?.();
      const rawData = event?.data;
      let reply: unknown = null;
      try {
        reply = sendSync(typeof rawData === 'string' ? rawData : '{}');
      } catch {
        reply = null;
      }
      const normalized = typeof reply === 'string' ? reply : null;
      win.dispatchEvent(createRes(resType, JSON.stringify(normalized)));
    },
    { capture: true },
  );

  try {
    Promise.resolve(
      frame.executeJavaScript(
        `(${runWebviewPromptMainWorldBootstrap.toString()})(typeof window!=='undefined'?window:undefined,${JSON.stringify(reqType)},${JSON.stringify(resType)},${JSON.stringify(SET_PROMPT_FALLBACK_SYMBOL_KEY)});`,
      ),
    ).catch(() => undefined);
    return true;
  } catch {
    return false;
  }
}

installWebviewPromptBridge();
