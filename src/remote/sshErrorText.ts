/** The Hub's words for a failed connection: what went wrong, what to do. */
import log from 'electron-log/main';
import { getAppDataDirName } from '../paths';
import { discoveryFileNameFor } from './bootstrapProtocol';
import { describeRemoteError, type PromptSupport } from './sshArgs';
import { canPrompt } from './sshAskpass';
import {
  REMOTE_LS_WORKSPACE_ID,
  type RemoteError,
  sanitizeRemoteText,
} from './types';

function logPath(): string {
  return log.transports.file.getFile().path;
}

export function codeAndLogs(err: RemoteError): string {
  return `Error code: ${err.code}\nLogs: ${logPath()}`;
}

export function detailOf(err: RemoteError, label: string): string {
  const text = sanitizeRemoteText(err.message);
  return err.reportedByRemote ? `${label} reported: ${text}` : text;
}

/** Automatic reconnects can't ask, but Reconnect can; Windows can't yet. */
function promptSupportOf(err: RemoteError): PromptSupport {
  if (!canPrompt()) {
    return 'no';
  }
  return err.couldPrompt ? 'yes' : 'onReconnect';
}

/** The Hub's own words: what went wrong, and what to do. */
export function explanationOf(err: RemoteError, label: string): string {
  if (err.code === 'CANCELLED') {
    // A prompt's Cancel says so; else the connect was closed under the caller.
    return err.userCancelled
      ? err.message
      : 'The connection was closed before it finished.';
  }
  if (err.explainsItself) {
    return detailOf(err, label);
  }
  const what = describeRemoteError(err.code, promptSupportOf(err));
  if (err.code !== 'SHARED_HOME_UNSUPPORTED') {
    return what;
  }
  // Named here, not by the host: its text could name any file.
  const note = `~/.gemini/${getAppDataDirName()}/daemon/${discoveryFileNameFor(REMOTE_LS_WORKSPACE_ID)}.host`;
  return `${what}\nFile: ${note} on ${label}`;
}

export function userMessage(err: RemoteError, label: string): string {
  const what = explanationOf(err, label);
  if (err.code === 'CANCELLED') {
    return what;
  }
  const detail = detailOf(err, label);
  const text = detail && detail !== what ? `${what}\n${detail}` : what;
  return err.code === 'UNKNOWN' ? `${text}\nLogs: ${logPath()}` : text;
}
