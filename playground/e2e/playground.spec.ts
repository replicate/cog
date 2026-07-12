import { expect, test } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Cog Playground" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "text" })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("status").filter({ hasText: "ready" })).toBeVisible();
});

test("runs a synchronous prediction from the embedded JSON editor", async ({ page }) => {
  const run = page.getByRole("button", { name: "Run" });
  await expect(run).toBeDisabled();
  await page.getByRole("tab", { name: "Sync", exact: true }).click();
  await page.getByRole("tab", { name: "JSON" }).click();

  const editorHost = page.locator(".ace-input");
  const bounds = await editorHost.boundingBox();
  expect(bounds?.height).toBeGreaterThanOrEqual(280);

  const editor = page.getByRole("textbox", { name: "Prediction input JSON" });
  await editor.focus();
  await page.keyboard.press("ControlOrMeta+A");
  await page.keyboard.insertText('{"text":"playground"}');
  await expect(run).toBeEnabled();

  await page.getByRole("tab", { name: "Form" }).click();
  await expect(page.getByRole("textbox", { name: "text" })).toHaveValue("playground");
  await page.getByRole("tab", { name: "JSON" }).click();
  await run.click();

  await expect(
    page.locator("#output-panel").getByRole("status").filter({ hasText: "succeeded" }),
  ).toBeVisible({
    timeout: 60_000,
  });
  await expect(page.getByText("hello playground")).toBeVisible();
});

test("streams a prediction", async ({ page }) => {
  await page.getByRole("textbox", { name: "text" }).fill("stream");
  await page.getByRole("tab", { name: "Stream", exact: true }).click();
  await page.getByRole("button", { name: "Run" }).click();

  await expect(
    page.locator("#output-panel").getByRole("status").filter({ hasText: "succeeded" }),
  ).toBeVisible({
    timeout: 60_000,
  });
  await expect(page.getByText("hello stream")).toBeVisible();
});

test("receives an asynchronous prediction through webhooks", async ({ page }) => {
  await page.getByRole("textbox", { name: "text" }).fill("webhook");
  await page.getByRole("tab", { name: "Async", exact: true }).click();
  await expect(page.getByText(/Webhook: .*\/webhook\/\.\.\./)).toBeVisible();
  await page.getByRole("button", { name: "Run" }).click();

  await expect(
    page.locator("#output-panel").getByRole("status").filter({ hasText: "succeeded" }),
  ).toBeVisible({
    timeout: 60_000,
  });
  await expect(page.getByText("hello webhook")).toBeVisible();
});

test("fits the input controls on a narrow viewport", async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });

  const widths = await page.evaluate(() => ({
    document: document.documentElement.scrollWidth,
    viewport: document.documentElement.clientWidth,
  }));
  expect(widths.document).toBeLessThanOrEqual(widths.viewport);
});
