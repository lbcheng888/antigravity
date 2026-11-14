import { isGuestAgentControlled } from './guestAgentControl';

/**
 * Minimal `WebContents` surface inspected by {@link shouldDenyGuestPermission},
 * satisfied by Electron's `WebContents` as well as lightweight structural mocks.
 */
export interface GuestPermissionWebContents {
  readonly id?: unknown;
}

/**
 * Permission request handler passed to `Session.setPermissionRequestHandler`
 * by {@link configureSessionAgentPermissionPolicy}.
 */
export type GuestPermissionRequestHandler = (
  webContents: GuestPermissionWebContents | null | undefined,
  permission: string,
  callback: (permissionGranted: boolean) => void,
) => void;

/**
 * Minimal `Session` surface required by
 * {@link configureSessionAgentPermissionPolicy}, satisfied by Electron's
 * `Session` as well as lightweight structural mocks.
 */
export interface GuestPermissionSession {
  setPermissionRequestHandler?: (
    handler: GuestPermissionRequestHandler,
  ) => void;
}

/**
 * Permissions denied to built-in browser guests while they are under active
 * agent control. Fullscreen lets a page cover the Hub chrome (tab strip, URL
 * bar, agent overlay) so the model and the user lose the trusted UI; pointer
 * lock and keyboard lock complete that spoof by hiding the real cursor and
 * swallowing Escape and system shortcuts; see go/browser-use-agent-security.
 * All of them reach the session permission request handler (Electron
 * `WebContentsPermissionHelper::Request{Fullscreen,PointerLock,KeyboardLock}Permission`).
 */
export const AGENT_DENIED_PERMISSIONS: ReadonlySet<string> = new Set([
  'fullscreen',
  'automatic-fullscreen',
  'pointerLock',
  'keyboardLock',
]);

const configuredPermissionSessions = new WeakSet<GuestPermissionSession>();

/**
 * Returns `true` when `permission` must be denied for `webContents` because the
 * guest is currently agent-controlled and the permission is in
 * {@link AGENT_DENIED_PERMISSIONS}.
 */
export function shouldDenyGuestPermission(
  webContents: GuestPermissionWebContents | null | undefined,
  permission: string,
): boolean {
  if (!AGENT_DENIED_PERMISSIONS.has(permission)) {
    return false;
  }
  const id = webContents?.id;
  return typeof id === 'number' && isGuestAgentControlled(id);
}

/**
 * Installs (once per session) a permission request handler on a built-in
 * browser guest session that denies {@link AGENT_DENIED_PERMISSIONS} for
 * agent-controlled guests and grants every other request, matching Electron's
 * default behavior when no handler is installed. The decision is resolved per
 * requesting `WebContents` because user and agent tabs share a partition.
 *
 * No permission check handler is installed on purpose: Electron only consults
 * the request handler for fullscreen, pointer lock and keyboard lock, and
 * installing a check handler changes defaults for unrelated permissions (e.g.
 * File System Access grants).
 */
export function configureSessionAgentPermissionPolicy(
  targetSession: GuestPermissionSession | null | undefined,
): void {
  if (
    !targetSession ||
    typeof targetSession.setPermissionRequestHandler !== 'function' ||
    configuredPermissionSessions.has(targetSession)
  ) {
    return;
  }
  configuredPermissionSessions.add(targetSession);
  targetSession.setPermissionRequestHandler(
    (webContents, permission, callback) => {
      callback(!shouldDenyGuestPermission(webContents, permission));
    },
  );
}
