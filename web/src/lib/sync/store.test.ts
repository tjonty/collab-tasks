import { describe, expect, test, vi } from "vitest";

import type { Comment, Project, Task, TaskPage, WsClientMessage, WsServerMessage } from "@/lib/api/types";

import type { PostOutcome } from "./changes";
import type { ConnectionStatus } from "./connection";
import { ProjectSync, type ProjectApi } from "./store";
import { ACTOR, PROJECT, ev, summary } from "./testing";

const A = "0000000a-0000-7000-8000-000000000000";
const B = "0000000b-0000-7000-8000-000000000000";

const project = (seq: number, name = "Launch"): Project => ({ id: PROJECT, name, description: "", metadata: {}, seq });

/** The API as the store sees it, answering from data the test sets up. */
class FakeApi implements ProjectApi {
  projects: Project[] = [];
  pages: TaskPage[] = [];
  tasks = new Map<string, Task[]>(); // successive answers per task
  comments = new Map<string, Comment[]>();
  posted: { changeId: string }[] = [];
  outcome: PostOutcome = { kind: "ok", seq: 1_000 };

  async getProject() {
    return this.projects.shift() ?? project(0);
  }
  async listTasks(_: string, after?: string) {
    const i = after === undefined ? 0 : this.pages.findIndex((p) => p.tasks.at(-1)?.id === after) + 1;
    return this.pages[i];
  }
  async getTask(taskId: string) {
    const answers = this.tasks.get(taskId) ?? [];
    return answers.length > 1 ? answers.shift()! : (answers[0] ?? null);
  }
  async listComments(taskId: string, afterSeq: number) {
    const all = (this.comments.get(taskId) ?? []).filter((c) => c.seq > afterSeq);
    const page = all.slice(0, 2);
    return { comments: page, nextCursor: all.length > 2 ? page[1].seq : null };
  }
  async postChange(_: string, changeId: string) {
    this.posted.push({ changeId });
    return this.outcome;
  }
}

/** The WebSocket as the store sees it. */
function fakeTransport() {
  const sent: WsClientMessage[] = [];
  let handlers!: { onOpen(): void; onMessage(m: WsServerMessage): void; onStatus(s: ConnectionStatus): void };
  let started = 0;
  return {
    sent,
    connect: (h: typeof handlers) => {
      handlers = h;
      return {
        start: () => started++,
        stop: () => {},
        send: (m: WsClientMessage) => (sent.push(m), true),
      };
    },
    started: () => started,
    open: () => handlers.onOpen(),
    push: (m: WsServerMessage) => handlers.onMessage(m),
  };
}

function setup() {
  const api = new FakeApi();
  const ws = fakeTransport();
  const sync = new ProjectSync({ projectId: PROJECT, api, connect: ws.connect, schedule: (fn) => fn() });
  return { api, ws, sync };
}

const event = (e: ReturnType<typeof ev>): WsServerMessage => ({ type: "event", projectId: PROJECT, event: e });

describe("ProjectSync", () => {
  test("opening a project reads its seq, subscribes from there and loads every page", async () => {
    const { api, ws, sync } = setup();
    api.projects = [project(5)];
    api.pages = [
      { tasks: [summary(A, 2)], nextCursor: A, seq: 5 },
      { tasks: [summary(B, 4)], nextCursor: null, seq: 5 },
    ];
    await sync.start();
    expect(ws.started()).toBe(1);
    ws.open();
    expect(ws.sent).toEqual([{ type: "subscribe", projectId: PROJECT, fromSeq: 5 }]);
    expect(sync.getTaskList().map((t) => t.id)).toEqual([A, B]);
    expect(sync.getMeta()).toMatchObject({ project: project(5), loading: false });
  });

  test("events apply in order and wake only the readers of the tasks they touch", async () => {
    const { api, ws, sync } = setup();
    api.projects = [project(2)];
    api.pages = [{ tasks: [summary(A, 1), summary(B, 2)], nextCursor: null, seq: 2 }];
    await sync.start();
    const onA = vi.fn();
    const onB = vi.fn();
    sync.subscribeTask(A, onA);
    sync.subscribeTask(B, onB);

    ws.push(event(ev(3, { type: "task.update", taskId: A, title: "Renamed" }, [A])));
    expect(sync.getTask(A)?.title).toBe("Renamed");
    expect(onA).toHaveBeenCalledTimes(1);
    expect(onB).not.toHaveBeenCalled();
    expect(sync.getMeta().lastAppliedSeq).toBe(3);
  });

  test("a gap in the stream resubscribes once from the last applied seq", async () => {
    const { api, ws, sync } = setup();
    api.projects = [project(2)];
    api.pages = [{ tasks: [summary(A, 1)], nextCursor: null, seq: 2 }];
    await sync.start();
    ws.open();
    ws.sent.length = 0;

    ws.push(event(ev(4, { type: "task.addTag", taskId: A, tag: "x" }, [A]))); // 3 is missing
    ws.push(event(ev(5, { type: "task.addTag", taskId: A, tag: "y" }, [A])));
    expect(ws.sent).toEqual([{ type: "subscribe", projectId: PROJECT, fromSeq: 2 }]);
    expect(sync.getTask(A)?.tags).toEqual([]);

    // The replay starts right after 2.
    ws.push(event(ev(3, { type: "task.addTag", taskId: A, tag: "w" }, [A])));
    ws.push(event(ev(4, { type: "task.addTag", taskId: A, tag: "x" }, [A])));
    ws.push({ type: "caughtUp", projectId: PROJECT, seq: 4 });
    expect(sync.getTask(A)?.tags).toEqual(["w", "x"]);
    expect(sync.getMeta().caughtUp).toBe(true);
  });

  test("a reconnect subscribes from the last applied seq, not where the project was opened", async () => {
    const { api, ws, sync } = setup();
    api.projects = [project(2)];
    api.pages = [{ tasks: [summary(A, 1)], nextCursor: null, seq: 2 }];
    await sync.start();
    ws.open();
    ws.push(event(ev(3, { type: "task.addTag", taskId: A, tag: "x" }, [A])));
    ws.sent.length = 0;
    ws.open();
    expect(ws.sent).toEqual([{ type: "subscribe", projectId: PROJECT, fromSeq: 3 }]);
  });

  test("resync reloads the project from scratch", async () => {
    const { api, ws, sync } = setup();
    api.projects = [project(2), project(9000, "Reloaded")];
    api.pages = [{ tasks: [summary(A, 1)], nextCursor: null, seq: 2 }];
    await sync.start();
    ws.open();
    ws.sent.length = 0;
    api.pages = [{ tasks: [summary(B, 8999)], nextCursor: null, seq: 9000 }];

    ws.push({ type: "resync", projectId: PROJECT, reason: "too_far_behind" });
    await vi.waitFor(() => expect(sync.getMeta().loading).toBe(false));
    expect(sync.getMeta().project?.name).toBe("Reloaded");
    expect(sync.getTaskList().map((t) => t.id)).toEqual([B]);
    expect(ws.sent).toEqual([{ type: "subscribe", projectId: PROJECT, fromSeq: 9000 }]);
  });

  test("a change is pending until its own event comes back", async () => {
    const { api, ws, sync } = setup();
    api.projects = [project(1)];
    api.pages = [{ tasks: [summary(A, 1)], nextCursor: null, seq: 1 }];
    api.outcome = { kind: "ok", seq: 2 };
    await sync.start();
    const changeId = sync.send({ type: "task.update", taskId: A, title: "Mine" });
    await vi.waitFor(() => expect(sync.pending()).toMatchObject([{ changeId, state: "waiting" }]));

    const e = ev(2, { type: "task.update", taskId: A, title: "Mine" }, [A]);
    ws.push(event({ ...e, changeId }));
    expect(sync.pending()).toEqual([]);
    expect(sync.getTask(A)?.title).toBe("Mine");
  });

  test("opening a task loads its details, refetching a stale answer, and its whole thread", async () => {
    const { api, ws, sync } = setup();
    api.projects = [project(4)];
    api.pages = [{ tasks: [summary(A, 4)], nextCursor: null, seq: 4 }];
    const detail = (updatedSeq: number): Task => ({
      id: A, projectId: PROJECT, title: "T", status: "todo", assignedTo: [], dependencies: [], updatedSeq,
      configuration: { priority: "low", description: `as of ${updatedSeq}`, tags: [], customFields: {} },
    });
    api.tasks.set(A, [detail(3), detail(4)]); // the first answer is older than the summary
    const comment = (seq: number): Comment => ({ id: `c${seq}`, taskId: A, content: `#${seq}`, author: ACTOR, timestamp: "2026-01-01T00:00:00Z", seq });
    api.comments.set(A, [comment(1), comment(2), comment(3)]); // two pages
    await sync.start();

    await sync.openTask(A);
    expect(sync.getTask(A)?.details?.description).toBe("as of 4");
    expect(sync.getThread(A)?.map((c) => c.seq)).toEqual([1, 2, 3]);

    ws.push(event(ev(5, { type: "comment.create", id: "c5", taskId: A, content: "live" })));
    expect(sync.getThread(A)?.map((c) => c.seq)).toEqual([1, 2, 3, 5]);
  });

  test("the task list is the same array until a task changes", async () => {
    const { api, ws, sync } = setup();
    api.projects = [project(1)];
    api.pages = [{ tasks: [summary(A, 1)], nextCursor: null, seq: 1 }];
    await sync.start();
    const first = sync.getTaskList();
    expect(sync.getTaskList()).toBe(first);
    ws.push(event(ev(2, { type: "comment.create", id: "c", taskId: A, content: "no task change" })));
    expect(sync.getTaskList()).toBe(first);
    ws.push(event(ev(3, { type: "task.setStatus", taskId: A, to: "in_progress" }, [A])));
    expect(sync.getTaskList()).not.toBe(first);
  });

  test("messages about another project are ignored", async () => {
    const { api, ws, sync } = setup();
    api.projects = [project(1)];
    api.pages = [{ tasks: [summary(A, 1)], nextCursor: null, seq: 1 }];
    await sync.start();
    const other = { ...ev(2, { type: "task.setStatus", taskId: A, to: "in_progress" }, [A]), projectId: "other" };
    ws.push({ type: "event", projectId: "other", event: other });
    expect(sync.getTask(A)?.status).toBe("todo");
  });
});
