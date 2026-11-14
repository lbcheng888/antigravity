/**
 * The laptop's end of a remote language server's (LS) loopback sign-in.
 * Tunnelling the port would give the remote every browser request to
 * localhost:<port>, cookies included, and let a remote process answer. So the
 * Hub takes one GET /auth/callback with the sign-in's `state`, relays only
 * its path and query, shows a fixed page and stops; anything else gets a 404.
 */
import * as http from 'http';

/** The LS's OAuth loopback redirect path (auth_client/acquirer.go). */
export const SIGN_IN_CALLBACK_PATH = '/auth/callback';

/** Reason the sign-in callback listener stopped accepting requests. */
export type SignInCallbackEndReason = 'relayed' | 'expired' | 'closed';

/** Configuration for {@link startSignInCallback}. */
export interface SignInCallbackOptions {
  port: number;
  state: string;
  listenHosts: readonly string[];
  ttlMs: number;
  relay: (pathAndQuery: string) => Promise<number> | number;
  log?: (line: string) => void;
}

/** Handle for an active loopback sign-in callback listener. */
export interface SignInCallback {
  port: number;
  state: string;
  ended: Promise<SignInCallbackEndReason>;
  close: () => Promise<void>;
}

interface CallbackPage {
  status: number;
  body: string;
}

/** Rejects with the listen error (e.g. EADDRINUSE). */
export async function startSignInCallback(
  opts: SignInCallbackOptions,
): Promise<SignInCallback> {
  let handled = false;
  let ending: Promise<void> | undefined;
  let endedWith: (how: SignInCallbackEndReason) => void = () => {};
  const ended = new Promise<SignInCallbackEndReason>((r) => (endedWith = r));
  const servers: http.Server[] = [];

  const end = (how: SignInCallbackEndReason): Promise<void> => {
    ending ??= (async () => {
      clearTimeout(timer);
      await Promise.all(
        servers.map(
          (server) =>
            new Promise<void>((resolve) => {
              server.close(() => resolve());
              server.closeAllConnections();
            }),
        ),
      );
      endedWith(how);
    })();
    return ending;
  };

  const onRequest = (
    req: http.IncomingMessage,
    res: http.ServerResponse,
  ): void => {
    // The base only makes relative request targets parseable.
    let url: URL | undefined;
    try {
      url = new URL(req.url ?? '', 'http://localhost');
    } catch {
      url = undefined;
    }
    const states = url?.searchParams.getAll('state') ?? [];
    if (
      handled ||
      ending ||
      !url ||
      req.method !== 'GET' ||
      url.pathname !== SIGN_IN_CALLBACK_PATH ||
      states.length !== 1 ||
      states[0] !== opts.state
    ) {
      respond(res, 404, 'text/plain; charset=utf-8', 'Not found\n');
      return;
    }
    handled = true;
    clearTimeout(timer);
    const pathAndQuery = `${SIGN_IN_CALLBACK_PATH}${url.search}`;
    // Subscribed now: 'close' fires once, maybe during the relay.
    const closed = new Promise<void>((resolve) => res.once('close', resolve));
    void (async () => {
      let page: CallbackPage;
      try {
        const status = await opts.relay(pathAndQuery);
        page = status >= 200 && status < 400 ? SIGNED_IN : failedPage(status);
        opts.log?.(`Passed the sign-in callback on (HTTP ${status}).`);
      } catch (err) {
        page = UNREACHABLE;
        // Code or name only: messages may quote the auth code.
        opts.log?.(
          `Could not pass the sign-in callback on (${errorLabel(err)}).`,
        );
      }
      if (!ending && !res.destroyed) {
        respond(res, page.status, 'text/html; charset=utf-8', page.body);
      }
      // Stop listening only once the browser has the page (or is gone).
      await closed;
      await end('relayed');
    })();
  };

  const timer = setTimeout(() => {
    opts.log?.('Gave up waiting for the sign-in callback.');
    void end('expired');
  }, opts.ttlMs);
  timer.unref();

  try {
    for (const host of opts.listenHosts) {
      const server = http.createServer(onRequest);
      try {
        await new Promise<void>((resolve, reject) => {
          server.once('error', reject);
          server.listen({ host, port: opts.port }, () => {
            server.removeListener('error', reject);
            resolve();
          });
        });
      } catch (err) {
        const code = errorLabel(err);
        if (
          host === '::1' &&
          (code === 'EADDRNOTAVAIL' || code === 'EAFNOSUPPORT')
        ) {
          continue; // No IPv6 loopback here, so nobody else has it either.
        }
        throw err;
      }
      server.on('error', (err: Error) =>
        opts.log?.(`Sign-in callback error: ${err.message}`),
      );
      servers.push(server);
    }
  } catch (err) {
    await end('closed');
    throw err;
  }

  return {
    port: opts.port,
    state: opts.state,
    ended,
    close: () => end('closed'),
  };
}

function errorLabel(err: unknown): string {
  if (
    typeof err === 'object' &&
    err !== null &&
    'code' in err &&
    typeof err.code === 'string'
  ) {
    return err.code;
  }
  return err instanceof Error ? err.name : 'unknown error';
}

function respond(
  res: http.ServerResponse,
  status: number,
  contentType: string,
  body: string,
): void {
  res.writeHead(status, {
    'Content-Type': contentType,
    'Content-Length': Buffer.byteLength(body),
    'Cache-Control': 'no-store',
    'Content-Security-Policy': "default-src 'none'; style-src 'unsafe-inline'",
    'Referrer-Policy': 'no-referrer',
    'X-Content-Type-Options': 'nosniff',
    Connection: 'close',
  });
  res.end(body);
}

function page(title: string, text: string): string {
  return (
    '<!doctype html><html><head><meta charset="utf-8">' +
    `<title>${title}</title><style>` +
    'body{font:16px system-ui,sans-serif;margin:4em auto;max-width:36em;' +
    'padding:0 1em;color:#202124}</style></head>' +
    `<body><h1>${title}</h1><p>${text}</p></body></html>\n`
  );
}

const SIGNED_IN: CallbackPage = {
  status: 200,
  body: page('Signed in', 'Signed in. You can close this tab.'),
};

const UNREACHABLE: CallbackPage = {
  status: 502,
  body: page(
    'Sign-in did not complete',
    'The Antigravity server on the SSH host could not be reached. ' +
      'Sign in again from Antigravity.',
  ),
};

function failedPage(status: number): CallbackPage {
  return {
    status: 502,
    body: page(
      'Sign-in did not complete',
      `The Antigravity server on the SSH host answered with HTTP ${status}. ` +
        'Sign in again from Antigravity.',
    ),
  };
}
