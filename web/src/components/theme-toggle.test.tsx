// @vitest-environment jsdom
import { act, cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test } from "vitest";

import { createThemeStore } from "@/lib/theme-store";
import { fakeBrowser } from "@/lib/theme-testing";

import { ThemeToggle } from "./theme-toggle";

afterEach(cleanup);

test("shows the current choice, and choosing another applies and saves it", async () => {
  const browser = fakeBrowser({ systemDark: true });
  const store = createThemeStore(browser.env);
  store.start();
  render(<ThemeToggle store={store} />);
  expect(screen.getByRole("group", { name: "Theme" })).toBeTruthy();
  expect(screen.getByRole("button", { name: "System", pressed: true })).toBeTruthy();
  expect(browser.theme()).toBe("dark");

  await userEvent.click(screen.getByRole("button", { name: "Light" }));
  expect(screen.getByRole("button", { name: "Light", pressed: true })).toBeTruthy();
  expect(screen.getByRole("button", { name: "System", pressed: false })).toBeTruthy();
  expect(browser.theme()).toBe("light");
  expect(browser.saved()).toBe("light");
});

test("follows a choice made in another tab", () => {
  const browser = fakeBrowser();
  const store = createThemeStore(browser.env);
  store.start();
  render(<ThemeToggle store={store} />);

  act(() => browser.otherTabSaves("dark"));
  expect(screen.getByRole("button", { name: "Dark", pressed: true })).toBeTruthy();
});
