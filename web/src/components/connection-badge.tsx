"use client";

import type { ConnectionStatus } from "@/lib/sync/connection";

const LABEL: Record<ConnectionStatus, string> = {
  connecting: "Connecting…",
  live: "Live",
  reconnecting: "Reconnecting…",
  offline: "Offline",
};

const DOT: Record<ConnectionStatus, string> = {
  connecting: "bg-muted",
  // The one bit of motion in the UI: live means live. Skipped for people who
  // ask their system for less motion.
  live: "bg-success motion-safe:animate-pulse",
  reconnecting: "bg-warning",
  offline: "bg-danger",
};

export function ConnectionBadge({ status }: { status: ConnectionStatus }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-muted">
      <span aria-hidden className={`h-2 w-2 rounded-full ${DOT[status]}`} />
      <span role="status">{LABEL[status]}</span>
    </span>
  );
}
