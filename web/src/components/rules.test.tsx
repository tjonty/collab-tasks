// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, test, vi } from "vitest";

import type { TaskState } from "@/lib/sync/apply";

import { ConnectionBadge } from "./connection-badge";
import { DeleteTaskButton } from "./delete-task-button";
import { DependencyPicker } from "./dependency-picker";
import { NamePrompt } from "./name-prompt";
import { StatusSelect } from "./status-select";
import { TaskRow } from "./task-row";

afterEach(cleanup);

const task = (id: string, fields: Partial<TaskState> = {}): TaskState => ({
  id, title: `Task ${id}`, status: "todo", priority: "medium", assignedTo: [], tags: [], dependencies: [], updatedSeq: 1, ...fields,
});

const options = (select: HTMLSelectElement) =>
  [...select.options].map((o) => ({ value: o.value, disabled: o.disabled }));

describe("StatusSelect", () => {
  test("offers only the moves the workflow allows", () => {
    render(<StatusSelect status="todo" waitingOn={[]} onChange={() => {}} />);
    expect(options(screen.getByLabelText<HTMLSelectElement>("Status"))).toEqual([
      { value: "todo", disabled: false },
      { value: "in_progress", disabled: false },
    ]);
  });

  test("disables Done while a dependency is open, and says which", () => {
    render(<StatusSelect status="in_progress" waitingOn={["Design API", "Write tests"]} onChange={() => {}} />);
    const select = screen.getByLabelText<HTMLSelectElement>("Status");
    const done = [...select.options].find((o) => o.value === "done")!;
    expect(done.disabled).toBe(true);
    expect(done.title).toBe("Waiting on: Design API, Write tests");
    expect(screen.getByText("Waiting on: Design API, Write tests")).toBeTruthy();
  });

  test("reports the chosen move", async () => {
    const onChange = vi.fn();
    render(<StatusSelect status="in_progress" waitingOn={[]} onChange={onChange} />);
    await userEvent.selectOptions(screen.getByLabelText("Status"), "done");
    expect(onChange).toHaveBeenCalledWith("done");
  });
});

describe("TaskRow", () => {
  test("shows Blocked and saving… when they apply", () => {
    const { rerender } = render(<TaskRow task={task("a")} blocked={false} saving={false} onOpen={() => {}} />);
    expect(screen.queryByText("Blocked")).toBeNull();
    expect(screen.queryByText("saving…")).toBeNull();
    rerender(<TaskRow task={task("a")} blocked={true} saving={true} onOpen={() => {}} />);
    expect(screen.getByText("Blocked")).toBeTruthy();
    expect(screen.getByText("saving…")).toBeTruthy();
  });

  test("opens the task when clicked", async () => {
    const onOpen = vi.fn();
    render(<TaskRow task={task("a", { title: "Design API" })} blocked={false} saving={false} onOpen={onOpen} />);
    await userEvent.click(screen.getByText("Design API"));
    expect(onOpen).toHaveBeenCalledWith("a");
  });
});

describe("DeleteTaskButton", () => {
  test("warns how many tasks depend on it, and deletes only when confirmed", async () => {
    const onDelete = vi.fn();
    const confirm = vi.fn().mockReturnValueOnce(false).mockReturnValueOnce(true);
    render(<DeleteTaskButton dependents={3} onDelete={onDelete} confirm={confirm} />);
    await userEvent.click(screen.getByRole("button", { name: "Delete task" }));
    expect(confirm).toHaveBeenLastCalledWith("3 tasks depend on this. Delete anyway?");
    expect(onDelete).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "Delete task" }));
    expect(onDelete).toHaveBeenCalledTimes(1);
  });

  test("with no dependents it still asks, more plainly", async () => {
    const confirm = vi.fn().mockReturnValue(true);
    render(<DeleteTaskButton dependents={0} onDelete={() => {}} confirm={confirm} />);
    await userEvent.click(screen.getByRole("button", { name: "Delete task" }));
    expect(confirm).toHaveBeenCalledWith("Delete this task?");
  });
});

describe("DependencyPicker", () => {
  test("hides the task itself, its dependencies, and any task that would make a loop", () => {
    // a → b → c; d is free. Adding a dependency to b: a would loop (a depends on b), c is already one.
    const tasks = [
      task("a", { dependencies: ["b"] }),
      task("b", { dependencies: ["c"] }),
      task("c"),
      task("d"),
    ];
    const byId = new Map(tasks.map((t) => [t.id, t]));
    render(<DependencyPicker task={tasks[1]} candidates={tasks} lookup={(id) => byId.get(id)} onAdd={() => {}} />);
    const values = [...screen.getByLabelText<HTMLSelectElement>("Add dependency").options].map((o) => o.value);
    expect(values).toEqual(["", "d"]);
  });

  test("adds the chosen task", async () => {
    const onAdd = vi.fn();
    const tasks = [task("a"), task("b")];
    const byId = new Map(tasks.map((t) => [t.id, t]));
    render(<DependencyPicker task={tasks[0]} candidates={tasks} lookup={(id) => byId.get(id)} onAdd={onAdd} />);
    await userEvent.selectOptions(screen.getByLabelText("Add dependency"), "b");
    expect(onAdd).toHaveBeenCalledWith("b");
  });
});

test("ConnectionBadge says Live, Reconnecting… or Offline", () => {
  const { rerender } = render(<ConnectionBadge status="live" />);
  expect(screen.getByRole("status").textContent).toBe("Live");
  rerender(<ConnectionBadge status="reconnecting" />);
  expect(screen.getByRole("status").textContent).toBe("Reconnecting…");
  rerender(<ConnectionBadge status="offline" />);
  expect(screen.getByRole("status").textContent).toBe("Offline");
});

describe("NamePrompt", () => {
  test("creates a user with the trimmed name", async () => {
    const onCreate = vi.fn().mockResolvedValue(undefined);
    render(<NamePrompt onCreate={onCreate} />);
    await userEvent.type(screen.getByLabelText("Display name"), "  Ana  ");
    await userEvent.click(screen.getByRole("button", { name: "Continue" }));
    expect(onCreate).toHaveBeenCalledWith("Ana");
  });

  test("won't submit a blank name, and shows why a create failed", async () => {
    const onCreate = vi.fn().mockRejectedValue(new Error("The API is unreachable"));
    render(<NamePrompt onCreate={onCreate} />);
    const button = screen.getByRole<HTMLButtonElement>("button", { name: "Continue" });
    expect(button.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Display name"), { target: { value: "Raj" } });
    await userEvent.click(button);
    expect(await screen.findByText("The API is unreachable")).toBeTruthy();
  });
});
