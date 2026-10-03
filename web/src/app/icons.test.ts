// The app's icon. icon.svg is the drawing; favicon.ico and apple-icon.png are
// made from it by `npm run icons`. Next.js links all three by their file names.
import { readFileSync } from "node:fs";

import { expect, test } from "vitest";

const read = (file: string) => readFileSync(new URL(`./${file}`, import.meta.url));

/** A token from the light palette in globals.css. */
function token(name: string): string {
  const root = read("globals.css").toString("utf8").match(/:root\s*\{([^}]*)\}/)?.[1] ?? "";
  const value = root.match(new RegExp(`--${name}:\\s*(#[0-9a-f]{6})\\s*;`, "i"))?.[1];
  if (!value) throw new Error(`globals.css has no --${name} in :root`);
  return value.toLowerCase();
}

/** A PNG's size and color type, from its header; undefined if it isn't a PNG. */
function png(data: Buffer) {
  if (!data.subarray(0, 8).equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))) return undefined;
  return { width: data.readUInt32BE(16), height: data.readUInt32BE(20), colorType: data[25] };
}

test("the icon is a gold tile with an ink check, in the palette's colors", () => {
  const svg = read("icon.svg").toString("utf8");
  expect(svg).toMatch(new RegExp(`<rect[^>]*fill="${token("accent")}"`, "i"));
  expect(svg).toMatch(new RegExp(`<polyline[^>]*stroke="${token("ink")}"`, "i"));
  const colors = [...svg.matchAll(/#[0-9a-f]{6}\b/gi)].map((m) => m[0].toLowerCase());
  expect(colors.filter((color) => color !== token("accent") && color !== token("ink"))).toEqual([]);
});

test("favicon.ico holds the icon as PNGs at 16, 32 and 48 px", () => {
  const ico = read("favicon.ico");
  expect(ico.readUInt16LE(2), "an .ico file is type 1").toBe(1);
  const images = Array.from({ length: ico.readUInt16LE(4) }, (_, i) => {
    const entry = 6 + 16 * i;
    const offset = ico.readUInt32LE(entry + 12);
    const image = png(ico.subarray(offset, offset + ico.readUInt32LE(entry + 8)));
    return { listed: ico[entry], width: image?.width, height: image?.height };
  });
  expect(images).toEqual([16, 32, 48].map((size) => ({ listed: size, width: size, height: size })));
});

// iOS rounds the corners itself and turns transparent pixels black.
test("apple-icon.png is 180 px and opaque", () => {
  const data = read("apple-icon.png");
  expect(png(data)).toEqual({ width: 180, height: 180, colorType: 2 }); // 2: RGB, no alpha channel
  expect(data.includes("tRNS"), "no transparent color either").toBe(false);
});
