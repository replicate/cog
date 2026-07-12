import { expect, test, type Page } from "@playwright/test";
import { readFile } from "node:fs/promises";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Cog Playground" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "text" })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("status").filter({ hasText: "ready" })).toBeVisible({
    timeout: 30_000,
  });
});

test("keeps Form and CodeMirror JSON input synchronized", async ({ page }) => {
  const run = page.getByRole("button", { name: "Run" });
  await expect(run).toBeDisabled();
  await page.getByRole("textbox", { name: "text" }).fill("from form");
  await page.getByRole("tab", { name: "JSON" }).click();

  const editor = page.getByRole("textbox", { name: "Prediction input JSON" });
  await expect(editor).toContainText('"text": "from form"');
  await editor.click();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("button", { name: "Copy" })).toBeFocused();
  await replaceEditor(page, "Prediction input JSON", '{"text":"from json"}');
  await page.getByRole("button", { name: "Format" }).click();
  await expect(editor).toContainText('"text": "from json"');
  await expect(run).toBeEnabled();

  await replaceEditor(page, "Prediction input JSON", "{");
  await expect(run).toBeDisabled();
  await page.getByRole("tab", { name: "Form" }).click();
  await expect(page.getByRole("textbox", { name: "text" })).toHaveValue("from json");

  await page.getByRole("tab", { name: "JSON" }).click();
  await expect(page.getByRole("textbox", { name: "Prediction input JSON" })).toContainText(
    '"text": "from json"',
  );
  await expect(run).toBeEnabled();
});

test("runs a synchronous prediction and inspects its response", async ({ page }) => {
  await page.getByRole("textbox", { name: "text" }).fill("sync");
  await page.getByRole("tab", { name: "Sync", exact: true }).click();
  await page.getByRole("button", { name: "Run" }).click();

  await expectPredictionStatus(page, "succeeded");
  await expect(page.getByRole("log")).toContainText("hello sync");

  await page.getByRole("tab", { name: "Response", exact: true }).click();
  const response = page.getByRole("textbox", { name: "Prediction response" });
  await expect(response).toHaveAttribute("aria-readonly", "true");
  await expect(response).toHaveAttribute("contenteditable", "false");
  await expect(response).toContainText('"status": "succeeded"');
  await expect(page.locator(".response-editor .cm-content span").first()).toBeVisible();

  await page.getByRole("tab", { name: "Timeline" }).click();
  await expect(page.getByText(/POST \/predictions/)).toBeVisible();
  await page.getByRole("tab", { name: "Request" }).click();
  await expect(page.getByText("Total duration")).toBeVisible();
  await expect(page.getByText("200", { exact: true })).toBeVisible();
});

test("shows progressive streaming output before completion", async ({ page }) => {
  await page.getByRole("textbox", { name: "text" }).fill("slow");
  await page.getByRole("tab", { name: "Stream", exact: true }).click();
  await page.getByRole("button", { name: "Run" }).click();

  const output = page.getByRole("log");
  await expect(output).toHaveAttribute("aria-busy", "true");
  await expect(
    page.locator("#output-panel").getByRole("status").filter({ hasText: "processing" }),
  ).toBeVisible();
  await expect(output).toContainText("hello", { timeout: 30_000 });
  await expect(output).not.toContainText("slow");

  await expectPredictionStatus(page, "succeeded");
  await expect(output).toHaveAttribute("aria-busy", "false");
  await expect(output).toContainText("hello slow");
});

test("stops a running prediction", async ({ page }) => {
  await page.getByRole("textbox", { name: "text" }).fill("slow");
  await page.getByRole("textbox", { name: "Prediction ID" }).fill("playground-stop-id");
  await page.getByRole("tab", { name: "Stream", exact: true }).click();
  await page.getByRole("button", { name: "Run" }).click();
  await expect(page.getByRole("button", { name: "Stop" })).toBeEnabled();
  await expect(page.getByRole("log")).toContainText("hello", { timeout: 30_000 });

  const cancelResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/proxy/predictions/playground-stop-id/cancel",
  );
  await page.getByRole("button", { name: "Stop" }).click();
  expect((await cancelResponse).ok()).toBe(true);

  await expectPredictionStatus(page, "canceled");
  await expect(page.getByRole("button", { name: "Run" })).toBeEnabled();
  await expect(page.getByRole("status").filter({ hasText: "ready" })).toBeVisible({
    timeout: 30_000,
  });
});

test("configures and receives an asynchronous webhook prediction", async ({ page }) => {
  await page.getByRole("textbox", { name: "text" }).fill("webhook");
  await page.getByRole("tab", { name: "Async", exact: true }).click();
  await expect(page.getByText(/Webhook: .*\/webhook\/\.\.\./)).toBeVisible();
  await page.getByRole("checkbox", { name: "output" }).click();
  await page.getByRole("checkbox", { name: "logs" }).click();
  await expect(page.getByRole("checkbox", { name: "completed" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  await page.getByRole("button", { name: "Run" }).click();

  await expectPredictionStatus(page, "succeeded");
  await expect(page.getByRole("log")).toContainText("hello webhook");
  await page.getByRole("tab", { name: "Request" }).click();
  const requestBody = page.getByLabel("Request body");
  await expect(requestBody).toContainText("start");
  await expect(requestBody).toContainText("completed");
  await expect(requestBody).not.toContainText('"logs"');
});

test("reconnects and downloads the loaded schema", async ({ page }) => {
  const target = page.getByRole("textbox", { name: "Target" });
  const targetURL = await target.inputValue();
  await target.fill(" ");
  await expect(page.getByRole("button", { name: "Connect" })).toBeDisabled();
  await target.fill(`${targetURL}/`);
  const schemaResponse = page.waitForResponse(
    (response) => new URL(response.url()).pathname === "/proxy/openapi.json",
  );
  await target.press("Enter");
  expect((await schemaResponse).ok()).toBe(true);
  await expect(page.getByRole("status").filter({ hasText: "ready" })).toBeVisible();

  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Schema" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("openapi.json");
  const downloadPath = await download.path();
  if (!downloadPath) throw new Error("Schema download has no local path");
  const schema = JSON.parse(await readFile(downloadPath, "utf8")) as { openapi?: string };
  expect(schema.openapi).toMatch(/^3\./);
});

test("toggles the color theme", async ({ page }) => {
  const root = page.locator("html");
  const initialTheme = await root.getAttribute("data-mode");
  const nextTheme = initialTheme === "dark" ? "light" : "dark";
  const toggleLabel = initialTheme === "dark" ? "Light" : "Dark";
  await page.getByRole("button", { name: toggleLabel }).click();
  await expect(root).toHaveAttribute("data-mode", nextTheme);
});

test("uses a custom prediction ID and resets the playground", async ({ page }) => {
  await page.getByRole("textbox", { name: "text" }).fill("identified");
  await page.getByRole("textbox", { name: "Prediction ID" }).fill("playground-e2e-id");
  await page.getByRole("tab", { name: "Sync", exact: true }).click();
  const predictionRequest = page.waitForRequest(
    (request) =>
      request.method() === "PUT" &&
      new URL(request.url()).pathname === "/proxy/predictions/playground-e2e-id",
  );
  await page.getByRole("button", { name: "Run" }).click();
  await predictionRequest;
  await expectPredictionStatus(page, "succeeded");
  await expect(page.getByRole("log")).toContainText("hello identified");

  await page.getByRole("button", { name: "Reset" }).click();
  await expect(page.getByRole("textbox", { name: "text" })).toHaveValue("");
  await expect(page.getByRole("textbox", { name: "Prediction ID" })).toHaveValue("");
  await expect(page.getByText("Run a prediction to see its output.")).toBeVisible();
});

test("fits every primary control on a narrow viewport", async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });

  const widths = await page.evaluate(() => ({
    document: document.documentElement.scrollWidth,
    viewport: document.documentElement.clientWidth,
  }));
  expect(widths.document).toBeLessThanOrEqual(widths.viewport);
  await expect(page.getByRole("button", { name: "Run" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Form" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Output" })).toBeVisible();
});

async function replaceEditor(page: Page, label: string, value: string): Promise<void> {
  const editor = page.getByRole("textbox", { name: label });
  await editor.click();
  await page.keyboard.press("ControlOrMeta+A");
  await page.keyboard.insertText(value);
}

async function expectPredictionStatus(page: Page, status: string): Promise<void> {
  await expect(
    page.locator("#output-panel").getByRole("status").filter({ hasText: status }),
  ).toBeVisible({ timeout: 60_000 });
}
