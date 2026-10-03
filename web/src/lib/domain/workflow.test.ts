// These cases mirror api/internal/domain's table tests, so the UI and the
// server agree on what a task may do.
import { describe, expect, test } from "vitest";

import type { Status } from "@/lib/api/types";

import { allowedMoves, canMoveTo, dependentsOf, isBlocked, wouldCycle } from "./workflow";

describe("canMoveTo: the workflow", () => {
  const cases: [Status, Status, boolean][] = [
    ["todo", "todo", false],
    ["todo", "in_progress", true],
    ["todo", "in_review", false],
    ["todo", "done", false],
    ["in_progress", "todo", true],
    ["in_progress", "in_progress", false],
    ["in_progress", "in_review", true],
    ["in_progress", "done", true],
    ["in_review", "todo", false],
    ["in_review", "in_progress", true],
    ["in_review", "in_review", false],
    ["in_review", "done", true],
    ["done", "todo", false],
    ["done", "in_progress", true],
    ["done", "in_review", false],
    ["done", "done", false],
  ];
  test.each(cases)("%s → %s allowed: %s", (from, to, allowed) => {
    expect(canMoveTo(from, to, [])).toBe(allowed ? null : "invalid_transition");
  });
});

describe("canMoveTo: the Done rule", () => {
  test.each<[string, Status, Status, Status[], string | null]>([
    ["done with no deps", "in_progress", "done", [], null],
    ["done with all deps done", "in_review", "done", ["done", "done"], null],
    ["done with one dep open", "in_progress", "done", ["done", "in_review"], "dependencies_not_done"],
    ["start with deps open", "todo", "in_progress", ["todo"], null],
    ["reopen with deps open", "done", "in_progress", ["todo"], null],
    ["illegal move checked first", "todo", "done", ["todo"], "invalid_transition"],
  ])("%s", (_, from, to, deps, want) => {
    expect(canMoveTo(from, to, deps)).toBe(want);
  });
});

test("allowedMoves lists the statuses a task can move to", () => {
  expect(allowedMoves("todo")).toEqual(["in_progress"]);
  expect(allowedMoves("in_progress")).toEqual(["todo", "in_review", "done"]);
  expect(allowedMoves("in_review")).toEqual(["in_progress", "done"]);
  expect(allowedMoves("done")).toEqual(["in_progress"]);
});

describe("isBlocked", () => {
  test.each<[string, Status, Status[], boolean]>([
    ["no deps", "todo", [], false],
    ["all deps done", "in_progress", ["done", "done"], false],
    ["one dep open", "todo", ["done", "in_review"], true],
    ["done with a reopened dep", "done", ["in_progress"], false],
  ])("%s", (_, status, deps, want) => {
    expect(isBlocked(status, deps)).toBe(want);
  });
});

describe("wouldCycle", () => {
  // a→b, b→c, a→d, d→c (a diamond over c); e has no links.
  const graph: Record<string, string[]> = { a: ["b", "d"], b: ["c"], c: [], d: ["c"], e: [] };
  const deps = (id: string) => graph[id];
  test.each<[string, string, string, boolean]>([
    ["direct reverse b→a", "b", "a", true],
    ["indirect c→a", "c", "a", true],
    ["through either side c→b", "c", "b", true],
    ["itself a→a", "a", "a", true],
    ["shortcut a→c", "a", "c", false],
    ["sibling b→d", "b", "d", false],
    ["new root e→a", "e", "a", false],
    ["new leaf c→e", "c", "e", false],
  ])("%s", (_, task, dependsOn, want) => {
    expect(wouldCycle(deps, task, dependsOn)).toBe(want);
  });

  test("tolerates a cycle already in the data", () => {
    const loop: Record<string, string[]> = { x: ["y"], y: ["x"], z: [] };
    expect(wouldCycle((id) => loop[id], "z", "x")).toBe(false);
  });
});

test("dependentsOf lists the tasks that depend on a task", () => {
  const tasks = [
    { id: "a", dependencies: ["c"] },
    { id: "b", dependencies: ["c", "a"] },
    { id: "c", dependencies: [] },
  ];
  expect(dependentsOf(tasks, "c")).toEqual(["a", "b"]);
  expect(dependentsOf(tasks, "a")).toEqual(["b"]);
  expect(dependentsOf(tasks, "b")).toEqual([]);
});
