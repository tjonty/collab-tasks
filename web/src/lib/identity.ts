// Who is using this tab. There is no login: picking a display name creates a
// user, and its id goes with every request. It lives in sessionStorage, so
// each tab can be a different person, which makes a two-window demo easy.
export interface Identity {
  id: string;
  displayName: string;
}

const KEY = "collab-tasks:identity";

function sessionStore(): Storage | undefined {
  try {
    return typeof window === "undefined" ? undefined : window.sessionStorage;
  } catch {
    return undefined; // storage blocked
  }
}

export function loadIdentity(storage: Storage | undefined = sessionStore()): Identity | null {
  try {
    const value: unknown = JSON.parse(storage?.getItem(KEY) ?? "null");
    if (
      typeof value === "object" &&
      value !== null &&
      typeof (value as Identity).id === "string" &&
      typeof (value as Identity).displayName === "string"
    ) {
      return { id: (value as Identity).id, displayName: (value as Identity).displayName };
    }
  } catch {
    // Malformed: treat as no identity.
  }
  return null;
}

export function saveIdentity(identity: Identity, storage: Storage | undefined = sessionStore()): void {
  try {
    storage?.setItem(KEY, JSON.stringify(identity));
  } catch {
    // Storage full or blocked: the identity lasts until the tab reloads.
  }
}

export function clearIdentity(storage: Storage | undefined = sessionStore()): void {
  try {
    storage?.removeItem(KEY);
  } catch {
    // Nothing to clear.
  }
}
