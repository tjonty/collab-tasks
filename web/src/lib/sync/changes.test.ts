import { describe, expect, test, vi } from "vitest";

import type { Change, ErrorCode } from "@/lib/api/types";

import { ChangeSender, type PostOutcome } from "./changes";

const rename: Change = { type: "task.update", taskId: "t1", title: "New" };
const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

/** A sender whose transport answers from a script, one outcome per POST. */
function setup(outcomes: PostOutcome[], lastApplied = 0) {
  const posts: { changeId: string; change: Change }[] = [];
  const delays: number[] = [];
  const rejected: { code: ErrorCode; message: string }[] = [];
  let applied = lastApplied;
  const sender = new ChangeSender({
    post: async (changeId, change) => {
      posts.push({ changeId, change });
      return outcomes.shift() ?? { kind: "retry", reason: "no more scripted outcomes" };
    },
    lastAppliedSeq: () => applied,
    onRejected: (_, code, message) => rejected.push({ code, message }),
    sleep: async (ms) => {
      delays.push(ms);
    },
  });
  return { sender, posts, delays, rejected, setApplied: (seq: number) => (applied = seq) };
}

const settle = () => new Promise((r) => setTimeout(r, 0));

describe("ChangeSender", () => {
  test("a change is posted with a fresh UUIDv7 and stays pending until its event is applied", async () => {
    const { sender, posts } = setup([{ kind: "ok", seq: 5 }], 4);
    const changeId = sender.send(rename);
    expect(changeId).toMatch(UUID_V7);
    expect(sender.list()).toMatchObject([{ changeId, change: rename, state: "sending" }]);
    await settle();
    expect(posts).toEqual([{ changeId, change: rename }]);
    expect(sender.list()).toMatchObject([{ changeId, state: "waiting", seq: 5 }]);

    sender.confirmed(changeId);
    expect(sender.list()).toEqual([]);
  });

  test("a change whose event was already applied clears when the POST returns", async () => {
    const { sender } = setup([{ kind: "ok", seq: 5 }], 5);
    sender.send(rename);
    await settle();
    expect(sender.list()).toEqual([]);
  });

  test("catching up past a waiting change's seq clears it", async () => {
    const { sender } = setup([{ kind: "ok", seq: 7 }], 5);
    sender.send(rename);
    await settle();
    sender.caughtUp(6);
    expect(sender.list()).toHaveLength(1);
    sender.caughtUp(7);
    expect(sender.list()).toEqual([]);
  });

  test("an event that arrives before the POST returns clears the change; the late response is ignored", async () => {
    const { sender } = setup([{ kind: "ok", seq: 9 }]);
    const changeId = sender.send(rename);
    sender.confirmed(changeId);
    expect(sender.list()).toEqual([]);
    await settle();
    expect(sender.list()).toEqual([]);
  });

  test("a rejected change is dropped and reported, not retried", async () => {
    const { sender, posts, rejected } = setup([{ kind: "rejected", code: "invalid_transition", message: "no" }]);
    sender.send(rename);
    await settle();
    expect(posts).toHaveLength(1);
    expect(sender.list()).toEqual([]);
    expect(rejected).toEqual([{ code: "invalid_transition", message: "no" }]);
  });

  test("a failed POST is retried with the same changeId, backing off from 0.5 s to 8 s", async () => {
    const { sender, posts, delays } = setup([
      { kind: "retry", reason: "network" },
      { kind: "retry", reason: "503" },
      { kind: "ok", seq: 3 },
    ]);
    const changeId = sender.send(rename);
    await vi.waitFor(() => expect(sender.list()).toMatchObject([{ state: "waiting" }]));
    expect(posts.map((p) => p.changeId)).toEqual([changeId, changeId, changeId]);
    expect(delays).toEqual([500, 1000]);
  });

  test("after five retries the change is marked failed; it can be retried or discarded", async () => {
    const { sender, posts, delays } = setup([]);
    const changeId = sender.send(rename);
    await vi.waitFor(() => expect(sender.list()).toMatchObject([{ changeId, state: "failed" }]));
    expect(posts).toHaveLength(6);
    expect(delays).toEqual([500, 1000, 2000, 4000, 8000]);

    sender.retry(changeId);
    expect(sender.list()).toMatchObject([{ changeId, state: "sending" }]);
    await vi.waitFor(() => expect(sender.list()).toMatchObject([{ state: "failed" }]));
    expect(new Set(posts.map((p) => p.changeId))).toEqual(new Set([changeId]));

    sender.discard(changeId);
    expect(sender.list()).toEqual([]);
  });

  test("list() returns the same array until something changes, for useSyncExternalStore", async () => {
    const { sender } = setup([{ kind: "ok", seq: 9 }]);
    const before = sender.list();
    expect(sender.list()).toBe(before);
    sender.send(rename);
    expect(sender.list()).not.toBe(before);
    const sending = sender.list();
    expect(sender.list()).toBe(sending);
  });

  test("subscribers hear every change to the pending list", async () => {
    const { sender } = setup([{ kind: "ok", seq: 9 }]);
    const heard = vi.fn();
    const unsubscribe = sender.subscribe(heard);
    const changeId = sender.send(rename);
    await settle();
    sender.confirmed(changeId);
    expect(heard).toHaveBeenCalledTimes(3); // sending, waiting, cleared
    unsubscribe();
    sender.send(rename);
    expect(heard).toHaveBeenCalledTimes(3);
  });
});
