// Everyone's display names, for assignees and comment authors.
import { useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef } from "react";

import type { ApiClient } from "@/lib/api/client";
import type { User } from "@/lib/api/types";

const NONE: User[] = [];

/** The users list, and a name for any id. `referenced` is every user id on screen. */
export function useUsers(api: ApiClient, referenced: readonly string[]) {
  const users = useQuery({
    queryKey: ["users"],
    // Throwing keeps the last list: a failed fetch doesn't forget anyone.
    queryFn: async () => {
      const { data, error } = await api.GET("/users");
      if (!data) throw new Error(error?.message ?? "Couldn't load the users.");
      return data;
    },
    refetchInterval: 30_000,
  });
  const list = users.data ?? NONE;
  const names = useMemo(() => new Map(list.map((u) => [u.id, u.displayName])), [list]);

  // People join at any time: an id the list doesn't know is someone new.
  // Fetch once per such id, not on every render while the answer is on its
  // way (a refetch restarts the one in flight).
  const asked = useRef(new Set<string>());
  const { isSuccess, refetch } = users;
  useEffect(() => {
    if (!isSuccess) return;
    const unknown = referenced.filter((id) => !names.has(id) && !asked.current.has(id));
    if (unknown.length === 0) return;
    for (const id of unknown) asked.current.add(id);
    void refetch();
  }, [isSuccess, refetch, referenced, names]);

  const nameOf = useCallback((id: string) => names.get(id) ?? "Someone", [names]);
  return { list, nameOf };
}
