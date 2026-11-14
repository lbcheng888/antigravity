import * as fs from 'fs';
import * as path from 'path';
import type { DomPathStep } from './guestFramePolicy';

export type { DomPathStep };

/** Hard per-file upload size ceiling (50 MiB). */
export const MAX_UPLOAD_FILE_BYTES = 50 * 1024 * 1024;
/** Hard combined upload size ceiling across all files in one call (100 MiB). */
export const MAX_UPLOAD_TOTAL_BYTES = 100 * 1024 * 1024;
/** Maximum number of files attached in a single `setInputFiles` / `setFiles` call. */
export const MAX_UPLOAD_FILE_COUNT = 64;

/** Minimal frame surface inspected by {@link isGuestOnProtectedIdp}. */
export interface ProtectedIdpWebFrameLike {
  isDestroyed?(): boolean;
  url?: string | null;
  origin?: string | null;
  framesInSubtree?:
    | readonly (ProtectedIdpWebFrameLike | null | undefined)[]
    | null;
}

/** Minimal `WebContents` surface inspected by {@link isGuestOnProtectedIdp}. */
export interface ProtectedIdpWebContentsLike {
  isDestroyed?(): boolean;
  getURL?(): string | undefined;
  mainFrame?: ProtectedIdpWebFrameLike | null;
}

/** Validated `.../brain/<cascadeId>/browser_sandbox/uploads` directory paths. */
export interface ValidatedBrainUploadsDir {
  normalized: string;
  realUploadsDir: string;
  cascadeId: string;
}

/** Optional tighter ceilings and cascade constraint for upload validation. */
export interface ResolveStagedUploadFilesOptions {
  expectedCascadeId?: string;
  maxFileCount?: number;
  maxFileBytes?: number;
  maxTotalBytes?: number;
}

/** Metadata for a single validated staged upload file. */
export interface StagedUploadFileMetadata {
  realPath: string;
  sizeBytes: number;
}

/** Failure result returned when staged upload validation rejects input. */
export interface UploadValidationError {
  ok: false;
  errorCode: 'SCRIPT_ERROR';
  error: string;
}

/** Result of {@link resolveStagedUploadFilesMetadata}. */
export type ResolveStagedUploadFilesMetadataResult =
  | {
      ok: true;
      realUploadsRoot: string;
      files: StagedUploadFileMetadata[];
      totalBytes: number;
    }
  | UploadValidationError;

/** Result of {@link resolveValidatedStagedUploadFiles}. */
export type ResolveValidatedStagedUploadFilesResult =
  | {
      ok: true;
      files: string[];
    }
  | UploadValidationError;

const SAFE_CASCADE_ID_RE = /^[A-Za-z0-9._-]+$/;
const CITC_PATH_PREFIXES = [
  '/google/src/cloud',
  '/volumes/google/src/cloud',
  '/google_src/cloud',
  '/google_src/.symlinks/cloud',
];
const PROTECTED_IDP_EXACT_HOSTS =
  'accounts.google.com,myaccount.google.com,accounts.youtube.com,gds.google.com,login.corp.google.com,uberproxy-pen-redirect.corp.google.com,idp.prp.tpcpe.goog,login.microsoftonline.com,login.live.com,appleid.apple.com'.split(
    ',',
  );
const PROTECTED_IDP_WILDCARD_SUFFIXES =
  '.okta.com,.oktapreview.com,.okta-emea.com,.auth0.com,.duosecurity.com,.onelogin.com,.pingidentity.com'.split(
    ',',
  );
const PROTECTED_IDP_PUBLIC_HOSTS =
  'developer.okta.com,docs.okta.com,help.okta.com,support.okta.com,status.okta.com,docs.auth0.com,developer.auth0.com,help.auth0.com,support.auth0.com,community.auth0.com,status.auth0.com'.split(
    ',',
  );

const scriptErr = (error: string): UploadValidationError => ({
  ok: false,
  errorCode: 'SCRIPT_ERROR',
  error,
});

function isCitcPath(candidatePath: string): boolean {
  const slash = candidatePath.replace(/\\/g, '/').toLowerCase();
  return CITC_PATH_PREFIXES.some(
    (prefix) => slash === prefix || slash.startsWith(`${prefix}/`),
  );
}

function extractNormalizedHostname(rawUrl?: string | null): string {
  const current = (rawUrl || '').trim();
  if (!current || current === 'null' || /^about:/i.test(current)) {
    return '';
  }
  try {
    const outer = new URL(current);
    const parsed = outer.protocol === 'blob:' ? new URL(outer.pathname) : outer;
    return parsed.hostname
      .replace(/^\[|\]$/g, '')
      .toLowerCase()
      .replace(/\.+$/, '')
      .replace(/^www\./, '');
  } catch {
    return '';
  }
}

/** Returns `true` when `rawUrl` resolves to a protected Identity Provider host. */
export function isProtectedIdentityProviderOriginUrl(
  rawUrl?: string | null,
): boolean {
  const host = extractNormalizedHostname(rawUrl);
  if (
    !host ||
    PROTECTED_IDP_PUBLIC_HOSTS.some((p) => host === p || host.endsWith(`.${p}`))
  ) {
    return false;
  }
  return (
    PROTECTED_IDP_EXACT_HOSTS.some(
      (d) => host === d || host.endsWith(`.${d}`),
    ) ||
    PROTECTED_IDP_WILDCARD_SUFFIXES.some(
      (s) => host === s.slice(1) || host.endsWith(s),
    )
  );
}

/** Returns `true` when `wc` or any live frame in its subtree is on a protected IdP origin. */
export function isGuestOnProtectedIdp(
  wc: ProtectedIdpWebContentsLike,
): boolean {
  if (wc.isDestroyed?.()) {
    return false;
  }
  if (isProtectedIdentityProviderOriginUrl(wc.getURL?.() || '')) {
    return true;
  }
  const mf = wc.mainFrame;
  const frames: Array<ProtectedIdpWebFrameLike | null | undefined> = [
    ...(mf ? [mf] : []),
    ...(Array.isArray(mf?.framesInSubtree) ? mf.framesInSubtree : []),
  ];
  return frames.some(
    (f) =>
      f &&
      !f.isDestroyed?.() &&
      ((typeof f.url === 'string' &&
        isProtectedIdentityProviderOriginUrl(f.url)) ||
        (typeof f.origin === 'string' &&
          isProtectedIdentityProviderOriginUrl(f.origin))),
  );
}

/** Validates a structural `domPath` from `window.top.document` to `<input type="file">`. */
export function isValidDomPath(steps: unknown): steps is DomPathStep[] {
  return (
    Array.isArray(steps) &&
    steps.length > 0 &&
    steps.length <= 512 &&
    steps.every((step: unknown) => {
      if (!step || typeof step !== 'object') {
        return false;
      }
      const r = step as Record<string, unknown>;
      return (
        r.kind === 'shadow' ||
        r.kind === 'frame' ||
        (r.kind === 'child' &&
          typeof r.index === 'number' &&
          Number.isInteger(r.index) &&
          r.index >= 0 &&
          r.index <= 100000)
      );
    })
  );
}

function assertTrailingUploadsSegments(
  targetPath: string,
  expectedCascadeId?: string,
): string {
  const segs = targetPath.split(/[\\/]+/).filter(Boolean);
  if (segs.length < 4) {
    throw new Error(
      `Invalid sandboxUploadsDir "${targetPath}": missing brain/<cascadeId>/browser_sandbox/uploads hierarchy`,
    );
  }
  const [brainSeg, cascadeId, sandboxSeg, uploadsSeg] = segs.slice(-4);
  if (
    brainSeg !== 'brain' ||
    sandboxSeg !== 'browser_sandbox' ||
    uploadsSeg !== 'uploads' ||
    !SAFE_CASCADE_ID_RE.test(cascadeId) ||
    cascadeId === '.' ||
    cascadeId === '..' ||
    (expectedCascadeId !== undefined && cascadeId !== expectedCascadeId)
  ) {
    throw new Error(
      `Invalid sandboxUploadsDir "${targetPath}": path must end with brain/<cascadeId>/browser_sandbox/uploads`,
    );
  }
  return cascadeId;
}

/** Validates an existing, non-symlinked, non-CitC `.../brain/<cascadeId>/browser_sandbox/uploads` directory. */
export function validateBrainUploadsDir(
  rawDir: string,
  expectedCascadeId?: string,
): ValidatedBrainUploadsDir {
  if (typeof rawDir !== 'string' || rawDir.trim().length === 0) {
    throw new Error(
      'sandboxUploadsDir is not configured; ask the caller agent to stage upload files in <Artifact Directory Path>/browser_sandbox/uploads/<filename>',
    );
  }
  if (rawDir.includes('\0') || !path.isAbsolute(rawDir)) {
    throw new Error(`Invalid sandboxUploadsDir path: "${rawDir}"`);
  }
  if (rawDir.split(/[\\/]+/).some((seg) => seg === '.' || seg === '..')) {
    throw new Error(
      `sandboxUploadsDir must not contain relative traversal segments: "${rawDir}"`,
    );
  }
  const normalized = path.resolve(rawDir);
  if (isCitcPath(normalized)) {
    throw new Error(
      `sandboxUploadsDir must not reside inside a CitC workspace: "${normalized}"`,
    );
  }
  const cascadeId = assertTrailingUploadsSegments(normalized, expectedCascadeId);
  const sandboxDir = path.dirname(normalized);
  const cascadeDir = path.dirname(sandboxDir);
  const brainDir = path.dirname(cascadeDir);
  for (const levelDir of [brainDir, cascadeDir, sandboxDir, normalized]) {
    const st = fs.lstatSync(levelDir, { throwIfNoEntry: false });
    if (!st) {
      throw new Error(
        `browser_sandbox/uploads directory does not exist ("${normalized}"); ask the caller agent to copy/write the file into "${normalized}/<filename>" first`,
      );
    }
    if (st.isSymbolicLink() || !st.isDirectory()) {
      throw new Error(
        `sandboxUploadsDir rejects symlink or non-directory segment: "${levelDir}"`,
      );
    }
  }
  const realUploadsDir = fs.realpathSync(normalized);
  if (isCitcPath(realUploadsDir)) {
    throw new Error(
      `sandboxUploadsDir realpath must not reside inside CitC: "${realUploadsDir}"`,
    );
  }
  assertTrailingUploadsSegments(realUploadsDir, cascadeId);
  return { normalized, realUploadsDir, cascadeId };
}

/** Resolves caller-staged upload filenames strictly inside `rawUploadsDir` (`<= 50 MiB` per file, `<= 100 MiB` total, no symlinks). */
export function resolveStagedUploadFilesMetadata(
  rawUploadsDir: string | null | undefined,
  requestedFiles: unknown,
  options?: ResolveStagedUploadFilesOptions,
): ResolveStagedUploadFilesMetadataResult {
  if (!Array.isArray(requestedFiles)) {
    return scriptErr('files must be an array of staged filenames');
  }
  const entries: readonly unknown[] = requestedFiles;
  const maxCount = Math.min(
    MAX_UPLOAD_FILE_COUNT,
    options?.maxFileCount && options.maxFileCount > 0
      ? options.maxFileCount
      : MAX_UPLOAD_FILE_COUNT,
  );
  const maxFileBytes = Math.min(
    MAX_UPLOAD_FILE_BYTES,
    options?.maxFileBytes && options.maxFileBytes > 0
      ? options.maxFileBytes
      : MAX_UPLOAD_FILE_BYTES,
  );
  const maxTotalBytes = Math.min(
    MAX_UPLOAD_TOTAL_BYTES,
    options?.maxTotalBytes && options.maxTotalBytes > 0
      ? options.maxTotalBytes
      : MAX_UPLOAD_TOTAL_BYTES,
  );
  if (entries.length > maxCount) {
    return scriptErr(
      `Cannot attach more than ${maxCount} files in one call (got ${entries.length})`,
    );
  }
  let uploadsRoot: ValidatedBrainUploadsDir;
  try {
    uploadsRoot = validateBrainUploadsDir(
      rawUploadsDir ?? '',
      options?.expectedCascadeId,
    );
  } catch (err) {
    if (entries.length === 0) {
      return { ok: true, realUploadsRoot: '', files: [], totalBytes: 0 };
    }
    return scriptErr(err instanceof Error ? err.message : String(err));
  }
  if (entries.length === 0) {
    return {
      ok: true,
      realUploadsRoot: uploadsRoot.realUploadsDir,
      files: [],
      totalBytes: 0,
    };
  }
  const resolvedFiles: StagedUploadFileMetadata[] = [];
  let totalBytes = 0;
  for (const rawEntry of entries) {
    if (typeof rawEntry !== 'string' || rawEntry.trim().length === 0) {
      return scriptErr(
        'Each staged upload file path must be a non-empty filename string inside browser_sandbox/uploads/',
      );
    }
    const rawFile = rawEntry.trim();
    if (rawFile.includes('\0')) {
      return scriptErr('Upload file path must not contain NUL bytes');
    }
    if (rawFile.startsWith('~') || /^file:/i.test(rawFile)) {
      return scriptErr(
        `External host path "${rawFile}" is sandbox-blocked; stage the file in "${uploadsRoot.normalized}/<filename>" and pass "<filename>"`,
      );
    }
    if (rawFile.split(/[\\/]+/).some((seg) => seg === '..')) {
      return scriptErr(
        `Path traversal ("..") in "${rawFile}" is forbidden; stage files directly inside "${uploadsRoot.normalized}/<filename>"`,
      );
    }
    let relSubPath: string;
    if (path.isAbsolute(rawFile)) {
      const abs = path.resolve(rawFile);
      const relN = path.relative(uploadsRoot.normalized, abs);
      const relR = path.relative(uploadsRoot.realUploadsDir, abs);
      const validRel = (r: string): boolean =>
        Boolean(r && r !== '.' && !r.startsWith('..') && !path.isAbsolute(r));
      if (validRel(relN)) {
        relSubPath = relN;
      } else if (validRel(relR)) {
        relSubPath = relR;
      } else {
        return scriptErr(
          `Host path "${rawFile}" is outside browser_sandbox/uploads ("${uploadsRoot.normalized}") and is sandbox-blocked against file exfiltration; do not retry with host paths or read_file — ask the caller agent to copy/write the file to "${uploadsRoot.normalized}/<filename>" and pass "<filename>"`,
        );
      }
    } else {
      let cleaned = rawFile.replace(/\\/g, '/').replace(/^(?:\.\/)+/, '');
      for (const prefix of ['browser_sandbox/uploads/', 'uploads/']) {
        if (cleaned.startsWith(prefix)) {
          cleaned = cleaned.slice(prefix.length);
          break;
        }
      }
      relSubPath = cleaned;
    }
    const relSegs = relSubPath.split(/[\\/]+/).filter(Boolean);
    if (
      relSegs.length === 0 ||
      relSegs.some((seg) => seg === '.' || seg === '..')
    ) {
      return scriptErr(
        `Invalid staged upload filename "${rawFile}"; pass a filename inside "${uploadsRoot.normalized}/"`,
      );
    }
    let curr = uploadsRoot.normalized;
    for (let i = 0; i < relSegs.length; i++) {
      curr = path.join(curr, relSegs[i]);
      const st = fs.lstatSync(curr, { throwIfNoEntry: false });
      if (!st) {
        const dest = path.join(uploadsRoot.normalized, ...relSegs);
        return scriptErr(
          `File "${rawFile}" is not staged in browser_sandbox/uploads ("${uploadsRoot.normalized}"). Do not retry with host paths or read_file; call report_browser_outcome (BLOCKED) asking the caller agent to copy/write the file to "${dest}" and resume`,
        );
      }
      if (st.isSymbolicLink()) {
        return scriptErr(
          `Symbolic link "${rawFile}" is not allowed in browser_sandbox/uploads`,
        );
      }
      const isLeaf = i === relSegs.length - 1;
      if (!isLeaf && !st.isDirectory()) {
        return scriptErr(
          `Upload path "${rawFile}" traverses non-directory segment "${relSegs[i]}"`,
        );
      }
      if (isLeaf) {
        if (!st.isFile()) {
          return scriptErr(`Upload path "${rawFile}" is not a regular file`);
        }
        if (st.size < 0 || st.size > maxFileBytes) {
          const capLabel =
            maxFileBytes === MAX_UPLOAD_FILE_BYTES
              ? '50 MiB'
              : `${maxFileBytes}-byte`;
          return scriptErr(
            `Upload file "${rawFile}" is ${st.size} bytes, exceeding the ${capLabel} limit`,
          );
        }
      }
    }
    let realFile: string;
    try {
      realFile = fs.realpathSync(curr);
    } catch {
      return scriptErr(`Failed to resolve realpath for "${rawFile}"`);
    }
    const realRel = path.relative(uploadsRoot.realUploadsDir, realFile);
    const realSt = fs.lstatSync(realFile, { throwIfNoEntry: false });
    if (
      !realRel ||
      realRel === '.' ||
      realRel.startsWith('..') ||
      path.isAbsolute(realRel) ||
      isCitcPath(realFile) ||
      !realSt ||
      realSt.isSymbolicLink() ||
      !realSt.isFile() ||
      realSt.size < 0 ||
      realSt.size > maxFileBytes
    ) {
      return scriptErr(
        `Upload file "${rawFile}" resolves outside browser_sandbox/uploads`,
      );
    }
    totalBytes += realSt.size;
    if (totalBytes > maxTotalBytes) {
      return scriptErr(
        `Combined upload size (${totalBytes} bytes) exceeds the ${maxTotalBytes}-byte total limit`,
      );
    }
    resolvedFiles.push({ realPath: realFile, sizeBytes: realSt.size });
  }
  return {
    ok: true,
    realUploadsRoot: uploadsRoot.realUploadsDir,
    files: resolvedFiles,
    totalBytes,
  };
}

/** Resolves caller-staged upload filenames strictly inside `rawUploadsDir` (`<= 50 MiB`, no symlinks). */
export function resolveValidatedStagedUploadFiles(
  rawUploadsDir: string | null | undefined,
  requestedFiles: unknown,
  options?: ResolveStagedUploadFilesOptions,
): ResolveValidatedStagedUploadFilesResult {
  const res = resolveStagedUploadFilesMetadata(
    rawUploadsDir,
    requestedFiles,
    options,
  );
  return res.ok ? { ok: true, files: res.files.map((f) => f.realPath) } : res;
}
