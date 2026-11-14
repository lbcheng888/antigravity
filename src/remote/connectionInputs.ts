/**
 * What each SSH connection needs from this computer, each looked up once: the
 * login shell's environment and the server to install.
 */

import { net } from 'electron';
import { LoginShellEnvReads } from '../loginShellEnvReads';
import { resolveStableArtifactDir } from '../wsl';
import {
  resolveRemoteServerArtifacts,
  serverArtifactsWithoutLookup,
  type ResolvedRemoteServerArtifacts,
} from './serverArtifacts';

const RELEASE_LOOKUP_TIMEOUT_MS = 10000;

export interface ConnectionInputsApp {
  appVersion: string;
  isInsiders?: boolean;
}

export class ConnectionInputs {
  private readonly loginEnv: LoginShellEnvReads;
  private artifacts: Promise<ResolvedRemoteServerArtifacts> | undefined;

  constructor(private readonly app: ConnectionInputsApp) {
    this.loginEnv = new LoginShellEnvReads(true);
  }

  /** ssh needs the login shell's PATH and SSH_AUTH_SOCK, even from the Dock. */
  loginShellEnv(): Promise<Record<string, string>> {
    return this.loginEnv.take();
  }

  /** An attach installs nothing: it needs no release lookup. */
  async serverFor(attachOnly: boolean): Promise<ResolvedRemoteServerArtifacts> {
    if (attachOnly) {
      return {
        artifacts: serverArtifactsWithoutLookup({
          appVersion: this.app.appVersion,
          env: process.env,
        }),
      };
    }
    return this.resolveArtifacts();
  }

  /**
   * Kept once found. A failed lookup isn't fatal (a running server is
   * attached), and the next connect looks again.
   */
  private resolveArtifacts(): Promise<ResolvedRemoteServerArtifacts> {
    if (this.artifacts) {
      return this.artifacts;
    }
    const pending = resolveRemoteServerArtifacts({
      appVersion: this.app.appVersion,
      isInsiders: this.app.isInsiders,
      env: process.env,
      // Electron's fetch: it honors the system proxy.
      resolveStableDir: (version: string) =>
        resolveStableArtifactDir(version, (url: string) =>
          net.fetch(url, {
            signal: AbortSignal.timeout(RELEASE_LOOKUP_TIMEOUT_MS),
          }),
        ),
    });
    this.artifacts = pending;
    const forget = (): void => {
      if (this.artifacts === pending) {
        this.artifacts = undefined;
      }
    };
    void pending.then((r) => {
      if (r.resolutionError) {
        forget();
      }
    }, forget);
    return pending;
  }
}
