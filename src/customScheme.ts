import { protocol } from 'electron';
import { FAVICON_SCHEME, lookupFaviconToken } from './faviconStore';
import { fetchWithoutFollowing, pluginResponse } from './redirects';

// A map of extension authority -> original URL (http://localhost:<port>)
// The authority is usually a hash of unique extension identifiers
// like extension ID + port + project ID. An extension running on localhost:<port>
// is then exposed on plugin://<authority>.
export const extensionAuthorities = new Map<string, string>();

export function registerCustomSchemes(): void {
  protocol.registerSchemesAsPrivileged([
    {
      scheme: 'plugin',
      privileges: {
        standard: true,
        secure: true,
        supportFetchAPI: true,
        corsEnabled: true,
        allowServiceWorkers: true,
        codeCache: true,
      },
    },
    {
      // Serves cached favicon bytes to the Hub renderer's <img> tags only, so
      // no fetch, CORS, or service-worker privileges.
      scheme: FAVICON_SCHEME,
      privileges: { standard: true, secure: true },
    },
  ]);
}

/**
 * Handles `agy-favicon://<token>/` by looking the token up in the favicon
 * store. Never fetches: unknown tokens and any other path are 404.
 */
export function handleFaviconProtocolRequest(
  request: Pick<Request, 'method' | 'url'>,
): Response {
  if (request.method !== 'GET') {
    return new Response(null, { status: 405 });
  }
  let url: URL;
  try {
    url = new URL(request.url);
  } catch {
    return new Response(null, { status: 404 });
  }
  const favicon =
    url.pathname === '/' ? lookupFaviconToken(url.hostname) : undefined;
  if (!favicon) {
    return new Response(null, { status: 404 });
  }
  return new Response(new Uint8Array(favicon.bytes), {
    status: 200,
    headers: {
      'Content-Type': favicon.mime,
      'X-Content-Type-Options': 'nosniff',
      // A top-level load of an SVG favicon must not run as a document.
      'Content-Security-Policy': "default-src 'none'; sandbox",
      'Cache-Control': 'no-store',
    },
  });
}

export function registerCustomSchemeHandlers(): void {
  // Handle custom scheme for UI extensions
  protocol.handle('plugin', async (request: Request): Promise<Response> => {
    const url = new URL(request.url);
    const authority = url.hostname;
    const originalHost = extensionAuthorities.get(authority);
    if (!originalHost) {
      return new Response(null, { status: 404 });
    }
    // Origin plus path: resolving a path like "//host/x" against the sidecar
    // URL would switch to another host.
    const sidecar = new URL(originalHost);
    const targetUrl = new URL(`${sidecar.origin}${url.pathname}${url.search}`);
    if (targetUrl.host !== sidecar.host) {
      return new Response(null, { status: 404 });
    }
    try {
      // With the main process's network and cookies. A redirect goes back to
      // the frame if it stays on the sidecar's origin (redirects.ts).
      const response = await fetchWithoutFollowing(targetUrl.href, request);
      return pluginResponse(response, targetUrl, url);
    } catch (err: unknown) {
      console.error(`Failed to proxy request to ${targetUrl}:`, err);
      return new Response(null, { status: 500 });
    }
  });
  // Module-level `protocol` binds to the default session only: the Hub
  // renderer's session, never a guest partition.
  protocol.handle(FAVICON_SCHEME, handleFaviconProtocolRequest);
}
