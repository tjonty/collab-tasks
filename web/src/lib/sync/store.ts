// One open project, kept in sync (spec sections 3–5). ProjectSync owns the
// SyncState, feeds it pages, task details and the event stream, and lets
// React components read it through useSyncExternalStore: per task, so an
// event re-renders one row, not ten thousand.
import type {
  Change,
  Comment,
  CommentPage,
  Project,
  Task,
  TaskPage,
  WsClientMessage,
  WsServerMessage,
} from "@/lib/api/types";

import {
  applyComments,
  applyEvent,
  applyPage,
  applyProject,
  applyTaskDetail,
  emptyState,
  noChange,
  openThread,
  type Changed,
  type SyncState,
  type TaskState,
} from "./apply";
import { ChangeSender, type Pending, type PostOutcome } from "./changes";
import type { ConnectionStatus } from "./connection";

/** The HTTP calls the store makes. */
export interface ProjectApi {
  getProject(projectId: string): Promise<Project>;
  listTasks(projectId: string, after?: string): Promise<TaskPage>;
  /** null when the task no longer exists. */
  getTask(taskId: string): Promise<Task | null>;
  /** null when the task no longer exists. */
  listComments(taskId: string, afterSeq: number): Promise<CommentPage | null>;
  postChange(projectId: string, changeId: string, change: Change): Promise<PostOutcome>;
}

/** The WebSocket, as the store uses it (see Connection). */
export interface Transport {
  start(): void;
  stop(): void;
  send(msg: WsClientMessage): boolean;
}

export interface TransportHandlers {
  onOpen(): void;
  onMessage(msg: WsServerMessage): void;
  onStatus(status: ConnectionStatus): void;
}

export interface SyncDeps {
  projectId: string;
  api: ProjectApi;
  connect: (handlers: TransportHandlers) => Transport;
  /** Called when the server rejects a change, to tell the user. */
  onRejected?: (change: Change, code: string, message: string) => void;
  /**
   * Runs list and meta notifications. The default waits for the next frame,
   * so a replay of thousands of events re-renders the list once, not once
   * per event. Tests run them right away.
   */
  schedule?: (fn: () => void) => void;
}

export interface SyncMeta {
  project: Project | null;
  status: ConnectionStatus;
  /** Task pages are still loading. */
  loading: boolean;
  /** The stream has replayed everything since the subscribe and is live. */
  caughtUp: boolean;
  lastAppliedSeq: number;
  error: string | null;
}

type Listener = () => void;

const nextFrame = (fn: () => void) =>
  typeof requestAnimationFrame === "function" ? requestAnimationFrame(() => fn()) : setTimeout(fn, 16);

export class ProjectSync {
  private state: SyncState;
  private meta: SyncMeta = {
    project: null,
    status: "connecting",
    loading: true,
    caughtUp: false,
    lastAppliedSeq: 0,
    error: null,
  };
  private transport: Transport | null = null;
  private sender: ChangeSender;
  /** Bumped by every (re)load, so answers to an older load are dropped. */
  private generation = 0;
  /** A resubscribe is in flight: ignore out-of-order events until caughtUp. */
  private resubscribing = false;

  private taskListeners = new Map<string, Set<Listener>>();
  private threadListeners = new Map<string, Set<Listener>>();
  private listListeners = new Set<Listener>();
  private metaListeners = new Set<Listener>();
  private listVersion = 0;
  private listSnapshot: TaskState[] = [];
  private listSnapshotVersion = -1;
  private flushPending = false;

  constructor(private deps: SyncDeps) {
    this.state = emptyState(deps.projectId);
    this.sender = new ChangeSender({
      post: (changeId, change) => deps.api.postChange(deps.projectId, changeId, change),
      lastAppliedSeq: () => this.state.lastAppliedSeq,
      onRejected: (change, code, message) => deps.onRejected?.(change, code, message),
    });
  }

  /** Opens the project: its seq, then the live stream from there, then every task page. */
  async start(): Promise<void> {
    await this.load();
  }

  stop(): void {
    this.generation++;
    this.transport?.stop();
    this.transport = null;
  }

  send(change: Change): string {
    return this.sender.send(change);
  }

  retry(changeId: string): void {
    this.sender.retry(changeId);
  }

  discard(changeId: string): void {
    this.sender.discard(changeId);
  }

  pending(): Pending[] {
    return this.sender.list();
  }

  subscribePending(listener: Listener): () => void {
    return this.sender.subscribe(listener);
  }

  /** Loads a task's details and its whole comment thread. */
  async openTask(taskId: string): Promise<void> {
    const gen = this.generation;
    openThread(this.state, taskId);
    this.notify({ ...noChange(), threads: new Set([taskId]) });

    for (let attempt = 0; attempt < 3; attempt++) {
      const task = await this.deps.api.getTask(taskId);
      if (gen !== this.generation || !task) break;
      const ch = noChange();
      const result = applyTaskDetail(this.state, task, ch);
      this.notify(ch);
      if (result !== "stale") break; // stale: an older answer than what we hold; ask again
    }

    let after = 0;
    for (;;) {
      const page = await this.deps.api.listComments(taskId, after);
      if (gen !== this.generation || !page) return;
      this.notify(applyComments(this.state, taskId, page.comments));
      if (page.nextCursor === null) return;
      after = page.nextCursor;
    }
  }

  // Reads, for useSyncExternalStore.

  getTask(id: string): TaskState | undefined {
    return this.state.tasks.get(id);
  }

  /** Every task, as one array that stays the same until a task changes. */
  getTaskList(): TaskState[] {
    if (this.listSnapshotVersion !== this.listVersion) {
      this.listSnapshot = [...this.state.tasks.values()];
      this.listSnapshotVersion = this.listVersion;
    }
    return this.listSnapshot;
  }

  getThread(taskId: string): Comment[] | undefined {
    return this.state.threads.get(taskId);
  }

  isDeleted(taskId: string): boolean {
    return this.state.tombstones.has(taskId);
  }

  getMeta(): SyncMeta {
    return this.meta;
  }

  subscribeTask(id: string, listener: Listener): () => void {
    return subscribeTo(this.taskListeners, id, listener);
  }

  subscribeThread(taskId: string, listener: Listener): () => void {
    return subscribeTo(this.threadListeners, taskId, listener);
  }

  subscribeList(listener: Listener): () => void {
    this.listListeners.add(listener);
    return () => this.listListeners.delete(listener);
  }

  subscribeMeta(listener: Listener): () => void {
    this.metaListeners.add(listener);
    return () => this.metaListeners.delete(listener);
  }

  // The stream.

  private async load(): Promise<void> {
    const gen = ++this.generation;
    this.state = emptyState(this.deps.projectId);
    this.listVersion++;
    this.setMeta({ loading: true, caughtUp: false, error: null });

    let project: Project;
    try {
      project = await this.deps.api.getProject(this.deps.projectId);
    } catch (err) {
      if (gen === this.generation) this.setMeta({ loading: false, error: errorText(err) });
      return;
    }
    if (gen !== this.generation) return;
    applyProject(this.state, project);
    this.state.lastAppliedSeq = project.seq;
    // Changes accepted before this seq will never come back as events.
    this.sender.caughtUp(project.seq);
    this.setMeta({ project: this.state.project, lastAppliedSeq: project.seq });

    // Subscribe from the seq just read, so nothing between it and the pages is missed.
    if (this.transport) this.subscribe();
    else {
      this.transport = this.deps.connect({
        onOpen: () => this.subscribe(),
        onMessage: (msg) => this.receive(msg),
        onStatus: (status) => this.setMeta({ status }),
      });
      this.transport.start();
    }

    let after: string | undefined;
    try {
      do {
        const page = await this.deps.api.listTasks(this.deps.projectId, after);
        if (gen !== this.generation) return;
        this.notify(applyPage(this.state, page.tasks));
        after = page.nextCursor ?? undefined;
      } while (after);
    } catch (err) {
      if (gen === this.generation) this.setMeta({ loading: false, error: errorText(err) });
      return;
    }
    this.setMeta({ loading: false });
  }

  private subscribe(): void {
    this.transport?.send({ type: "subscribe", projectId: this.deps.projectId, fromSeq: this.state.lastAppliedSeq });
  }

  private receive(msg: WsServerMessage): void {
    if ("projectId" in msg && msg.projectId !== this.deps.projectId) return;
    switch (msg.type) {
      case "event": {
        const e = msg.event;
        if (e.seq <= this.state.lastAppliedSeq) return; // already applied
        if (e.seq !== this.state.lastAppliedSeq + 1) {
          // A gap: ask for a replay from the last event applied.
          if (!this.resubscribing) {
            this.resubscribing = true;
            this.subscribe();
          }
          return;
        }
        const ch = applyEvent(this.state, e);
        this.sender.confirmed(e.changeId);
        this.notify(ch);
        this.setMeta({ lastAppliedSeq: this.state.lastAppliedSeq });
        return;
      }
      case "caughtUp":
        this.resubscribing = false;
        this.sender.caughtUp(this.state.lastAppliedSeq);
        this.setMeta({ caughtUp: true });
        return;
      case "resync":
        // Too far behind to replay: start over.
        this.resubscribing = false;
        void this.load();
        return;
      case "error":
        if (msg.code === "project_not_found") this.setMeta({ error: msg.message });
        return;
    }
  }

  // Notifications.

  private notify(ch: Changed): void {
    for (const id of ch.tasks) fire(this.taskListeners.get(id));
    for (const id of ch.threads) fire(this.threadListeners.get(id));
    if (ch.tasks.size > 0) {
      this.listVersion++;
      this.scheduleFlush();
    }
    if (ch.project) this.setMeta({ project: this.state.project });
  }

  private setMeta(patch: Partial<SyncMeta>): void {
    this.meta = { ...this.meta, ...patch };
    this.scheduleFlush();
  }

  private scheduleFlush(): void {
    if (this.flushPending) return;
    this.flushPending = true;
    (this.deps.schedule ?? nextFrame)(() => {
      this.flushPending = false;
      fire(this.listListeners);
      fire(this.metaListeners);
    });
  }
}

function subscribeTo(map: Map<string, Set<Listener>>, key: string, listener: Listener): () => void {
  const set = map.get(key) ?? new Set();
  set.add(listener);
  map.set(key, set);
  return () => {
    set.delete(listener);
    if (set.size === 0) map.delete(key);
  };
}

function fire(listeners: Set<Listener> | undefined): void {
  if (!listeners) return;
  for (const listener of [...listeners]) listener();
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
