/**
 * Shared test helpers and utilities.
 *
 * For module mocks (electron, electron-updater), use the auto-mock files
 * in `src/__mocks__/` instead. This file is for runtime helpers that
 * are called in beforeEach/afterEach blocks.
 */
import { vi } from 'vitest';
import { DYNAMIC_PORT, WINDOW_ORIGIN } from '../constants';

export const DEFAULT_WINDOW_URL = `${WINDOW_ORIGIN}:${DYNAMIC_PORT}/`;

/**
 * Silence console output during tests. Call in `beforeEach`.
 * Restoring is handled by `vi.restoreAllMocks()` in `afterEach`.
 */
export function silenceConsole(): void {
  vi.spyOn(console, 'log').mockImplementation(() => {});
  vi.spyOn(console, 'warn').mockImplementation(() => {});
  vi.spyOn(console, 'error').mockImplementation(() => {});
}
