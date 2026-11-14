/**
 * The local page an SSH window shows while disconnected (so it makes no
 * requests without a tunnel). A script-less data: URL whose buttons link to
 * `https://agy-ssh.invalid/<action>?page=<token>`: the navigation policy
 * catches them in will-navigate and honors only the shown page's token, so a
 * remote page can't press them. (Chromium blocks a data: page's #fragment
 * links.)
 */

import * as crypto from 'crypto';

export type DisconnectedPageState = 'connecting' | 'disconnected';
export type DisconnectedPageAction = 'reconnect' | 'close';

export interface DisconnectedPage {
  state: DisconnectedPageState;
  label: string;
  detail?: string;
  dark?: boolean;
}

export interface DisconnectedPageContents {
  getURL(): string;
  isDestroyed?(): boolean;
}

export interface DisconnectedPageWindow {
  webContents: DisconnectedPageContents;
  loadURL(url: string): Promise<unknown>;
  isDestroyed?(): boolean;
}

/** Origin of the buttons' links; `.invalid` names never resolve. */
const ACTION_ORIGIN = 'https://agy-ssh.invalid';

const HTML_ESCAPES: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
};

function escapeHtml(text: string): string {
  return text.replace(/[&<>"']/g, (c) => HTML_ESCAPES[c] ?? c);
}

/** Returns the data: URL of the page; `token` goes into its buttons. */
export function disconnectedPageUrl(
  page: DisconnectedPage,
  token: string,
): string {
  const bg = page.dark ? '#131313' : '#FAFAFA';
  const fg = page.dark ? '#F5F5F5' : '#202124';
  const sub = page.dark ? '#A0A0A0' : '#5F6368';
  const line = page.dark ? '#3C4043' : '#DADCE0';
  const host = escapeHtml(page.label);
  const connecting = page.state === 'connecting';
  const title = connecting ? 'Reconnecting…' : 'Disconnected';
  const heading = connecting
    ? `Reconnecting to ${host}…`
    : `Disconnected from ${host}`;
  const details = connecting
    ? []
    : (page.detail ?? '').split('\n').filter((l) => l.trim());
  const body = connecting
    ? `Agents keep running on ${host}.`
    : `Agents keep running on ${host} unless its server stopped.`;
  const action = (name: DisconnectedPageAction): string =>
    `${ACTION_ORIGIN}/${name}?page=${encodeURIComponent(token)}`;
  const spinner = connecting ? '<div class="spinner"></div>' : '';
  const reconnect = connecting
    ? ''
    : `<a class="button primary" href="${action('reconnect')}">Reconnect</a>`;
  const html = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">
<title>${title}</title>
<style>
  html, body { height: 100%; margin: 0; }
  body {
    display: flex; flex-direction: column; align-items: center; justify-content: center;
    gap: 14px; background: ${bg}; color: ${fg}; text-align: center; padding: 0 32px;
    font: 14px system-ui, -apple-system, 'Segoe UI', sans-serif; -webkit-app-region: drag;
  }
  h1 { font-size: 18px; font-weight: 600; margin: 0; }
  p { margin: 0; max-width: 520px; color: ${sub}; line-height: 1.5; overflow-wrap: anywhere; }
  .buttons { display: flex; gap: 8px; margin-top: 6px; }
  .button {
    -webkit-app-region: no-drag; padding: 6px 16px; border-radius: 6px;
    border: 1px solid ${line}; color: ${fg}; text-decoration: none;
  }
  .button.primary { background: ${fg}; border-color: ${fg}; color: ${bg}; }
  .spinner {
    width: 22px; height: 22px; border: 2px solid ${sub}; border-top-color: ${fg};
    border-radius: 50%; animation: spin 1s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
  @media (prefers-reduced-motion: reduce) { .spinner { animation: none; } }
</style>
</head>
<body>
  ${spinner}
  <h1>${heading}</h1>
  ${details.map((d) => `<p>${escapeHtml(d)}</p>`).join('\n  ')}
  <p>${body}</p>
  <div class="buttons">${reconnect}<a class="button" href="${action('close')}">Close</a></div>
</body>
</html>`;
  return `data:text/html;charset=utf-8,${encodeURIComponent(html)}`;
}

/** The token of the page last loaded into each window's contents. */
const pageTokens = new WeakMap<DisconnectedPageContents, string>();

/** Loads the page with a fresh token, unless the window is gone. */
export function showDisconnectedPage(
  win: DisconnectedPageWindow,
  page: DisconnectedPage,
): void {
  if (win.isDestroyed?.() || win.webContents?.isDestroyed?.()) {
    return;
  }
  const token = crypto.randomBytes(16).toString('hex');
  pageTokens.set(win.webContents, token);
  // ERR_ABORTED when the window closes or navigates during the load.
  win.loadURL(disconnectedPageUrl(page, token)).catch(() => {});
}

/** Whether `url` is a button link of the page (never to be opened). */
export function isDisconnectedPageLink(url: string): boolean {
  try {
    return new URL(url).origin === ACTION_ORIGIN;
  } catch {
    return false;
  }
}

/**
 * The button `url` presses, if its token is that of the page `contents` shows
 * (on a data: URL, which only the Hub's page can be).
 */
export function disconnectedPageAction(
  contents: DisconnectedPageContents,
  url: string,
): DisconnectedPageAction | undefined {
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return undefined;
  }
  const token = pageTokens.get(contents);
  if (
    parsed.origin !== ACTION_ORIGIN ||
    !token ||
    parsed.searchParams.get('page') !== token ||
    contents.isDestroyed?.() ||
    !contents.getURL().startsWith('data:')
  ) {
    return undefined;
  }
  switch (parsed.pathname) {
    case '/reconnect':
      return 'reconnect';
    case '/close':
      return 'close';
    default:
      return undefined;
  }
}
