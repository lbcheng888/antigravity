import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

export interface ChromeDiscoveryEnv {
  platform: NodeJS.Platform;
  env: NodeJS.ProcessEnv;
  homedir: string;
  isExecutable: (file: string) => boolean;
}

/**
 * Candidate install locations of Official Google Chrome (stable channel),
 * most specific first. Only stable is listed: Beta/Dev/Canary auto-update to
 * versions the policy proxy has not been validated against.
 */
export function officialChromeCandidates(
  platform: NodeJS.Platform,
  _env: NodeJS.ProcessEnv,
  homedir: string,
): string[] {
  switch (platform) {
    case 'darwin': {
      const bundle = path.posix.join(
        'Google Chrome.app',
        'Contents',
        'MacOS',
        'Google Chrome',
      );
      return [
        path.posix.join('/Applications', bundle),
        path.posix.join(homedir, 'Applications', bundle),
      ];
    }
    case 'linux':
      return [
        '/usr/bin/google-chrome-stable',
        '/usr/bin/google-chrome',
        '/opt/google/chrome/chrome',
      ];
    case 'win32':
      // Chrome's --remote-debugging-pipe on stdio fds 3/4 is not supported on Windows.
      return [];
    default:
      return [];
  }
}

/** Returns the first installed Official Google Chrome binary, or undefined. */
export function findOfficialChrome(
  discovery: ChromeDiscoveryEnv = defaultDiscoveryEnv(),
): string | undefined {
  return officialChromeCandidates(
    discovery.platform,
    discovery.env,
    discovery.homedir,
  ).find((candidate) => discovery.isExecutable(candidate));
}

function defaultDiscoveryEnv(): ChromeDiscoveryEnv {
  return {
    platform: process.platform,
    env: process.env,
    homedir: os.homedir(),
    isExecutable: (file: string): boolean => {
      try {
        fs.accessSync(
          file,
          process.platform === 'win32' ? fs.constants.F_OK : fs.constants.X_OK,
        );
        return true;
      } catch {
        return false;
      }
    },
  };
}
