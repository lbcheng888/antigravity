/**
 * Private dirs for one connection's files (askpass's socket, a copy of an SSH
 * config file), in a base where only this user can add entries: shared /tmp
 * isn't one.
 */
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { RemoteError } from './types';

export interface PrivateCheck {
  real: string;
  why?: string;
}

/** Why `base`, whose real path is `real` ('' if none), can't be a base. */
async function unfitBase(
  base: string,
  real: string,
): Promise<string | undefined> {
  if (!path.isAbsolute(base)) {
    return 'not an absolute path';
  }
  const stat = real
    ? await fs.promises.stat(real).catch(() => undefined)
    : undefined;
  if (!stat?.isDirectory()) {
    return 'missing, or not a folder';
  }
  if (stat.uid !== process.getuid?.()) {
    return 'not yours';
  }
  return (stat.mode & 0o022) === 0 ? undefined : 'others can write to it';
}

/**
 * `dir`'s real path ('' if none), and why it isn't private, if it isn't: a
 * folder of this user's that no one else can add entries to.
 */
export async function checkPrivate(dir: string): Promise<PrivateCheck> {
  // Its entries then go where the checks looked, not through a link.
  const real = path.isAbsolute(dir)
    ? await fs.promises.realpath(dir).catch(() => '')
    : '';
  return { real, why: await unfitBase(dir, real) };
}

/**
 * A new 0700 dir in the first private base (XDG_RUNTIME_DIR, then macOS's
 * per-user temp dir, then `userDataDir`) where `misfit`, given the new dir's
 * path, finds nothing wrong. Rejects with a RemoteError saying it can't do
 * `purpose`, and why it skipped each base.
 */
export async function makePrivateDir(
  userDataDir: string,
  purpose: string,
  misfit: (dir: string) => string | undefined,
): Promise<string> {
  const skipped: string[] = [];
  for (const base of [
    process.env.XDG_RUNTIME_DIR,
    process.platform === 'darwin' ? os.tmpdir() : undefined,
    userDataDir,
  ]) {
    if (!base) {
      continue;
    }
    const checked = await checkPrivate(base);
    const { real } = checked;
    // mkdtemp replaces the Xs with as many letters and digits.
    const why = checked.why ?? misfit(path.join(real, 'agy-ssh-XXXXXX'));
    if (why === undefined) {
      return fs.promises.mkdtemp(path.join(real, 'agy-ssh-'));
    }
    skipped.push(`${base}: ${why}`);
  }
  throw new RemoteError(
    'UNKNOWN',
    `Can't ${purpose}: no private folder to use (${skipped.join('; ')}).`,
  );
}
