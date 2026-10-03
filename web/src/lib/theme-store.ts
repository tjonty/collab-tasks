// The theme as React state. The choice is saved in localStorage, so every
// tab of the browser shares it (the identity, by contrast, is per tab). The
// store sets data-theme on <html> and keeps it current: when the system
// theme changes while System is chosen, and when another tab picks a theme.
import { useLayoutEffect, useSyncExternalStore } from "react";

import { parseChoice, resolveTheme, SYSTEM_DARK_QUERY, THEME_KEY, type ThemeChoice } from "./theme";

/** The system's dark-mode media query: a MediaQueryList in the browser. */
export interface SystemTheme extends EventTarget {
  readonly matches: boolean;
}

/** What the store touches. Tests pass fakes (theme-testing.ts); the page uses the browser's. */
export interface ThemeEnv {
  /** null when the browser blocks storage. */
  storage: Pick<Storage, "getItem" | "setItem"> | null;
  /** null where matchMedia doesn't exist. */
  systemDark: SystemTheme | null;
  /** Where "storage" events from other tabs arrive: window. */
  events: EventTarget;
  /** The element that gets data-theme: <html>. */
  root: HTMLElement;
}

export interface ThemeStore {
  get(): ThemeChoice;
  set(choice: ThemeChoice): void;
  subscribe(listener: () => void): () => void;
  /** Applies the theme now and keeps it current until the returned function is called. */
  start(): () => void;
}

export function createThemeStore(env: ThemeEnv): ThemeStore {
  const listeners = new Set<() => void>();
  let choice = read();

  function read(): ThemeChoice {
    try {
      return parseChoice(env.storage?.getItem(THEME_KEY) ?? null);
    } catch {
      return "system"; // storage blocked
    }
  }

  function changed(): void {
    env.root.setAttribute("data-theme", resolveTheme(choice, env.systemDark?.matches ?? false));
    for (const listener of listeners) listener();
  }

  const onStorage = (e: Event) => {
    const key = (e as StorageEvent).key;
    if (key !== THEME_KEY && key !== null) return; // null: storage was cleared
    choice = read();
    changed();
  };

  return {
    get: () => choice,
    set(next) {
      choice = next;
      try {
        env.storage?.setItem(THEME_KEY, next);
      } catch {
        // Storage blocked: the choice lasts until the page reloads.
      }
      changed();
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    start() {
      choice = read();
      changed();
      // A fixed choice ignores the system (resolveTheme), so re-applying on
      // every system change is enough: only System moves.
      env.systemDark?.addEventListener("change", changed);
      env.events.addEventListener("storage", onStorage);
      return () => {
        env.systemDark?.removeEventListener("change", changed);
        env.events.removeEventListener("storage", onStorage);
      };
    },
  };
}

let shared: ThemeStore | undefined;

/** The page's store, made on first use. Browser only. */
export function browserThemeStore(): ThemeStore {
  shared ??= createThemeStore({
    storage: localStore(),
    systemDark: typeof window.matchMedia === "function" ? window.matchMedia(SYSTEM_DARK_QUERY) : null,
    events: window,
    root: document.documentElement,
  });
  return shared;
}

function localStore(): Storage | null {
  try {
    return window.localStorage;
  } catch {
    return null; // storage blocked
  }
}

/** Keeps <html data-theme> right. Mount once, at the root (app/providers.tsx). */
export function useApplyTheme(store?: ThemeStore): void {
  // A layout effect runs before paint. Besides following the system and
  // other tabs, it restores a data-theme that React cleared by remounting
  // <html>, which Strict Mode can do in development (see the Next.js guide
  // "Preventing flash before hydration").
  useLayoutEffect(() => (store ?? browserThemeStore()).start(), [store]);
}

const system = (): ThemeChoice => "system";
const noSubscribe = () => () => {};

/** The person's choice, and a way to change it. "system" on the server. */
export function useThemeChoice(store?: ThemeStore): [ThemeChoice, (choice: ThemeChoice) => void] {
  const s = store ?? (typeof window === "undefined" ? undefined : browserThemeStore());
  const choice = useSyncExternalStore(s?.subscribe ?? noSubscribe, s?.get ?? system, system);
  return [choice, (next) => s?.set(next)];
}
