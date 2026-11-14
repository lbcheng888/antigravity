import type { WebSource } from 'electron';
import {
  ensureGuestDebuggerAttached,
  type GuestDebugger,
} from './guestDebugger';
import {
  isGuestOnProtectedIdp,
  isProtectedIdentityProviderOriginUrl,
  isValidDomPath,
  MAX_UPLOAD_FILE_COUNT,
  resolveStagedUploadFilesMetadata,
  resolveValidatedStagedUploadFiles,
  type DomPathStep,
  type ProtectedIdpWebContentsLike,
  type ProtectedIdpWebFrameLike,
  type ResolveStagedUploadFilesMetadataResult,
  type UploadValidationError,
} from './webviewUploadValidation';

/** IPC channel emitted to the embedder renderer when an intercepted file chooser opens. */
export const WEBVIEW_FILE_CHOOSER_OPENED_CHANNEL =
  'webview:file-chooser-opened';
/** Maximum decoded size in bytes for a single virtual file attachment (10 MiB). */
export const MAX_VIRTUAL_UPLOAD_FILE_BYTES = 10 * 1024 * 1024;
/** Maximum combined decoded size in bytes for all virtual file attachments in one call (25 MiB). */
export const MAX_VIRTUAL_UPLOAD_TOTAL_BYTES = 25 * 1024 * 1024;

/** Minimal CDP debugger surface required by file attachment helpers. */
export interface GuestCdpDebugger {
  sendCommand?(
    method: string,
    commandParams?: Record<string, unknown>,
  ): Promise<unknown>;
}

/** Combined debugger surface supporting attachment and CDP command dispatch. */
export interface GuestFileAttachmentDebugger
  extends GuestDebugger,
    GuestCdpDebugger {}

/** Frame surface supporting isolated-world popup guard synchronization. */
export interface GuestFileAttachmentWebFrame extends ProtectedIdpWebFrameLike {
  framesInSubtree?:
    | readonly (GuestFileAttachmentWebFrame | null | undefined)[]
    | null;
  executeJavaScriptInIsolatedWorld?(
    worldId: number,
    scripts: WebSource[],
    userGesture?: boolean,
  ): unknown;
}

/** `WebContents` surface supporting popup-guard isolated-world synchronization. */
export interface GuestPopupGuardWebContents extends ProtectedIdpWebContentsLike {
  mainFrame?: GuestFileAttachmentWebFrame | null;
  executeJavaScriptInIsolatedWorld?(
    worldId: number,
    scripts: WebSource[],
    userGesture?: boolean,
  ): unknown;
}

/** `WebContents` surface required for DOM-path staged file attachment. */
export interface GuestFileAttachmentWebContents
  extends GuestPopupGuardWebContents {
  debugger?: GuestFileAttachmentDebugger | null;
}

/** Request payload for {@link resolveGuestUploadFiles}. */
export interface ResolveGuestUploadFilesRequest {
  uploadsRoot?: string | null;
  files?: unknown;
  expectedConversationId?: string;
  maxFileCount?: number;
  maxFileBytes?: number;
  maxTotalBytes?: number;
}

/** Standard action-failure error result for guest file attachment operations. */
export interface GuestFileActionError {
  ok: false;
  errorCode: 'ACTION_FAILED';
  error: string;
}

/** Error result returned when upload is blocked on a protected IdP surface. */
export interface GuestIdpBlockedError {
  ok: false;
  errorCode: 'NAVIGATION_NEEDS_PERMISSION';
  error: string;
}

/** Result returned by {@link verifySameOriginChooserFrame}. */
export interface VerifySameOriginChooserFrameResult {
  allowed: boolean;
  mainOrigin: string;
  frameOrigin?: string;
  frameUrl?: string;
}

/** Unvalidated virtual file payload passed in a chooser decision. */
export interface VirtualUploadFile {
  name: string;
  mimeType?: string;
  base64: string;
}

/** Validated virtual file entry ready to pass into the page isolated world. */
export interface ValidatedVirtualUploadFile {
  name: string;
  mimeType: string;
  base64: string;
}

/** Pending intercepted file chooser target passed to {@link applyInterceptedChooserDecision}. */
export interface PendingFileChooserTarget {
  frameId?: string;
  backendNodeId?: number;
  multiple: boolean;
}

/** Decision payload for resolving an intercepted guest file chooser. */
export interface GuestFileChooserDecision {
  chooserId?: string;
  files?: unknown;
  virtualFiles?: unknown;
  allowedUploadsDir?: string | null;
  expectedConversationId?: string;
  maxFileCount?: number;
  maxFileBytes?: number;
  maxTotalBytes?: number;
}

/** Request payload for {@link attachStagedFilesByDomPath}. */
export interface AttachStagedFilesRequest {
  domPath?: unknown;
  files?: unknown;
  allowedUploadsDir?: string | null;
  expectedConversationId?: string;
  maxFileCount?: number;
  maxFileBytes?: number;
  maxTotalBytes?: number;
}

/** Return type for {@link attachVirtualFilesToBackendNode}. */
export type AttachVirtualFilesResult = { ok: true } | GuestFileActionError;

/** Return type for {@link applyInterceptedChooserDecision}. */
export type ApplyInterceptedChooserResult =
  | { ok: true }
  | GuestFileActionError
  | UploadValidationError;

/** Return type for {@link attachStagedFilesByDomPath}. */
export type AttachStagedFilesResult =
  | { ok: true }
  | GuestFileActionError
  | GuestIdpBlockedError
  | UploadValidationError;

interface CdpFrameInfo {
  id?: string;
  url?: string;
  securityOrigin?: string;
}

interface CdpFrameTreeNode {
  frame?: CdpFrameInfo;
  childFrames?: CdpFrameTreeNode[];
}

interface CdpGetFrameTreeResult {
  frameTree?: CdpFrameTreeNode;
}

interface CdpCreateIsolatedWorldResult {
  executionContextId?: number;
}

interface CdpResolveNodeResult {
  object?: {
    objectId?: string;
  };
}

interface CdpRuntimeCallArgument {
  value?: unknown;
}

interface CdpRuntimeCallFunctionOnResult {
  exceptionDetails?: {
    text?: string;
  };
  result?: {
    value?: unknown;
  };
}

interface CdpRuntimeEvaluateResult {
  exceptionDetails?: {
    text?: string;
  };
  result?: {
    value?: unknown;
    objectId?: string;
  };
}

interface VirtualFilesApplyResult {
  ok?: boolean;
  error?: string;
}

type ValidateVirtualUploadFilesResult =
  | { ok: true; files: ValidatedVirtualUploadFile[] }
  | UploadValidationError;

/** Resolves and validates staged upload file metadata for a guest upload request. */
export function resolveGuestUploadFiles(
  req?: ResolveGuestUploadFilesRequest | null,
): ResolveStagedUploadFilesMetadataResult {
  return resolveStagedUploadFilesMetadata(
    req?.uploadsRoot,
    Array.isArray(req?.files) ? req.files : [],
    {
      expectedCascadeId: req?.expectedConversationId,
      maxFileCount: req?.maxFileCount,
      maxFileBytes: req?.maxFileBytes,
      maxTotalBytes: req?.maxTotalBytes,
    },
  );
}

/** Returns `true` when `wc` currently has an empty or `about:blank` URL. */
export function isGuestOnBlankOrEmptyUrl(
  wc: Pick<ProtectedIdpWebContentsLike, 'getURL'>,
): boolean {
  const url = (wc.getURL?.() ?? '').trim();
  return url === '' || /^about:blank(?:[?#].*)?$/i.test(url);
}

/** Propagates the intercepted file-chooser allow flag into the guest's isolated world and subframes. */
export async function syncPopupGuardIsolatedFlag(
  wc: GuestPopupGuardWebContents,
  allow: boolean,
  isolatedWorldId = 9997,
): Promise<void> {
  if (wc.isDestroyed?.() || isGuestOnBlankOrEmptyUrl(wc)) {
    return;
  }
  const code = `window.__wcvAllowInterceptedFileChooser = ${allow ? 'true' : 'false'};`;
  const subframes = (wc.mainFrame?.framesInSubtree ?? []).filter(
    (f): f is GuestFileAttachmentWebFrame =>
      Boolean(f && f !== wc.mainFrame && !f.isDestroyed?.()),
  );
  await Promise.all(
    [wc, ...subframes].map((t) =>
      typeof t.executeJavaScriptInIsolatedWorld === 'function'
        ? Promise.resolve(
            t.executeJavaScriptInIsolatedWorld(isolatedWorldId, [{ code }]),
          ).catch(() => undefined)
        : Promise.resolve(),
    ),
  );
}

/** Builds an `ACTION_FAILED` error result. */
export const actionErr = (error: string): GuestFileActionError => ({
  ok: false,
  errorCode: 'ACTION_FAILED',
  error,
});

/** Builds a `NAVIGATION_NEEDS_PERMISSION` error result for protected IdP pages. */
export const idpBlockedErr = (): GuestIdpBlockedError => ({
  ok: false,
  errorCode: 'NAVIGATION_NEEDS_PERMISSION',
  error:
    'File upload is blocked on protected authentication pages. Ask the user to complete sign-in directly in the browser pane.',
});

function isExplicitOpaqueOrigin(raw?: string | null): boolean {
  const s = (raw ?? '').trim().toLowerCase();
  return s === 'null' || s === '://';
}

const stripUrlFragment = (u: string): string => u.replace(/#.*$/, '');

/** Extracts a normalized comparable origin from `securityOrigin` or fallback `rawUrl`. */
export function extractComparableOrigin(
  securityOrigin?: string | null,
  rawUrl?: string | null,
): string {
  const sec = (securityOrigin ?? '').trim();
  if (isExplicitOpaqueOrigin(sec)) {
    return '';
  }
  const candidates = sec ? [sec] : [rawUrl];
  for (const raw of candidates) {
    const s = (raw || '').trim();
    if (!s || s === 'null' || s === '://' || /^about:/i.test(s)) {
      continue;
    }
    if (/^file:\/\/\/?$/i.test(s)) {
      return 'file://';
    }
    try {
      const outer = new URL(s);
      if (outer.protocol === 'file:') {
        return 'file://';
      }
      const parsed = outer.protocol === 'blob:' ? new URL(outer.pathname) : outer;
      if (parsed.origin && parsed.origin !== 'null') {
        return parsed.origin.toLowerCase();
      }
    } catch {
      // Ignore opaque or unparseable origins.
    }
  }
  return '';
}

/** Returns the normalized comparable origin of `wc`'s main frame, or `''` if opaque/unavailable. */
export function getGuestMainOrigin(wc: ProtectedIdpWebContentsLike): string {
  return extractComparableOrigin(
    wc.mainFrame?.origin,
    wc.getURL?.() || wc.mainFrame?.url,
  );
}

function findFramePathInCdpTree(
  node: CdpFrameTreeNode | undefined,
  targetId: string,
): CdpFrameInfo[] | undefined {
  if (!node?.frame) {
    return undefined;
  }
  if (node.frame.id === targetId) {
    return [node.frame];
  }
  for (const child of node.childFrames ?? []) {
    const subPath = findFramePathInCdpTree(child, targetId);
    if (subPath) {
      return [node.frame, ...subPath];
    }
  }
  return undefined;
}

/** Verifies `frameId` and every ancestor frame share the guest main frame's non-opaque, non-IdP origin. */
export async function verifySameOriginChooserFrame(
  dbg: GuestCdpDebugger | null | undefined,
  wc: ProtectedIdpWebContentsLike,
  frameId?: string,
): Promise<VerifySameOriginChooserFrameResult> {
  const rawMainFrameOrigin = (wc.mainFrame?.origin ?? '').trim();
  if (
    isExplicitOpaqueOrigin(rawMainFrameOrigin) ||
    (rawMainFrameOrigin !== '' && !extractComparableOrigin(rawMainFrameOrigin))
  ) {
    return { allowed: false, mainOrigin: '' };
  }
  const wcOrigin = getGuestMainOrigin(wc);
  if (isGuestOnProtectedIdp(wc)) {
    return { allowed: false, mainOrigin: wcOrigin };
  }
  if (!dbg?.sendCommand) {
    const fallbackUrl = wc.getURL?.();
    return {
      allowed: !frameId && Boolean(wcOrigin),
      mainOrigin: wcOrigin,
      ...(wcOrigin ? { frameOrigin: wcOrigin } : {}),
      ...(fallbackUrl ? { frameUrl: fallbackUrl } : {}),
    };
  }
  try {
    const res = (await dbg.sendCommand('Page.getFrameTree')) as
      | CdpGetFrameTreeResult
      | undefined;
    const rootNode = res?.frameTree;
    const rootFrame = rootNode?.frame;
    if (!rootFrame?.id || isExplicitOpaqueOrigin(rootFrame.securityOrigin)) {
      return { allowed: false, mainOrigin: '' };
    }
    const mainOrigin =
      extractComparableOrigin(rootFrame.securityOrigin, rootFrame.url) ||
      wcOrigin;
    if (
      !mainOrigin ||
      isProtectedIdentityProviderOriginUrl(rootFrame.url || '') ||
      isProtectedIdentityProviderOriginUrl(rootFrame.securityOrigin || '')
    ) {
      return { allowed: false, mainOrigin };
    }
    if (!frameId || frameId === rootFrame.id) {
      return {
        allowed: true,
        mainOrigin,
        frameOrigin: mainOrigin,
        ...(rootFrame.url ? { frameUrl: rootFrame.url } : {}),
      };
    }
    const framePath = findFramePathInCdpTree(rootNode, frameId);
    if (!framePath || framePath.length === 0) {
      return { allowed: false, mainOrigin };
    }
    const opaqueSubframeUrls = new Set<string>();
    let hasOpaqueAboutSubframe = false;
    for (const wf of wc.mainFrame?.framesInSubtree ?? []) {
      if (wf && wf !== wc.mainFrame && !wf.isDestroyed?.()) {
        const wfOrig = (wf.origin ?? '').trim();
        if (
          isExplicitOpaqueOrigin(wfOrig) ||
          (wfOrig !== '' && !extractComparableOrigin(wfOrig))
        ) {
          const wfUrl = (wf.url ?? '').trim();
          if (!wfUrl || /^about:/i.test(wfUrl)) {
            hasOpaqueAboutSubframe = true;
          } else {
            opaqueSubframeUrls.add(wfUrl);
            opaqueSubframeUrls.add(stripUrlFragment(wfUrl));
          }
        }
      }
    }
    let leafUrl: string | undefined;
    for (let i = 1; i < framePath.length; i++) {
      const f = framePath[i];
      const fUrl = (f.url ?? '').trim();
      leafUrl = fUrl || leafUrl;
      const fUrlNoHash = stripUrlFragment(fUrl);
      const isOpaqueMatch = fUrl
        ? opaqueSubframeUrls.has(fUrl) || opaqueSubframeUrls.has(fUrlNoHash)
        : hasOpaqueAboutSubframe;
      if (
        isProtectedIdentityProviderOriginUrl(fUrl) ||
        isProtectedIdentityProviderOriginUrl(f.securityOrigin || '') ||
        isOpaqueMatch ||
        (/^about:/i.test(fUrl) && hasOpaqueAboutSubframe)
      ) {
        return { allowed: false, mainOrigin };
      }
      const fOrigin = extractComparableOrigin(f.securityOrigin, fUrl);
      if (!fOrigin || fOrigin !== mainOrigin) {
        return { allowed: false, mainOrigin };
      }
    }
    return {
      allowed: true,
      mainOrigin,
      frameOrigin: mainOrigin,
      ...(leafUrl ? { frameUrl: leafUrl } : {}),
    };
  } catch {
    return { allowed: false, mainOrigin: wcOrigin };
  }
}

const buildResolveDomPathExpr = (
  domPath: readonly DomPathStep[],
  fileCount: number,
): string =>
  `(() => { const steps = ${JSON.stringify(domPath)}; let node = document; for (let i = 0; i < steps.length && node; i++) { const s = steps[i]; if (s.kind === 'child') node = node.childNodes && s.index < node.childNodes.length ? node.childNodes[s.index] : null; else if (s.kind === 'shadow') node = node.shadowRoot || null; else if (s.kind === 'frame') { try { node = node.contentDocument || null; } catch { node = null; } } else node = null; } if (!node || node.nodeType !== 1 || String(node.tagName || '').toUpperCase() !== 'INPUT' || String(node.type || '').toLowerCase() !== 'file') return 'ERR:element is not an <input type="file">'; if (node.disabled) return 'ERR:element is disabled'; if (${Number(fileCount) || 0} > 1 && !node.multiple && !node.hasAttribute('multiple')) return 'ERR:non-multiple file input accepts at most one file'; return node; })()`;

const COMPUTE_CONNECTED_DOM_PATH_FN =
  "function() { const el = this; if (!el || !el.isConnected) return null; const rev = []; for (let guard = 0, curr = el; guard < 512 && curr; guard++) { const p = curr.parentNode; if (p) { const idx = Array.prototype.indexOf.call(p.childNodes, curr); if (idx < 0) return null; rev.push({kind: 'child', index: idx}); curr = p; continue; } if (curr.host) { rev.push({kind: 'shadow'}); curr = curr.host; continue; } const win = curr.defaultView; if (!win) return null; if (win === win.top) return rev.reverse(); let frameEl = null; try { frameEl = win.frameElement; } catch { return null; } if (!frameEl) return null; rev.push({kind: 'frame'}); curr = frameEl; } return null; }";

const APPLY_VIRTUAL_FILES_FN =
  "function(entries) { const el = this; if (!el || String(el.tagName || '').toUpperCase() !== 'INPUT' || String(el.type || '').toLowerCase() !== 'file') return {ok: false, error: 'element is not an <input type=\"file\">'}; if (el.disabled) return {ok: false, error: 'element is disabled'}; const list = Array.isArray(entries) ? entries : []; if (list.length > 1 && !el.multiple && !el.hasAttribute('multiple')) return {ok: false, error: 'non-multiple file chooser accepts at most one file'}; const win = (el.ownerDocument && el.ownerDocument.defaultView) || window; const dt = new (win.DataTransfer || DataTransfer)(); const FileCtor = win.File || File; for (let i = 0; i < list.length; i++) { const bin = atob(list[i].base64 || ''); const bytes = new Uint8Array(bin.length); for (let b = 0; b < bin.length; b++) bytes[b] = bin.charCodeAt(b); dt.items.add(new FileCtor([bytes], list[i].name || 'file', {type: list[i].mimeType || 'application/octet-stream'})); } el.files = dt.files; const Ev = win.Event || Event; el.dispatchEvent(new Ev('input', {bubbles: true, composed: true})); el.dispatchEvent(new Ev('change', {bubbles: true})); return {ok: true}; }";

async function callFunctionOnBackendNode<T>(
  dbg: GuestCdpDebugger,
  frameId: string | undefined,
  backendNodeId: number | undefined,
  functionDeclaration: string,
  args: CdpRuntimeCallArgument[],
  mapResult: (value: unknown) => T,
  fallback: T,
): Promise<T> {
  if (
    !dbg.sendCommand ||
    typeof backendNodeId !== 'number' ||
    backendNodeId <= 0
  ) {
    return fallback;
  }
  let objectId: string | undefined;
  try {
    let targetFrameId = frameId;
    if (!targetFrameId) {
      const ft = (await dbg.sendCommand('Page.getFrameTree')) as
        | CdpGetFrameTreeResult
        | undefined;
      targetFrameId = ft?.frameTree?.frame?.id;
    }
    const w = targetFrameId
      ? ((await dbg.sendCommand('Page.createIsolatedWorld', {
          frameId: targetFrameId,
          worldName: 'agy_file_upload',
          grantUniversalAccess: false,
        })) as CdpCreateIsolatedWorldResult | undefined)
      : undefined;
    const res = (await dbg.sendCommand('DOM.resolveNode', {
      backendNodeId,
      ...(typeof w?.executionContextId === 'number'
        ? { executionContextId: w.executionContextId }
        : {}),
    })) as CdpResolveNodeResult | undefined;
    objectId = res?.object?.objectId;
    if (!objectId) {
      return fallback;
    }
    const callRes = (await dbg.sendCommand('Runtime.callFunctionOn', {
      objectId,
      functionDeclaration,
      arguments: args,
      returnByValue: true,
    })) as CdpRuntimeCallFunctionOnResult | undefined;
    if (callRes?.exceptionDetails) {
      return fallback;
    }
    return mapResult(callRes?.result?.value);
  } catch {
    return fallback;
  } finally {
    if (objectId) {
      void dbg
        .sendCommand('Runtime.releaseObject', { objectId })
        .catch(() => undefined);
    }
  }
}

/** Resolves a structural DOM path from `window.top.document` for a connected `<input type="file">` backend node. */
export async function resolveConnectedDomPathForBackendNode(
  dbg: GuestCdpDebugger,
  frameId: string | undefined,
  backendNodeId: number | undefined,
): Promise<DomPathStep[] | undefined> {
  return callFunctionOnBackendNode<DomPathStep[] | undefined>(
    dbg,
    frameId,
    backendNodeId,
    COMPUTE_CONNECTED_DOM_PATH_FN,
    [],
    (val) => (isValidDomPath(val) ? [...val] : undefined),
    undefined,
  );
}

/** Populates (or clears when `virtualFiles` is empty) an `<input type="file">` backend node in an isolated world. */
export async function attachVirtualFilesToBackendNode(
  dbg: GuestCdpDebugger,
  frameId: string | undefined,
  backendNodeId: number | undefined,
  virtualFiles: readonly VirtualUploadFile[],
): Promise<AttachVirtualFilesResult> {
  return callFunctionOnBackendNode<AttachVirtualFilesResult>(
    dbg,
    frameId,
    backendNodeId,
    APPLY_VIRTUAL_FILES_FN,
    [{ value: virtualFiles }],
    (val) => {
      const r = val as VirtualFilesApplyResult | null | undefined;
      return r?.ok === true
        ? { ok: true }
        : actionErr(r?.error || 'Failed to set virtual files');
    },
    actionErr('Intercepted file chooser did not expose a target input node'),
  );
}

function validateVirtualUploadFiles(
  virtualFiles: readonly unknown[],
): ValidateVirtualUploadFilesResult {
  if (virtualFiles.length > MAX_UPLOAD_FILE_COUNT) {
    return {
      ok: false,
      errorCode: 'SCRIPT_ERROR',
      error: `Cannot attach more than ${MAX_UPLOAD_FILE_COUNT} virtual files in one call (got ${virtualFiles.length})`,
    };
  }
  const out: ValidatedVirtualUploadFile[] = [];
  let totalBytes = 0;
  for (const item of virtualFiles) {
    if (!item || typeof item !== 'object' || Array.isArray(item)) {
      return {
        ok: false,
        errorCode: 'SCRIPT_ERROR',
        error: 'Invalid virtual file entry',
      };
    }
    const r = item as Record<string, unknown>;
    const name = typeof r.name === 'string' ? r.name.trim() : '';
    const mimeType =
      typeof r.mimeType === 'string' && r.mimeType
        ? r.mimeType
        : 'application/octet-stream';
    const base64 = typeof r.base64 === 'string' ? r.base64 : null;
    if (
      !name ||
      name.length > 255 ||
      /[\0/\\]/.test(name) ||
      name === '.' ||
      name === '..' ||
      mimeType.length > 255 ||
      mimeType.includes('\0') ||
      base64 === null ||
      base64.length % 4 !== 0 ||
      !/^[A-Za-z0-9+/]*={0,2}$/.test(base64)
    ) {
      return {
        ok: false,
        errorCode: 'SCRIPT_ERROR',
        error: `Invalid virtual file "${name || 'unknown'}"`,
      };
    }
    const pad = base64.endsWith('==') ? 2 : base64.endsWith('=') ? 1 : 0;
    const decodedBytes =
      base64.length === 0 ? 0 : (base64.length * 3) / 4 - pad;
    if (decodedBytes > MAX_VIRTUAL_UPLOAD_FILE_BYTES) {
      return {
        ok: false,
        errorCode: 'SCRIPT_ERROR',
        error: `Virtual file "${name}" (${decodedBytes} bytes) exceeds ${MAX_VIRTUAL_UPLOAD_FILE_BYTES}-byte limit`,
      };
    }
    totalBytes += decodedBytes;
    if (totalBytes > MAX_VIRTUAL_UPLOAD_TOTAL_BYTES) {
      return {
        ok: false,
        errorCode: 'SCRIPT_ERROR',
        error: `Total virtual upload size (${totalBytes} bytes) exceeds ${MAX_VIRTUAL_UPLOAD_TOTAL_BYTES}-byte limit`,
      };
    }
    out.push({ name, mimeType, base64 });
  }
  return { ok: true, files: out };
}

/** Applies an intercepted `GuestFileChooserDecision` onto `pending.backendNodeId` (including `[]` clear). */
export async function applyInterceptedChooserDecision(
  dbg: GuestCdpDebugger,
  pending: PendingFileChooserTarget,
  decision: GuestFileChooserDecision,
  consumePending: () => void,
): Promise<ApplyInterceptedChooserResult> {
  if (
    Array.isArray(decision.virtualFiles) &&
    decision.virtualFiles.length > 0
  ) {
    const validatedVirt = validateVirtualUploadFiles(decision.virtualFiles);
    if (!validatedVirt.ok) {
      return validatedVirt;
    }
    if (!pending.multiple && validatedVirt.files.length > 1) {
      return actionErr('non-multiple file chooser accepts at most one file');
    }
    consumePending();
    if (
      typeof pending.backendNodeId === 'number' &&
      pending.backendNodeId > 0
    ) {
      await dbg
        .sendCommand?.('DOM.setFileInputFiles', {
          files: [],
          backendNodeId: pending.backendNodeId,
        })
        ?.catch(() => undefined);
    }
    return attachVirtualFilesToBackendNode(
      dbg,
      pending.frameId,
      pending.backendNodeId,
      validatedVirt.files,
    );
  }
  const rawFiles = Array.isArray(decision.files) ? decision.files : [];
  if (!pending.multiple && rawFiles.length > 1) {
    return actionErr('non-multiple file chooser accepts at most one file');
  }
  const validated = resolveValidatedStagedUploadFiles(
    decision.allowedUploadsDir,
    rawFiles,
    {
      expectedCascadeId: decision.expectedConversationId,
      maxFileCount: decision.maxFileCount,
      maxFileBytes: decision.maxFileBytes,
      maxTotalBytes: decision.maxTotalBytes,
    },
  );
  if (!validated.ok) {
    return validated;
  }
  consumePending();
  if (typeof pending.backendNodeId !== 'number' || pending.backendNodeId <= 0) {
    return actionErr(
      'Intercepted file chooser did not expose a target input node',
    );
  }
  if (validated.files.length === 0) {
    await dbg
      .sendCommand?.('DOM.setFileInputFiles', {
        files: [],
        backendNodeId: pending.backendNodeId,
      })
      ?.catch(() => undefined);
    return attachVirtualFilesToBackendNode(
      dbg,
      pending.frameId,
      pending.backendNodeId,
      [],
    );
  }
  try {
    await dbg.sendCommand?.('DOM.setFileInputFiles', {
      files: validated.files,
      backendNodeId: pending.backendNodeId,
    });
    return { ok: true };
  } catch (err) {
    return actionErr(err instanceof Error ? err.message : String(err));
  }
}

/** Validates staged `uploads/` files and attaches (or clears when `[]`) onto `request.domPath`. */
export async function attachStagedFilesByDomPath(
  wc: GuestFileAttachmentWebContents,
  request: AttachStagedFilesRequest,
): Promise<AttachStagedFilesResult> {
  if (isGuestOnProtectedIdp(wc)) {
    return idpBlockedErr();
  }
  const domPath = request?.domPath;
  if (!isValidDomPath(domPath)) {
    return actionErr('Invalid target DOM path for <input type="file">');
  }
  const validated = resolveValidatedStagedUploadFiles(
    request.allowedUploadsDir,
    Array.isArray(request.files) ? request.files : [],
    {
      expectedCascadeId: request.expectedConversationId,
      maxFileCount: request.maxFileCount,
      maxFileBytes: request.maxFileBytes,
      maxTotalBytes: request.maxTotalBytes,
    },
  );
  if (!validated.ok) {
    return validated;
  }
  const dbg = wc.debugger;
  if (!dbg || wc.isDestroyed?.() || !ensureGuestDebuggerAttached(dbg)) {
    return actionErr('Guest debugger is unavailable for setInputFiles');
  }
  let objectId: string | undefined;
  try {
    const ft = (await dbg.sendCommand?.('Page.getFrameTree')) as
      | CdpGetFrameTreeResult
      | undefined;
    const mainFrameId = ft?.frameTree?.frame?.id;
    if (!mainFrameId) {
      return actionErr('Unable to resolve guest main frame for setInputFiles');
    }
    const world = (await dbg.sendCommand?.('Page.createIsolatedWorld', {
      frameId: mainFrameId,
      worldName: 'agy_file_upload',
      grantUniversalAccess: false,
    })) as CdpCreateIsolatedWorldResult | undefined;
    if (typeof world?.executionContextId !== 'number') {
      return actionErr('Unable to create isolated world for setInputFiles');
    }
    const evalRes = (await dbg.sendCommand?.('Runtime.evaluate', {
      contextId: world.executionContextId,
      expression: buildResolveDomPathExpr(domPath, validated.files.length),
      returnByValue: false,
    })) as CdpRuntimeEvaluateResult | undefined;
    if (evalRes?.exceptionDetails) {
      return actionErr(
        evalRes.exceptionDetails.text || 'Failed to resolve <input type="file">',
      );
    }
    if (
      typeof evalRes?.result?.value === 'string' &&
      evalRes.result.value.startsWith('ERR:')
    ) {
      return actionErr(evalRes.result.value.slice('ERR:'.length));
    }
    objectId = evalRes?.result?.objectId;
    if (!objectId) {
      return actionErr('Target <input type="file"> could not be resolved');
    }
    if (validated.files.length === 0) {
      const clearRes = (await dbg.sendCommand?.('Runtime.callFunctionOn', {
        objectId,
        functionDeclaration: APPLY_VIRTUAL_FILES_FN,
        arguments: [{ value: [] }],
        returnByValue: true,
      })) as CdpRuntimeCallFunctionOnResult | undefined;
      const r = clearRes?.result?.value as
        | VirtualFilesApplyResult
        | null
        | undefined;
      return !clearRes?.exceptionDetails && r?.ok === true
        ? { ok: true }
        : actionErr(
            clearRes?.exceptionDetails?.text ||
              r?.error ||
              'Failed to clear file input',
          );
    }
    await dbg.sendCommand?.('DOM.setFileInputFiles', {
      objectId,
      files: validated.files,
    });
    return { ok: true };
  } catch (err) {
    return actionErr(err instanceof Error ? err.message : String(err));
  } finally {
    if (objectId) {
      void dbg
        .sendCommand?.('Runtime.releaseObject', { objectId })
        ?.catch(() => undefined);
    }
  }
}
