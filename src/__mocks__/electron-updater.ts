/**
 * Shared electron-updater mock for all test files.
 *
 * This file is automatically used by Vitest when a test calls
 * `vi.mock('electron-updater')` without a factory argument.
 *
 * The `autoUpdaterEvents` export allows tests to trigger event callbacks
 * that were registered via `autoUpdater.on(event, callback)`.
 */
import { vi } from 'vitest';

export const autoUpdaterEvents: Record<string, (...args: unknown[]) => void> = {};

export const autoUpdater = {
  autoDownload: false,
  autoInstallOnAppQuit: false,
  forceDevUpdateConfig: false,
  updateConfigPath: '',
  on: vi.fn().mockImplementation((event: string, cb: (...args: unknown[]) => void): void => {
    autoUpdaterEvents[event] = cb;
  }),
  checkForUpdates: vi.fn().mockResolvedValue(undefined),
  quitAndInstall: vi.fn(),
};
