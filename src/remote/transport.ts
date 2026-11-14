/**
 * Routes Electron sessions through an SSH connection's SOCKS proxy
 * (`ssh -D`): every request, 127.0.0.1 included, exits on the remote host, and
 * none falls back to a direct connection.
 */
import { session, type ProxyConfig, type Session } from 'electron';
import {
  fetchWithoutFollowing,
  type RedirectFetchRequest,
} from '../redirects';

export interface Transport {
  fetch: (url: string, init?: RequestInit) => Promise<Response>;
  sidecars: {
    fetch: (url: string, request: RedirectFetchRequest) => Promise<Response>;
  };
  close: () => Promise<void>;
}

const isPort = (port: unknown): port is number =>
  typeof port === 'number' &&
  Number.isInteger(port) &&
  port > 0 &&
  port < 65536;

/** Checked as text, so no URL parser can read another host into it. */
function checkTransportUrl(url: string): void {
  const m = /^http:\/\/127\.0\.0\.1:([1-9]\d{0,4})\//.exec(url);
  if (m === null || !isPort(Number(m[1]))) {
    // Not echoed: a URL can carry secrets.
    throw new Error(
      'The SSH transport only fetches http://127.0.0.1:<port>/ URLs.',
    );
  }
}

/** `<-loopback>` drops Chromium's implicit bypass of loopback hosts. */
export function sshProxyConfig(socksPort: number): ProxyConfig {
  // An invalid port must never leave the session without a proxy.
  if (!isPort(socksPort)) {
    throw new Error(`Invalid SOCKS port: ${String(socksPort)}`);
  }
  return {
    mode: 'fixed_servers',
    proxyRules: `socks5://127.0.0.1:${socksPort}`,
    proxyBypassRules: '<-loopback>',
  };
}

/** Call for each new connection (new port) before the session's requests. */
export async function routeSessionThroughSsh(
  ses: Session,
  socksPort: number,
): Promise<void> {
  await ses.setProxy(sshProxyConfig(socksPort));
  await ses.closeAllConnections();
}

/**
 * Through the tunnel, 127.0.0.1 is the remote LS with its self-signed
 * certificate. Every other host, `localhost` included, gets Chromium's (-3).
 */
export function trustLoopbackCertificate(ses: Session): void {
  ses.setCertificateVerifyProc((request, callback) => {
    callback(request.hostname === '127.0.0.1' ? 0 : -3);
  });
}

/** In memory, not `persist:`. */
export function transportPartitionFor(hostId: string): string {
  return `ssh-transport-${hostId}`;
}

/** In memory too; see Transport.sidecars. */
export function sidecarPartitionFor(hostId: string): string {
  return `ssh-sidecars-${hostId}`;
}

/**
 * No request filter: only the Hub (and, on the sidecar partition, its SSH
 * window's plugin:// handler) fetch here, one validated loopback URL each.
 */
export async function openTransport(
  hostId: string,
  socksPort: number,
): Promise<Transport> {
  const [hub, sidecars] = await Promise.all([
    openRoutedSession(transportPartitionFor(hostId), socksPort),
    openRoutedSession(sidecarPartitionFor(hostId), socksPort),
  ]);
  return {
    fetch: async (url: string, init?: RequestInit): Promise<Response> => {
      checkTransportUrl(url);
      return hub.fetch(url, { ...init, redirect: 'manual' });
    },
    sidecars: {
      fetch: async (
        url: string,
        request: RedirectFetchRequest,
      ): Promise<Response> => {
        checkTransportUrl(url);
        return fetchWithoutFollowing(url, request, sidecars);
      },
    },
    close: async (): Promise<void> => {
      await Promise.all([
        hub.closeAllConnections(),
        sidecars.closeAllConnections(),
      ]);
    },
  };
}

async function openRoutedSession(
  partition: string,
  socksPort: number,
): Promise<Session> {
  const ses = session.fromPartition(partition);
  // No spellchecker dictionary downloads through the proxy.
  ses.setSpellCheckerLanguages([]);
  trustLoopbackCertificate(ses);
  await routeSessionThroughSsh(ses, socksPort);
  return ses;
}
