"use client";

import { createContext, useContext, useMemo, type ReactNode } from "react";

import { createApi, type ApiClient } from "@/lib/api/client";
import { API_URL } from "@/lib/config";
import type { Identity } from "@/lib/identity";
import { identityStore, useIdentity } from "@/lib/identity-store";

import { NamePrompt } from "./name-prompt";

interface Session {
  identity: Identity;
  api: ApiClient;
  /** Forget this tab's identity and pick a name again. */
  signOut: () => void;
}

const SessionContext = createContext<Session | null>(null);

// One client per tab. A 401 means the server doesn't know this user (e.g.
// the database was reset): forget the identity, which brings the prompt back.
const api = createApi({
  baseUrl: API_URL,
  userId: () => identityStore.get()?.id ?? null,
  onUnknownUser: () => identityStore.set(null),
});

/** Renders its children once the tab has an identity, asking for a name first. */
export function SessionGate({ children }: { children: ReactNode }) {
  const identity = useIdentity();
  const session = useMemo(
    () => (identity ? { identity, api, signOut: () => identityStore.set(null) } : null),
    [identity],
  );

  if (identity === undefined) return null; // still reading sessionStorage
  if (!session) {
    return (
      <NamePrompt
        onCreate={async (displayName) => {
          const { data, error } = await api.POST("/users", { body: { displayName } }).catch(() => ({
            data: undefined,
            error: { message: "The API is unreachable. Is it running?" },
          }));
          if (!data) throw new Error(error?.message ?? "Couldn't create the user.");
          identityStore.set({ id: data.id, displayName: data.displayName });
        }}
      />
    );
  }
  return <SessionContext.Provider value={session}>{children}</SessionContext.Provider>;
}

export function useSession(): Session {
  const session = useContext(SessionContext);
  if (!session) throw new Error("useSession outside SessionGate");
  return session;
}
