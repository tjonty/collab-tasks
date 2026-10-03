"use client";

import { wouldCycle } from "@/lib/domain/workflow";
import type { TaskState } from "@/lib/sync/apply";

/**
 * Picks a task for `task` to depend on. It leaves out the task itself, its
 * current dependencies, and every task that would close a loop.
 */
export function DependencyPicker({
  task,
  candidates,
  lookup,
  onAdd,
}: {
  task: TaskState;
  candidates: readonly TaskState[];
  lookup: (id: string) => TaskState | undefined;
  onAdd: (dependsOnId: string) => void;
}) {
  const choices = candidates.filter(
    (c) =>
      !task.dependencies.includes(c.id) &&
      !wouldCycle((id) => lookup(id)?.dependencies, task.id, c.id),
  );
  return (
    <select
      aria-label="Add dependency"
      className="w-full rounded border border-line-strong bg-surface px-2 py-1 text-sm"
      value=""
      onChange={(e) => e.target.value && onAdd(e.target.value)}
    >
      <option value="">Add a dependency…</option>
      {choices.map((c) => (
        <option key={c.id} value={c.id}>
          {c.title}
        </option>
      ))}
    </select>
  );
}
