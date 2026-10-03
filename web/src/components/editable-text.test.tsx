// @vitest-environment jsdom
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, test, vi } from "vitest";

import { EditableText } from "./editable-text";

afterEach(cleanup);

/** Renders the field. remoteSave delivers someone else's save, as the sync store does. */
function setup(value: string, { multiline = true } = {}) {
  const onCommit = vi.fn();
  const field = (v: string) => <EditableText label="Description" value={v} onCommit={onCommit} multiline={multiline} />;
  const { rerender } = render(field(value));
  return {
    box: screen.getByLabelText<HTMLTextAreaElement>("Description"),
    onCommit,
    remoteSave: (v: string) => rerender(field(v)),
  };
}

const leave = (box: HTMLElement) => act(() => box.blur());

describe("EditableText", () => {
  test("saves your edit when you leave the field", async () => {
    const { box, onCommit } = setup("Design API");
    await userEvent.type(box, " v2");
    leave(box);
    expect(onCommit.mock.calls).toEqual([["Design API v2"]]);
  });

  test("shows the live value again after you leave", async () => {
    const { box, remoteSave } = setup("Design API");
    await userEvent.type(box, " v2");
    leave(box);
    remoteSave("Their text");
    expect(box.value).toBe("Their text");
  });

  test("Enter saves a one-line field", async () => {
    const { box, onCommit } = setup("Design API", { multiline: false });
    await userEvent.type(box, " v2{Enter}");
    expect(onCommit.mock.calls).toEqual([["Design API v2"]]);
  });

  test("keeps what you are typing when someone else saves, and saves yours when you leave", async () => {
    const { box, onCommit, remoteSave } = setup("Design API");
    await userEvent.type(box, " v2");
    remoteSave("Their text");
    expect(box.value).toBe("Design API v2");
    leave(box);
    expect(onCommit.mock.calls).toEqual([["Design API v2"]]);
  });

  // The bug in the recording: one window took focus, the other saved, and
  // leaving the first window wrote its old text back over the new one.
  test("saves nothing when you leave without typing, even if someone else saved meanwhile", async () => {
    const { box, onCommit, remoteSave } = setup("");
    await userEvent.click(box);
    remoteSave("DESign API\n_ POST\n_ GET");
    leave(box);
    expect(onCommit).not.toHaveBeenCalled();
  });

  test("shows someone else's save while it has focus, until you type", async () => {
    const { box, remoteSave } = setup("");
    await userEvent.click(box);
    remoteSave("DESign API\n_ POST\n_ GET");
    expect(box.value).toBe("DESign API\n_ POST\n_ GET");
  });

  test("saves nothing when you undo your typing, even if someone else saved meanwhile", async () => {
    const { box, onCommit, remoteSave } = setup("Design API");
    await userEvent.type(box, "!");
    remoteSave("Their text");
    await userEvent.type(box, "{Backspace}");
    leave(box);
    expect(onCommit).not.toHaveBeenCalled();
  });

  // Switching windows in Safari fires "blur" on the field but not "focusout",
  // which is what React's onBlur listens for. The edit then sat unsaved.
  test("saves your edit when the window loses focus and the browser sends only a blur event", async () => {
    const { box, onCommit } = setup("DESign APO");
    await userEvent.type(box, " v2");
    act(() => box.dispatchEvent(new FocusEvent("blur")));
    expect(onCommit.mock.calls).toEqual([["DESign APO v2"]]);
  });
});
