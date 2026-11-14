/**
 * ssh's prompts on a Hub window: a modal window for secrets, sheets for yes/no
 * questions and security key touches.
 */
import {
  type BaseWindow,
  BrowserWindow,
  dialog,
  type IpcMainInvokeEvent,
  session,
} from 'electron';
import log from 'electron-log/main';
import * as path from 'path';
import { isLightTheme, markWebviewPopupWindow } from '../utils';
import { removeSessionPreloads } from './remoteWindowSecurity';
import type { AskpassAnswer, AskpassPrompt, PromptKind } from './sshAskpass';

interface SecretPromptText {
  label: string;
  message: string;
  retry: boolean;
}

const LOGGED: Record<PromptKind, string> = {
  secret: 'asked for a password',
  confirm: 'asked a yes/no question',
  notice: 'asked for a security key touch',
};
const CANCEL: AskpassAnswer = { kind: 'cancel' };
/** In memory: the page stores nothing. */
const PROMPT_PARTITION = 'ssh-prompt';
/** The secret windows open now. */
const promptWindows = new Set<BrowserWindow>();
/** The Hub's colors (utils.ts's createWindow). */
const COLORS = {
  dark: { background: '#131313', text: '#FAFAFA' },
  light: { background: '#FAFAFA', text: '#383A42' },
} as const;

/** In the Hub's theme, not the OS's: the page has it as it first paints. */
const page = (dark: boolean): string => `<!doctype html>
<html${dark ? ' class="dark"' : ''}>
<head>
<meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">
<style>
:root { color-scheme: light; background: ${COLORS.light.background};
  color: ${COLORS.light.text}; font: 13px system-ui, sans-serif; }
:root.dark { color-scheme: dark; background: ${COLORS.dark.background};
  color: ${COLORS.dark.text}; }
body { margin: 0; padding: 16px 20px; box-sizing: border-box; height: 100vh;
  display: flex; flex-direction: column; gap: 8px; }
#message { flex: 1; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; }
.note { color: #c5221f; }
.dark .note { color: #f28b82; }
input { font: inherit; padding: 4px 6px; }
.buttons { display: flex; justify-content: flex-end; gap: 8px; }
</style>
</head>
<body>
<strong id="asks"></strong>
<div id="message"></div>
<div id="retry" class="note" hidden>Asked again: your last answer may not have worked.</div>
<div id="long" class="note" hidden>That's too long.</div>
<input id="answer" type="password" maxlength="1023" autocomplete="off" aria-labelledby="asks">
<div class="buttons"><button id="cancel">Cancel</button><button id="continue">Continue</button></div>
</body>
</html>`;

/**
 * Destroys the open secret windows: their prompts end as Cancel. On quit, call
 * it before destroying the other windows: a modal child destroyed right after
 * its parent crashes Electron.
 */
export function destroyPromptWindows(): void {
  for (const win of promptWindows) {
    win.destroy();
  }
}

/**
 * Shows `prompt` on `parent`; a parent that's gone, or starts to close, means
 * Cancel. Rejects if the prompt can't show.
 */
export async function askOnWindow(
  parent: BaseWindow,
  label: string,
  { kind, message, retry, signal }: AskpassPrompt,
): Promise<AskpassAnswer> {
  log.info(`[SSH] ${label} ${LOGGED[kind]}.`);
  if (parent.isDestroyed() || signal.aborted) {
    return CANCEL;
  }
  // It needs an answer: a hidden (Cmd+H) or minimized parent comes back.
  if (parent.isMinimized()) {
    parent.restore();
  }
  parent.show();
  parent.focus();
  if (kind === 'secret') {
    const text = await askSecret(parent, { label, message, retry }, signal);
    return text === null ? CANCEL : { kind: 'secret', text };
  }
  // A message box may outlive its parent.
  const stop = new AbortController();
  const onStop = (): void => stop.abort();
  parent.once('closed', onStop);
  signal.addEventListener('abort', onStop);
  try {
    const confirm = kind === 'confirm';
    const { response } = await dialog.showMessageBox(parent, {
      type: confirm ? 'question' : 'info',
      title: `SSH: ${label}`,
      message: confirm ? `${label} asks:` : 'Touch your security key',
      detail: message,
      buttons: confirm ? ['Yes', 'No'] : ['Cancel'],
      defaultId: confirm ? 1 : 0,
      cancelId: confirm ? 1 : 0,
      // Also closes it once ssh no longer waits (e.g. after the touch).
      signal: stop.signal,
    });
    return confirm && !parent.isDestroyed()
      ? { kind: 'confirm', yes: response === 0 }
      : CANCEL;
  } finally {
    parent.off('closed', onStop);
    signal.removeEventListener('abort', onStop);
  }
}

function askSecret(
  parent: BaseWindow,
  text: SecretPromptText,
  signal: AbortSignal,
): Promise<string | null> {
  const ses = session.fromPartition(PROMPT_PARTITION);
  // Before the window: the app's session hooks add theirs to each session,
  // and a throw now leaves no window behind.
  removeSessionPreloads(ses);
  const dark = !isLightTheme();
  const win = new BrowserWindow({
    parent,
    modal: true,
    show: false,
    width: 440,
    height: 260,
    resizable: false,
    minimizable: false,
    maximizable: false,
    fullscreenable: false,
    title: `SSH: ${text.label}`,
    backgroundColor: COLORS[dark ? 'dark' : 'light'].background,
    webPreferences: {
      preload: path.join(__dirname, 'sshPromptPreload.js'),
      contextIsolation: true,
      sandbox: true,
      nodeIntegration: false,
      devTools: false,
      session: ses,
    },
  });
  promptWindows.add(win);
  win.once('closed', () => promptWindows.delete(win));
  // Not a Hub window: no Hub zoom, quit sheet or storage events.
  markWebviewPopupWindow(win);
  const contents = win.webContents;
  win.removeMenu();
  contents.setWindowOpenHandler(() => ({ action: 'deny' }));
  contents.on('will-navigate', (event) => event.preventDefault());
  return new Promise<string | null>((resolve, reject) => {
    let answer: string | null = null;
    let done = false;
    let failed = false;
    const finish = (value: string | null): void => {
      if (!done) {
        done = true;
        answer = value;
        if (!win.isDestroyed()) {
          win.close();
        }
      }
    };
    const cancel = (): void => finish(null);
    // As its parent starts to close: a modal child closed during or after its
    // parent's teardown crashes Electron.
    const destroy = (): void => {
      done = true;
      if (!win.isDestroyed()) {
        win.destroy();
      }
    };
    // Handlers on the window's own ipc: invokes, unlike sends, go no further.
    contents.ipc.handle(
      'ssh-prompt:submit',
      (_event: IpcMainInvokeEvent, value: unknown) =>
        finish(typeof value === 'string' ? value : null),
    );
    contents.ipc.handle('ssh-prompt:cancel', cancel);
    // Also after a reload, which empties the page.
    contents.on('did-finish-load', () => {
      contents.send('ssh-prompt:show', text);
      win.show();
    });
    signal.addEventListener('abort', cancel);
    parent.once('close', destroy);
    win.once('closed', () => {
      signal.removeEventListener('abort', cancel);
      parent.off('close', destroy);
      if (failed) {
        reject(new Error("The ssh prompt window didn't load."));
      } else {
        resolve(answer);
      }
    });
    win
      .loadURL(`data:text/html;charset=utf-8,${encodeURIComponent(page(dark))}`)
      .catch(() => {
        // It couldn't show: no answer, but no Cancel of the user's either.
        failed = !done;
        cancel();
      });
  });
}
