/**
 * Predicate deciding whether a guest `WebContents` (by numeric ID) is
 * agent-controlled or agent-owned.
 */
export type GuestAgentPredicate = (webContentsId: number) => boolean;

const predicates = new Set<GuestAgentPredicate>();

/**
 * Registers a predicate consulted by {@link isGuestAgentControlled} and returns
 * an idempotent callback that removes the registration.
 */
export function registerGuestAgentControlPredicate(
  predicate: GuestAgentPredicate,
): () => void {
  predicates.add(predicate);
  return () => {
    predicates.delete(predicate);
  };
}

const ownershipPredicates = new Set<GuestAgentPredicate>();

/**
 * Registers a predicate consulted by {@link isGuestAgentOwned}: the guest
 * belongs to an agent, between steps too (for example a tab the agent pinned).
 * Returns an idempotent callback that removes the registration.
 */
export function registerGuestAgentOwnershipPredicate(
  predicate: GuestAgentPredicate,
): () => void {
  ownershipPredicates.add(predicate);
  return () => {
    ownershipPredicates.delete(predicate);
  };
}

/**
 * Returns `true` when `webContentsId` is agent-controlled right now
 * ({@link isGuestAgentControlled}) or owned by an agent between its steps.
 * Policies that must hold for the agent's whole session (such as signing out
 * cross-site frames) use this; per-step policies use
 * {@link isGuestAgentControlled}.
 */
export function isGuestAgentOwned(webContentsId: number): boolean {
  if (isGuestAgentControlled(webContentsId)) {
    return true;
  }
  for (const predicate of ownershipPredicates) {
    try {
      if (predicate(webContentsId)) {
        return true;
      }
    } catch {
      // Ignore predicates throwing on tearing-down guests.
    }
  }
  return false;
}

/**
 * Returns `true` when `webContentsId` is a positive integer marked as
 * agent-controlled by at least one registered predicate.
 */
export function isGuestAgentControlled(webContentsId: number): boolean {
  if (
    typeof webContentsId !== 'number' ||
    !Number.isInteger(webContentsId) ||
    webContentsId <= 0
  ) {
    return false;
  }
  for (const predicate of predicates) {
    try {
      if (predicate(webContentsId)) {
        return true;
      }
    } catch {
      // Ignore predicates throwing on tearing-down guests so remaining predicates still run.
    }
  }
  return false;
}
