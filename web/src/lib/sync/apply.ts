// The client's merge rules (spec section 3), as plain functions over a
// SyncState. They mutate the state they're given and report what changed;
// nothing here does I/O.
//
// - Events arrive in seq order and apply to a task only if they're newer
//   than it (seq > updatedSeq).
// - A page summary or a task detail replaces the task only if it's newer.
// - Deleted ids are remembered (tombstones), so an older page can't bring a
//   task back.
// - An event for a task whose page hasn't loaded yet is held, then replayed
//   when the page arrives. Together these make pages and events safe to
//   combine in any order.
import type {
  ChangeOf,
  Change,
  Comment,
  CustomFieldValue,
  LogEvent,
  Priority,
  Project,
  Status,
  Task,
  TaskSummary,
} from "@/lib/api/types";

/** The heavy fields, loaded when the task is opened. */
export interface TaskDetails {
  description: string;
  customFields: Record<string, CustomFieldValue>;
}

/** A task as the client holds it: its summary, plus details once opened. */
export interface TaskState {
  id: string;
  title: string;
  status: Status;
  priority: Priority;
  assignedTo: string[];
  tags: string[];
  /** Ordered by id, like the server's summaries. */
  dependencies: string[];
  updatedSeq: number;
  details?: TaskDetails;
}

export interface SyncState {
  projectId: string;
  project: Project | null;
  tasks: Map<string, TaskState>;
  /** Ids of deleted tasks, kept for the session. */
  tombstones: Set<string>;
  /** Events for tasks whose summary hasn't loaded yet, by task id. */
  held: Map<string, LogEvent[]>;
  /** Comment threads of the tasks that were opened, ordered by seq. */
  threads: Map<string, Comment[]>;
  /** The seq of the last event applied. */
  lastAppliedSeq: number;
}

/** What one call changed, so the store can notify only those readers. */
export interface Changed {
  tasks: Set<string>;
  threads: Set<string>;
  project: boolean;
}

export function emptyState(projectId: string): SyncState {
  return {
    projectId,
    project: null,
    tasks: new Map(),
    tombstones: new Set(),
    held: new Map(),
    threads: new Map(),
    lastAppliedSeq: 0,
  };
}

export function noChange(): Changed {
  return { tasks: new Set(), threads: new Set(), project: false };
}

/** Applies one event of the log. Events already applied are ignored. */
export function applyEvent(s: SyncState, e: LogEvent, ch: Changed = noChange()): Changed {
  if (e.seq <= s.lastAppliedSeq) return ch;
  s.lastAppliedSeq = e.seq;
  const c = e.change;
  switch (c.type) {
    case "project.update":
      if (s.project && e.seq > s.project.seq) {
        s.project = {
          ...s.project,
          name: c.name ?? s.project.name,
          description: c.description ?? s.project.description,
          metadata: c.metadata ? mergePatch(s.project.metadata, c.metadata) : s.project.metadata,
          seq: e.seq,
        };
        ch.project = true;
      }
      return ch;

    case "comment.create":
      if (s.threads.has(c.taskId)) {
        addComments(s, c.taskId, [
          { id: c.id, taskId: c.taskId, content: c.content, author: e.actorId, timestamp: e.createdAt, seq: e.seq },
        ]);
        ch.threads.add(c.taskId);
      }
      return ch;

    case "task.create": {
      const existing = s.tasks.get(c.id);
      if (s.tombstones.has(c.id) || (existing && existing.updatedSeq >= e.seq)) return ch;
      s.tasks.set(c.id, created(c, e.seq));
      ch.tasks.add(c.id);
      return ch;
    }

    case "task.delete":
      s.tasks.delete(c.taskId);
      s.tombstones.add(c.taskId);
      s.held.delete(c.taskId);
      if (s.threads.delete(c.taskId)) ch.threads.add(c.taskId);
      ch.tasks.add(c.taskId);
      // The other tasks it touched lost a dependency on it.
      for (const id of e.touches) {
        if (id !== c.taskId) touch(s, id, e, ch);
      }
      return ch;

    default:
      touch(s, c.taskId, e, ch);
      return ch;
  }
}

/** Merges one page of task summaries. */
export function applyPage(s: SyncState, page: TaskSummary[], ch: Changed = noChange()): Changed {
  for (const p of page) {
    if (s.tombstones.has(p.id)) continue;
    const t = s.tasks.get(p.id);
    if (!t || p.updatedSeq > t.updatedSeq) {
      // Any details held are older than this summary, so they go.
      s.tasks.set(p.id, fromSummary(p));
      ch.tasks.add(p.id);
    }
    replayHeld(s, p.id, ch);
  }
  return ch;
}

/**
 * Merges a task fetched in full. "stale" means it's older than the summary
 * already held, so the caller should fetch it again; "gone" means the task
 * was deleted.
 */
export function applyTaskDetail(s: SyncState, task: Task, ch: Changed = noChange()): "applied" | "stale" | "gone" {
  if (s.tombstones.has(task.id)) return "gone";
  const t = s.tasks.get(task.id);
  if (t && task.updatedSeq < t.updatedSeq) return "stale";
  s.tasks.set(task.id, {
    id: task.id,
    title: task.title,
    status: task.status,
    priority: task.configuration.priority,
    assignedTo: [...task.assignedTo],
    tags: [...task.configuration.tags],
    dependencies: [...task.dependencies],
    updatedSeq: task.updatedSeq,
    details: { description: task.configuration.description, customFields: { ...task.configuration.customFields } },
  });
  ch.tasks.add(task.id);
  replayHeld(s, task.id, ch);
  return "applied";
}

/** Starts keeping a task's comment thread, so live comments are kept too. */
export function openThread(s: SyncState, taskId: string): void {
  if (!s.threads.has(taskId) && !s.tombstones.has(taskId)) s.threads.set(taskId, []);
}

/** Merges fetched comments into a task's thread: once each, by seq. */
export function applyComments(s: SyncState, taskId: string, comments: Comment[], ch: Changed = noChange()): Changed {
  if (s.tombstones.has(taskId)) return ch;
  openThread(s, taskId);
  addComments(s, taskId, comments);
  ch.threads.add(taskId);
  return ch;
}

/** Sets the project's own fields, unless the state already has newer ones. */
export function applyProject(s: SyncState, project: Project, ch: Changed = noChange()): Changed {
  if (!s.project || project.seq >= s.project.seq) {
    s.project = { ...project, metadata: { ...project.metadata } };
    ch.project = true;
  }
  return ch;
}

/** The summary fields of a task. */
export function summaryOf(t: TaskState): TaskSummary {
  const { id, title, status, priority, assignedTo, tags, dependencies, updatedSeq } = t;
  return { id, title, status, priority, assignedTo, tags, dependencies, updatedSeq };
}

// touch applies e to one task, or holds e until the task's summary arrives.
function touch(s: SyncState, id: string, e: LogEvent, ch: Changed): void {
  if (s.tombstones.has(id)) return;
  const t = s.tasks.get(id);
  if (!t) {
    const held = s.held.get(id) ?? [];
    held.push(e);
    s.held.set(id, held);
    return;
  }
  if (t.updatedSeq >= e.seq) return;
  s.tasks.set(id, { ...effect(t, e.change), updatedSeq: e.seq });
  ch.tasks.add(id);
}

function replayHeld(s: SyncState, id: string, ch: Changed): void {
  const held = s.held.get(id);
  if (!held) return;
  s.held.delete(id);
  for (const e of held) touch(s, id, e, ch);
}

// effect is what a change does to one task it touches.
function effect(t: TaskState, c: Change): TaskState {
  switch (c.type) {
    case "task.update": {
      const next = { ...t };
      if (c.title !== undefined) next.title = c.title;
      if (c.priority !== undefined) next.priority = c.priority;
      if (t.details && (c.description !== undefined || c.customFields !== undefined)) {
        next.details = {
          description: c.description ?? t.details.description,
          customFields: c.customFields ? mergePatch(t.details.customFields, c.customFields) : t.details.customFields,
        };
      }
      return next;
    }
    case "task.setStatus":
      return { ...t, status: c.to };
    case "task.addAssignee":
      return { ...t, assignedTo: withItem(t.assignedTo, c.userId) };
    case "task.removeAssignee":
      return { ...t, assignedTo: without(t.assignedTo, c.userId) };
    case "task.addTag":
      return { ...t, tags: withItem(t.tags, c.tag) };
    case "task.removeTag":
      return { ...t, tags: without(t.tags, c.tag) };
    case "task.addDependency":
      return { ...t, dependencies: withItem(t.dependencies, c.dependsOnId).sort() };
    case "task.removeDependency":
      return { ...t, dependencies: without(t.dependencies, c.dependsOnId) };
    case "task.delete":
      // t depended on the deleted task.
      return { ...t, dependencies: without(t.dependencies, c.taskId) };
    default:
      return t;
  }
}

function created(c: ChangeOf<"task.create">, seq: number): TaskState {
  const conf = c.configuration ?? {};
  return {
    id: c.id,
    title: c.title,
    status: "todo",
    priority: conf.priority ?? "medium",
    assignedTo: [...(c.assignedTo ?? [])],
    tags: [...(conf.tags ?? [])],
    dependencies: [...(c.dependencies ?? [])].sort(),
    updatedSeq: seq,
    details: { description: conf.description ?? "", customFields: { ...(conf.customFields ?? {}) } },
  };
}

function fromSummary(p: TaskSummary): TaskState {
  return { ...p, assignedTo: [...p.assignedTo], tags: [...p.tags], dependencies: [...p.dependencies] };
}

function addComments(s: SyncState, taskId: string, comments: Comment[]): void {
  const thread = s.threads.get(taskId) ?? [];
  const known = new Set(thread.map((c) => c.id));
  const added = comments.filter((c) => !known.has(c.id));
  if (added.length > 0) {
    s.threads.set(taskId, [...thread, ...added].sort((a, b) => a.seq - b.seq));
  }
}

/** Per-key merge: a null value removes the key. */
function mergePatch<V>(current: Record<string, V>, patch: Record<string, V | null | undefined>): Record<string, V> {
  const merged = { ...current };
  for (const [key, value] of Object.entries(patch)) {
    if (value === null || value === undefined) delete merged[key];
    else merged[key] = value;
  }
  return merged;
}

function withItem<T>(items: T[], item: T): T[] {
  return items.includes(item) ? items : [...items, item];
}

function without<T>(items: T[], item: T): T[] {
  return items.filter((x) => x !== item);
}
