// The task rules, mirroring api/internal/domain. The server stays the
// authority; the UI uses these to offer only the moves that will succeed.
import type { Status } from "@/lib/api/types";

export type MoveError = "invalid_transition" | "dependencies_not_done";

const transitions: Record<Status, Status[]> = {
  todo: ["in_progress"],
  in_progress: ["todo", "in_review", "done"],
  in_review: ["in_progress", "done"],
  done: ["in_progress"],
};

/** The statuses a task in `from` may move to. Staying put is not a move. */
export function allowedMoves(from: Status): Status[] {
  return transitions[from] ?? [];
}

/**
 * Whether a task may move from `current` to `to`, given the statuses of the
 * tasks it depends on. Only a move into Done looks at the dependencies.
 */
export function canMoveTo(current: Status, to: Status, depStatuses: readonly Status[]): MoveError | null {
  if (!allowedMoves(current).includes(to)) return "invalid_transition";
  if (to === "done" && !depStatuses.every((s) => s === "done")) return "dependencies_not_done";
  return null;
}

/** A task is blocked when it isn't Done and a dependency isn't Done either. */
export function isBlocked(status: Status, depStatuses: readonly Status[]): boolean {
  return status !== "done" && !depStatuses.every((s) => s === "done");
}

/**
 * Whether "taskId depends on dependsOnId" would close a loop: true when
 * dependsOnId already depends on taskId, directly or indirectly, or is
 * taskId itself.
 */
export function wouldCycle(
  dependenciesOf: (id: string) => readonly string[] | undefined,
  taskId: string,
  dependsOnId: string,
): boolean {
  const seen = new Set<string>();
  const stack = [dependsOnId];
  while (stack.length > 0) {
    const id = stack.pop()!;
    if (id === taskId) return true;
    if (seen.has(id)) continue;
    seen.add(id);
    stack.push(...(dependenciesOf(id) ?? []));
  }
  return false;
}

/** The ids of the tasks that depend on taskId. */
export function dependentsOf(
  tasks: Iterable<{ id: string; dependencies: readonly string[] }>,
  taskId: string,
): string[] {
  const out: string[] = [];
  for (const t of tasks) {
    if (t.dependencies.includes(taskId)) out.push(t.id);
  }
  return out;
}
