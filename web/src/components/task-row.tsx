"use client";

import type { TaskState } from "@/lib/sync/apply";
import { PRIORITY_LABEL, STATUS_LABEL } from "@/lib/ui/labels";

const PRIORITY_COLOR = {
  urgent: "text-red-600 dark:text-red-400",
  high: "text-orange-600 dark:text-orange-400",
  medium: "text-zinc-600 dark:text-zinc-400",
  low: "text-zinc-400 dark:text-zinc-500",
} as const;

/** One row of the task list. */
export function TaskRow({
  task,
  blocked,
  saving,
  assignees,
  onOpen,
}: {
  task: TaskState;
  blocked: boolean;
  saving: boolean;
  /** Display names of the assignees, joined. A string, so memo can compare it. */
  assignees?: string;
  onOpen: (taskId: string) => void;
}) {
  return (
    <button
      type="button"
      onClick={() => onOpen(task.id)}
      className="flex h-full w-full items-center gap-3 border-b border-zinc-100 px-3 text-left text-sm hover:bg-zinc-50 dark:border-zinc-800 dark:hover:bg-zinc-900"
    >
      <span className="w-24 shrink-0 text-xs text-zinc-500">{STATUS_LABEL[task.status]}</span>
      <span className={`min-w-0 flex-1 truncate ${task.status === "done" ? "text-zinc-400 line-through" : ""}`}>
        {task.title}
      </span>
      {blocked && (
        <span className="rounded bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-800 dark:bg-amber-900/40 dark:text-amber-300">
          Blocked
        </span>
      )}
      {saving && <span className="text-xs text-zinc-400">saving…</span>}
      {task.tags.slice(0, 3).map((tag) => (
        <span key={tag} className="hidden rounded bg-zinc-100 px-1.5 py-0.5 text-xs text-zinc-600 sm:inline dark:bg-zinc-800 dark:text-zinc-300">
          {tag}
        </span>
      ))}
      {assignees && <span className="hidden w-28 truncate text-xs text-zinc-500 md:inline">{assignees}</span>}
      <span className={`w-16 shrink-0 text-right text-xs ${PRIORITY_COLOR[task.priority]}`}>
        {PRIORITY_LABEL[task.priority]}
      </span>
    </button>
  );
}
