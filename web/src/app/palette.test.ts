// The palette in globals.css. Every text color has to stay readable on each
// background it's used on, in both themes: WCAG AA, 4.5:1 for text and 3:1
// for the status dots.
import { globSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, test } from "vitest";

const css = readFileSync(new URL("./globals.css", import.meta.url), "utf8");

/** The `--name: #rrggbb;` declarations of one rule in globals.css. */
function tokens(selector: string): Record<string, string> {
  const start = css.indexOf(`${selector} {`);
  if (start < 0) throw new Error(`globals.css has no ${selector} rule`);
  const body = css.slice(start, css.indexOf("}", start));
  return Object.fromEntries([...body.matchAll(/--([\w-]+):\s*(#[0-9a-f]{6})\s*;/gi)].map((m) => [m[1], m[2]]));
}

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = Number.parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: string, b: string): number {
  const [lighter, darker] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (lighter + 0.05) / (darker + 0.05);
}

const light = tokens(":root");
const dark = { ...light, ...tokens('[data-theme="dark"]') };

// Every text color on every background the components put it on.
const TEXT: [string, string][] = [
  ["ink", "canvas"], ["ink", "surface"], ["ink", "subtle"],
  ["muted", "canvas"], ["muted", "surface"], ["muted", "subtle"],
  ["on-accent", "accent"], ["on-accent", "accent-hover"],
  ["warning-ink", "warning-soft"], ["warning-ink", "canvas"], ["warning-ink", "surface"],
  ["danger-ink", "danger-soft"], ["danger-ink", "canvas"], ["danger-ink", "surface"], ["danger-ink", "subtle"],
  ["canvas", "ink"], // toasts
];
// Status and connection dots and the focus ring: on the page, on panels and on a
// hovered row. The gold is too light for thin marks on cream, so they use accent-strong.
const DOTS = ["success", "info", "warning", "danger", "accent-strong", "muted"];
const PAIRS: [string, string, number][] = [
  ...TEXT.map(([text, background]): [string, string, number] => [text, background, 4.5]),
  ...DOTS.flatMap((dot) => ["canvas", "surface", "subtle"].map((background): [string, string, number] => [dot, background, 3])),
];

test("the palette is built on the brand's gold and cream", () => {
  expect(light.accent).toBe("#f2c46a"); // buttons, in both themes
  expect(dark.accent).toBe("#f2c46a");
  expect(light.canvas).toBe("#fcf0da"); // the light page
  expect(dark.ink).toBe("#fcf0da"); // text in the dark theme
});

describe.each([
  ["light", light],
  ["dark", dark],
])("the %s theme", (_, palette) => {
  test.each(PAIRS)("%s on %s is at least %s:1", (text, background, minimum) => {
    expect(palette[text], `--${text} is missing`).toBeDefined();
    expect(palette[background], `--${background} is missing`).toBeDefined();
    expect(contrast(palette[text], palette[background])).toBeGreaterThanOrEqual(minimum);
  });
});

// Tailwind's built-in colors (zinc-500, red-600 …), white and black, and dark:
// variants. Components use the palette's names instead, so both themes come
// from globals.css alone.
const RAW_COLOR =
  /(?<![\w-])(?:dark:[\w-]+|(?:bg|text|border|divide|ring|outline|fill|stroke|from|via|to|placeholder|decoration|caret|shadow)-(?:white|black|(?:slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d{2,3}))/g;

test("components take every color from the palette", () => {
  const web = fileURLToPath(new URL("../..", import.meta.url));
  const files = globSync(["src/app/**/*.tsx", "src/components/**/*.tsx"], { cwd: web }).filter((f) => !f.endsWith(".test.tsx"));
  const raw = files.flatMap((file) =>
    [...readFileSync(join(web, file), "utf8").matchAll(RAW_COLOR)].map((m) => `${file}: ${m[0]}`),
  );
  expect(raw).toEqual([]);
});

// Thin marks have to stand out at 3:1, so status dots, focus borders and the
// focus ring may only use the colors checked as DOTS above.
test("status dots and focus marks only use colors that stand out", () => {
  const web = fileURLToPath(new URL("../..", import.meta.url));
  const read = (file: string) => readFileSync(join(web, file), "utf8");
  const block = (file: string, name: string) => read(file).match(new RegExp(`${name}[^{]*\\{([^}]*)\\}`))?.[1] ?? "";
  const tokensIn = (classes: string) => [...classes.matchAll(/(?:bg|border)-([a-z-]+)/g)].map((m) => m[1]);
  const marks = {
    "status dots": tokensIn(block("src/components/task-row.tsx", "STATUS_DOT")),
    "connection dots": tokensIn(block("src/components/connection-badge.tsx", "DOT")),
    "focus borders": globSync("src/components/**/*.tsx", { cwd: web }).flatMap((f) =>
      [...read(f).matchAll(/focus:border-([a-z-]+)/g)].map((m) => m[1]),
    ),
    "focus ring": [...read("src/app/globals.css").matchAll(/:focus-visible\s*\{[^}]*var\(--([a-z-]+)\)/g)].map((m) => m[1]),
  };
  for (const [what, used] of Object.entries(marks)) {
    expect(used.length, `no ${what} found`).toBeGreaterThan(0);
    expect(used.filter((token) => !DOTS.includes(token)), what).toEqual([]);
  }
});
