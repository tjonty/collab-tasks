"use client";

import { THEME_CHOICES, type ThemeChoice } from "@/lib/theme";
import { useThemeChoice, type ThemeStore } from "@/lib/theme-store";

const LABEL: Record<ThemeChoice, string> = { system: "System", light: "Light", dark: "Dark" };

/** System, Light or Dark, for the header. The choice is saved for the whole browser. */
export function ThemeToggle({ store }: { store?: ThemeStore }) {
  const [choice, choose] = useThemeChoice(store);
  return (
    <div role="group" aria-label="Theme" className="inline-flex rounded-md border border-line bg-subtle p-0.5 text-xs">
      {THEME_CHOICES.map((c) => (
        <button
          key={c}
          type="button"
          aria-pressed={choice === c}
          onClick={() => choose(c)}
          className={`rounded px-2 py-0.5 ${choice === c ? "bg-surface text-ink shadow-sm" : "text-muted hover:text-ink"}`}
        >
          {LABEL[c]}
        </button>
      ))}
    </div>
  );
}
