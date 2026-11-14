/**
 * Parser for the bootstrap script's stdout: `<marker> <EVENT> <payload>`
 * lines, with a secret marker per connection, so other output (rc files,
 * MOTD) can't forge events. The marker may start mid-line: rc output without
 * a trailing newline can glue onto a protocol line.
 */

import * as crypto from 'crypto';

export type BootstrapArch = 'x64' | 'arm64';

export type BootstrapErrorCode =
  | 'UNSUPPORTED_HOST'
  | 'SHARED_HOME_UNSUPPORTED'
  | 'INSTALL_FAILED'
  | 'LS_START_FAILED'
  | 'LS_STOPPED';

export interface BootstrapDiscovery {
  pid: number;
  httpsPort: number;
  httpPort: number;
  lspPort: number;
  lsVersion: string;
  csrfToken: string;
}

export type BootstrapEvent =
  | { type: 'status'; text: string }
  | { type: 'needUpload'; arch: BootstrapArch; reason: string }
  | { type: 'ready'; discovery: BootstrapDiscovery }
  | { type: 'openUrl'; url: string }
  | { type: 'error'; code: BootstrapErrorCode | 'UNKNOWN'; message: string };

/** The codes the script reports (agy_fail); any other is UNKNOWN. */
const BOOTSTRAP_ERROR_CODES: readonly BootstrapErrorCode[] = [
  'UNSUPPORTED_HOST',
  'SHARED_HOME_UNSUPPORTED',
  'INSTALL_FAILED',
  'LS_START_FAILED',
  'LS_STOPPED',
];

/** Markers accepted by buildBootstrapScript(); newMarker() produces them. */
export const MARKER_PATTERN = /^[a-f0-9]{16,128}$/;

export function newMarker(): string {
  return crypto.randomBytes(12).toString('hex');
}

/** Must match writeDiscoveryFile in the language server (server.go). */
export function discoveryFileNameFor(workspaceId: string): string {
  const hash = crypto.createHash('sha256').update(workspaceId, 'utf8');
  return `ls_${hash.digest('hex').slice(0, 16)}.json`;
}

/**
 * Null for non-protocol lines, unknown events, OPEN_URL payloads that aren't
 * one token and NEED_UPLOAD without `<x64|arm64> <reason>`. A bad READY's
 * error never echoes it: it has the token.
 */
export function parseBootstrapLine(
  line: string,
  marker: string,
): BootstrapEvent | null {
  const prefix = `${marker} `;
  const at = marker ? line.indexOf(prefix) : -1;
  if (at < 0) {
    return null;
  }
  const rest = line.slice(at + prefix.length).replace(/[\r\n]+$/, '');
  const space = rest.indexOf(' ');
  const event = space < 0 ? rest : rest.slice(0, space);
  const payload = space < 0 ? '' : rest.slice(space + 1);
  switch (event) {
    case 'STATUS':
      return { type: 'status', text: payload };
    case 'NEED_UPLOAD': {
      const [, arch, reason = ''] = /^(\S+) (.*)$/.exec(payload) ?? [];
      return arch === 'x64' || arch === 'arm64'
        ? { type: 'needUpload', arch, reason }
        : null;
    }
    case 'READY':
      return parseReady(payload);
    case 'OPEN_URL':
      return /^\S+$/.test(payload) ? { type: 'openUrl', url: payload } : null;
    case 'ERROR':
      return parseError(payload);
    default:
      return null;
  }
}

type BootstrapErrorEvent = Extract<BootstrapEvent, { type: 'error' }>;

function readyError(message: string): BootstrapErrorEvent {
  return { type: 'error', code: 'UNKNOWN', message: `READY: ${message}` };
}

function intInRange(v: unknown, min: number, max: number): number | undefined {
  return typeof v === 'number' && Number.isInteger(v) && v >= min && v <= max
    ? v
    : undefined;
}

function nonEmptyString(v: unknown): string | undefined {
  return typeof v === 'string' && v !== '' ? v : undefined;
}

function parseReady(payload: string): BootstrapEvent {
  let parsed: unknown;
  try {
    parsed = JSON.parse(payload);
  } catch {
    return readyError('discovery data is not valid JSON');
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
    return readyError('discovery data is not a JSON object');
  }
  const d = parsed as Record<string, unknown>;
  const fields = {
    pid: intInRange(d.pid, 1, 0x7fffffff),
    httpsPort: intInRange(d.httpsPort, 1, 65535),
    httpPort: intInRange(d.httpPort, 0, 65535),
    lspPort: intInRange(d.lspPort, 0, 65535),
    lsVersion: nonEmptyString(d.lsVersion),
    csrfToken: nonEmptyString(d.csrfToken),
  };
  const { pid, httpsPort, httpPort, lspPort, lsVersion, csrfToken } = fields;
  if (
    pid !== undefined &&
    httpsPort !== undefined &&
    httpPort !== undefined &&
    lspPort !== undefined &&
    lsVersion !== undefined &&
    csrfToken !== undefined
  ) {
    return {
      type: 'ready',
      discovery: { pid, httpsPort, httpPort, lspPort, lsVersion, csrfToken },
    };
  }
  const invalid = Object.entries(fields)
    .filter(([, value]) => value === undefined)
    .map(([name]) => name);
  return readyError(`invalid ${invalid.join(', ')}`);
}

function parseError(payload: string): BootstrapErrorEvent {
  const m = /^code=(\S+)(?: (.*))?$/.exec(payload);
  if (!m) {
    return { type: 'error', code: 'UNKNOWN', message: payload };
  }
  const [, code, message = ''] = m;
  const known = BOOTSTRAP_ERROR_CODES.find((c) => c === code);
  if (known !== undefined) {
    return { type: 'error', code: known, message };
  }
  // Keep the unrecognized code visible for debugging.
  return {
    type: 'error',
    code: 'UNKNOWN',
    message: message ? `${code}: ${message}` : code,
  };
}
