// Two people on one project, in two browsers (spec section 6). This is also
// the script for the demo video: `npm run test:e2e:demo`.
import { expect, test, type Browser, type BrowserContext, type Page } from "@playwright/test";

/** A person in their own browser context: their own tab, so their own identity. */
interface Person {
  context: BrowserContext;
  page: Page;
}

async function arrive(browser: Browser, name: string, path: string): Promise<Person> {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.goto(path);
  await page.getByLabel("Display name").fill(name);
  await page.getByRole("button", { name: "Continue" }).click();
  return { context, page };
}

const row = (page: Page, title: string) =>
  page.getByTestId("task-list").getByRole("button", { name: new RegExp(title) });

const drawer = (page: Page) => page.getByRole("complementary", { name: "Task" });

async function addTask(page: Page, title: string) {
  await page.getByLabel("New task title").fill(title);
  await page.getByRole("button", { name: "Add task" }).click();
}

async function openTask(page: Page, title: string) {
  await row(page, title).click();
  await expect(drawer(page).getByLabel("Title")).toHaveValue(title);
}

test("two people work on one project in real time", async ({ browser }) => {
  const ana = await arrive(browser, "Ana", "/");
  await ana.page.getByLabel("New project name").fill(`Launch ${new Date().toISOString().slice(0, 19)}`);
  await ana.page.getByRole("button", { name: "New project" }).click();
  await ana.page.waitForURL(/\/projects\//);
  const raj = await arrive(browser, "Raj", ana.page.url());
  await expect(ana.page.getByRole("status")).toHaveText("Live");
  await expect(raj.page.getByRole("status")).toHaveText("Live");

  await test.step("Ana creates a task, and Raj sees it in under 2 s", async () => {
    await addTask(ana.page, "Design API");
    await expect(row(raj.page, "Design API")).toBeVisible({ timeout: 2_000 });
  });

  await test.step("Raj comments, and Ana sees it in under 2 s", async () => {
    await openTask(ana.page, "Design API");
    await openTask(raj.page, "Design API");
    await raj.page.getByLabel("New comment").fill("I'll take this one.");
    await raj.page.getByRole("button", { name: "Comment" }).click();
    await expect(drawer(ana.page).getByText("I'll take this one.")).toBeVisible({ timeout: 2_000 });
  });

  await test.step("Done is disabled for Ana while a dependency is still open", async () => {
    await addTask(ana.page, "Build");
    await openTask(ana.page, "Build");
    await ana.page.getByLabel("Add dependency").selectOption({ label: "Design API" });
    await ana.page.getByLabel("Status", { exact: true }).selectOption("in_progress");
    await expect(ana.page.getByLabel("Status", { exact: true }).locator('option[value="done"]')).toBeDisabled();
    await expect(drawer(ana.page).getByText("Waiting on: Design API")).toBeVisible();
    await expect(row(raj.page, "Build")).toContainText("Blocked");
  });

  await test.step("Raj goes offline, Ana edits 3 tasks, and Raj's tab catches up", async () => {
    await addTask(ana.page, "Write tests");
    await expect(row(raj.page, "Write tests")).toBeVisible();
    await raj.context.setOffline(true);
    await expect(raj.page.getByRole("status")).toHaveText(/Offline|Reconnecting…/);

    await openTask(ana.page, "Design API");
    await drawer(ana.page).getByLabel("Title").fill("Design the API");
    await drawer(ana.page).getByLabel("Title").press("Enter");
    await openTask(ana.page, "Build");
    await ana.page.getByLabel("Priority", { exact: true }).selectOption("urgent");
    await openTask(ana.page, "Write tests");
    await ana.page.getByLabel("Add tag").fill("backend");
    await ana.page.getByLabel("Add tag").press("Enter");
    await expect(row(ana.page, "Write tests")).toContainText("backend");
    // Raj's tab, offline, still shows the old state.
    await expect(row(raj.page, "Design API")).toBeVisible();

    await raj.context.setOffline(false);
    await expect(raj.page.getByRole("status")).toHaveText("Live", { timeout: 10_000 });
    await expect(row(raj.page, "Design the API")).toBeVisible();
    await expect(row(raj.page, "Build")).toContainText("Urgent");
    await expect(row(raj.page, "Write tests")).toContainText("backend");
  });
});
