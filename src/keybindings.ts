import type { BrowserWindow, Event, Input } from 'electron';
import { getHubWebContents, isMacOS } from './utils';

export interface KeybindingActions {
  createNewWindow: () => void;
  onQuitRequested: () => void;
}

export function registerKeybindings(
  win: BrowserWindow,
  actions: KeybindingActions,
): void {
  const hubContents = getHubWebContents(win) ?? win.webContents;
  const handleBeforeInput = (event: Event, input: Input): void => {
    if (win.isDestroyed?.()) {
      return;
    }
    if (input.type === 'keyDown') {
      const isCmdOrCtrl = isMacOS() ? input.meta : input.control;
      if (isCmdOrCtrl && input.shift && input.key.toLowerCase() === 'n') {
        actions.createNewWindow();
        event.preventDefault();
      }
      if (isCmdOrCtrl && input.key.toLowerCase() === 'q') {
        actions.onQuitRequested();
        event.preventDefault();
      }
    }
  };
  hubContents?.on('before-input-event', handleBeforeInput);
  win.once?.('closed', () => {
    hubContents?.off?.('before-input-event', handleBeforeInput);
  });
}
