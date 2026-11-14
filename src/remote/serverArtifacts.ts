/**
 * The language server build an SSH remote installs: the Hub's own version
 * from the public release bucket, as in WSL mode. Unreleased (dev) Hubs name
 * a release or a binary on the remote with the variables below.
 */

import {
  PUBLIC_ARTIFACT_BASE_URL,
  linuxServerUrl,
  resolveStableArtifactDir,
} from '../wsl';
import { RemoteError } from './types';

/** The release to install (dev Hubs report 0.0.0). */
export const SSH_LS_VERSION_ENV = 'ANTIGRAVITY_SSH_LS_VERSION';

/** A server binary on the remote (absolute path) to run instead. */
export const SSH_LS_REMOTE_PATH_ENV = 'ANTIGRAVITY_SSH_LS_REMOTE_PATH';

const VERSION_RE = /^[A-Za-z0-9._-]{1,128}$/;

export interface RemoteArchArtifact {
  url?: string;
}

export interface RemoteServerArtifacts {
  version: string;
  x64: RemoteArchArtifact;
  arm64: RemoteArchArtifact;
  remotePath?: string;
}

export interface ServerArtifactsOptions {
  appVersion: string;
  env?: NodeJS.ProcessEnv;
}

export interface ResolveRemoteServerArtifactsOptions
  extends ServerArtifactsOptions {
  isInsiders?: boolean;
  resolveStableDir?: (version: string) => Promise<string>;
}

export interface ResolvedRemoteServerArtifacts {
  artifacts: RemoteServerArtifacts;
  resolutionError?: string;
}

/**
 * The version (and remote binary, if set) without download URLs: all an
 * attach-only connect needs.
 * @throws RemoteError INSTALL_FAILED for a bad version or remote path.
 */
export function serverArtifactsWithoutLookup(
  opts: ServerArtifactsOptions,
): RemoteServerArtifacts {
  const env = opts.env ?? process.env;
  // '||': an empty value means unset.
  const version = env[SSH_LS_VERSION_ENV] || opts.appVersion;
  if (!VERSION_RE.test(version) || /^\.+$/.test(version)) {
    throw new RemoteError(
      'INSTALL_FAILED',
      `Invalid server version: ${JSON.stringify(version)}`,
    );
  }
  const remotePath = env[SSH_LS_REMOTE_PATH_ENV];
  if (!remotePath) {
    return { version, x64: {}, arm64: {} };
  }
  // eslint-disable-next-line no-control-regex
  if (!/^\/[^\u0000-\u001f\u007f]{0,4095}$/.test(remotePath)) {
    throw new RemoteError(
      'INSTALL_FAILED',
      `${SSH_LS_REMOTE_PATH_ENV} must be an absolute path on the SSH host: ${JSON.stringify(remotePath)}`,
    );
  }
  return { version, x64: {}, arm64: {}, remotePath };
}

/**
 * Adds the release's download URLs.
 * @throws RemoteError INSTALL_FAILED for a bad version or remote path.
 */
export async function resolveRemoteServerArtifacts(
  opts: ResolveRemoteServerArtifactsOptions,
): Promise<ResolvedRemoteServerArtifacts> {
  const artifacts = serverArtifactsWithoutLookup(opts);
  if (artifacts.remotePath) {
    return { artifacts };
  }
  const { version } = artifacts;
  let artifactDir: string;
  try {
    artifactDir = opts.isInsiders
      ? `${PUBLIC_ARTIFACT_BASE_URL}/insiders/${version}`
      : await (opts.resolveStableDir ?? resolveStableArtifactDir)(version);
  } catch (err: unknown) {
    return {
      artifacts,
      resolutionError:
        `Could not find the Antigravity server build ${version}: ` +
        `${err instanceof Error ? err.message : String(err)}. Development builds must set ` +
        `${SSH_LS_VERSION_ENV} to a released version.`,
    };
  }
  return {
    artifacts: {
      version,
      x64: { url: linuxServerUrl(artifactDir, 'x64') },
      arm64: { url: linuxServerUrl(artifactDir, 'arm64') },
    },
  };
}
