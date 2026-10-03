"use client";

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
import { useUsers } from "@/lib/users";

import { ConnectionBadge } from "./connection-badge";
import { EditableText } from "./editable-text";
import { useSession } from "./session";
import { TaskDrawer } from "./task-drawer";
import { TaskList } from "./task-list";
import { ThemeToggle } from "./theme-toggle";
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
  const assignees = useMemo(() => [...new Set(tasks.flatMap((t) => t.assignedTo))], [tasks]);
  const { list: userList, nameOf: userName } = useUsers(api, assignees);

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
        <p className="text-sm text-muted">{meta.error}</p>
        <div className="flex gap-3 text-sm">
          <button type="button" onClick={() => void sync.start()} className="rounded border border-line-strong bg-surface px-3 py-1 hover:bg-subtle">
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
      <header className="flex items-center gap-4 border-b border-line bg-surface px-4 py-3">
        <Link href="/" className="text-sm text-muted hover:text-ink">
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
                className="text-sm text-muted"
                onCommit={(description) => sync.send({ type: "project.update", description })}
              />
            </>
          )}
        </div>
        <ConnectionBadge status={meta.status} />
        <span className="text-sm text-muted">{identity.displayName}</span>
        <button type="button" onClick={signOut} className="text-sm text-muted underline hover:text-ink">
          Switch user
        </button>
        <ThemeToggle />
      </header>

      <div className="flex flex-wrap items-center gap-2 border-b border-line bg-surface px-4 py-2 text-sm">
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
            className="w-56 rounded border border-line-strong bg-surface px-2 py-1"
          />
          <button type="submit" disabled={!title.trim()} className="rounded bg-accent px-3 py-1 text-on-accent enabled:hover:bg-accent-hover disabled:opacity-40">
            Add task
          </button>
        </form>
        <span className="mx-2 h-5 w-px bg-line" />
        <input
          aria-label="Search"
          placeholder="Search titles"
          value={filters.text}
          onChange={(e) => setFilters({ ...filters, text: e.target.value })}
          className="w-40 rounded border border-line-strong bg-surface px-2 py-1"
        />
        <select aria-label="Filter by status" value={filters.status} onChange={(e) => setFilters({ ...filters, status: e.target.value as Status | "all" })} className="rounded border border-line-strong bg-surface px-2 py-1">
          <option value="all">Any status</option>
          {STATUSES.map((s) => (
            <option key={s} value={s}>
              {STATUS_LABEL[s]}
            </option>
          ))}
        </select>
        <select aria-label="Filter by assignee" value={filters.assignee} onChange={(e) => setFilters({ ...filters, assignee: e.target.value })} className="rounded border border-line-strong bg-surface px-2 py-1">
          <option value="">Anyone</option>
          {userList.map((u) => (
            <option key={u.id} value={u.id}>
              {u.displayName}
            </option>
          ))}
        </select>
        <select aria-label="Filter by tag" value={filters.tag} onChange={(e) => setFilters({ ...filters, tag: e.target.value })} className="rounded border border-line-strong bg-surface px-2 py-1">
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
        <select aria-label="Sort" value={sort} onChange={(e) => setSort(e.target.value as SortKey)} className="rounded border border-line-strong bg-surface px-2 py-1">
          <option value="created">Oldest first</option>
          <option value="priority">Priority</option>
          <option value="status">Status</option>
          <option value="title">Title</option>
        </select>
        <span className="ml-auto text-xs text-muted">
          {meta.loading ? `Loading tasks… ${tasks.length.toLocaleString()} so far` : `${visible.length.toLocaleString()} of ${tasks.length.toLocaleString()} tasks`}
        </span>
      </div>

      {meta.error && meta.project && (
        <div role="alert" className="flex items-center gap-3 border-b border-danger-line bg-danger-soft px-4 py-2 text-sm text-danger-ink">
          <span>Couldn&apos;t load every task: {meta.error}</span>
          <button type="button" className="underline" onClick={() => void sync.start()}>
            Try again
          </button>
        </div>
      )}

      {failed.length > 0 && (
        <div role="alert" className="flex flex-col gap-1 border-b border-danger-line bg-danger-soft px-4 py-2 text-sm text-danger-ink">
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
        {openTaskId && <TaskDrawer key={openTaskId} sync={sync} taskId={openTaskId} onClose={close} />}
      </div>
    </main>
  );
}
