"use client";

/** Deletes a task after the user confirms, warning when other tasks depend on it. */
export function DeleteTaskButton({
  dependents,
  onDelete,
  confirm = (message) => window.confirm(message),
}: {
  /** How many tasks depend on this one. */
  dependents: number;
  onDelete: () => void;
  confirm?: (message: string) => boolean;
}) {
  const ask = () => {
    const message =
      dependents > 0
        ? `${dependents} ${dependents === 1 ? "task depends" : "tasks depend"} on this. Delete anyway?`
        : "Delete this task?";
    if (confirm(message)) onDelete();
  };
  return (
    <button
      type="button"
      onClick={ask}
      className="rounded border border-red-300 px-2 py-1 text-sm text-red-700 hover:bg-red-50 dark:border-red-800 dark:text-red-400 dark:hover:bg-red-950"
    >
      Delete task
    </button>
  );
}
