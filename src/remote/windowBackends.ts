// Which windows show a remote (SSH) backend. Imports only Electron, so that
// utils, main and later modules can use it without import cycles.
import { BrowserWindow, session } from 'electron';

export interface RemoteBackendInfo {
  kind: 'ssh';
  hostId: string;
  label: string;
}

export interface WebContentsLike {
  id?: number;
}

export interface RemoteWindowWebContents {
  id: number;
  once?: (event: 'destroyed', listener: () => void) => unknown;
}

export interface RemoteWindowTarget {
  webContents: RemoteWindowWebContents;
  once(event: 'closed', listener: () => void): unknown;
}

export interface AppWindowWebContents {
  id?: number;
  session?: unknown;
  isDestroyed?: () => boolean;
}

export interface AppWindowLike {
  isDestroyed?: () => boolean;
  webContents?: AppWindowWebContents | null;
}

const byWebContentsId = new Map<number, RemoteBackendInfo>();

/** Marks `win` remote until it closes or its webContents is destroyed. */
export function registerRemoteWindow(
  win: RemoteWindowTarget,
  info: RemoteBackendInfo,
): void {
  const id = win.webContents.id;
  byWebContentsId.set(id, info);
  const forget = (): void => {
    byWebContentsId.delete(id);
  };
  win.once('closed', forget);
  win.webContents.once?.('destroyed', forget);
}

/** Remote backend of an IPC sender, or undefined for local windows. */
export function remoteBackendOf(
  contents: WebContentsLike | null | undefined,
): RemoteBackendInfo | undefined {
  // No isDestroyed() check: the entry goes on 'destroyed', and callers that
  // must fail closed for a destroyed sender handle that.
  if (!contents || typeof contents.id !== 'number') {
    return undefined;
  }
  return byWebContentsId.get(contents.id);
}

export function remoteBackendOfWindow(
  win: AppWindowLike,
): RemoteBackendInfo | undefined {
  // A closing window's webContents can be destroyed first; reading it throws.
  if (win.isDestroyed?.() || win.webContents?.isDestroyed?.()) {
    return undefined;
  }
  return remoteBackendOf(win.webContents);
}

export function windowsForHost(hostId: string): BrowserWindow[] {
  return BrowserWindow.getAllWindows().filter(
    (w) => remoteBackendOfWindow(w)?.hostId === hostId,
  );
}

/** Whether Hub window `win` is on the local backend (not an SSH window). */
export function isLocalAppWindow(win: AppWindowLike): boolean {
  if (win.isDestroyed?.() || win.webContents?.isDestroyed?.()) {
    return false;
  }
  // Local app windows use the default session; SSH windows have their own.
  const ses = win.webContents?.session;
  if (ses && session?.defaultSession && ses !== session.defaultSession) {
    return false;
  }
  // BaseWindows have no webContents, and only local windows are BaseWindows.
  const id = win.webContents?.id;
  return id === undefined || !byWebContentsId.has(id);
}
