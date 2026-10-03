// @vitest-environment jsdom
import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, test } from "vitest";

import { createThemeStore, useApplyTheme } from "./theme-store";
import { fakeBrowser } from "./theme-testing";

afterEach(cleanup);

describe("the theme store", () => {
  test("starting applies the saved choice, or the system's theme when there's none", () => {
    const saved = fakeBrowser({ saved: "light", systemDark: true });
    createThemeStore(saved.env).start();
    expect(saved.theme()).toBe("light");

    const none = fakeBrowser({ systemDark: true });
    createThemeStore(none.env).start();
    expect(none.theme()).toBe("dark");
  });

  test("choosing a theme applies it, saves it and tells subscribers", () => {
    const browser = fakeBrowser();
    const store = createThemeStore(browser.env);
    store.start();
    let heard = 0;
    store.subscribe(() => heard++);

    store.set("dark");
    expect(store.get()).toBe("dark");
    expect(browser.theme()).toBe("dark");
    expect(browser.saved()).toBe("dark");
    expect(heard).toBe(1);
  });

  test("System follows the system as it changes; Light and Dark stay put", () => {
    const browser = fakeBrowser({ systemDark: false });
    const store = createThemeStore(browser.env);
    store.start();

    browser.systemTurns(true);
    expect(browser.theme()).toBe("dark");

    store.set("light");
    browser.systemTurns(false);
    browser.systemTurns(true);
    expect(browser.theme()).toBe("light");
  });

  test("a choice made in another tab applies here too", () => {
    const browser = fakeBrowser();
    const store = createThemeStore(browser.env);
    store.start();
    let heard = 0;
    store.subscribe(() => heard++);

    browser.otherTabSaves("dark");
    expect(store.get()).toBe("dark");
    expect(browser.theme()).toBe("dark");
    expect(heard).toBe(1);
  });

  test("with storage blocked it follows the system, and a choice lasts while the page is open", () => {
    const browser = fakeBrowser({ systemDark: true });
    browser.blockStorage();
    const store = createThemeStore(browser.env);
    store.start();
    expect(browser.theme()).toBe("dark");

    store.set("light");
    expect(store.get()).toBe("light");
    expect(browser.theme()).toBe("light");
  });

  test("stopping stops following the system and other tabs", () => {
    const browser = fakeBrowser();
    const stop = createThemeStore(browser.env).start();
    stop();

    browser.systemTurns(true);
    browser.otherTabSaves("dark");
    expect(browser.theme()).toBe("light");
  });
});

test("useApplyTheme puts data-theme back after React clears it on a remount", () => {
  const browser = fakeBrowser({ saved: "dark" });
  const store = createThemeStore(browser.env);
  function Root() {
    useApplyTheme(store);
    return null;
  }

  const first = render(<Root />);
  expect(browser.theme()).toBe("dark");
  first.unmount();
  browser.env.root.removeAttribute("data-theme"); // what remounting <html> does in development
  render(<Root />);
  expect(browser.theme()).toBe("dark");
});
