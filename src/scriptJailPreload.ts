/**
 * Minimal preload script for the ephemeral Electron script jail.
 *
 * Runs in an isolated preload context inside the sandboxed `about:blank`
 * `WebContentsView`. Its sole responsibility is to receive the single
 * `MessagePortMain` transferred from the main process on channel
 * `'agy-script-jail:port'` and forward it to the jail's main world via
 * `window.postMessage`. No `contextBridge` APIs or Node/Electron capabilities
 * are exposed.
 */
import { ipcRenderer, type IpcRendererEvent } from 'electron';

let pendingPorts: MessagePort[] | null = null;
let mainWorldReady = false;

function tryFlushPort(): void {
  if (!pendingPorts || !mainWorldReady) {
    return;
  }
  const ports = pendingPorts;
  pendingPorts = null;
  window.postMessage({ type: 'agy-script-jail-port' }, '*', ports);
}

window.addEventListener('message', (event: MessageEvent<unknown>) => {
  const data: unknown = event?.data;
  if (
    typeof data === 'object' &&
    data !== null &&
    'type' in data &&
    data.type === 'agy-script-jail-ready'
  ) {
    mainWorldReady = true;
    tryFlushPort();
  }
});

ipcRenderer.on('agy-script-jail:port', (event: IpcRendererEvent) => {
  pendingPorts = event.ports;
  tryFlushPort();
});
