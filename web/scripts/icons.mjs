// Makes favicon.ico and apple-icon.png from src/app/icon.svg, so the icon has
// one drawing. Run it after changing the drawing: npm run icons
//
// sharp comes with Next.js, which uses it to optimize images.
import { readFileSync, writeFileSync } from "node:fs";

import sharp from "sharp";

const app = new URL("../src/app/", import.meta.url);
const svg = readFileSync(new URL("icon.svg", app));
// The tile's color: the iPhone icon has no transparent corners (iOS would turn
// them black and rounds the corners itself), so the tile fills the square.
const TILE = "#f2c46a";

// The drawing is 32 units square. Rendering it at the target size, rather than
// scaling a bigger render down, keeps the check sharp at 16 px.
const render = (size) => sharp(svg, { density: (72 * size) / 32 }).resize(size, size);

// favicon.ico: a PNG per size, for browsers that ask for /favicon.ico.
const sizes = [16, 32, 48];
const images = await Promise.all(sizes.map((size) => render(size).png().toBuffer()));
const directory = Buffer.alloc(6 + 16 * sizes.length);
directory.writeUInt16LE(1, 2); // type 1: an icon
directory.writeUInt16LE(sizes.length, 4);
let offset = directory.length;
sizes.forEach((size, i) => {
  const entry = 6 + 16 * i;
  directory[entry] = size; // width
  directory[entry + 1] = size; // height
  directory.writeUInt16LE(1, entry + 4); // color planes
  directory.writeUInt16LE(32, entry + 6); // bits per pixel
  directory.writeUInt32LE(images[i].length, entry + 8);
  directory.writeUInt32LE(offset, entry + 12);
  offset += images[i].length;
});
writeFileSync(new URL("favicon.ico", app), Buffer.concat([directory, ...images]));

writeFileSync(new URL("apple-icon.png", app), await render(180).flatten({ background: TILE }).png().toBuffer());

console.log("Wrote src/app/favicon.ico (16, 32 and 48 px) and src/app/apple-icon.png (180 px).");
