import { describe, expect, test } from "vitest";

import type { Change } from "@/lib/api/types";

import type { TaskState } from "./apply";
import type { Pending } from "./changes";
import { emptyFilters, parseFieldValue, pendingFields, rejectionMessage, visibleTasks, waitingOn } from "./view";

const task = (id: string, fields: Partial<TaskState> = {}): TaskState => ({
  id, title: `Task ${id}`, status: "todo", priority: "medium", assignedTo: [], tags: [], dependencies: [], updatedSeq: 1, ...fields,
});

const tasks = [
  task("a", { title: "Design API", status: "done", priority: "high", tags: ["api"] }),
  task("b", { title: "Write tests", status: "in_progress", priority: "urgent", assignedTo: ["u1"], dependencies: ["a"] }),
  task("c", { title: "Ship it", priority: "low", dependencies: ["b", "gone"] }),
  task("d", { title: "Docs", status: "in_review", tags: ["docs", "api"], assignedTo: ["u2"] }),
];
const ids = (ts: TaskState[]) => ts.map((t) => t.id);

describe("visibleTasks", () => {
  test("with no filters, every task in creation (id) order", () => {
    expect(ids(visibleTasks([...tasks].reverse(), emptyFilters(), "created"))).toEqual(["a", "b", "c", "d"]);
  });

  test("filters by title text, ignoring case", () => {
    expect(ids(visibleTasks(tasks, { ...emptyFilters(), text: "IT" }, "created"))).toEqual(["b", "c"]);
  });

  test("filters by status, assignee and tag", () => {
    expect(ids(visibleTasks(tasks, { ...emptyFilters(), status: "in_review" }, "created"))).toEqual(["d"]);
    expect(ids(visibleTasks(tasks, { ...emptyFilters(), assignee: "u1" }, "created"))).toEqual(["b"]);
    expect(ids(visibleTasks(tasks, { ...emptyFilters(), tag: "api" }, "created"))).toEqual(["a", "d"]);
  });

  test("blocked only: not done, and waiting on a task that isn't done", () => {
    // c waits on b (in progress); "gone" isn't loaded, so it doesn't count.
    expect(ids(visibleTasks(tasks, { ...emptyFilters(), blockedOnly: true }, "created"))).toEqual(["c"]);
  });

  test("sorts by priority, status or title, ties by id", () => {
    expect(ids(visibleTasks(tasks, emptyFilters(), "priority"))).toEqual(["b", "a", "d", "c"]);
    expect(ids(visibleTasks(tasks, emptyFilters(), "status"))).toEqual(["c", "b", "d", "a"]);
    expect(ids(visibleTasks(tasks, emptyFilters(), "title"))).toEqual(["a", "d", "c", "b"]);
  });
});

test("waitingOn lists the dependencies that aren't done yet", () => {
  const byId = new Map(tasks.map((t) => [t.id, t]));
  expect(waitingOn(tasks[2], (id) => byId.get(id)).map((t) => t.title)).toEqual(["Write tests"]);
  expect(waitingOn(tasks[1], (id) => byId.get(id))).toEqual([]);
});

test("pendingFields says which parts of a task are still saving", () => {
  const p = (change: Change): Pending => ({ changeId: change.type, change, state: "sending" });
  const pending = [
    p({ type: "task.update", taskId: "a", title: "x", customFields: { k: 1 } }),
    p({ type: "task.setStatus", taskId: "a", to: "done" }),
    p({ type: "task.addTag", taskId: "a", tag: "t" }),
    p({ type: "task.removeAssignee", taskId: "a", userId: "u" }),
    p({ type: "task.addDependency", taskId: "a", dependsOnId: "b" }),
    p({ type: "comment.create", id: "c", taskId: "a", content: "hi" }),
    p({ type: "task.update", taskId: "b", priority: "low" }), // another task
  ];
  expect(pendingFields(pending, "a")).toEqual(
    new Set(["title", "customFields", "status", "tags", "assignedTo", "dependencies", "comments"]),
  );
  expect(pendingFields(pending, "c")).toEqual(new Set());
});

test("rejectionMessage turns a rejection into something a person can act on", () => {
  expect(rejectionMessage("dependencies_not_done", "2 of the task's dependencies aren't done")).toBe(
    "Can't mark Done: waiting on 2 tasks.",
  );
  expect(rejectionMessage("dependencies_not_done", "1 of the task's dependencies aren't done")).toBe(
    "Can't mark Done: waiting on 1 task.",
  );
  expect(rejectionMessage("invalid_transition", "x")).toBe("Someone moved this task first, so that status change no longer applies.");
  expect(rejectionMessage("dependency_cycle", "x")).toBe("That would make the tasks depend on each other in a loop.");
  expect(rejectionMessage("task_not_found", "x")).toBe("That task was deleted.");
  expect(rejectionMessage("limit_exceeded", "the task already has 20 tags, max 20")).toBe(
    "Limit reached: the task already has 20 tags, max 20.",
  );
  expect(rejectionMessage("validation_failed", "title: is required")).toBe("Not saved: title: is required.");
});

test("parseFieldValue reads what was typed as a number, a boolean or text", () => {
  expect(parseFieldValue("3")).toBe(3);
  expect(parseFieldValue(" 2.5 ")).toBe(2.5);
  expect(parseFieldValue("true")).toBe(true);
  expect(parseFieldValue("false")).toBe(false);
  expect(parseFieldValue("core team")).toBe("core team");
  expect(parseFieldValue("")).toBe("");
  expect(parseFieldValue("1e3")).toBe(1000);
  expect(parseFieldValue("0x10")).toBe("0x10"); // not a plain decimal number
});
