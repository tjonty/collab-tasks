import { describe, expect, test } from "vitest";

import type { Change, Comment, LogEvent, Status, Task, TaskSummary } from "@/lib/api/types";

import {
  applyComments,
  applyEvent,
  applyPage,
  applyProject,
  applyTaskDetail,
  emptyState,
  openThread,
  summaryOf,
  type SyncState,
} from "./apply";
import { ACTOR, PROJECT, ev, summary } from "./testing";

const A = "0000000a-0000-7000-8000-000000000000";
const B = "0000000b-0000-7000-8000-000000000000";
const C = "0000000c-0000-7000-8000-000000000000";
const U1 = "000000f1-0000-7000-8000-000000000000";

function at(lastAppliedSeq: number): SyncState {
  const s = emptyState(PROJECT);
  s.lastAppliedSeq = lastAppliedSeq;
  return s;
}

describe("applyEvent", () => {
  test("task.create fills in the server's defaults, including details", () => {
    const s = at(0);
    const ch = applyEvent(s, ev(1, { type: "task.create", id: A, title: "Design", dependencies: [C, B] }, [A]));
    expect(s.tasks.get(A)).toEqual({
      id: A, title: "Design", status: "todo", priority: "medium", assignedTo: [], tags: [],
      dependencies: [B, C], // ordered by id, like the server's summaries
      updatedSeq: 1,
      details: { description: "", customFields: {} },
    });
    expect([...ch.tasks]).toEqual([A]);
    expect(s.lastAppliedSeq).toBe(1);
  });

  test("an event applies only to tasks it is newer than", () => {
    const s = at(4);
    applyPage(s, [summary(A, 6, { title: "From a newer page" })]);
    applyEvent(s, ev(5, { type: "task.update", taskId: A, title: "Older edit" }, [A]));
    expect(s.tasks.get(A)?.title).toBe("From a newer page");
    applyEvent(s, ev(7, { type: "task.update", taskId: A, title: "Newer edit" }, [A]));
    expect(s.tasks.get(A)).toMatchObject({ title: "Newer edit", updatedSeq: 7 });
  });

  test("an event already applied is ignored", () => {
    const s = at(0);
    applyEvent(s, ev(1, { type: "task.create", id: A, title: "T" }, [A]));
    applyEvent(s, ev(2, { type: "task.addTag", taskId: A, tag: "api" }, [A]));
    const again = ev(2, { type: "task.addTag", taskId: A, tag: "dup" }, [A]);
    const ch = applyEvent(s, again);
    expect(s.tasks.get(A)?.tags).toEqual(["api"]);
    expect(ch.tasks.size).toBe(0);
  });

  test("sets add and remove one item at a time", () => {
    const s = at(1);
    applyPage(s, [summary(A, 1, { tags: ["api"], assignedTo: [U1], dependencies: [C] })]);
    const changes: Change[] = [
      { type: "task.addTag", taskId: A, tag: "web" },
      { type: "task.addTag", taskId: A, tag: "api" }, // already there
      { type: "task.removeTag", taskId: A, tag: "nope" }, // absent
      { type: "task.removeAssignee", taskId: A, userId: U1 },
      { type: "task.addDependency", taskId: A, dependsOnId: B }, // B sorts before C
      { type: "task.removeDependency", taskId: A, dependsOnId: C },
      { type: "task.setStatus", taskId: A, to: "in_progress" },
      { type: "task.update", taskId: A, priority: "urgent" },
    ];
    changes.forEach((c, i) => applyEvent(s, ev(2 + i, c, [A])));
    expect(s.tasks.get(A)).toMatchObject({
      tags: ["api", "web"], assignedTo: [], dependencies: [B], status: "in_progress", priority: "urgent", updatedSeq: 9,
    });
  });

  test("task.update merges custom fields per key into loaded details", () => {
    const s = at(0);
    applyEvent(s, ev(1, { type: "task.create", id: A, title: "T", configuration: { customFields: { a: 1, b: 2 } } }, [A]));
    applyEvent(s, ev(2, { type: "task.update", taskId: A, description: "New", customFields: { a: 5, b: null, c: "x" } }, [A]));
    expect(s.tasks.get(A)?.details).toEqual({ description: "New", customFields: { a: 5, c: "x" } });
  });

  test("task.delete removes the task, tombstones it and unlinks its dependents", () => {
    const s = at(1);
    applyPage(s, [summary(A, 1, { dependencies: [C] }), summary(C, 1)]);
    openThread(s, C);
    const ch = applyEvent(s, ev(2, { type: "task.delete", taskId: C }, [C, A]));
    expect(s.tasks.has(C)).toBe(false);
    expect(s.tombstones.has(C)).toBe(true);
    expect(s.threads.has(C)).toBe(false);
    expect(s.tasks.get(A)).toMatchObject({ dependencies: [], updatedSeq: 2 });
    expect([...ch.tasks].sort()).toEqual([A, C]);
    // A page read before the delete can't bring it back.
    applyPage(s, [summary(C, 1)]);
    expect(s.tasks.has(C)).toBe(false);
  });

  test("comment.create joins an open thread in seq order, once", () => {
    const s = at(3);
    applyPage(s, [summary(A, 1)]);
    openThread(s, A);
    applyEvent(s, ev(4, { type: "comment.create", id: "c4", taskId: A, content: "Live" }));
    const older: Comment = { id: "c2", taskId: A, content: "Older", author: ACTOR, timestamp: "2026-01-01T00:00:00Z", seq: 2 };
    const live: Comment = { ...older, id: "c4", content: "Live", seq: 4 };
    applyComments(s, A, [older, live]); // the first page also has the live one
    expect(s.threads.get(A)?.map((c) => c.id)).toEqual(["c2", "c4"]);
    expect(s.threads.get(A)?.[1]).toMatchObject({ author: ACTOR, content: "Live", seq: 4 });
  });

  test("comment.create for a thread nobody opened is not kept", () => {
    const s = at(0);
    applyEvent(s, ev(1, { type: "comment.create", id: "c1", taskId: A, content: "Hi" }));
    expect(s.threads.has(A)).toBe(false);
  });

  test("project.update changes the project once it's loaded and older", () => {
    const s = at(2);
    applyProject(s, { id: PROJECT, name: "Launch", description: "", metadata: { color: "red", size: "S" }, seq: 2 });
    applyEvent(s, ev(3, { type: "project.update", name: "Launch v2", metadata: { color: null, size: "L" } }));
    expect(s.project).toEqual({ id: PROJECT, name: "Launch v2", description: "", metadata: { size: "L" }, seq: 3 });
  });
});

describe("applyPage", () => {
  test("a newer summary replaces the task and drops stale details; an older one is ignored", () => {
    const s = at(0);
    applyEvent(s, ev(1, { type: "task.create", id: A, title: "T", configuration: { description: "Old" } }, [A]));
    applyPage(s, [summary(A, 0, { title: "Older" })]);
    expect(s.tasks.get(A)?.title).toBe("T");
    applyPage(s, [summary(A, 5, { title: "Newer" })]);
    expect(s.tasks.get(A)).toMatchObject({ title: "Newer", updatedSeq: 5 });
    expect(s.tasks.get(A)?.details).toBeUndefined();
  });

  test("events for a task whose page hasn't loaded wait for it", () => {
    const s = at(10);
    applyEvent(s, ev(11, { type: "task.addTag", taskId: A, tag: "early" }, [A]));
    applyEvent(s, ev(12, { type: "task.update", taskId: A, title: "Renamed" }, [A]));
    expect(s.tasks.has(A)).toBe(false);
    // The page was read at seq 11: it already has the tag, not the rename.
    applyPage(s, [summary(A, 11, { tags: ["early"] })]);
    expect(s.tasks.get(A)).toMatchObject({ title: "Renamed", tags: ["early"], updatedSeq: 12 });
    expect(s.held.size).toBe(0);
  });

  test("a delete unlinks a dependent whose page loads later", () => {
    const s = at(10);
    applyEvent(s, ev(11, { type: "task.delete", taskId: C }, [C, A]));
    applyPage(s, [summary(A, 9, { dependencies: [B, C] })]);
    expect(s.tasks.get(A)?.dependencies).toEqual([B]);
  });
});

describe("applyTaskDetail", () => {
  const detail = (updatedSeq: number, title = "T"): Task => ({
    id: A, projectId: PROJECT, title, status: "todo", assignedTo: [], dependencies: [], updatedSeq,
    configuration: { priority: "high", description: "Long", tags: [], customFields: { k: 1 } },
  });

  test("a detail at least as new as the summary is kept with its details", () => {
    const s = at(3);
    applyPage(s, [summary(A, 3)]);
    expect(applyTaskDetail(s, detail(3))).toBe("applied");
    expect(s.tasks.get(A)).toMatchObject({ priority: "high", details: { description: "Long", customFields: { k: 1 } } });
  });

  test("a detail older than the summary held is stale", () => {
    const s = at(5);
    applyPage(s, [summary(A, 5, { title: "Newer" })]);
    expect(applyTaskDetail(s, detail(4, "Older"))).toBe("stale");
    expect(s.tasks.get(A)?.title).toBe("Newer");
  });

  test("a deleted task's detail is ignored", () => {
    const s = at(1);
    applyEvent(s, ev(2, { type: "task.delete", taskId: A }, [A]));
    expect(applyTaskDetail(s, detail(1))).toBe("gone");
    expect(s.tasks.has(A)).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Convergence: a small model of the server produces random histories; the
// client gets the events in order with pages, snapshotted at random log
// positions, interleaved in random order. It must end up where the server is.

function prng(seed: number) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

type Rand = ReturnType<typeof prng>;
const pick = <T,>(r: Rand, xs: readonly T[]): T => xs[Math.floor(r() * xs.length)];

function uuidFrom(r: Rand): string {
  const hex = Array.from({ length: 32 }, () => Math.floor(r() * 16).toString(16)).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

/** The server's rules for the summary fields, as api/internal/changes applies them. */
class ModelServer {
  seq = 0;
  log: LogEvent[] = [];
  tasks = new Map<string, TaskSummary>();

  constructor(private r: Rand) {}

  summaries(): TaskSummary[] {
    return [...this.tasks.values()].map((t) => structuredClone(t)).sort((a, b) => (a.id < b.id ? -1 : 1));
  }

  /** Applies one random valid change and logs its event. */
  step(): void {
    const r = this.r;
    const ids = [...this.tasks.keys()];
    const commit = (change: Change, touches: string[]) => {
      this.seq += 1;
      for (const id of touches) {
        const t = this.tasks.get(id);
        if (t) t.updatedSeq = this.seq;
      }
      this.log.push(ev(this.seq, change, touches));
    };
    const kind = ids.length < 3 ? 0 : Math.floor(r() * 10);
    if (kind === 0) {
      const id = uuidFrom(r);
      const deps = ids.filter(() => r() < 0.2).sort();
      this.tasks.set(id, summary(id, 0, { title: `T${this.seq}`, dependencies: deps }));
      return commit({ type: "task.create", id, title: `T${this.seq}`, dependencies: deps }, [id]);
    }
    const id = pick(r, ids);
    const t = this.tasks.get(id)!;
    switch (kind) {
      case 1: {
        t.title = `Edit ${this.seq}`;
        return commit({ type: "task.update", taskId: id, title: t.title }, [id]);
      }
      case 2: {
        const deps = t.dependencies.map((d) => this.tasks.get(d)!.status);
        const to = pick(r, ["todo", "in_progress", "in_review", "done"] as Status[]);
        const moves: Record<Status, Status[]> = { todo: ["in_progress"], in_progress: ["todo", "in_review", "done"], in_review: ["in_progress", "done"], done: ["in_progress"] };
        if (!moves[t.status].includes(to) || (to === "done" && deps.some((d) => d !== "done"))) return this.step();
        t.status = to;
        return commit({ type: "task.setStatus", taskId: id, to }, [id]);
      }
      case 3: {
        const tag = pick(r, ["api", "web", "ops"]);
        if (r() < 0.5) {
          if (!t.tags.includes(tag) && t.tags.length < 20) t.tags.push(tag);
          return commit({ type: "task.addTag", taskId: id, tag }, [id]);
        }
        t.tags = t.tags.filter((x) => x !== tag);
        return commit({ type: "task.removeTag", taskId: id, tag }, [id]);
      }
      case 4: {
        const user = pick(r, [U1, ACTOR]);
        if (r() < 0.5) {
          if (!t.assignedTo.includes(user)) t.assignedTo.push(user);
          return commit({ type: "task.addAssignee", taskId: id, userId: user }, [id]);
        }
        t.assignedTo = t.assignedTo.filter((x) => x !== user);
        return commit({ type: "task.removeAssignee", taskId: id, userId: user }, [id]);
      }
      case 5: {
        const on = pick(r, ids);
        const reaches = (from: string): boolean =>
          from === id || this.tasks.get(from)!.dependencies.some((d) => reaches(d));
        if (on === id || reaches(on)) return this.step(); // would be a cycle
        if (!t.dependencies.includes(on)) t.dependencies = [...t.dependencies, on].sort();
        return commit({ type: "task.addDependency", taskId: id, dependsOnId: on }, [id]);
      }
      case 6: {
        const on = t.dependencies.length > 0 ? pick(r, t.dependencies) : pick(r, ids);
        t.dependencies = t.dependencies.filter((d) => d !== on);
        return commit({ type: "task.removeDependency", taskId: id, dependsOnId: on }, [id]);
      }
      case 7: {
        const dependents = [...this.tasks.values()].filter((x) => x.dependencies.includes(id)).map((x) => x.id).sort();
        this.tasks.delete(id);
        for (const d of dependents) {
          const dt = this.tasks.get(d)!;
          dt.dependencies = dt.dependencies.filter((x) => x !== id);
        }
        return commit({ type: "task.delete", taskId: id }, [id, ...dependents]);
      }
      default:
        return commit({ type: "comment.create", id: uuidFrom(r), taskId: id, content: "hi" }, []);
    }
  }
}

function converge(seed: number) {
  const r = prng(seed);
  const server = new ModelServer(r);
  const before = 5 + Math.floor(r() * 20);
  while (server.seq < before) server.step();
  const s0 = server.seq; // the client opens the project here

  // Pages split the id space at random boundaries; each is read at its own
  // random log position at or after s0.
  const bounds = Array.from({ length: Math.floor(r() * 4) }, () => uuidFrom(r)).sort();
  const ranges = [...bounds, "g"].map((hi, i) => ({ lo: i === 0 ? "" : bounds[i - 1], hi }));
  const after = 10 + Math.floor(r() * 40);
  const final = s0 + after;
  const readAt = ranges.map(() => s0 + Math.floor(r() * (after + 1)));
  const pages: { at: number; tasks: TaskSummary[] }[] = [];
  const snapshot = () => {
    ranges.forEach((range, i) => {
      if (readAt[i] === server.seq) {
        pages.push({ at: server.seq, tasks: server.summaries().filter((t) => t.id > range.lo && t.id <= range.hi) });
      }
    });
  };
  snapshot();
  while (server.seq < final) {
    server.step();
    snapshot();
  }

  // The client: events in order, with the pages, in random order, slotted in
  // at random points of the stream.
  const steps: ({ event: LogEvent } | { page: TaskSummary[] })[] = server.log.slice(s0).map((event) => ({ event }));
  for (const page of [...pages].sort(() => r() - 0.5)) {
    steps.splice(Math.floor(r() * (steps.length + 1)), 0, { page: page.tasks });
  }
  const client = at(s0);
  for (const step of steps) {
    if ("event" in step) applyEvent(client, step.event);
    else applyPage(client, step.page);
  }

  const got = [...client.tasks.values()].map(summaryOf).sort((a, b) => (a.id < b.id ? -1 : 1));
  return { got, want: server.summaries(), held: client.held.size, seq: client.lastAppliedSeq, final };
}

test("random histories with pages interleaved in random orders converge on the server's state", () => {
  for (let seed = 1; seed <= 300; seed++) {
    const { got, want, held, seq, final } = converge(seed);
    expect(got, `seed ${seed}`).toEqual(want);
    expect(held, `seed ${seed}: events left waiting`).toBe(0);
    expect(seq, `seed ${seed}`).toBe(final);
  }
});
