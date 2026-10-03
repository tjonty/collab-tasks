// React's view of a ProjectSync, through useSyncExternalStore.
import { useCallback, useSyncExternalStore } from "react";

import type { Comment } from "@/lib/api/types";

import type { TaskState } from "./apply";
import type { Pending } from "./changes";
import type { ProjectSync, SyncMeta } from "./store";

const noop = () => () => {};

/** One task; re-renders only when that task changes. */
export function useTask(sync: ProjectSync, id: string): TaskState | undefined {
  const subscribe = useCallback((cb: () => void) => sync.subscribeTask(id, cb), [sync, id]);
  return useSyncExternalStore(subscribe, () => sync.getTask(id), () => undefined);
}

/** Every task; re-renders at most once a frame. */
export function useTaskList(sync: ProjectSync): TaskState[] {
  const subscribe = useCallback((cb: () => void) => sync.subscribeList(cb), [sync]);
  return useSyncExternalStore(subscribe, () => sync.getTaskList(), () => EMPTY);
}

export function useThread(sync: ProjectSync, taskId: string): Comment[] | undefined {
  const subscribe = useCallback((cb: () => void) => sync.subscribeThread(taskId, cb), [sync, taskId]);
  return useSyncExternalStore(subscribe, () => sync.getThread(taskId), () => undefined);
}

export function useSyncMeta(sync: ProjectSync): SyncMeta {
  const subscribe = useCallback((cb: () => void) => sync.subscribeMeta(cb), [sync]);
  return useSyncExternalStore(subscribe, () => sync.getMeta(), () => sync.getMeta());
}

export function usePending(sync: ProjectSync | null): Pending[] {
  const subscribe = useCallback((cb: () => void) => (sync ? sync.subscribePending(cb) : noop()), [sync]);
  return useSyncExternalStore(subscribe, () => sync?.pending() ?? NONE, () => NONE);
}

const EMPTY: TaskState[] = [];
const NONE: Pending[] = [];
