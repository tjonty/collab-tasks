// What the task list and drawer show, computed from the store's state:
// filtering, sorting, Blocked, "saving…" and the messages for rejected
// changes. All in memory, so 10k tasks filter in a few milliseconds.
import type { Change, Status } from "@/lib/api/types";
import { isBlocked } from "@/lib/domain/workflow";

import type { TaskState } from "./apply";
import type { Pending } from "./changes";

export interface Filters {
  /** Matches anywhere in the title, ignoring case. */
  text: string;
  status: Status | "all";
  /** A user id, or "" for anyone. */
  assignee: string;
  /** A tag, or "" for any. */
  tag: string;
  blockedOnly: boolean;
}

export type SortKey = "created" | "title" | "priority" | "status";

export function emptyFilters(): Filters {
  return { text: "", status: "all", assignee: "", tag: "", blockedOnly: false };
}

const priorityRank = { urgent: 0, high: 1, medium: 2, low: 3 } as const;
const statusRank = { todo: 0, in_progress: 1, in_review: 2, done: 3 } as const;

const byId = (a: TaskState, b: TaskState) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);

const comparators: Record<SortKey, (a: TaskState, b: TaskState) => number> = {
  // UUIDv7 ids sort in creation order.
  created: byId,
  title: (a, b) => a.title.localeCompare(b.title) || byId(a, b),
  priority: (a, b) => priorityRank[a.priority] - priorityRank[b.priority] || byId(a, b),
  status: (a, b) => statusRank[a.status] - statusRank[b.status] || byId(a, b),
};

/** The tasks that pass the filters, in the chosen order. */
export function visibleTasks(tasks: readonly TaskState[], filters: Filters, sort: SortKey): TaskState[] {
  const lookup = new Map(tasks.map((t) => [t.id, t]));
  const text = filters.text.trim().toLowerCase();
  return tasks
    .filter(
      (t) =>
        (!text || t.title.toLowerCase().includes(text)) &&
        (filters.status === "all" || t.status === filters.status) &&
        (!filters.assignee || t.assignedTo.includes(filters.assignee)) &&
        (!filters.tag || t.tags.includes(filters.tag)) &&
        (!filters.blockedOnly || blocked(t, (id) => lookup.get(id))),
    )
    .sort(comparators[sort]);
}

/** Whether the task is blocked. Dependencies not loaded yet don't count. */
export function blocked(task: TaskState, lookup: (id: string) => TaskState | undefined): boolean {
  return isBlocked(task.status, dependencyStatuses(task, lookup));
}

/** The task's dependencies that aren't done yet. */
export function waitingOn(task: TaskState, lookup: (id: string) => TaskState | undefined): TaskState[] {
  return task.dependencies
    .map(lookup)
    .filter((d): d is TaskState => d !== undefined && d.status !== "done");
}

export function dependencyStatuses(task: TaskState, lookup: (id: string) => TaskState | undefined): Status[] {
  return task.dependencies.flatMap((id) => {
    const d = lookup(id);
    return d ? [d.status] : [];
  });
}

/** The parts of a task with a change still on its way: "title", "status", "tags" … */
export function pendingFields(pending: readonly Pending[], taskId: string): Set<string> {
  const fields = new Set<string>();
  for (const { change } of pending) {
    for (const field of fieldsOf(change, taskId)) fields.add(field);
  }
  return fields;
}

function fieldsOf(c: Change, taskId: string): string[] {
  switch (c.type) {
    case "task.create":
      return c.id === taskId ? ["create"] : [];
    case "project.update":
      return [];
    case "comment.create":
      return c.taskId === taskId ? ["comments"] : [];
  }
  if (c.taskId !== taskId) return [];
  switch (c.type) {
    case "task.update":
      return (["title", "priority", "description", "customFields"] as const).filter((k) => c[k] !== undefined);
    case "task.setStatus":
      return ["status"];
    case "task.delete":
      return ["delete"];
    case "task.addAssignee":
    case "task.removeAssignee":
      return ["assignedTo"];
    case "task.addTag":
    case "task.removeTag":
      return ["tags"];
    case "task.addDependency":
    case "task.removeDependency":
      return ["dependencies"];
  }
}

/** What to tell the user when the server turns a change down. */
export function rejectionMessage(code: string, message: string): string {
  switch (code) {
    case "dependencies_not_done": {
      const n = Number.parseInt(message, 10);
      return Number.isNaN(n)
        ? "Can't mark Done: some dependencies aren't done yet."
        : `Can't mark Done: waiting on ${n} ${n === 1 ? "task" : "tasks"}.`;
    }
    case "invalid_transition":
      return "Someone moved this task first, so that status change no longer applies.";
    case "dependency_cycle":
      return "That would make the tasks depend on each other in a loop.";
    case "task_not_found":
      return "That task was deleted.";
    case "limit_exceeded":
      return `Limit reached: ${message}.`;
    case "validation_failed":
      return `Not saved: ${message}.`;
    case "unknown_user":
      return "Your session ended. Pick a display name to continue.";
    case "project_not_found":
      return "This project no longer exists.";
    default:
      return message;
  }
}

/** Reads a typed custom-field value: "3" is a number, "true" a boolean, anything else text. */
export function parseFieldValue(raw: string): string | number | boolean {
  const t = raw.trim();
  if (t === "true") return true;
  if (t === "false") return false;
  if (/^-?(\d+\.?\d*|\.\d+)(e[-+]?\d+)?$/i.test(t)) return Number(t);
  return raw;
}
