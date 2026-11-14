/**
 * Preload script for the IDE Install Wizard window.
 *
 * This is a minimal, self-contained preload that exposes only the APIs
 * needed by the wizard's inline HTML UI. It runs in its own
 * BrowserWindow, separate from the main app window and its preload.
 */
import { contextBridge, ipcRenderer } from 'electron';

export interface WizardAPI {
    completeWizard: (shouldDownload: boolean) => Promise<void>;
    onSetupComplete: (callback: () => void) => () => void;
}

const wizardAPI: WizardAPI = {
    completeWizard: (shouldDownload: boolean): Promise<void> =>
        ipcRenderer.invoke('wizard:complete', shouldDownload),
    onSetupComplete: (callback: () => void): (() => void) => {
        const handler = (): void => {
            callback();
        };
        ipcRenderer.on('wizard:setup-complete', handler);
        return (): void => {
            ipcRenderer.removeListener('wizard:setup-complete', handler);
        };
    },
};

contextBridge.exposeInMainWorld('wizardAPI', wizardAPI);
