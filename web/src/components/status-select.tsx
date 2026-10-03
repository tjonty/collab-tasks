"use client";

import type { Status } from "@/lib/api/types";
import { allowedMoves } from "@/lib/domain/workflow";
import { STATUS_LABEL } from "@/lib/ui/labels";

/**
 * The task's status, offering only the moves the workflow allows. Done is
 * disabled, with the reason, while any dependency isn't done.
 */
export function StatusSelect({
  status,
  waitingOn,
  onChange,
}: {
  status: Status;
  /** Titles of the dependencies that aren't done. */
  waitingOn: string[];
  onChange: (to: Status) => void;
}) {
  const reason = waitingOn.length > 0 ? `Waiting on: ${waitingOn.join(", ")}` : undefined;
  const moves = allowedMoves(status);
  return (
    <div className="flex flex-col gap-1">
      <select
        aria-label="Status"
        className="rounded border border-zinc-300 bg-transparent px-2 py-1 dark:border-zinc-700"
        value={status}
        onChange={(e) => onChange(e.target.value as Status)}
      >
        <option value={status}>{STATUS_LABEL[status]}</option>
        {moves.map((to) => (
          <option key={to} value={to} disabled={to === "done" && !!reason} title={to === "done" ? reason : undefined}>
            {STATUS_LABEL[to]}
          </option>
        ))}
      </select>
      {reason && moves.includes("done") && <p className="text-xs text-amber-700 dark:text-amber-400">{reason}</p>}
    </div>
  );
}
