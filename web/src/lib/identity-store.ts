// The tab's identity as React state, kept in sessionStorage (see identity.ts).
import { useSyncExternalStore } from "react";

import { clearIdentity, loadIdentity, saveIdentity, type Identity } from "./identity";

const listeners = new Set<() => void>();
let cached: Identity | null | undefined;

export const identityStore = {
  subscribe(listener: () => void): () => void {
    listeners.add(listener);
    return () => listeners.delete(listener);
  },
  get(): Identity | null {
    if (cached === undefined) cached = loadIdentity();
    return cached;
  },
  set(identity: Identity | null): void {
    if (identity) saveIdentity(identity);
    else clearIdentity();
    cached = identity;
    for (const listener of listeners) listener();
  },
};

/**
 * The tab's identity: undefined while it can't be known yet (on the server
 * and during hydration), null when the user still has to pick a name.
 */
export function useIdentity(): Identity | null | undefined {
  return useSyncExternalStore(identityStore.subscribe, identityStore.get, () => undefined);
}
