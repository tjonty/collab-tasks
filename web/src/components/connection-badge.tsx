"use client";

import type { ConnectionStatus } from "@/lib/sync/connection";

const LABEL: Record<ConnectionStatus, string> = {
  connecting: "Connecting…",
  live: "Live",
  reconnecting: "Reconnecting…",
  offline: "Offline",
};

const DOT: Record<ConnectionStatus, string> = {
  connecting: "bg-zinc-400",
  live: "bg-emerald-500",
  reconnecting: "bg-amber-500",
  offline: "bg-red-500",
};

export function ConnectionBadge({ status }: { status: ConnectionStatus }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-zinc-600 dark:text-zinc-400">
      <span aria-hidden className={`h-2 w-2 rounded-full ${DOT[status]}`} />
      <span role="status">{LABEL[status]}</span>
    </span>
  );
}
