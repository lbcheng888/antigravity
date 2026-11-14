/**
 * Login shell environment reads (loginShellEnv.ts) that callers share. Each
 * caller waits at most LOGIN_SHELL_ENV_WAIT_SEC: a read still running then
 * answers a later caller. A failed read is read again by the next one.
 * TODO(nathanjuan): One reader for LS starts and SSH connects, kept until an
 * LS restart calls refresh(): one login shell at startup, not two.
 */
import { loadLoginShellEnv } from './loginShellEnv';
import { LOGIN_SHELL_ENV_WAIT_SEC } from './loginShellPolicy';

interface PendingRead {
  env: Promise<Record<string, string> | undefined>;
  done: boolean;
}

export class LoginShellEnvReads {
  private read: PendingRead | undefined;

  /**
   * @param keep Whether a read answers every later caller (SSH connects, so
   *   that no connect waits for the rc files again), or only the next one (LS
   *   starts, so that a restart picks up rc edits).
   */
  constructor(private readonly keep: boolean) {}

  /** Starts a read for the next caller, unless one is under way. */
  prefetch(): void {
    this.read ??= this.start();
  }

  /** The environment; `{}` if it can't be read, or not within the wait. */
  async take(): Promise<Record<string, string>> {
    const read = (this.read ??= this.start());
    const env = await waitAtMost(read.env);
    if (!this.keep && read.done && this.read === read) {
      this.read = undefined;
    }
    return env ?? {};
  }

  private start(): PendingRead {
    const read: PendingRead = { env: loadLoginShellEnv(), done: false };
    void read.env.then((env) => {
      read.done = true;
      if (!env && this.read === read) {
        this.read = undefined;
      }
    });
    return read;
  }
}

async function waitAtMost(
  read: Promise<Record<string, string> | undefined>,
): Promise<Record<string, string> | undefined> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const late = new Promise<'late'>((resolve) => {
    timer = setTimeout(() => resolve('late'), LOGIN_SHELL_ENV_WAIT_SEC * 1000);
  });
  try {
    const env = await Promise.race([read, late]);
    if (env !== 'late') {
      return env;
    }
    console.warn(
      `No login shell environment within ${LOGIN_SHELL_ENV_WAIT_SEC} s: going on without it, and a later read may use it`,
    );
    return undefined;
  } finally {
    clearTimeout(timer);
  }
}
