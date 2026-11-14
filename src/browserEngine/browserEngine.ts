import { spawn, type SpawnOptions } from 'child_process';
import * as path from 'path';
import { CdpPipeConnection } from './cdpPipe';
import { findOfficialChrome } from './chromeDiscovery';

export const FRAME_CHANNEL = 'standalone-browser:frame';
export const GUEST_EVENT_CHANNEL = 'standalone-browser:guest-event';

export type BrowserEngineKind = 'official-chrome' | 'managed-chromium';

export interface ViewportSize {
  width: number;
  height: number;
}

export interface ScreencastViewport extends ViewportSize {
  deviceScaleFactor: number;
}

export interface ScreencastFrameMetadata {
  deviceWidth?: unknown;
  deviceHeight?: unknown;
}

export interface JpegDimensions {
  w: number;
  h: number;
}

export interface BrowserVersion {
  protocolVersion?: string;
  product?: string;
  revision?: string;
  userAgent?: string;
  jsVersion?: string;
  [key: string]: unknown;
}

export interface SpawnedProcess {
  readonly pid?: number | undefined;
  readonly exitCode: number | null;
  readonly signalCode: NodeJS.Signals | null;
  readonly stdio: readonly unknown[];
  once(event: 'exit' | 'error', listener: () => void): unknown;
  kill(signal?: NodeJS.Signals | number): unknown;
}

export type SpawnProcess = (
  command: string,
  args: string[],
  options: SpawnOptions,
) => SpawnedProcess;

export interface BrowserSession {
  readonly kind: BrowserEngineKind;
  readonly pid: number | undefined;
  readonly version: BrowserVersion;
  readonly cdp: CdpPipeConnection;
  readonly exited: Promise<void>;
  close(): Promise<void>;
}

export interface BrowserEngine {
  readonly kind: BrowserEngineKind;
  readonly executablePath: string;
  launch(profileDir: string): Promise<BrowserSession>;
}

export function httpOrigin(url: string): string {
  const p = URL.canParse(url) ? new URL(url) : undefined;
  return p?.protocol === 'http:' || p?.protocol === 'https:' ? p.origin : '';
}

export function windowBoundsFor(vp: ViewportSize): ViewportSize {
  return {
    width: Math.max(500, vp.width + 64),
    height: Math.max(500, vp.height + 200),
  };
}

export function isScreencastFrameStale(
  vp: ScreencastViewport,
  data: string,
  meta?: ScreencastFrameMetadata | Record<string, unknown> | null,
): boolean {
  const dw = meta?.deviceWidth;
  const dh = meta?.deviceHeight;
  if (typeof dw === 'number' && dw > 0 && Math.abs(dw - vp.width) > 2) {
    return true;
  }
  if (typeof dh === 'number' && dh > 0 && Math.abs(dh - vp.height) > 2) {
    return true;
  }
  const dims = jpegDimensions(data);
  if (!dims) {
    return false;
  }
  const expW = Math.round(vp.width * vp.deviceScaleFactor);
  const expH = Math.round(vp.height * vp.deviceScaleFactor);
  return Math.abs(dims.w - expW) > 6 || Math.abs(dims.h - expH) > 6;
}

const PROFILE_ID_PATTERN = /^[A-Za-z0-9_-]{1,64}$/;
const READY_TIMEOUT_MS = 30000;
const CLOSE_GRACE_MS = 5000;

/**
 * Returns the dedicated profile directory for `profileId`. Chrome 136+ refuses
 * remote debugging on the OS default user-data-dir, and a dedicated directory
 * also keeps agent browsing out of the user's everyday profile.
 */
export function browserProfileDir(
  appDataDir: string,
  profileId: string,
): string {
  if (!PROFILE_ID_PATTERN.test(profileId)) {
    throw new Error(`Invalid browser profile id: ${JSON.stringify(profileId)}`);
  }
  return path.join(appDataDir, 'browser-profiles', profileId);
}

/**
 * Chrome switches for an invisible (`--headless=new`), agent-drivable,
 * port-less browser. The pipe alone turns on Blink's AutomationControlled
 * feature (navigator.webdriver), which Google sign-in rejects, so it is always
 * paired with the disable switch. Passing `--force-device-scale-factor` when
 * the host display is HiDPI/Retina ensures headless Metal/IOSurface backing
 * buffers match `Emulation.setDeviceMetricsOverride` without `1x <-> 2x`
 * surface stride mismatches. On Linux without an active desktop keyring (such
 * as Xvfb or remote SSH), `--password-store=basic` avoids a 25s
 * org.freedesktop.secrets D-Bus stall.
 */
export function chromeLaunchArgs(
  profileDir: string,
  platform: NodeJS.Platform = process.platform,
  env: NodeJS.ProcessEnv = process.env,
  deviceScaleFactor = 1,
): string[] {
  const args = [
    '--headless=new',
    '--remote-debugging-pipe',
    '--disable-blink-features=AutomationControlled',
    `--user-data-dir=${profileDir}`,
    '--no-first-run',
    '--no-default-browser-check',
  ];
  if (Number.isFinite(deviceScaleFactor) && deviceScaleFactor > 1) {
    const scale = Math.min(
      3,
      Math.max(1, Number(deviceScaleFactor.toFixed(2))),
    );
    args.push(`--force-device-scale-factor=${scale}`);
  }
  if (platform === 'linux' && !env.XDG_CURRENT_DESKTOP) {
    args.push('--password-store=basic');
  }
  args.push('about:blank');
  return args;
}

const liveSessions = new Set<BrowserSession>();

/** Closes every browser launched by this process; call on app quit. */
export async function closeAllBrowserSessions(): Promise<void> {
  await Promise.all([...liveSessions].map((session) => session.close()));
}

/**
 * Returns the engine to use on this machine, or undefined if none is
 * available. Prefers the installed Official Google Chrome and falls back to an
 * optional managed Chromium binary under the same pipe launch contract.
 */
export function resolveBrowserEngine(
  findChrome: () => string | undefined = findOfficialChrome,
  findManagedChromium?: () => string | undefined,
  deviceScaleFactor: number | (() => number) = 1,
): PipeChromiumEngine | undefined {
  const officialPath = findChrome();
  if (officialPath) {
    return new PipeChromiumEngine(
      'official-chrome',
      officialPath,
      spawn,
      deviceScaleFactor,
    );
  }
  const managedPath = findManagedChromium?.();
  return managedPath
    ? new PipeChromiumEngine(
        'managed-chromium',
        managedPath,
        spawn,
        deviceScaleFactor,
      )
    : undefined;
}

/** Launches a Chromium-family binary with CDP on fds 3 (in) and 4 (out). */
export class PipeChromiumEngine implements BrowserEngine {
  constructor(
    readonly kind: BrowserEngineKind,
    readonly executablePath: string,
    private readonly spawnProcess: SpawnProcess = spawn,
    private readonly deviceScaleFactor: number | (() => number) = 1,
  ) {}

  async launch(profileDir: string): Promise<BrowserSession> {
    const dpr =
      typeof this.deviceScaleFactor === 'function'
        ? this.deviceScaleFactor()
        : this.deviceScaleFactor;
    const child = this.spawnProcess(
      this.executablePath,
      chromeLaunchArgs(profileDir, process.platform, process.env, dpr),
      { stdio: ['ignore', 'ignore', 'ignore', 'pipe', 'pipe'] },
    );
    const exited = new Promise<void>((resolve) => {
      if (child.exitCode !== null || child.signalCode !== null) {
        resolve();
        return;
      }
      child.once('exit', () => resolve());
      child.once('error', () => resolve());
    });
    const toBrowser = child.stdio[3];
    const fromBrowser = child.stdio[4];
    if (!isWritableStream(toBrowser) || !isReadableStream(fromBrowser)) {
      child.kill('SIGKILL');
      throw new Error('Failed to create CDP pipe stdio streams');
    }
    const cdp = new CdpPipeConnection(toBrowser, fromBrowser);
    let version: BrowserVersion;
    try {
      version = await withTimeout(
        cdp.send<BrowserVersion>('Browser.getVersion'),
        READY_TIMEOUT_MS,
        'Browser did not answer Browser.getVersion',
      );
    } catch (err) {
      child.kill('SIGKILL');
      throw err;
    }
    const session = new PipeBrowserSession(
      this.kind,
      child,
      version,
      cdp,
      exited,
    );
    liveSessions.add(session);
    void exited.then(() => liveSessions.delete(session));
    return session;
  }
}

class PipeBrowserSession implements BrowserSession {
  private closing?: Promise<void>;

  constructor(
    readonly kind: BrowserEngineKind,
    private readonly child: SpawnedProcess,
    readonly version: BrowserVersion,
    readonly cdp: CdpPipeConnection,
    readonly exited: Promise<void>,
  ) {}

  get pid(): number | undefined {
    return this.child.pid;
  }

  close(): Promise<void> {
    this.closing ??= this.doClose();
    return this.closing;
  }

  private async doClose(): Promise<void> {
    // Browser.close usually loses its reply to the exit itself; the exit is
    // the signal that matters.
    this.cdp.send('Browser.close').catch(() => {});
    const timer = setTimeout(() => this.child.kill('SIGKILL'), CLOSE_GRACE_MS);
    await this.exited;
    clearTimeout(timer);
  }
}

function isWritableStream(stream: unknown): stream is NodeJS.WritableStream {
  return (
    typeof stream === 'object' &&
    stream !== null &&
    'write' in stream &&
    typeof stream.write === 'function'
  );
}

function isReadableStream(stream: unknown): stream is NodeJS.ReadableStream {
  return (
    typeof stream === 'object' &&
    stream !== null &&
    'read' in stream &&
    typeof stream.read === 'function'
  );
}

/** Clamps finite numeric input `v` into `[min, max]`, rounded to an integer. */
export function clampInt(
  v: unknown,
  min: number,
  max: number,
  fb: number,
): number {
  return typeof v === 'number' && Number.isFinite(v)
    ? Math.max(min, Math.min(max, Math.round(v)))
    : fb;
}

/** Clamps device pixel ratio `v` into `[1, 3]`, falling back to `fb`. */
export function clampDpr(v: unknown, fb: number): number {
  return typeof v === 'number' && Number.isFinite(v) && v > 0
    ? Math.max(1, Math.min(3, v))
    : fb;
}

/** Extracts pixel width and height from a base64 JPEG's `SOF0..SOF2` marker. */
export function jpegDimensions(base64: string): JpegDimensions | null {
  const buf = Buffer.from(base64, 'base64');
  if (buf.length < 11 || buf[0] !== 0xff || buf[1] !== 0xd8) {
    return null;
  }
  let i = 2;
  while (i < buf.length - 9) {
    if (buf[i] !== 0xff) {
      i++;
      continue;
    }
    const m = buf[i + 1];
    if (m === 0xc0 || m === 0xc1 || m === 0xc2) {
      return { h: buf.readUInt16BE(i + 5), w: buf.readUInt16BE(i + 7) };
    }
    if (m === 0xd8 || m === 0xd9 || (m >= 0xd0 && m <= 0xd7)) {
      i += 2;
      continue;
    }
    i += 2 + buf.readUInt16BE(i + 2);
  }
  return null;
}

function withTimeout<T>(
  promise: Promise<T>,
  ms: number,
  message: string,
): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error(message)), ms);
  });
  return Promise.race([promise, timeout]).finally(() => clearTimeout(timer));
}
