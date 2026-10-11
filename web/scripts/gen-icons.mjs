// Rasterises public/icons/icon.svg into the PWA icon set using the Playwright
// Chromium from tests/e2e (no npm dependency in web/). The output PNGs are
// committed: they are build inputs, so CI and Docker builds never need Chromium.
//   (cd tests/e2e && npm ci && npx playwright install chromium)
//   npm --prefix web run gen:icons
// public/favicon.svg is a copy of public/icons/icon.svg (keep them in sync).
import { createRequire } from "node:module";
import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pub = resolve(here, "../public");
const e2e = resolve(here, "../../tests/e2e");
const { chromium } = createRequire(join(e2e, "package.json"))("@playwright/test");

const anySvg = readFileSync(join(pub, "icons/icon.svg"), "utf8");

// Full-bleed brand background with the data rows scaled about the centre.
// The rows span 18x16 units (half-diagonal ~12.04 of 32), so scale 1 fits the
// maskable safe zone (circle of radius 40% = 12.8). Apple icons are masked by
// iOS with a small corner radius only, so the artwork can be larger.
function fullBleed(scale) {
  const rows = anySvg.match(/<rect x="[^>]*\/>/g).join("\n    ");
  const defs = anySvg.match(/<defs>[\s\S]*?<\/defs>/)[0];
  return `<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512" viewBox="0 0 32 32">
  ${defs}
  <rect width="32" height="32" fill="url(#g)"/>
  <g transform="translate(16 16) scale(${scale}) translate(-16 -16)">
    ${rows}
  </g>
</svg>`;
}

const jobs = [
  ["icons/icon-192.png", anySvg, 192, true],
  ["icons/icon-512.png", anySvg, 512, true],
  ["icons/maskable-192.png", fullBleed(1), 192, false],
  ["icons/maskable-512.png", fullBleed(1), 512, false],
  ["icons/apple-touch-icon.png", fullBleed(1.3), 180, false],
  ["icons/favicon-32.png", anySvg, 32, true],
];

mkdirSync(join(pub, "icons"), { recursive: true });
copyFileSync(join(pub, "icons/icon.svg"), join(pub, "favicon.svg"));
const browser = await chromium.launch();
try {
  for (const [file, svg, size, transparent] of jobs) {
    const page = await browser.newPage({ viewport: { width: size, height: size } });
    await page.setContent(
      `<style>html,body{margin:0;background:${transparent ? "transparent" : "#4f46e5"}}svg{display:block;width:${size}px;height:${size}px}</style>${svg}`,
    );
    const png = await page.screenshot({ omitBackground: transparent, clip: { x: 0, y: 0, width: size, height: size } });
    writeFileSync(join(pub, file), png);
    await page.close();
    console.log(`wrote public/${file} (${size}x${size})`);
  }
} finally {
  await browser.close();
}
