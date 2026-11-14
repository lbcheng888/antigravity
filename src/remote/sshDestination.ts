// TODO(nathanjuan): Share with the IDE's SSH extension via a shared package
// (antigravity-remote-openssh, src/ssh/sshDestination.ts).
import * as crypto from 'crypto';
import * as net from 'net';
import { RemoteError } from './types';

export interface SshDestination {
  destination: string;
  label: string;
}

export interface SshTarget {
  options: string[];
  destination: string;
}

interface ParsedHostPort {
  host: string;
  port?: number;
  ipv6: boolean;
}

interface ParsedDestination {
  destination: string;
  label: string;
  user: string | undefined;
  host: string;
  port?: number;
  ipv6: boolean;
}

const SSH_URI_PREFIX = 'ssh://';
/** Generous bound; real host names are <= 253 chars and user names short. */
const MAX_DESTINATION_LENGTH = 1024;
const DISALLOWED_CHAR = /[^A-Za-z0-9._@:%[\]-]/u;
/** Segments starting with a letter, digit or `_`, joined by `@` (email logins). */
const USER_RE =
  /^[A-Za-z0-9_][A-Za-z0-9._-]*(?:@[A-Za-z0-9_][A-Za-z0-9._-]*)*$/;
/** Like OpenSSH's valid_domain() for URI hosts; `..` is checked separately. */
const HOST_RE = /^[A-Za-z0-9_][A-Za-z0-9._-]*$/;
const PORT_RE = /^[1-9][0-9]{0,4}$/;
const ZONE_RE = /^[A-Za-z0-9._-]+$/;

function invalid(reason: string): never {
  throw new RemoteError('INVALID_DESTINATION', reason);
}

function describeChar(ch: string): string {
  const code = ch.codePointAt(0) ?? 0;
  return code > 0x20 && code < 0x7f
    ? `"${ch}"`
    : `U+${code.toString(16).toUpperCase().padStart(4, '0')}`;
}

function checkUser(user: string, shown = user): void {
  if (user === '') {
    invalid('The user name before "@" is missing.');
  }
  if (user.startsWith('-')) {
    invalid('The user name must not start with "-".');
  }
  if (!USER_RE.test(user)) {
    invalid(`"${shown}" isn't a valid user name.`);
  }
}

function checkHost(host: string): void {
  if (host === '') {
    invalid('The host name is missing.');
  }
  if (host.startsWith('-')) {
    invalid('The host name must not start with "-".');
  }
  if (!HOST_RE.test(host) || host.includes('..')) {
    invalid(`"${host}" isn't a valid host name.`);
  }
}

function checkPort(port: string): number {
  const n = Number(port);
  if (!PORT_RE.test(port) || n > 65535) {
    invalid(`Invalid port "${port}": use a number from 1 to 65535.`);
  }
  return n;
}

function checkIPv6(addr: string): void {
  // Zones are checked here: Node's isIPv6() handles them differently by version.
  const pct = addr.indexOf('%');
  const ip = pct === -1 ? addr : addr.slice(0, pct);
  const zone = pct === -1 ? undefined : addr.slice(pct + 1);
  if (!net.isIPv6(ip) || (zone !== undefined && !ZONE_RE.test(zone))) {
    invalid(
      `"${addr}" isn't a valid IPv6 address. Write an IPv6 address with a port in brackets, for example [::1]:2222.`,
    );
  }
}

/** Parses `host`, `host:port`, `[v6]`, `[v6]:port` or a bare IPv6 address. */
function parseHostPort(hostPort: string): ParsedHostPort {
  if (hostPort.startsWith('[')) {
    const close = hostPort.indexOf(']');
    if (close === -1) {
      invalid(`Missing "]" after the IPv6 address in "${hostPort}".`);
    }
    const host = hostPort.slice(1, close);
    checkIPv6(host);
    const rest = hostPort.slice(close + 1);
    if (rest === '') {
      return { host, ipv6: true };
    }
    if (!rest.startsWith(':')) {
      invalid(`Unexpected "${rest}" after "]".`);
    }
    return { host, port: checkPort(rest.slice(1)), ipv6: true };
  }
  const colons = hostPort.split(':').length - 1;
  if (colons >= 2) {
    // A bare IPv6 address; it can't carry a port without brackets.
    checkIPv6(hostPort);
    return { host: hostPort, ipv6: true };
  }
  if (colons === 1) {
    const i = hostPort.indexOf(':');
    const host = hostPort.slice(0, i);
    checkHost(host);
    return { host, port: checkPort(hostPort.slice(i + 1)), ipv6: false };
  }
  checkHost(hostPort);
  return { host: hostPort, ipv6: false };
}

/** Strict %XX decoding of a URI user name (bytes, like OpenSSH's urldecode). */
function percentDecode(s: string): string {
  if (/%(?![0-9A-Fa-f]{2})/.test(s)) {
    invalid(`Invalid percent-encoding in the user name "${s}".`);
  }
  return s.replace(/%([0-9A-Fa-f]{2})/g, (_m: string, hex: string) =>
    String.fromCharCode(parseInt(hex, 16)),
  );
}

function formatLabel(
  user: string | undefined,
  host: string,
  port: number | undefined,
  ipv6: boolean,
): string {
  const hostPart =
    port === undefined ? host : `${ipv6 ? `[${host}]` : host}:${port}`;
  return user === undefined ? hostPart : `${user}@${hostPart}`;
}

function canonical(
  user: string | undefined,
  host: string,
  port: number | undefined,
  ipv6: boolean,
): ParsedDestination {
  const label = formatLabel(user, host, port, ipv6);
  if (port === undefined) {
    // Brackets (if any) are dropped: ssh would take "[::1]" literally.
    return { destination: label, label, user, host, ipv6 };
  }
  // ssh only accepts a port inside the destination in URI form (OpenSSH >=
  // 7.7). User names here are [A-Za-z0-9._@-], so only '@' needs encoding.
  const userInfo = user === undefined ? '' : `${user.replace(/@/g, '%40')}@`;
  const hostInfo = ipv6 ? `[${host}]` : host;
  return {
    destination: `${SSH_URI_PREFIX}${userInfo}${hostInfo}:${port}`,
    label,
    user,
    host,
    port,
    ipv6,
  };
}

function parseUri(body: string): ParsedDestination {
  // Like OpenSSH: the user ends at the first '@' and is percent-decoded.
  const parts = body.split('@');
  if (parts.length > 2) {
    invalid('Write "@" inside the user name of an ssh:// URI as %40.');
  }
  let user: string | undefined;
  if (parts.length === 2) {
    if (parts[0] === '') {
      invalid('The user name before "@" is missing.');
    }
    // Check the decoded name: that's what %r expansions and the remote see.
    user = percentDecode(parts[0]);
    checkUser(user, parts[0]);
  }
  const hostPort = parts[parts.length - 1];
  if (!hostPort.startsWith('[') && hostPort.split(':').length > 2) {
    invalid(
      'Write an IPv6 address in an ssh:// URI in brackets, for example ssh://[::1]:2222.',
    );
  }
  const { host, port, ipv6 } = parseHostPort(hostPort);
  return canonical(user, host, port, ipv6);
}

function parsePlain(input: string): ParsedDestination {
  // Like ssh, split the user from the host at the last '@'.
  const at = input.lastIndexOf('@');
  const user = at === -1 ? undefined : input.slice(0, at);
  if (user !== undefined) {
    checkUser(user);
  }
  const { host, port, ipv6 } = parseHostPort(input.slice(at + 1));
  return canonical(user, host, port, ipv6);
}

function parseDestination(input: string): ParsedDestination {
  const s = input.trim();
  if (s === '') {
    invalid('Enter an SSH host, for example "devbox" or "user@host:2222".');
  }
  if (s.length > MAX_DESTINATION_LENGTH) {
    invalid(
      `The SSH host is too long (at most ${MAX_DESTINATION_LENGTH} characters).`,
    );
  }
  if (s.startsWith('-')) {
    invalid('The SSH host must not start with "-".');
  }
  if (/[\s\p{Cc}]/u.test(s)) {
    invalid('The SSH host must not contain spaces or control characters.');
  }
  // The scheme is case-sensitive for ssh too: "SSH://h" is not a URI to it.
  const isUri = s.startsWith(SSH_URI_PREFIX);
  const body = isUri ? s.slice(SSH_URI_PREFIX.length) : s;
  const bad = DISALLOWED_CHAR.exec(body);
  if (bad) {
    invalid(`The SSH host must not contain ${describeChar(bad[0])}.`);
  }
  return isUri ? parseUri(body) : parsePlain(s);
}

/**
 * Validates and normalizes a host the user typed or picked (alias, host,
 * `user@host:port`, IPv6, `ssh://` URI). Only `[A-Za-z0-9._@:%[]-]` and no
 * part starting with `-`: it can't become an ssh option or put shell
 * metacharacters into `%h`/`%r` expansions. Throws INVALID_DESTINATION.
 */
export function parseSshDestination(input: string): SshDestination {
  const { destination, label } = parseDestination(input);
  return { destination, label };
}

/**
 * ssh argv for a canonical destination: `[user@]host` after `--`, a port as
 * `-p` before it (OpenSSH rejects IPv6 literals in ssh:// URIs). Callers put
 * `options` after their own, so an explicit `-p` of theirs wins.
 */
export function toSshTarget(destination: string): SshTarget {
  const parsed = parseDestination(destination);
  if (parsed.destination !== destination) {
    invalid(
      `"${destination.trim()}" must be normalized with parseSshDestination first.`,
    );
  }
  if (parsed.port === undefined) {
    return { options: [], destination };
  }
  return {
    options: ['-p', String(parsed.port)],
    destination:
      parsed.user === undefined ? parsed.host : `${parsed.user}@${parsed.host}`,
  };
}

/**
 * Names a host's partitions and stores without exposing the host name. A
 * config file (`ssh -F`, absolute) makes another host; without one the id is
 * the destination's alone.
 */
export function hostIdFor(destination: string, configFile?: string): string {
  return crypto
    .createHash('sha256')
    .update(
      configFile === undefined ? destination : `${destination}\0${configFile}`,
      'utf8',
    )
    .digest('hex')
    .slice(0, 12);
}
