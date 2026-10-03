"use client";

import type { Status } from "@/lib/api/types";
import type { TaskState } from "@/lib/sync/apply";
import { PRIORITY_LABEL, STATUS_LABEL } from "@/lib/ui/labels";

const PRIORITY_COLOR = {
  urgent: "text-danger-ink",
  high: "text-warning-ink",
  medium: "text-muted",
  low: "text-muted",
} as const;

// A dot before the status: an open ring for To do, filled once work starts.
const STATUS_DOT: Record<Status, string> = {
  todo: "border border-muted",
  in_progress: "bg-info",
  in_review: "bg-accent-strong",
  done: "bg-success",
};

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
      className="flex h-full w-full items-center gap-3 border-b border-line px-3 text-left text-sm hover:bg-subtle"
    >
      <span className="flex w-24 shrink-0 items-center gap-1.5 text-xs text-muted">
        <span aria-hidden className={`h-2 w-2 shrink-0 rounded-full ${STATUS_DOT[task.status]}`} />
        {STATUS_LABEL[task.status]}
      </span>
      <span className={`min-w-0 flex-1 truncate ${task.status === "done" ? "text-muted line-through" : ""}`}>
        {task.title}
      </span>
      {blocked && (
        <span className="rounded bg-warning-soft px-1.5 py-0.5 text-xs font-medium text-warning-ink">Blocked</span>
      )}
      {saving && <span className="text-xs text-muted">saving…</span>}
      {task.tags.slice(0, 3).map((tag) => (
        <span key={tag} className="hidden rounded bg-subtle px-1.5 py-0.5 text-xs text-muted sm:inline">
          {tag}
        </span>
      ))}
      {assignees && <span className="hidden w-28 truncate text-xs text-muted md:inline">{assignees}</span>}
      <span className={`w-16 shrink-0 text-right text-xs ${PRIORITY_COLOR[task.priority]}`}>
        {PRIORITY_LABEL[task.priority]}
      </span>
    </button>
  );
}
