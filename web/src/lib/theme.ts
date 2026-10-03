// The color theme. A person picks System, Light or Dark; the choice is saved
// for the whole browser, and <html data-theme> holds the theme it resolves
// to. THEME_SCRIPT applies it before the first paint (app/layout.tsx), and
// theme-store.ts keeps it current after that.

export type ThemeChoice = "system" | "light" | "dark";
export type Theme = "light" | "dark";

/** In the order the header's switch shows them. */
export const THEME_CHOICES: readonly ThemeChoice[] = ["system", "light", "dark"];

/** Where the choice is saved, in localStorage. */
export const THEME_KEY = "collab-tasks:theme";

export const SYSTEM_DARK_QUERY = "(prefers-color-scheme: dark)";

/** Reads a saved choice. Anything missing or unknown means System. */
export function parseChoice(saved: string | null): ThemeChoice {
  return saved === "light" || saved === "dark" ? saved : "system";
}

export function resolveTheme(choice: ThemeChoice, systemDark: boolean): Theme {
  if (choice !== "system") return choice;
  return systemDark ? "dark" : "light";
}

/**
 * Runs in <head> while the HTML is parsed, before anything is painted, so no
 * page ever flashes the wrong theme. It can't import anything, so it repeats
 * parseChoice and resolveTheme in plain JavaScript; a test checks that they
 * agree. Blocked storage counts as System, and no matchMedia as light.
 */
export const THEME_SCRIPT =
  `(function(){var c=null;try{c=localStorage.getItem(${JSON.stringify(THEME_KEY)})}catch(e){}` +
  `if(c!=="light"&&c!=="dark"){try{c=matchMedia(${JSON.stringify(SYSTEM_DARK_QUERY)}).matches?"dark":"light"}catch(e){c="light"}}` +
  `document.documentElement.setAttribute("data-theme",c)})()`;
