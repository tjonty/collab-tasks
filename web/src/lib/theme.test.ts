// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from "vitest";

import { parseChoice, resolveTheme, THEME_KEY, THEME_SCRIPT } from "./theme";

test("parseChoice keeps light and dark; anything else is System", () => {
  expect(parseChoice("light")).toBe("light");
  expect(parseChoice("dark")).toBe("dark");
  for (const saved of [null, "", "system", "blue", "DARK", '"dark"']) {
    expect(parseChoice(saved)).toBe("system");
  }
});

test("resolveTheme: System follows the system; Light and Dark don't", () => {
  expect(resolveTheme("system", true)).toBe("dark");
  expect(resolveTheme("system", false)).toBe("light");
  expect(resolveTheme("light", true)).toBe("light");
  expect(resolveTheme("dark", false)).toBe("dark");
});

describe("THEME_SCRIPT", () => {
  afterEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute("data-theme");
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  /** Runs the script as the browser does, with the system set to light or dark. */
  function run(systemDark: boolean): string | null {
    vi.stubGlobal("matchMedia", (query: string) => ({
      media: query,
      matches: systemDark && query === "(prefers-color-scheme: dark)",
    }));
    new Function(THEME_SCRIPT)();
    return document.documentElement.getAttribute("data-theme");
  }

  test.each([null, "system", "light", "dark", "blue"])("with %s saved, it agrees with parseChoice and resolveTheme", (saved) => {
    for (const systemDark of [false, true]) {
      localStorage.clear();
      if (saved !== null) localStorage.setItem(THEME_KEY, saved);
      expect(run(systemDark), `system ${systemDark ? "dark" : "light"}`).toBe(resolveTheme(parseChoice(saved), systemDark));
    }
  });

  test("with storage blocked, it follows the system", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError");
    });
    expect(run(true)).toBe("dark");
  });

  test("without matchMedia, it falls back to light", () => {
    vi.stubGlobal("matchMedia", undefined);
    new Function(THEME_SCRIPT)();
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });
});
