import * as fs from 'fs/promises';
import { existsSync, readFileSync } from 'fs';
import * as path from 'path';
import { EventEmitter } from 'events';
import type { BrowserWindow } from 'electron';
import { getHubWebContents, getHubWindows } from './utils';

export type StorageItems = Record<string, string>;
export type StorageChanges = Record<string, string | null>;
export type StorageDefaults = ReadonlyMap<string, unknown>;
export type StorageScope = (
  win: BrowserWindow,
  changes: StorageChanges,
) => StorageChanges | null | undefined;
export type StorageChangeListener = (changes: StorageChanges) => void;

export interface StorageDisposable {
  dispose: () => void;
}

interface RawStorageReadResult {
  items: StorageItems;
  corrupt: boolean;
}

/** Refuses an update too large for a store's limits (e.g. its maxBytes). */
export class StorageFullError extends Error {
  constructor() {
    super('Storage is full');
    this.name = 'StorageFullError';
  }
}

/**
 * Error codes with which rename can refuse to replace an existing file, e.g.
 * on Windows while another program has it open.
 */
const REPLACE_REFUSED_CODES = new Set([
  'EPERM',
  'EACCES',
  'EBUSY',
]);

/**
 * Waits before each new try of a refused rename: on Windows, an antivirus
 * scanner or indexer that has the file open usually lets go soon.
 */
const RENAME_RETRY_DELAYS_MS = [50, 100, 150, 200];

function errnoCode(e: unknown): string | undefined {
  return e instanceof Error && 'code' in e && typeof e.code === 'string'
    ? e.code
    : undefined;
}

function isStorageObject(value: unknown): value is StorageItems {
  return Boolean(value && typeof value === 'object' && !Array.isArray(value));
}

/**
 * Manages persistent storage for the application.
 * Stores key-value pairs.
 */
export class StorageManager {
  private readonly emitter = new EventEmitter();
  private writeChain: Promise<void> = Promise.resolve();
  private writeSeq = 0;
  /** The last rename stayed refused through its retries. */
  private renameRefused = false;

  readonly onDidChange = (
    listener: StorageChangeListener,
  ): StorageDisposable => {
    this.emitter.on('changed', listener);
    return {
      dispose: () => {
        this.emitter.off('changed', listener);
      },
    };
  };

  /**
   * @param scope The changes each window hears (default: all of them).
   * @param maxBytes Size cap: a larger update fails, but its deletions apply
   *   (so a full store can shrink).
   */
  constructor(
    private readonly storagePath: string,
    private readonly defaults?: StorageDefaults,
    private readonly scope: StorageScope = (_win, changes) => changes,
    private readonly maxBytes: number = Infinity,
  ) {}

  /**
   * Gets raw items from the storage file. `corrupt` is true when the file
   * was read but is not a JSON object (unparseable, `null`, an array, ...).
   * Throws when the file exists but can't be read.
   */
  private async readRawItems(): Promise<RawStorageReadResult> {
    if (!existsSync(this.storagePath)) {
      return { items: {}, corrupt: false };
    }
    let content: string;
    try {
      content = await fs.readFile(this.storagePath, 'utf-8');
    } catch (e) {
      // Removed since existsSync: nothing stored.
      if (errnoCode(e) === 'ENOENT') {
        return { items: {}, corrupt: false };
      }
      throw e;
    }
    try {
      if (!content || content.trim() === '') {
        return { items: {}, corrupt: false };
      }
      const parsed: unknown = JSON.parse(content);
      if (isStorageObject(parsed)) {
        return { items: parsed, corrupt: false };
      }
      console.error('Storage file is not a JSON object:', this.storagePath);
    } catch (e) {
      console.error('Error parsing storage items:', e);
    }
    return { items: {}, corrupt: true };
  }

  private async getRawItems(): Promise<StorageItems> {
    try {
      return (await this.readRawItems()).items;
    } catch (e) {
      console.error('Error reading storage items:', e);
      return {};
    }
  }

  private withDefaults(items: StorageItems): StorageItems {
    const merged: Record<string, string | undefined> = { ...items };
    this.defaults?.forEach((v, k) => {
      merged[k] ??= String(v);
    });
    return merged as StorageItems;
  }

  /**
   * Gets all items from the storage, with defaults applied.
   *
   * @returns A record of key-value pairs.
   */
  async getItems(): Promise<StorageItems> {
    return this.withDefaults(await this.getRawItems());
  }

  getItemsSync(): StorageItems {
    try {
      const s = existsSync(this.storagePath)
        ? readFileSync(this.storagePath, 'utf-8').trim()
        : '';
      const p: unknown = s ? JSON.parse(s) : null;
      return this.withDefaults(isStorageObject(p) ? p : {});
    } catch (e) {
      console.error('Error reading storage items synchronously:', e);
      return this.withDefaults({});
    }
  }

  /**
   * Updates items in the storage.
   *
   * @param changes A record of key-value pairs to update. If a value is null, the key will be deleted.
   */
  async updateItems(changes: StorageChanges): Promise<void> {
    const prev = this.writeChain;
    let done!: () => void;
    this.writeChain = new Promise<void>((r) => {
      done = r;
    });
    await prev;
    try {
      await this.applyChanges(changes);
    } finally {
      done();
    }
  }

  /** Only updateItems calls this, one update at a time. */
  private async applyChanges(changes: StorageChanges): Promise<void> {
    try {
      const { items: currentItems, corrupt } = await this.readRawItems();
      if (corrupt) {
        // Keep the unreadable file for recovery instead of silently replacing
        // every other key with just `changes`.
        const backup = `${this.storagePath}.corrupt-${Date.now()}.bak`;
        try {
          await fs.copyFile(this.storagePath, backup);
          console.error('Backed up corrupt storage file to', backup);
        } catch (e) {
          // Failing the update would fail every later one too, so storage
          // would stop working.
          console.error(`Error copying ${this.storagePath} to ${backup}:`, e);
        }
      }
      for (const [key, value] of Object.entries(changes)) {
        if (value === null) {
          delete currentItems[key];
        } else {
          currentItems[key] = value;
        }
      }
      const data = JSON.stringify(currentItems, null, 2);
      if (
        Buffer.byteLength(data) > this.maxBytes &&
        Object.values(changes).some((value) => value !== null)
      ) {
        const deletions = Object.entries(changes).filter(
          ([, v]) => v === null,
        );
        if (deletions.length > 0) {
          await this.applyChanges(Object.fromEntries(deletions));
        }
        throw new StorageFullError();
      }
      // Ensure directory exists
      const dir = path.dirname(this.storagePath);
      if (!existsSync(dir)) {
        await fs.mkdir(dir, { recursive: true });
      }
      const tmp = `${this.storagePath}.${process.pid}.${++this.writeSeq}.tmp`;
      let replaced: boolean;
      try {
        await fs.writeFile(tmp, data, 'utf-8');
        replaced = await this.renameOver(tmp);
      } catch (e) {
        await fs.unlink(tmp).catch(() => undefined);
        throw e;
      }
      if (!replaced) {
        // In place, then. Not atomic: the next update backs up a file that a
        // crash left half-written.
        try {
          await fs.writeFile(this.storagePath, data, 'utf-8');
        } catch (e) {
          // The tmp file is then the only full copy.
          console.error(`The new ${this.storagePath} stays in ${tmp}`);
          throw e;
        }
        await fs.unlink(tmp).catch(() => undefined);
      }
      for (const win of getHubWindows()) {
        const hubContents = getHubWebContents(win);
        if (hubContents && !hubContents.isDestroyed?.()) {
          // The update is saved: one window must not keep it from the others.
          try {
            const scoped = this.scope(win, changes);
            if (scoped && Object.keys(scoped).length > 0) {
              hubContents.send('storage:changed', scoped);
            }
          } catch (e) {
            console.error('Error sending storage changes to a window:', e);
          }
        }
      }
      this.emitter.emit('changed', changes);
    } catch (e) {
      // A full store is expected (the caller is told), not an error to log.
      if (!(e instanceof StorageFullError)) {
        console.error('Error updating storage items:', e);
      }
      throw e;
    }
  }

  /**
   * Renames `tmp` over the store file, trying again for a while if that's
   * refused; false if it stays refused.
   */
  private async renameOver(tmp: string): Promise<boolean> {
    // A refusal that outlasted the retries (an ACL, a handle kept open) isn't
    // a scanner's moment: until a rename works again, each update tries once,
    // or every update would wait half a second, one after the other.
    const delays = this.renameRefused ? [] : RENAME_RETRY_DELAYS_MS;
    for (const delayMs of [0, ...delays]) {
      if (delayMs > 0) {
        await new Promise<void>((resolve) => setTimeout(resolve, delayMs));
      }
      try {
        await fs.rename(tmp, this.storagePath);
        this.renameRefused = false;
        return true;
      } catch (e) {
        const code = errnoCode(e);
        if (code === undefined || !REPLACE_REFUSED_CODES.has(code)) {
          throw e;
        }
      }
    }
    this.renameRefused = true;
    return false;
  }
}
