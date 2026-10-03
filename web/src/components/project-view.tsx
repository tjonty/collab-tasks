"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useCallback, useEffect, useMemo, useState } from "react";
import { v7 as uuidv7 } from "uuid";

import { projectApi } from "@/lib/api/client";
import type { Status } from "@/lib/api/types";
import { wsUrl } from "@/lib/config";
import { Connection } from "@/lib/sync/connection";
import { usePending, useSyncMeta, useTaskList } from "@/lib/sync/hooks";
import { ProjectSync } from "@/lib/sync/store";
import { emptyFilters, rejectionMessage, visibleTasks, type Filters, type SortKey } from "@/lib/sync/view";
import { STATUSES, STATUS_LABEL } from "@/lib/ui/labels";

import { ConnectionBadge } from "./connection-badge";
import { EditableText } from "./editable-text";
import { useSession } from "./session";
import { TaskDrawer } from "./task-drawer";
import { TaskList } from "./task-list";
import { useToast } from "./toaster";

/** One project, live: header, filters, the task list and the task drawer. */
export function ProjectView({ projectId }: { projectId: string }) {
  const { api, identity, signOut } = useSession();
  const toast = useToast();
  const [sync] = useState(
    () =>
      new ProjectSync({
        projectId,
        api: projectApi(api),
        connect: (handlers) => new Connection({ url: wsUrl(identity.id), ...handlers }),
        onRejected: (_, code, message) => toast(rejectionMessage(code, message)),
      }),
  );
  useEffect(() => {
    void sync.start();
    return () => sync.stop();
  }, [sync]);

  const meta = useSyncMeta(sync);
  const tasks = useTaskList(sync);
  const pending = usePending(sync);
  const users = useQuery({
    queryKey: ["users"],
    queryFn: async () => (await api.GET("/users")).data ?? [],
    refetchInterval: 30_000,
  });
  const userList = useMemo(() => users.data ?? [], [users.data]);
  const userName = useCallback(
    (id: string) => userList.find((u) => u.id === id)?.displayName ?? "Someone",
    [userList],
  );

  const [filters, setFilters] = useState<Filters>(emptyFilters);
  const [sort, setSort] = useState<SortKey>("created");
  const visible = useMemo(() => visibleTasks(tasks, filters, sort), [tasks, filters, sort]);
  const lookupMap = useMemo(() => new Map(tasks.map((t) => [t.id, t])), [tasks]);
  const lookup = useCallback((id: string) => lookupMap.get(id), [lookupMap]);
  const tags = useMemo(() => [...new Set(tasks.flatMap((t) => t.tags))].sort(), [tasks]);
  const saving = useMemo(
    () => new Set(pending.flatMap((p) => ("taskId" in p.change ? [p.change.taskId] : p.change.type === "task.create" ? [p.change.id] : []))),
    [pending],
  );

  // ?task=<id> opens the drawer, so a task can be linked to. The native
  // History API updates useSearchParams without a round trip to the server.
  const openTaskId = useSearchParams().get("task");
  const open = useCallback((id: string) => window.history.replaceState(null, "", `?task=${id}`), []);
  const close = useCallback(() => window.history.replaceState(null, "", window.location.pathname), []);

  const [title, setTitle] = useState("");
  const failed = pending.filter((p) => p.state === "failed");

  if (meta.error && !meta.project) {
    return (
      <main className="mx-auto mt-24 flex max-w-md flex-col gap-3 px-6">
        <h2 className="text-lg font-semibold">This project couldn&apos;t be loaded</h2>
        <p className="text-sm text-zinc-600 dark:text-zinc-400">{meta.error}</p>
        <div className="flex gap-3 text-sm">
          <button type="button" onClick={() => void sync.start()} className="rounded border px-3 py-1">
            Try again
          </button>
          <Link href="/" className="px-3 py-1 underline">
            All projects
          </Link>
        </div>
      </main>
    );
  }

  return (
    <main className="flex h-screen flex-col">
      <header className="flex items-center gap-4 border-b border-zinc-200 px-4 py-3 dark:border-zinc-800">
        <Link href="/" className="text-sm text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100">
          ← Projects
        </Link>
        <div className="min-w-0 flex-1">
          {meta.project && (
            <>
              <EditableText
                label="Project name"
                value={meta.project.name}
                maxLength={100}
                className="text-lg font-semibold"
                onCommit={(name) => name.trim() && sync.send({ type: "project.update", name })}
              />
              <EditableText
                label="Project description"
                value={meta.project.description}
                maxLength={2000}
                placeholder="Add a description"
                className="text-sm text-zinc-500"
                onCommit={(description) => sync.send({ type: "project.update", description })}
              />
            </>
          )}
        </div>
        <ConnectionBadge status={meta.status} />
        <span className="text-sm text-zinc-500">{identity.displayName}</span>
        <button type="button" onClick={signOut} className="text-sm text-zinc-500 underline">
          Switch user
        </button>
      </header>

      <div className="flex flex-wrap items-center gap-2 border-b border-zinc-200 px-4 py-2 text-sm dark:border-zinc-800">
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (!title.trim()) return;
            sync.send({ type: "task.create", id: uuidv7(), title: title.trim() });
            setTitle("");
          }}
        >
          <input
            aria-label="New task title"
            placeholder="New task"
            maxLength={200}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            className="w-56 rounded border border-zinc-300 bg-transparent px-2 py-1 dark:border-zinc-700"
          />
          <button type="submit" disabled={!title.trim()} className="rounded bg-zinc-900 px-3 py-1 text-white disabled:opacity-40 dark:bg-zinc-100 dark:text-zinc-900">
            Add task
          </button>
        </form>
        <span className="mx-2 h-5 w-px bg-zinc-200 dark:bg-zinc-800" />
        <input
          aria-label="Search"
          placeholder="Search titles"
          value={filters.text}
          onChange={(e) => setFilters({ ...filters, text: e.target.value })}
          className="w-40 rounded border border-zinc-300 bg-transparent px-2 py-1 dark:border-zinc-700"
        />
        <select aria-label="Filter by status" value={filters.status} onChange={(e) => setFilters({ ...filters, status: e.target.value as Status | "all" })} className="rounded border border-zinc-300 bg-transparent px-2 py-1 dark:border-zinc-700">
          <option value="all">Any status</option>
          {STATUSES.map((s) => (
            <option key={s} value={s}>
              {STATUS_LABEL[s]}
            </option>
          ))}
        </select>
        <select aria-label="Filter by assignee" value={filters.assignee} onChange={(e) => setFilters({ ...filters, assignee: e.target.value })} className="rounded border border-zinc-300 bg-transparent px-2 py-1 dark:border-zinc-700">
          <option value="">Anyone</option>
          {userList.map((u) => (
            <option key={u.id} value={u.id}>
              {u.displayName}
            </option>
          ))}
        </select>
        <select aria-label="Filter by tag" value={filters.tag} onChange={(e) => setFilters({ ...filters, tag: e.target.value })} className="rounded border border-zinc-300 bg-transparent px-2 py-1 dark:border-zinc-700">
          <option value="">Any tag</option>
          {tags.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>
        <label className="flex items-center gap-1">
          <input type="checkbox" checked={filters.blockedOnly} onChange={(e) => setFilters({ ...filters, blockedOnly: e.target.checked })} />
          Blocked only
        </label>
        <select aria-label="Sort" value={sort} onChange={(e) => setSort(e.target.value as SortKey)} className="rounded border border-zinc-300 bg-transparent px-2 py-1 dark:border-zinc-700">
          <option value="created">Oldest first</option>
          <option value="priority">Priority</option>
          <option value="status">Status</option>
          <option value="title">Title</option>
        </select>
        <span className="ml-auto text-xs text-zinc-500">
          {meta.loading ? `Loading tasks… ${tasks.length.toLocaleString()} so far` : `${visible.length.toLocaleString()} of ${tasks.length.toLocaleString()} tasks`}
        </span>
      </div>

      {meta.error && meta.project && (
        <div role="alert" className="flex items-center gap-3 border-b border-red-200 bg-red-50 px-4 py-2 text-sm text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
          <span>Couldn&apos;t load every task: {meta.error}</span>
          <button type="button" className="underline" onClick={() => void sync.start()}>
            Try again
          </button>
        </div>
      )}

      {failed.length > 0 && (
        <div role="alert" className="flex flex-col gap-1 border-b border-red-200 bg-red-50 px-4 py-2 text-sm text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
          {failed.map((p) => (
            <div key={p.changeId} className="flex items-center gap-3">
              <span>Couldn&apos;t save a change ({p.change.type}).</span>
              <button type="button" className="underline" onClick={() => sync.retry(p.changeId)}>
                Retry
              </button>
              <button type="button" className="underline" onClick={() => sync.discard(p.changeId)}>
                Discard
              </button>
            </div>
          ))}
        </div>
      )}

      <div className="flex min-h-0 flex-1">
        <TaskList tasks={visible} lookup={lookup} saving={saving} userName={userName} onOpen={open} />
        {openTaskId && <TaskDrawer key={openTaskId} sync={sync} taskId={openTaskId} users={userList} onClose={close} />}
      </div>
    </main>
  );
}
