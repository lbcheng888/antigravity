/**
 * Per-host stores for SSH windows, so UI state never leaks between backends or
 * hosts. The app settings stay in the main store, where SettingsService acts.
 */
import * as path from 'path';
import { SettingKey } from '../services/settingsService';
import {
  StorageFullError,
  StorageManager,
  type StorageChanges,
  type StorageItems,
} from '../storage';
import {
  isLocalAppWindow,
  remoteBackendOf,
  remoteBackendOfWindow,
  type AppWindowLike,
  type WebContentsLike,
} from './windowBackends';

const APP_SETTINGS = new Set<string>(Object.values(SettingKey));

/** Size limits (UTF-8 bytes) of a host store's entries and of the store. */
export const MAX_HOST_ENTRY_BYTES = 256 * 1024;
export const MAX_HOST_STORE_BYTES = 1024 * 1024;

/**
 * Storage calls per SSH window, at once and then per second: each reads (or
 * rewrites) a whole store on the main thread. The toolkit stays far below.
 */
export const STORAGE_CALL_BURST = 20;
export const STORAGE_CALLS_PER_SECOND = 10;

/** Local windows hear all main-store changes; others only the app settings. */
export const mainStoreScope = (
  win: AppWindowLike,
  changes: StorageChanges,
): StorageChanges | null => {
  if (isLocalAppWindow(win)) {
    return changes;
  }
  const app = Object.entries(changes).filter(([key]) => APP_SETTINGS.has(key));
  return app.length > 0 ? Object.fromEntries(app) : null;
};

export function hostStoreScope(
  hostId: string,
): (win: AppWindowLike, changes: StorageChanges) => StorageChanges | null {
  return (win, changes) =>
    remoteBackendOfWindow(win)?.hostId === hostId ? changes : null;
}

/** hostIdFor's output: the store's file name is built from it. */
const HOST_ID_RE = /^[a-f0-9]{12}$/;

export interface HostMainStore {
  getItems(): Promise<StorageItems>;
  updateItems(changes: StorageChanges): Promise<void>;
}

export interface HostSenderStore {
  getItems(): Promise<StorageItems>;
  updateItems(changes: Record<string, unknown>): Promise<void>;
}

interface CallBudget {
  calls: number;
  at: number;
}

export class HostStores {
  private readonly stores = new Map<string, StorageManager>();
  /** The storage calls each SSH window has left (a token bucket). */
  private readonly budgets = new WeakMap<WebContentsLike, CallBudget>();

  constructor(
    private readonly dir: string,
    private readonly main: HostMainStore,
  ) {}

  /** The store of `hostId` (see sshDestination.ts's hostIdFor). */
  get(hostId: string): StorageManager {
    if (!HOST_ID_RE.test(hostId)) {
      throw new Error(`Invalid SSH host id: ${JSON.stringify(hostId)}`);
    }
    let store = this.stores.get(hostId);
    if (!store) {
      store = new StorageManager(
        path.join(this.dir, `app_storage.ssh-${hostId}.json`),
        undefined,
        hostStoreScope(hostId),
        MAX_HOST_STORE_BYTES,
      );
      this.stores.set(hostId, store);
    }
    return store;
  }

  /** Counts a storage call of `sender`; throws when it has none left. */
  private spend(sender: WebContentsLike): void {
    const now = Date.now();
    const budget = this.budgets.get(sender) ?? {
      calls: STORAGE_CALL_BURST,
      at: now,
    };
    budget.calls = Math.min(
      STORAGE_CALL_BURST,
      budget.calls +
        (Math.max(0, now - budget.at) * STORAGE_CALLS_PER_SECOND) / 1000,
    );
    budget.at = now;
    this.budgets.set(sender, budget);
    if (budget.calls < 1) {
      throw new Error('Too many storage calls');
    }
    budget.calls -= 1;
  }

  /**
   * An SSH window's store, also while disconnected: its host's, plus the main
   * store's app settings, which it may set only to 'true', 'false' or null,
   * never autoCheckForUpdates (else ignored). Undefined for other senders.
   */
  forSender(
    sender: WebContentsLike | null | undefined,
  ): HostSenderStore | undefined {
    const info = remoteBackendOf(sender);
    if (!sender || !info) {
      return undefined;
    }
    const host = this.get(info.hostId);
    return {
      getItems: async (): Promise<StorageItems> => {
        this.spend(sender);
        const [items, main] = await Promise.all([
          host.getItems(),
          this.main.getItems(),
        ]);
        // Main-store app settings win (an older host store may have its own).
        for (const key of APP_SETTINGS) {
          if (main[key] !== undefined) {
            items[key] = main[key];
          } else {
            delete items[key];
          }
        }
        return items;
      },
      updateItems: async (changes: Record<string, unknown>): Promise<void> => {
        this.spend(sender);
        const own: StorageChanges = {};
        const app: StorageChanges = {};
        let bytes = 0;
        for (const [key, value] of Object.entries(changes)) {
          // From the page: anything may come over IPC.
          if (value !== null && typeof value !== 'string') {
            throw new Error('Invalid storage value');
          }
          if (!APP_SETTINGS.has(key)) {
            const size =
              value === null
                ? 0
                : Buffer.byteLength(key) + Buffer.byteLength(value);
            bytes += size;
            // Refused before it is stringified or queued.
            if (size > MAX_HOST_ENTRY_BYTES || bytes > MAX_HOST_STORE_BYTES) {
              throw new StorageFullError();
            }
            own[key] = value;
          } else if (
            key !== SettingKey.AUTO_CHECK_FOR_UPDATES &&
            [null, 'true', 'false'].includes(value)
          ) {
            app[key] = value;
          }
        }
        // Independent: app settings may apply even if the host's entries fail.
        const updates: Promise<void>[] = [];
        if (Object.keys(app).length > 0) {
          updates.push(this.main.updateItems(app));
        }
        if (Object.keys(own).length > 0) {
          updates.push(host.updateItems(own));
        }
        try {
          await Promise.all(updates);
        } catch (err: unknown) {
          // Node's I/O errors name the laptop's files.
          throw err instanceof StorageFullError
            ? err
            : new Error("Couldn't save");
        }
      },
    };
  }
}
