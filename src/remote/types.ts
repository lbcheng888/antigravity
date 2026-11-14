// Shared types for "Connect to SSH Host". The Hub runs the user's own ssh,
// one connection per host (`ssh -T -D 127.0.0.1:<socks> … -- <host> sh -s`),
// and SSH windows reach the remote language server through that SOCKS proxy.

export type RemoteErrorCode =
  | 'INVALID_DESTINATION'
  | 'AUTH_FAILED'
  | 'HOSTKEY_REJECTED'
  | 'CONNECTION_FAILED'
  | 'FORWARDING_PROHIBITED'
  | 'UNSUPPORTED_HOST'
  | 'SHARED_HOME_UNSUPPORTED'
  | 'INSTALL_FAILED'
  | 'LS_START_FAILED'
  | 'LS_STOPPED'
  | 'CONNECTION_CLOSED'
  | 'CANCELLED'
  | 'UNKNOWN';

export interface RemoteErrorOptions {
  reportedByRemote?: boolean;
  retryable?: boolean;
  userCancelled?: boolean;
  couldPrompt?: boolean;
  explainsItself?: boolean;
}

export class RemoteError extends Error {
  readonly code: RemoteErrorCode;
  readonly reportedByRemote: boolean;
  readonly retryable: boolean;
  readonly userCancelled: boolean;
  readonly couldPrompt: boolean;
  readonly explainsItself: boolean;

  constructor(
    code: RemoteErrorCode,
    message: string,
    options: RemoteErrorOptions = {},
  ) {
    super(message);
    this.code = code;
    this.name = 'RemoteError';
    this.reportedByRemote = options.reportedByRemote ?? false;
    this.retryable = options.retryable ?? false;
    this.userCancelled = options.userCancelled ?? false;
    this.couldPrompt = options.couldPrompt ?? false;
    this.explainsItself = options.explainsItself ?? false;
  }
}

export interface RecentSshHost {
  host: string;
  configFile?: string;
}

/** The Hub's connect timeout; the host's install-lock wait ends before it. */
export const REMOTE_CONNECT_TIMEOUT_SEC = 15 * 60;

/** An upload's limit on the host; the Hub's connect deadline moves by it. */
export const REMOTE_UPLOAD_TIMEOUT_SEC = 15 * 60;

/** Install root on the remote, relative to $HOME. */
export const REMOTE_INSTALL_ROOT = '.antigravity-hub-server';

/** C0/C1, bidi, line separators, zero-width and other invisibles, BOM. */
const UNSAFE_TEXT =
  // eslint-disable-next-line no-control-regex
  /[\u0000-\u001f\u007f-\u009f\u061c\u200b\u200e\u200f\u2028-\u202e\u2060-\u2069\ufeff]/g;

export interface SanitizeRemoteTextOptions {
  keepNewlines?: boolean;
}

/**
 * Strips control, bidi and invisible characters, keeping line breaks as \n
 * with keepNewlines; cuts at `max`.
 */
export function sanitizeRemoteText(
  text: string,
  max = 500,
  { keepNewlines = false }: SanitizeRemoteTextOptions = {},
): string {
  const clean = keepNewlines
    ? text
        .split(/\r\n|\r|\n/)
        .map((line) => line.replace(UNSAFE_TEXT, ' '))
        .join('\n')
    : text.replace(UNSAFE_TEXT, ' ');
  return clean.length > max ? `${clean.slice(0, max)}…` : clean;
}

/** Fixed --workspace_id for the persistent remote LS (one per user+app). */
export const REMOTE_LS_WORKSPACE_ID = 'agy-hub-ssh';
