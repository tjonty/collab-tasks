"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { v7 as uuidv7 } from "uuid";

import type { ProjectSummary } from "@/lib/api/types";
import { wsUrl } from "@/lib/config";
import { Connection, type ConnectionStatus } from "@/lib/sync/connection";

import { ConnectionBadge } from "./connection-badge";
import { useSession } from "./session";

const KEY = ["projects"];

function upsert(list: ProjectSummary[], p: ProjectSummary): ProjectSummary[] {
  return list.some((x) => x.id === p.id) ? list.map((x) => (x.id === p.id ? p : x)) : [...list, p];
}

/** The project list, kept live, and "New project". */
export function ProjectsHome() {
  const { api, identity, signOut } = useSession();
  const queryClient = useQueryClient();
  const router = useRouter();
  const [status, setStatus] = useState<ConnectionStatus>("connecting");
  const [name, setName] = useState("");

  const projects = useQuery({
    queryKey: KEY,
    queryFn: async () => {
      const { data, error } = await api.GET("/projects");
      if (!data) throw new Error(error?.message ?? "Couldn't load the projects.");
      return data;
    },
  });

  // Creates and renames arrive as projectsChanged. They aren't replayed, so
  // every (re)connect also refetches the list to catch what was missed.
  useEffect(() => {
    const conn: Connection = new Connection({
      url: wsUrl(identity.id),
      onOpen: () => {
        conn.send({ type: "subscribeProjects" });
        void queryClient.invalidateQueries({ queryKey: KEY });
      },
      onMessage: (msg) => {
        if (msg.type === "projectsChanged") {
          queryClient.setQueryData<ProjectSummary[]>(KEY, (list = []) => upsert(list, msg.project));
        }
      },
      onStatus: setStatus,
    });
    conn.start();
    return () => conn.stop();
  }, [identity.id, queryClient]);

  const create = useMutation({
    mutationFn: async (projectName: string) => {
      const { data, error } = await api.POST("/projects", { body: { id: uuidv7(), name: projectName } });
      if (!data) throw new Error(error?.message ?? "Couldn't create the project.");
      return data;
    },
    onSuccess: (p) => {
      queryClient.setQueryData<ProjectSummary[]>(KEY, (list = []) => upsert(list, p));
      router.push(`/projects/${p.id}`);
    },
  });

  return (
    <main className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-6 py-10">
      <header className="flex items-center justify-between gap-4">
        <h1 className="text-2xl font-semibold tracking-tight">Projects</h1>
        <div className="flex items-center gap-3 text-sm">
          <ConnectionBadge status={status} />
          <span className="text-zinc-500">{identity.displayName}</span>
          <button type="button" onClick={signOut} className="text-zinc-500 underline">
            Switch user
          </button>
        </div>
      </header>

      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (name.trim()) create.mutate(name.trim());
        }}
      >
        <input
          aria-label="New project name"
          placeholder="New project name"
          maxLength={100}
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="flex-1 rounded border border-zinc-300 bg-transparent px-3 py-2 text-sm dark:border-zinc-700"
        />
        <button
          type="submit"
          disabled={!name.trim() || create.isPending}
          className="rounded bg-zinc-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-40 dark:bg-zinc-100 dark:text-zinc-900"
        >
          New project
        </button>
      </form>
      {create.error && <p className="text-sm text-red-600">{create.error.message}</p>}

      {projects.isPending && <p className="text-sm text-zinc-500">Loading projects…</p>}
      {projects.error && (
        <p className="text-sm text-red-600">
          {projects.error.message}{" "}
          <button type="button" className="underline" onClick={() => projects.refetch()}>
            Try again
          </button>
        </p>
      )}
      {projects.data?.length === 0 && <p className="text-sm text-zinc-500">No projects yet. Create the first one.</p>}
      <ul className="flex flex-col divide-y divide-zinc-200 rounded border border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        {projects.data?.map((p) => (
          <li key={p.id}>
            <Link href={`/projects/${p.id}`} className="flex items-baseline justify-between gap-4 px-4 py-3 hover:bg-zinc-50 dark:hover:bg-zinc-900">
              <span className="font-medium">{p.name}</span>
              <span className="truncate text-sm text-zinc-500">{p.description}</span>
            </Link>
          </li>
        ))}
      </ul>
    </main>
  );
}
