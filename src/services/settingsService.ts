import { SleepBlocker } from '../utils';

// Setting keys
export enum SettingKey {
  RUN_IN_BACKGROUND = 'runInBackground',
  KEEP_COMPUTER_AWAKE = 'keepComputerAwake',
  AUTO_CHECK_FOR_UPDATES = 'autoCheckForUpdates',
}

// Default values
export const DEFAULTS: Map<SettingKey, boolean> = new Map([
  // The following setting is only on by default for macOS because closing
  // all windows without quitting is standard macOS behavior. On Linux and
  // Windows, tray icons are less discoverable or unsupported by default,
  // and closing all windows is expected to quit the application.
  [SettingKey.RUN_IN_BACKGROUND, process.platform === 'darwin'],
  [SettingKey.KEEP_COMPUTER_AWAKE, false],
  [SettingKey.AUTO_CHECK_FOR_UPDATES, true],
]);

export type SettingsRecord = Record<string, string | null | undefined>;

export interface SettingSubscription {
  dispose: () => void;
}

export interface SettingsStorageManager {
  getItems(): Promise<SettingsRecord>;
  onDidChange(
    listener: (changes: SettingsRecord) => void,
  ): SettingSubscription;
}

/**
 * A thin wrapper around StorageManager to listen for changes
 * in settings and apply their side effects.
 */
export class SettingsService {
  constructor(private readonly storageManager: SettingsStorageManager) {
    this.storageManager.onDidChange((changes) => {
      this.applySideEffects(changes);
    });
    void this.initialize();
  }

  private async initialize(): Promise<void> {
    const items = await this.storageManager.getItems();
    this.applySideEffects(items);
  }

  private applySideEffects(settings: SettingsRecord): void {
    const val = settings[SettingKey.KEEP_COMPUTER_AWAKE];
    if (val !== undefined) {
      const preventSleep =
        val === null
          ? DEFAULTS.get(SettingKey.KEEP_COMPUTER_AWAKE)!
          : val === 'true';
      SleepBlocker.getInstance().shouldKeepComputerAwake(preventSleep);
    }
  }

  async getSetting(key: SettingKey): Promise<boolean> {
    const items = await this.storageManager.getItems();
    return items[key] === 'true';
  }

  onSettingChanged(
    key: SettingKey,
    listener: (value: boolean) => void,
  ): SettingSubscription {
    return this.storageManager.onDidChange((changes) => {
      const val = changes[key];
      if (val !== undefined) {
        listener(val === null ? DEFAULTS.get(key)! : val === 'true');
      }
    });
  }
}
