// A browser for theme tests: storage, the system theme and other tabs, all
// in memory. Only tests import this file.
import { THEME_KEY } from "./theme";
import type { ThemeEnv } from "./theme-store";

export function fakeBrowser({ saved, systemDark = false }: { saved?: string; systemDark?: boolean } = {}) {
  const items = new Map<string, string>(saved === undefined ? [] : [[THEME_KEY, saved]]);
  const system = Object.assign(new EventTarget(), { matches: systemDark });
  const env: ThemeEnv = {
    storage: { getItem: (key) => items.get(key) ?? null, setItem: (key, value) => void items.set(key, value) },
    systemDark: system,
    events: new EventTarget(),
    root: document.createElement("html"),
  };
  const refuse = () => {
    throw new DOMException("blocked", "SecurityError");
  };
  return {
    env,
    /** What's saved under THEME_KEY. */
    saved: () => items.get(THEME_KEY),
    /** The root's data-theme. */
    theme: () => env.root.getAttribute("data-theme"),
    /** The system switches to dark or light, as macOS does at sunset. */
    systemTurns(dark: boolean) {
      system.matches = dark;
      system.dispatchEvent(new Event("change"));
    },
    /** Another tab of the same browser saves a choice. */
    otherTabSaves(value: string) {
      items.set(THEME_KEY, value);
      env.events.dispatchEvent(new StorageEvent("storage", { key: THEME_KEY, newValue: value }));
    },
    /** The browser refuses storage, as some private modes do. */
    blockStorage() {
      env.storage = { getItem: refuse, setItem: refuse };
    },
  };
}
