// The color theme, against the running stack: System, Light or Dark,
// applied before the first paint.
import { expect, test, type Page } from "@playwright/test";

const THEME_KEY = "collab-tasks:theme";

const html = (page: Page) => page.locator("html");

/** How light the page background is: 0 is black, 1 is white. */
async function lightness(page: Page): Promise<number> {
  const color = await page.evaluate(() => getComputedStyle(document.body).backgroundColor);
  const [r, g, b] = (color.match(/\d+/g) ?? []).map(Number);
  return (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255;
}

test.describe("on a dark system", () => {
  test.use({ colorScheme: "dark" });

  test("the page starts dark, native controls included", async ({ page }) => {
    await page.goto("/");
    await expect(html(page)).toHaveAttribute("data-theme", "dark");
    await expect(html(page)).toHaveCSS("color-scheme", "dark");
    expect(await lightness(page)).toBeLessThan(0.2);
  });
});

test.describe("on a light system", () => {
  test.use({ colorScheme: "light" });

  test("the page starts light", async ({ page }) => {
    await page.goto("/");
    await expect(html(page)).toHaveAttribute("data-theme", "light");
    expect(await lightness(page)).toBeGreaterThan(0.8);
  });

  test("a saved choice applies before any of the app's JavaScript runs", async ({ page }) => {
    await page.addInitScript((key) => localStorage.setItem(key, "dark"), THEME_KEY);
    // With its scripts blocked, React never starts: only the inline script in
    // <head> can set the theme. CSS shares the scripts' URL path, so block by
    // resource type, not by URL.
    await page.route("**/*", (route) => (route.request().resourceType() === "script" ? route.abort() : route.continue()));
    await page.goto("/");
    await expect(html(page)).toHaveAttribute("data-theme", "dark");
    expect(await lightness(page)).toBeLessThan(0.2);
  });

  test("a chosen theme survives a reload, and other tabs follow it", async ({ page, context }) => {
    await page.goto("/");
    await page.getByLabel("Display name").fill("Ana");
    await page.getByRole("button", { name: "Continue" }).click();

    await page.getByRole("button", { name: "Dark", exact: true }).click();
    await expect(html(page)).toHaveAttribute("data-theme", "dark");
    await page.reload();
    await expect(html(page)).toHaveAttribute("data-theme", "dark");
    await expect(page.getByRole("button", { name: "Dark", exact: true, pressed: true })).toBeVisible();

    // A second tab: its own person (sessionStorage), the same theme (localStorage).
    const other = await context.newPage();
    await other.goto("/");
    await expect(html(other)).toHaveAttribute("data-theme", "dark");
    await page.getByRole("button", { name: "Light", exact: true }).click();
    await expect(html(other)).toHaveAttribute("data-theme", "light");
  });
});
