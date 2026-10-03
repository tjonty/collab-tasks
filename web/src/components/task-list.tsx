"use client";

import { useVirtualizer } from "@tanstack/react-virtual";
import { memo, useRef } from "react";

import type { TaskState } from "@/lib/sync/apply";
import { blocked } from "@/lib/sync/view";

import { TaskRow } from "./task-row";

// A row re-renders only when its own task, Blocked or saving state changes.
const Row = memo(TaskRow);

const ROW_HEIGHT = 40;

/** The visible tasks, virtualized: only the rows on screen are mounted. */
export function TaskList({
  tasks,
  lookup,
  saving,
  userName,
  onOpen,
}: {
  tasks: TaskState[];
  lookup: (id: string) => TaskState | undefined;
  /** Ids of tasks with a change on its way. */
  saving: Set<string>;
  userName: (id: string) => string;
  onOpen: (taskId: string) => void;
}) {
  const parent = useRef<HTMLDivElement>(null);
  // TanStack Virtual returns functions the React Compiler can't memoize; the
  // compiler isn't enabled here, so nothing is lost.
  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count: tasks.length,
    getScrollElement: () => parent.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 12,
  });

  return (
    <div ref={parent} data-testid="task-list" className="min-h-0 flex-1 overflow-auto">
      <div style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
        {virtualizer.getVirtualItems().map((item) => {
          const task = tasks[item.index];
          return (
            <div
              key={task.id}
              style={{ position: "absolute", top: 0, left: 0, width: "100%", height: item.size, transform: `translateY(${item.start}px)` }}
            >
              <Row
                task={task}
                blocked={blocked(task, lookup)}
                saving={saving.has(task.id)}
                assignees={task.assignedTo.map(userName).join(", ")}
                onOpen={onOpen}
              />
            </div>
          );
        })}
      </div>
    </div>
  );
}
