// Builders for sync tests. Only tests import this file.
import type { Change, LogEvent, TaskSummary } from "@/lib/api/types";

export const PROJECT = "00000000-0000-7000-8000-00000000000p".replace("p", "1");
export const ACTOR = "00000000-0000-7000-8000-0000000000a1";

let changeCounter = 0;

/** An event of PROJECT's log. */
export function ev(seq: number, change: Change, touches: string[] = [], actor = ACTOR): LogEvent {
  changeCounter += 1;
  return {
    projectId: PROJECT,
    seq,
    changeId: `00000000-0000-7000-9000-${changeCounter.toString(16).padStart(12, "0")}`,
    actorId: actor,
    createdAt: new Date(1_700_000_000_000 + seq * 1000).toISOString(),
    touches,
    change,
  };
}

/** A task summary with defaults. */
export function summary(id: string, updatedSeq: number, fields: Partial<TaskSummary> = {}): TaskSummary {
  return {
    id,
    title: `Task ${id}`,
    status: "todo",
    priority: "medium",
    assignedTo: [],
    tags: [],
    dependencies: [],
    updatedSeq,
    ...fields,
  };
}
