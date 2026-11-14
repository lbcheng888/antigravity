import {
  app,
  webContents,
  type DownloadURLOptions,
  type WebFrameMain,
} from 'electron';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { registerGuestAgentControlPredicate } from './guestAgentControl';

/** Maximum byte/character length of a sanitized download filename. */
export const MAX_DOWNLOAD_FILENAME_LENGTH = 128;

/** Fallback filename used when a raw download name has no safe characters. */
export const DEFAULT_DOWNLOAD_FILENAME = 'download.bin';

/** App storage key storing the user-configured built-in browser download folder. */
export const HUMAN_DOWNLOAD_DIR_STORAGE_KEY = 'builtinBrowserDownloadDir';

/**
 * Grace window (ms) after `did-navigate` before revealing an agent-armed child
 * popup via `showInactive()`, so HTML-landing + JS/meta-refresh export popups
 * trigger `will-download` without ever flashing on screen.
 */
export const AGENT_POPUP_REVEAL_GRACE_MS = 750;

/**
 * How long an agent download may wait for the user's approval before it is
 * cancelled (fail closed). Re-exported by `webviewBrowser`.
 */
export const AGENT_DOWNLOAD_APPROVAL_TIMEOUT_MS = 10 * 60 * 1000;

/**
 * How long a reserved save path stays reserved before Chromium creates the
 * file. It outlives the approval wait so a second download cannot pick the
 * same path while the first is paused awaiting approval.
 */
export const IN_FLIGHT_RESERVATION_TTL_MS =
  AGENT_DOWNLOAD_APPROVAL_TIMEOUT_MS + 5 * 60000;

/**
 * How long a guest stays treated as agent-associated after it was disarmed
 * or destroyed, so late `will-download` events never fall back to the
 * default downloads folder.
 */
export const AGENT_CONTROL_TOMBSTONE_TTL_MS = 60000;

const MAX_COLLISION_ATTEMPTS = 1000;
const SAFE_CASCADE_ID_RE = /^[A-Za-z0-9._-]+$/;
const WINDOWS_RESERVED_STEM_RE = /^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\.|$)/i;
const SAFE_EXTENSION_RE = /(\.[A-Za-z0-9]{1,16})$/;

const EXECUTABLE_DOWNLOAD_EXTENSIONS = new Set<string>([
  '.app',
  '.appimage',
  '.applescript',
  '.appx',
  '.bash',
  '.bat',
  '.chm',
  '.cmd',
  '.com',
  '.command',
  '.cpl',
  '.csh',
  '.desktop',
  '.dmg',
  '.exe',
  '.hta',
  '.img',
  '.iso',
  '.jar',
  '.js',
  '.jse',
  '.ksh',
  '.lnk',
  '.mpkg',
  '.msc',
  '.msi',
  '.msix',
  '.pif',
  '.pkg',
  '.ps1',
  '.psm1',
  '.reg',
  '.run',
  '.scpt',
  '.scr',
  '.sh',
  '.terminal',
  '.tool',
  '.url',
  '.vbe',
  '.vbs',
  '.vhd',
  '.vhdx',
  '.webloc',
  '.workflow',
  '.wsf',
  '.wsh',
  '.zsh',
]);

/**
 * Extra native-code and script formats blocked for agent downloads on top of
 * {@link EXECUTABLE_DOWNLOAD_EXTENSIONS}.
 */
const AGENT_BLOCKED_EXTRA_EXTENSIONS = new Set<string>([
  '.apk',
  '.application',
  '.appref-ms',
  '.cjs',
  '.crx',
  '.deb',
  '.dll',
  '.dylib',
  '.gadget',
  '.inf',
  '.ipa',
  '.jnlp',
  '.mjs',
  '.msp',
  '.pl',
  '.ps1xml',
  '.psd1',
  '.py',
  '.rb',
  '.rpm',
  '.scf',
  '.so',
  '.vb',
  '.wasm',
  '.ws',
  '.xpi',
]);

/** Markup formats that run script when opened from local disk. */
const ACTIVE_HTML_DOWNLOAD_EXTENSIONS = new Set<string>([
  '.htc',
  '.htm',
  '.html',
  '.mht',
  '.mhtml',
  '.shtml',
  '.svg',
  '.svgz',
  '.xht',
  '.xhtml',
  '.xml',
  '.xsl',
  '.xslt',
]);

/** MIME types identifying active markup or executables regardless of extension. */
const AGENT_BLOCKED_DOWNLOAD_MIME_TYPES = new Set<string>([
  'application/ecmascript',
  'application/hta',
  'application/java-archive',
  'application/javascript',
  'application/vnd.android.package-archive',
  'application/vnd.apple.installer+xml',
  'application/vnd.debian.binary-package',
  'application/vnd.microsoft.portable-executable',
  'application/wasm',
  'application/x-apple-diskimage',
  'application/x-bat',
  'application/x-chrome-extension',
  'application/x-csh',
  'application/x-debian-package',
  'application/x-dosexec',
  'application/x-elf',
  'application/x-executable',
  'application/x-java-jnlp-file',
  'application/x-javascript',
  'application/x-mach-binary',
  'application/x-mimearchive',
  'application/x-ms-application',
  'application/x-ms-dos-executable',
  'application/x-ms-installer',
  'application/x-ms-shortcut',
  'application/x-msdos-program',
  'application/x-msdownload',
  'application/x-msi',
  'application/x-perl',
  'application/x-redhat-package-manager',
  'application/x-rpm',
  'application/x-ruby',
  'application/x-sh',
  'application/x-sharedlib',
  'application/x-shellscript',
  'application/x-xpinstall',
  'application/xhtml+xml',
  'application/xml',
  'application/xslt+xml',
  'image/svg+xml',
  'message/rfc822',
  'multipart/related',
  'text/ecmascript',
  'text/html',
  'text/javascript',
  'text/x-python',
  'text/x-sh',
  'text/x-shellscript',
  'text/xml',
]);

/** Minimal guest `WebContents` surface accepted for download routing state. */
export interface GuestDownloadWebContents {
  id: number;
  once?(event: 'destroyed', listener: () => void): unknown;
}

/** Minimal Hub host `WebContents` surface reachable from a resolved opener. */
export interface ChildPopupHostWebContents {
  id: number;
  isDestroyed(): boolean;
  send(channel: string, ...args: unknown[]): void;
}

/** Minimal opener `WebContents` surface tracked for child popup attribution. */
export interface ChildPopupOpenerWebContents {
  id: number;
  isDestroyed?(): boolean;
  getURL?(): string;
  downloadURL?(url: string, options?: DownloadURLOptions): void;
  hostWebContents?: ChildPopupHostWebContents | null;
}

/** Minimal child popup `WebContents` surface inspected for download routing. */
export interface ChildPopupWebContents {
  id?: number;
  isDestroyed?(): boolean;
  isCrashed?(): boolean;
  mainFrame?: unknown;
  opener?: unknown;
  on?(
    event: 'did-navigate',
    listener: (event: unknown, url: unknown) => void,
  ): unknown;
  once?(event: 'destroyed', listener: () => void): unknown;
  removeListener?(
    event: 'did-navigate',
    listener: (event: unknown, url: unknown) => void,
  ): unknown;
}

/** Minimal child popup `BrowserWindow` surface managed during download lifecycle. */
export interface ChildPopupWindow {
  webContents?: ChildPopupWebContents | null;
  once?(event: 'closed', listener: () => void): unknown;
  isDestroyed?(): boolean;
  isVisible?(): boolean;
  showInactive?(): void;
  show?(): void;
  hide?(): void;
  close?(): void;
}

/** Parameters passed to {@link trackChildPopupDownloadLifecycle}. */
export interface TrackChildPopupDownloadLifecycleParams {
  childWindow: ChildPopupWindow;
  openerContents: ChildPopupOpenerWebContents;
  isBlankPopupUrl: (url: string) => boolean;
  hasCommittedNavigation?: () => boolean;
  revealGraceMs?: number;
}

/** Reserved destination returned by {@link resolveSynchronousDownloadTarget}. */
export interface ResolvedDownloadTarget {
  savePath: string;
  isAgentDownload: boolean;
  release: () => void;
}

/** Parameters passed to {@link resolveSynchronousDownloadTarget}. */
export interface ResolveSynchronousDownloadTargetParams {
  webContentsId: number;
  openerWebContentsId?: number;
  fallbackArmedDir?: string;
  rawFilename?: string | null;
  getDefaultDownloadsDir?: () => string | null | undefined;
  getUserDataDir?: () => string | null | undefined;
  now?: () => number;
}

type SandboxDirKind = 'sandbox-downloads' | 'scratch';

interface SandboxDirExpectation {
  cascadeId: string;
  kind: SandboxDirKind;
}

interface SandboxDirMatch extends SandboxDirExpectation {
  hierarchyDirs: string[];
}

interface ChildPopupRecord {
  childWindow: ChildPopupWindow;
  openerContents: ChildPopupOpenerWebContents;
  openedWhileAgentArmed: boolean;
  hasCommittedNonBlankNavigation: boolean;
  hasCommittedNavigation?: () => boolean;
  totalDownloadCount: number;
  inFlightDownloadCount: number;
  revealed: boolean;
  revealTimer?: ReturnType<typeof setTimeout>;
}

interface DefaultDownloadDirCandidate {
  dir: string;
  isSharedTmpFallback: boolean;
  mustExist?: boolean;
}

const guestDownloadDirByWcId = new Map<number, string>();
const trackedDestroyListeners = new WeakSet<object>();
const inFlightReservedPaths = new Map<string, number>();
const childPopupRecordByWc = new WeakMap<object, ChildPopupRecord>();
const childPopupRecordByWcId = new Map<number, ChildPopupRecord>();

/**
 * Sanitizes an untrusted download filename to `[A-Za-z0-9._-]` bounded to
 * {@link MAX_DOWNLOAD_FILENAME_LENGTH} characters, with leading/trailing
 * punctuation stripped and Windows reserved device names prefixed.
 *
 * Every output is a fixed point under Go's `SanitizeSandboxDownloadFilename`.
 */
export function sanitizeDownloadFilename(rawFilename?: unknown): string {
  const trimmed = typeof rawFilename === 'string' ? rawFilename.trim() : '';
  const lastSlashIdx = Math.max(
    trimmed.lastIndexOf('/'),
    trimmed.lastIndexOf('\\'),
  );
  const rawBase = lastSlashIdx >= 0 ? trimmed.slice(lastSlashIdx + 1) : trimmed;
  let cleaned = rawBase
    .replace(/[^A-Za-z0-9._-]+/g, '_')
    .replace(/^[._-]+|[._-]+$/g, '');
  if (!cleaned) {
    return DEFAULT_DOWNLOAD_FILENAME;
  }
  if (WINDOWS_RESERVED_STEM_RE.test(cleaned)) {
    cleaned = `dl_${cleaned}`;
  }
  if (cleaned.length > MAX_DOWNLOAD_FILENAME_LENGTH) {
    const extMatch = SAFE_EXTENSION_RE.exec(cleaned);
    const ext =
      extMatch && extMatch[1].length < MAX_DOWNLOAD_FILENAME_LENGTH
        ? extMatch[1]
        : '';
    const stemBudget = MAX_DOWNLOAD_FILENAME_LENGTH - ext.length;
    let stem = cleaned.slice(0, stemBudget).replace(/[._-]+$/g, '');
    if (WINDOWS_RESERVED_STEM_RE.test(stem)) {
      stem = `dl_${stem}`.slice(0, stemBudget).replace(/[._-]+$/g, '');
    }
    cleaned = stem ? `${stem}${ext}` : DEFAULT_DOWNLOAD_FILENAME;
  }
  return cleaned;
}

function sliceUtf16Safe(input: string, maxUnits: number): string {
  if (maxUnits <= 0 || input.length <= maxUnits) {
    return maxUnits <= 0 ? '' : input;
  }
  const sliced = input.slice(0, maxUnits);
  const lastCode = sliced.charCodeAt(sliced.length - 1);
  return lastCode >= 0xd800 && lastCode <= 0xdbff
    ? sliced.slice(0, -1)
    : sliced;
}

/**
 * Sanitizes an untrusted human download filename while preserving readable
 * spaces, parentheses, brackets, and Unicode characters. Strips path prefixes,
 * control/bidi-override code points, Windows/POSIX illegal filename characters,
 * leading/trailing dots or whitespace, and prefixes Windows reserved stems.
 */
export function sanitizeHumanDownloadFilename(rawFilename?: unknown): string {
  const trimmed = typeof rawFilename === 'string' ? rawFilename.trim() : '';
  const lastSlashIdx = Math.max(
    trimmed.lastIndexOf('/'),
    trimmed.lastIndexOf('\\'),
  );
  const rawBase = lastSlashIdx >= 0 ? trimmed.slice(lastSlashIdx + 1) : trimmed;
  let cleaned = rawBase
    // eslint-disable-next-line no-control-regex
    .replace(/[\x00-\x1f\x7f-\x9f\u202a-\u202e\u2066-\u2069]+/g, '')
    .replace(/[<>:"/\\|?*]+/g, '_')
    .replace(/\s+/g, ' ')
    .replace(/^[.\s]+|[.\s]+$/g, '');
  if (!cleaned) {
    return DEFAULT_DOWNLOAD_FILENAME;
  }
  const stemBeforeDot = cleaned.split('.')[0]?.trim() ?? '';
  if (
    WINDOWS_RESERVED_STEM_RE.test(cleaned) ||
    WINDOWS_RESERVED_STEM_RE.test(stemBeforeDot)
  ) {
    cleaned = `dl_${cleaned}`;
  }
  if (cleaned.length > MAX_DOWNLOAD_FILENAME_LENGTH) {
    const extMatch = SAFE_EXTENSION_RE.exec(cleaned);
    const ext =
      extMatch && extMatch[1].length < MAX_DOWNLOAD_FILENAME_LENGTH
        ? extMatch[1]
        : '';
    const stemBudget = MAX_DOWNLOAD_FILENAME_LENGTH - ext.length;
    let stem = sliceUtf16Safe(cleaned, stemBudget).replace(/[.\s]+$/g, '');
    const truncatedStemBeforeDot = stem.split('.')[0]?.trim() ?? '';
    if (
      WINDOWS_RESERVED_STEM_RE.test(stem) ||
      WINDOWS_RESERVED_STEM_RE.test(truncatedStemBeforeDot)
    ) {
      stem = sliceUtf16Safe(`dl_${stem}`, stemBudget).replace(/[.\s]+$/g, '');
    }
    cleaned = stem ? `${stem}${ext}` : DEFAULT_DOWNLOAD_FILENAME;
  }
  return cleaned;
}

/**
 * Returns `true` when `targetPath` is an absolute path to an existing regular
 * non-symlink file on disk.
 */
export function isSafeExistingRegularFile(targetPath: unknown): boolean {
  if (
    typeof targetPath !== 'string' ||
    targetPath.length === 0 ||
    targetPath.includes('\0') ||
    !path.isAbsolute(targetPath)
  ) {
    return false;
  }
  try {
    const st = fs.lstatSync(targetPath, { throwIfNoEntry: false });
    return Boolean(st && !st.isSymbolicLink() && st.isFile());
  } catch {
    return false;
  }
}

/**
 * Returns `true` when `targetPath` carries an OS executable, script, or
 * shortcut extension that should be revealed in the file manager rather than
 * launched directly via `shell.openPath`.
 */
export function isExecutableDownloadPath(targetPath: unknown): boolean {
  if (typeof targetPath !== 'string' || targetPath.length === 0) {
    return false;
  }
  const normalized = targetPath.trim().replace(/[.\s]+$/g, '');
  const ext = path.extname(normalized).toLowerCase();
  return EXECUTABLE_DOWNLOAD_EXTENSIONS.has(ext);
}

/**
 * Returns `true` when an agent-initiated download must be refused because its
 * filename extension or MIME type identifies an executable, script, installer,
 * or active local markup (HTML/SVG/XML) format.
 */
export function isBlockedAgentDownloadFormat(
  filename?: unknown,
  mimeType?: unknown,
): boolean {
  const base =
    (typeof filename === 'string' ? filename : '')
      .trim()
      .replace(/[.\s]+$/g, '')
      .split(/[\\/]/)
      .pop() ?? '';
  const ext = path.extname(base).toLowerCase();
  if (
    EXECUTABLE_DOWNLOAD_EXTENSIONS.has(ext) ||
    AGENT_BLOCKED_EXTRA_EXTENSIONS.has(ext) ||
    ACTIVE_HTML_DOWNLOAD_EXTENSIONS.has(ext)
  ) {
    return true;
  }
  const mime = (typeof mimeType === 'string' ? mimeType : '')
    .split(';')[0]
    .trim()
    .toLowerCase();
  return AGENT_BLOCKED_DOWNLOAD_MIME_TYPES.has(mime);
}

/**
 * Synchronously reads the user-configured human download directory from
 * `<userData>/app_storage.json` (`builtinBrowserDownloadDir`), returning
 * `undefined` when unset, empty, relative, or unreadable.
 */
export function readConfiguredHumanDownloadsDir(
  userDataDir?: string | null,
  platform: NodeJS.Platform | string = process.platform,
): string | undefined {
  try {
    const resolvedUserData = userDataDir ?? app.getPath('userData');
    if (typeof resolvedUserData !== 'string' || resolvedUserData.length === 0) {
      return undefined;
    }
    const raw = fs.readFileSync(
      path.join(resolvedUserData, 'app_storage.json'),
      'utf8',
    );
    if (!raw || !raw.trim()) {
      return undefined;
    }
    const parsed: unknown = JSON.parse(raw);
    const configured =
      parsed && typeof parsed === 'object'
        ? (parsed as Record<string, unknown>)[HUMAN_DOWNLOAD_DIR_STORAGE_KEY]
        : undefined;
    if (typeof configured !== 'string') {
      return undefined;
    }
    const trimmed = configured.trim();
    const targetPath = platform === 'win32' ? path.win32 : path.posix;
    if (
      !trimmed ||
      trimmed.includes('\0') ||
      (platform === 'win32' && /^[\\/](?![\\/])/.test(trimmed)) ||
      !targetPath.isAbsolute(trimmed) ||
      trimmed.split(/[\\/]+/).some((seg) => seg === '..')
    ) {
      return undefined;
    }
    return targetPath.resolve(trimmed);
  } catch {
    return undefined;
  }
}

function buildCollisionFilename(
  safeFilename: string,
  attempt: number,
  isAgentDownload: boolean,
): string {
  const suffix = isAgentDownload ? `_${attempt}` : ` (${attempt})`;
  const extMatch = SAFE_EXTENSION_RE.exec(safeFilename);
  const ext =
    extMatch &&
    extMatch[1].length + suffix.length < MAX_DOWNLOAD_FILENAME_LENGTH
      ? extMatch[1]
      : '';
  const rawStem = ext ? safeFilename.slice(0, -ext.length) : safeFilename;
  const maxStemLen = Math.max(
    1,
    MAX_DOWNLOAD_FILENAME_LENGTH - suffix.length - ext.length,
  );
  const stem =
    sliceUtf16Safe(rawStem, maxStemLen).replace(
      isAgentDownload ? /[._-]+$/g : /[.\s]+$/g,
      '',
    ) || 'dl';
  return `${stem}${suffix}${ext}`;
}

function splitPathSegments(targetPath: string): string[] {
  return targetPath.split(/[\\/]+/).filter(Boolean);
}

function assertTrailingSandboxSegments(
  targetPath: string,
  expected?: SandboxDirExpectation,
): SandboxDirMatch {
  const segments = splitPathSegments(targetPath);
  if (
    segments.length >= 4 &&
    segments[segments.length - 4] === 'brain' &&
    segments[segments.length - 2] === 'browser_sandbox' &&
    segments[segments.length - 1] === 'downloads'
  ) {
    const cascadeId = segments[segments.length - 3];
    if (
      SAFE_CASCADE_ID_RE.test(cascadeId) &&
      cascadeId !== '.' &&
      cascadeId !== '..' &&
      (!expected ||
        (cascadeId === expected.cascadeId &&
          expected.kind === 'sandbox-downloads'))
    ) {
      const sandboxDir = path.dirname(targetPath);
      const cascadeDir = path.dirname(sandboxDir);
      const brainDir = path.dirname(cascadeDir);
      return {
        cascadeId,
        kind: 'sandbox-downloads',
        hierarchyDirs: [brainDir, cascadeDir, sandboxDir, targetPath],
      };
    }
  } else if (
    segments.length >= 3 &&
    segments[segments.length - 3] === 'brain' &&
    segments[segments.length - 1] === 'scratch'
  ) {
    const cascadeId = segments[segments.length - 2];
    if (
      SAFE_CASCADE_ID_RE.test(cascadeId) &&
      cascadeId !== '.' &&
      cascadeId !== '..' &&
      (!expected ||
        (cascadeId === expected.cascadeId && expected.kind === 'scratch'))
    ) {
      const cascadeDir = path.dirname(targetPath);
      const brainDir = path.dirname(cascadeDir);
      return {
        cascadeId,
        kind: 'scratch',
        hierarchyDirs: [brainDir, cascadeDir, targetPath],
      };
    }
  }
  throw new Error(
    `Invalid brainDownloadsDir "${targetPath}": path must end with brain/<cascadeId>/browser_sandbox/downloads or brain/<cascadeId>/scratch`,
  );
}

/**
 * Validates that `rawDir` is an absolute, non-traversal, non-symlinked
 * `.../brain/<cascadeId>/browser_sandbox/downloads` or
 * `.../brain/<cascadeId>/scratch` directory path and returns its normalized path.
 */
export function validateBrainDownloadsDir(rawDir: unknown): string {
  if (typeof rawDir !== 'string' || rawDir.length === 0) {
    throw new Error('brainDownloadsDir must be a non-empty string');
  }
  if (rawDir.includes('\0')) {
    throw new Error('brainDownloadsDir must not contain NUL bytes');
  }
  if (!path.isAbsolute(rawDir)) {
    throw new Error(`brainDownloadsDir must be an absolute path: "${rawDir}"`);
  }
  const rawSegments = rawDir.split(/[\\/]+/);
  if (rawSegments.some((seg) => seg === '.' || seg === '..')) {
    throw new Error(
      `brainDownloadsDir must not contain relative traversal segments: "${rawDir}"`,
    );
  }
  const normalized = path.resolve(rawDir);
  const { hierarchyDirs } = assertTrailingSandboxSegments(normalized);
  // None of the brain hierarchy levels may be a symbolic link or a regular file.
  for (const levelDir of hierarchyDirs) {
    const st = fs.lstatSync(levelDir, { throwIfNoEntry: false });
    if (st) {
      if (st.isSymbolicLink()) {
        throw new Error(
          `brainDownloadsDir rejects symbolic link segment: "${levelDir}"`,
        );
      }
      if (!st.isDirectory()) {
        throw new Error(
          `brainDownloadsDir segment is not a directory: "${levelDir}"`,
        );
      }
    }
  }
  return normalized;
}

/**
 * Arms or clears the per-guest sandbox download directory for `wc`.
 * Passing `null` clears any armed directory and restores default routing.
 */
export function setGuestDownloadDirectoryForWc(
  wc: GuestDownloadWebContents | null | undefined,
  downloadDir: string | null,
): string | null {
  if (
    !wc ||
    typeof wc.id !== 'number' ||
    !Number.isInteger(wc.id) ||
    wc.id <= 0
  ) {
    throw new Error('Invalid guest WebContents for download directory');
  }
  if (downloadDir === null) {
    if (guestDownloadDirByWcId.delete(wc.id)) {
      recordAgentControlTombstone(wc.id);
    }
    return null;
  }
  const validated = validateBrainDownloadsDir(downloadDir);
  const wcId = wc.id;
  guestDownloadDirByWcId.set(wcId, validated);
  if (typeof wc.once === 'function' && !trackedDestroyListeners.has(wc)) {
    trackedDestroyListeners.add(wc);
    wc.once('destroyed', () => {
      if (guestDownloadDirByWcId.delete(wcId)) {
        recordAgentControlTombstone(wcId);
      }
    });
  }
  return validated;
}

const agentControlTombstoneUntilByWcId = new Map<number, number>();

/**
 * Remembers for {@link AGENT_CONTROL_TOMBSTONE_TTL_MS} that `webContentsId`
 * was armed or agent-controlled (called on disarm and on destruction).
 */
export function recordAgentControlTombstone(
  webContentsId: number,
  nowMs: number = Date.now(),
): void {
  if (!Number.isInteger(webContentsId) || webContentsId <= 0) {
    return;
  }
  agentControlTombstoneUntilByWcId.set(
    webContentsId,
    nowMs + AGENT_CONTROL_TOMBSTONE_TTL_MS,
  );
}

/** Whether `webContentsId` was armed/agent-controlled within the last 60s. */
export function hasRecentAgentControlTombstone(
  webContentsId: number,
  nowMs: number = Date.now(),
): boolean {
  for (const [id, until] of agentControlTombstoneUntilByWcId) {
    if (until <= nowMs) {
      agentControlTombstoneUntilByWcId.delete(id);
    }
  }
  return agentControlTombstoneUntilByWcId.has(webContentsId);
}

/**
 * When `webContentsId` last stopped being armed/agent-controlled, if that was
 * within the last {@link AGENT_CONTROL_TOMBSTONE_TTL_MS}.
 */
export function getAgentControlReleasedAtMs(
  webContentsId: number,
  nowMs: number = Date.now(),
): number | undefined {
  if (!hasRecentAgentControlTombstone(webContentsId, nowMs)) {
    return undefined;
  }
  const until = agentControlTombstoneUntilByWcId.get(webContentsId);
  return until === undefined
    ? undefined
    : until - AGENT_CONTROL_TOMBSTONE_TTL_MS;
}

/** Clears agent-control tombstones for hermetic unit tests. */
export function clearAgentControlTombstonesForTesting(): void {
  agentControlTombstoneUntilByWcId.clear();
}

/**
 * Guests the host renderer reported as under agent control (e.g. legacy
 * `<webview>` tabs driven by JSON actions that never arm a download dir).
 */
const agentControlledGuestIds = new Set<number>();
const trackedAgentControlDestroyListeners = new WeakSet<object>();

/**
 * Marks guest `wc` as agent-controlled (`active = true`) or not. Clearing an
 * active mark, or destroying a marked guest, records a 60s agent-control
 * tombstone so late downloads never fall back to the default folder.
 */
export function setGuestAgentControlledForWc(
  wc: GuestDownloadWebContents | null | undefined,
  active: boolean,
): boolean {
  if (
    !wc ||
    typeof wc.id !== 'number' ||
    !Number.isInteger(wc.id) ||
    wc.id <= 0 ||
    typeof active !== 'boolean'
  ) {
    return false;
  }
  const wcId = wc.id;
  if (!active) {
    if (agentControlledGuestIds.delete(wcId)) {
      recordAgentControlTombstone(wcId);
    }
    return true;
  }
  agentControlledGuestIds.add(wcId);
  if (
    typeof wc.once === 'function' &&
    !trackedAgentControlDestroyListeners.has(wc)
  ) {
    trackedAgentControlDestroyListeners.add(wc);
    wc.once('destroyed', () => {
      if (agentControlledGuestIds.delete(wcId)) {
        recordAgentControlTombstone(wcId);
      }
    });
  }
  return true;
}

/** Whether the host renderer marked `webContentsId` as agent-controlled. */
export function isGuestMarkedAgentControlled(webContentsId: number): boolean {
  return agentControlledGuestIds.has(webContentsId);
}

/** Clears renderer-reported agent-control marks for hermetic unit tests. */
export function clearAgentControlledGuestsForTesting(): void {
  agentControlledGuestIds.clear();
}

registerGuestAgentControlPredicate(isGuestMarkedAgentControlled);

/** Returns the currently armed download directory for `webContentsId`, if any. */
export function getArmedGuestDownloadDirectory(
  webContentsId: number,
): string | undefined {
  return guestDownloadDirByWcId.get(webContentsId);
}

registerGuestAgentControlPredicate((webContentsId) =>
  Boolean(getArmedGuestDownloadDirectory(webContentsId)),
);

function getTrackedChildPopupRecord(
  wc: ChildPopupWebContents | null | undefined,
): ChildPopupRecord | undefined {
  if (!wc || typeof wc !== 'object') {
    return undefined;
  }
  const byRef = childPopupRecordByWc.get(wc);
  if (byRef) {
    return byRef;
  }
  const wcId = wc.id;
  if (typeof wcId === 'number' && Number.isInteger(wcId) && wcId > 0) {
    return childPopupRecordByWcId.get(wcId);
  }
  return undefined;
}

function clearChildPopupRevealTimer(record: ChildPopupRecord): void {
  if (record.revealTimer !== undefined) {
    clearTimeout(record.revealTimer);
    record.revealTimer = undefined;
  }
}

/**
 * Tracks a child popup `BrowserWindow` opened by `openerContents` so:
 * 1. Child-popup downloads deterministically attribute `openerWebContentsId`
 *    even when `COOP` or `noopener` clears `window.opener`.
 * 2. Popups spawned while `openerContents` is agent-armed defer `showInactive()`
 *    by {@link AGENT_POPUP_REVEAL_GRACE_MS} after `did-navigate` (cancelling the
 *    reveal if `will-download` fires first) and auto-close once all downloads
 *    triggered by that popup reach a terminal state, even if a landing HTML
 *    page (`"Preparing download..."`) committed before redirecting to the file.
 */
export function trackChildPopupDownloadLifecycle(
  params: TrackChildPopupDownloadLifecycleParams,
): void {
  const {
    childWindow,
    openerContents,
    isBlankPopupUrl,
    hasCommittedNavigation,
    revealGraceMs = AGENT_POPUP_REVEAL_GRACE_MS,
  } = params;
  const popupWc = childWindow?.webContents;
  const openedWhileAgentArmed = Boolean(
    getArmedGuestDownloadDirectory(openerContents.id),
  );
  const record: ChildPopupRecord = {
    childWindow,
    openerContents,
    openedWhileAgentArmed,
    hasCommittedNonBlankNavigation: Boolean(hasCommittedNavigation?.()),
    hasCommittedNavigation,
    totalDownloadCount: 0,
    inFlightDownloadCount: 0,
    revealed: false,
  };
  const popupWcId =
    popupWc &&
    typeof popupWc.id === 'number' &&
    Number.isInteger(popupWc.id) &&
    popupWc.id > 0
      ? popupWc.id
      : 0;
  if (popupWc && typeof popupWc === 'object') {
    childPopupRecordByWc.set(popupWc, record);
  }
  if (popupWcId > 0) {
    childPopupRecordByWcId.set(popupWcId, record);
  }

  let onDidNavigate:
    | ((_event: unknown, navigationUrl: unknown) => void)
    | undefined;
  const cleanup = (): void => {
    clearChildPopupRevealTimer(record);
    if (onDidNavigate) {
      popupWc?.removeListener?.('did-navigate', onDidNavigate);
      onDidNavigate = undefined;
    }
    if (popupWcId > 0 && childPopupRecordByWcId.get(popupWcId) === record) {
      childPopupRecordByWcId.delete(popupWcId);
    }
  };
  childWindow.once?.('closed', cleanup);
  popupWc?.once?.('destroyed', cleanup);

  if (openedWhileAgentArmed) {
    onDidNavigate = (_event: unknown, navigationUrl: unknown): void => {
      if (typeof navigationUrl !== 'string' || isBlankPopupUrl(navigationUrl)) {
        return;
      }
      record.hasCommittedNonBlankNavigation = true;
      if (onDidNavigate) {
        popupWc?.removeListener?.('did-navigate', onDidNavigate);
        onDidNavigate = undefined;
      }
      if (
        !record.revealed &&
        record.totalDownloadCount === 0 &&
        !childWindow.isDestroyed?.()
      ) {
        clearChildPopupRevealTimer(record);
        const timer = setTimeout(() => {
          record.revealTimer = undefined;
          if (
            !record.revealed &&
            record.totalDownloadCount === 0 &&
            !childWindow.isDestroyed?.() &&
            !popupWc?.isCrashed?.()
          ) {
            record.revealed = true;
            try {
              if (childWindow.isVisible?.() !== true) {
                if (typeof childWindow.showInactive === 'function') {
                  childWindow.showInactive();
                } else {
                  childWindow.show?.();
                }
              }
            } catch {
              // Ignore asynchronous BrowserWindow destruction races.
            }
          }
        }, Math.max(0, revealGraceMs));
        timer.unref?.();
        record.revealTimer = timer;
      }
    };
    popupWc?.on?.('did-navigate', onDidNavigate);
  }
}

function isPopupDocumentCommitted(record: ChildPopupRecord): boolean {
  return (
    record.hasCommittedNonBlankNavigation ||
    Boolean(record.hasCommittedNavigation?.())
  );
}

/**
 * Marks a tracked child popup on `will-download`, cancels any pending deferred
 * reveal timer, hides the popup window, and returns an idempotent callback that
 * closes the popup once all of its in-flight downloads terminate (for any
 * agent-armed child popup, or for an unarmed initial-blank download-only popup).
 */
export function prepareChildPopupDownloadCleanup(
  wc: ChildPopupWebContents | null | undefined,
): (() => void) | undefined {
  const record = getTrackedChildPopupRecord(wc);
  if (!record) {
    return undefined;
  }
  if (!record.openedWhileAgentArmed && isPopupDocumentCommitted(record)) {
    return undefined;
  }
  clearChildPopupRevealTimer(record);
  record.totalDownloadCount += 1;
  record.inFlightDownloadCount += 1;
  if (!record.childWindow.isDestroyed?.()) {
    try {
      record.childWindow.hide?.();
    } catch {
      // Ignore asynchronous BrowserWindow destruction races.
    }
  }
  let settled = false;
  return () => {
    if (settled) {
      return;
    }
    settled = true;
    record.inFlightDownloadCount = Math.max(
      0,
      record.inFlightDownloadCount - 1,
    );
    if (record.inFlightDownloadCount > 0) {
      return;
    }
    const shouldClose = record.openedWhileAgentArmed
      ? record.totalDownloadCount > 0
      : !isPopupDocumentCommitted(record);
    if (shouldClose && !record.childWindow.isDestroyed?.()) {
      clearChildPopupRevealTimer(record);
      try {
        record.childWindow.close?.();
      } catch {
        // Ignore asynchronous BrowserWindow destruction races.
      }
    }
  };
}

/** Resolves the live opener `WebContents` for a child popup `wc`, if present. */
export function resolveOpenerWebContents(
  wc: ChildPopupWebContents | null | undefined,
): ChildPopupOpenerWebContents | undefined {
  if (!wc || typeof wc !== 'object') {
    return undefined;
  }
  const tracked = getTrackedChildPopupRecord(wc);
  if (tracked && !tracked.openerContents.isDestroyed?.()) {
    return tracked.openerContents;
  }
  const mainFrame =
    wc.mainFrame && typeof wc.mainFrame === 'object'
      ? (wc.mainFrame as { opener?: unknown })
      : undefined;
  const openerFrame = mainFrame?.opener ?? wc.opener;
  const fromFrame = webContents?.fromFrame;
  if (!openerFrame || typeof fromFrame !== 'function') {
    return undefined;
  }
  try {
    const openerWc = fromFrame(openerFrame as WebFrameMain);
    return openerWc && !openerWc.isDestroyed?.() ? openerWc : undefined;
  } catch {
    return undefined;
  }
}

function pruneExpiredReservations(nowMs: number): void {
  for (const [key, expiresAt] of inFlightReservedPaths) {
    if (expiresAt <= nowMs) {
      inFlightReservedPaths.delete(key);
    }
  }
}

function allocateCollisionFreeSavePath(
  targetDir: string,
  realTargetDir: string,
  rawFilename: unknown,
  nowMs: number,
  isAgentDownload: boolean,
): ResolvedDownloadTarget {
  pruneExpiredReservations(nowMs);
  const safeBase = isAgentDownload
    ? sanitizeDownloadFilename(rawFilename)
    : sanitizeHumanDownloadFilename(rawFilename);
  for (let attempt = 0; attempt <= MAX_COLLISION_ATTEMPTS; attempt++) {
    const candidateName =
      attempt === 0
        ? safeBase
        : attempt < MAX_COLLISION_ATTEMPTS
          ? buildCollisionFilename(safeBase, attempt, isAgentDownload)
          : buildCollisionFilename(safeBase, nowMs, isAgentDownload);
    const savePath = path.join(targetDir, candidateName);
    const canonicalPath = path.join(realTargetDir, candidateName);
    // Ensure neither path escapes its directory root.
    if (
      path.dirname(savePath) !== targetDir ||
      path.dirname(canonicalPath) !== realTargetDir
    ) {
      throw new Error(`Resolved download path escaped directory: ${savePath}`);
    }
    // Use lstatSync rather than existsSync so dangling symlinks are treated as
    // occupied entries and never followed when Chromium opens the target file.
    if (
      inFlightReservedPaths.has(canonicalPath) ||
      fs.lstatSync(savePath, { throwIfNoEntry: false }) !== undefined ||
      fs.lstatSync(canonicalPath, { throwIfNoEntry: false }) !== undefined
    ) {
      continue;
    }
    // Chromium creates the destination file asynchronously after will-download
    // returns; reserve the canonical path so back-to-back downloads do not
    // select the same filename before the first file appears on disk.
    inFlightReservedPaths.set(
      canonicalPath,
      nowMs + IN_FLIGHT_RESERVATION_TTL_MS,
    );
    let released = false;
    return {
      savePath,
      isAgentDownload,
      release: () => {
        if (!released) {
          released = true;
          inFlightReservedPaths.delete(canonicalPath);
        }
      },
    };
  }
  throw new Error('Exhausted download filename collision attempts');
}

function resolveArmedDownloadTarget(
  armedDir: string,
  rawFilename: unknown,
  nowMs: number,
): ResolvedDownloadTarget | null {
  try {
    // Re-validate at download time to defeat TOCTOU symlink swaps after arming.
    const validatedDir = validateBrainDownloadsDir(armedDir);
    const expected = assertTrailingSandboxSegments(validatedDir);
    fs.mkdirSync(validatedDir, { recursive: true });
    validateBrainDownloadsDir(validatedDir);
    const realTargetDir = fs.realpathSync(validatedDir);
    assertTrailingSandboxSegments(realTargetDir, expected);
    return allocateCollisionFreeSavePath(
      validatedDir,
      realTargetDir,
      rawFilename,
      nowMs,
      true,
    );
  } catch {
    return null;
  }
}

function resolveDefaultDownloadTarget(
  rawFilename: unknown,
  getDefaultDownloadsDir: (() => string | null | undefined) | undefined,
  getUserDataDir: (() => string | null | undefined) | undefined,
  nowMs: number,
): ResolvedDownloadTarget | null {
  // mustExist marks the user-configured folder: it is used only if it already
  // exists as a directory, so an unmounted volume path is never recreated on
  // the root disk.
  const candidates: DefaultDownloadDirCandidate[] = [];
  if (getDefaultDownloadsDir) {
    try {
      const explicitDir = getDefaultDownloadsDir();
      if (typeof explicitDir === 'string' && explicitDir.length > 0) {
        candidates.push({ dir: explicitDir, isSharedTmpFallback: false });
      }
    } catch {
      // Fall through to fallbacks below.
    }
  } else {
    const configuredDir = readConfiguredHumanDownloadsDir(getUserDataDir?.());
    if (configuredDir) {
      candidates.push({
        dir: configuredDir,
        isSharedTmpFallback: false,
        mustExist: true,
      });
    }
    try {
      const systemDownloads = app.getPath('downloads');
      if (typeof systemDownloads === 'string' && systemDownloads.length > 0) {
        candidates.push({ dir: systemDownloads, isSharedTmpFallback: false });
      }
    } catch {
      // Fall through to os.tmpdir() fallback below.
    }
  }
  try {
    const tmpBase = os.tmpdir();
    if (typeof tmpBase === 'string' && tmpBase.length > 0) {
      const uidSuffix =
        typeof process.getuid === 'function' ? `-${process.getuid()}` : '';
      candidates.push({
        dir: path.join(tmpBase, `antigravity-browser-downloads${uidSuffix}`),
        isSharedTmpFallback: true,
      });
    }
  } catch {
    // Ignore os.tmpdir() failure.
  }

  for (const { dir: rawDir, isSharedTmpFallback, mustExist } of candidates) {
    if (rawDir.includes('\0') || !path.isAbsolute(rawDir)) {
      continue;
    }
    try {
      const targetDir = path.resolve(rawDir);
      if (isSharedTmpFallback) {
        const preLstat = fs.lstatSync(targetDir, { throwIfNoEntry: false });
        if (
          preLstat &&
          (preLstat.isSymbolicLink() || !preLstat.isDirectory())
        ) {
          continue;
        }
        fs.mkdirSync(targetDir, { recursive: true, mode: 0o700 });
        const postLstat = fs.lstatSync(targetDir, { throwIfNoEntry: false });
        if (
          !postLstat ||
          postLstat.isSymbolicLink() ||
          !postLstat.isDirectory() ||
          (typeof process.getuid === 'function' &&
            postLstat.uid !== process.getuid())
        ) {
          continue;
        }
      } else {
        if (!mustExist) {
          fs.mkdirSync(targetDir, { recursive: true });
        }
        const st = fs.statSync(targetDir, { throwIfNoEntry: false });
        if (!st || !st.isDirectory()) {
          continue;
        }
      }
      const realTargetDir = fs.realpathSync(targetDir);
      return allocateCollisionFreeSavePath(
        targetDir,
        realTargetDir,
        rawFilename,
        nowMs,
        false,
      );
    } catch {
      continue;
    }
  }
  return null;
}

/**
 * Synchronously resolves a safe, collision-free save path for an Electron
 * `will-download` event so Chromium never opens a native modal save dialog.
 *
 * Returns `null` when an armed sandbox directory fails validation or filesystem
 * setup so the caller can cancel the download synchronously (`item.cancel()`).
 */
export function resolveSynchronousDownloadTarget(
  params: ResolveSynchronousDownloadTargetParams,
): ResolvedDownloadTarget | null {
  const nowMs = (params.now ?? Date.now)();
  const armedDir =
    (params.webContentsId > 0
      ? guestDownloadDirByWcId.get(params.webContentsId)
      : undefined) ??
    (params.openerWebContentsId && params.openerWebContentsId > 0
      ? guestDownloadDirByWcId.get(params.openerWebContentsId)
      : undefined) ??
    params.fallbackArmedDir;
  if (armedDir !== undefined) {
    return resolveArmedDownloadTarget(armedDir, params.rawFilename, nowMs);
  }
  return resolveDefaultDownloadTarget(
    params.rawFilename,
    params.getDefaultDownloadsDir,
    params.getUserDataDir,
    nowMs,
  );
}
