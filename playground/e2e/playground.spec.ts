import { expect, test, type Page } from "@playwright/test";
import { createServer } from "node:http";

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
  await expect(run).toBeEnabled();
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
  await expect(run).toBeEnabled();
  await page.getByRole("tab", { name: "Form" }).click();
  await expect(page.getByRole("textbox", { name: "text" })).toHaveValue("from json");

  await page.getByRole("tab", { name: "JSON" }).click();
  await expect(page.getByRole("textbox", { name: "Prediction input JSON" })).toContainText(
    '"text": "from json"',
  );
  await expect(run).toBeEnabled();
});

test("folds JSON objects in CodeMirror", async ({ page }) => {
  await page.getByRole("tab", { name: "JSON" }).click();
  await replaceEditor(
    page,
    "Prediction input JSON",
    '{\n  "text": "folded",\n  "metadata": {\n    "count": 1\n  }\n}',
  );

  const fold = page.locator('.json-input .cm-foldGutter [title="Fold line"]').first();
  await expect(fold).toBeVisible();
  await fold.click();
  await expect(page.locator(".json-input .cm-foldPlaceholder")).toBeVisible();
});

test("validates Form and JSON input against OpenAPI before running", async ({ page }) => {
  const run = page.getByRole("button", { name: "Run" });
  const text = page.getByRole("textbox", { name: "text" });
  await text.fill("123");
  await expect(run).toBeEnabled();
  await expect(text).not.toHaveAttribute("aria-invalid", "true");
  await expect(page.getByText("Does not match the required pattern.")).toHaveCount(0);

  await run.click();
  await expect(text).toHaveAttribute("aria-invalid", "true");
  await expect(page.getByText("Does not match the required pattern.").first()).toBeVisible();

  await text.fill("valid input");
  await expect(run).toBeEnabled();
  await expect(text).not.toHaveAttribute("aria-invalid", "true");

  await page.getByRole("tab", { name: "JSON" }).click();
  const editor = page.getByRole("textbox", { name: "Prediction input JSON" });
  await replaceEditor(page, "Prediction input JSON", '{"text":42}');
  await expect(run).toBeEnabled();
  await expect(editor).not.toHaveAttribute("aria-invalid", "true");

  await run.click();
  await expect(editor).toHaveAttribute("aria-invalid", "true");
  await expect(page.getByText("Input does not match the OpenAPI schema.")).toBeVisible();

  await replaceEditor(page, "Prediction input JSON", '{"text":"valid"}');
  await expect(run).toBeEnabled();
  await expect(editor).not.toHaveAttribute("aria-invalid", "true");
});

test("runs a synchronous prediction and inspects its response", async ({ page }) => {
  await page.getByRole("textbox", { name: "text" }).fill("sync");
  await page.getByRole("button", { name: "Sync", exact: true }).click();
  await page.getByRole("button", { name: "Run" }).click();

  await expectPredictionStatus(page, "succeeded");
  await expect(predictionOutput(page)).toContainText("hello sync");

  await page.getByRole("tab", { name: "Raw", exact: true }).click();
  const raw = page.getByRole("textbox", { name: "Raw prediction response" });
  await expect(raw).toHaveAttribute("aria-readonly", "true");
  await expect(raw).toHaveAttribute("contenteditable", "false");
  await expect(raw).toContainText('"status": "succeeded"');
  await expect(page.locator(".response-editor .cm-content span").first()).toBeVisible();

  await page.getByRole("tab", { name: "Timeline" }).click();
  await expect(page.getByText(/POST \/predictions/)).toBeVisible();
  await page.getByRole("tab", { name: "Request" }).click();
  await expect(page.getByText("Prediction time")).toBeVisible();
  await expect(page.getByText("200", { exact: true })).toBeVisible();
  await page.getByText("Response headers", { exact: true }).click();
  const responseHeaders = page.getByLabel("Response headers", { exact: true });
  await expect(responseHeaders).toContainText("content-type");
  await expect(responseHeaders).not.toContainText("content-security-policy");
  await expect(responseHeaders).not.toContainText("x-frame-options");
});

test("keeps model targets isolated across browser workspaces", async ({ context, page }) => {
  const firstModel = await startTestModel("first model");
  const secondModel = await startTestModel("second model");
  const secondPage = await context.newPage();
  const thirdPage = await context.newPage();

  try {
    await Promise.all([secondPage.goto("/"), thirdPage.goto("/")]);
    await Promise.all([
      connectTarget(page, firstModel.url),
      connectTarget(secondPage, secondModel.url),
      connectTarget(thirdPage, firstModel.url),
    ]);

    await page.getByRole("textbox", { name: "text" }).fill("one");
    await secondPage.getByRole("textbox", { name: "text" }).fill("two");
    await thirdPage.getByRole("textbox", { name: "text" }).fill("three");
    await Promise.all([
      page.getByRole("button", { name: "Run" }).click(),
      secondPage.getByRole("button", { name: "Run" }).click(),
      thirdPage.getByRole("button", { name: "Run" }).click(),
    ]);

    await expect(predictionOutput(page)).toContainText("first model: one");
    await expect(predictionOutput(secondPage)).toContainText("second model: two");
    await expect(predictionOutput(thirdPage)).toContainText("first model: three");
  } finally {
    await Promise.all([secondPage.close(), thirdPage.close()]);
    await Promise.all([firstModel.close(), secondModel.close()]);
  }
});

test("shows progressive streaming output before completion", async ({ page }) => {
  await page.getByRole("textbox", { name: "text" }).fill("slow");
  await page.getByRole("button", { name: "Stream", exact: true }).click();
  await page.getByRole("button", { name: "Run" }).click();

  const output = predictionOutput(page);
  await expect(output).toHaveAttribute("aria-busy", "true");
  await expectPredictionStatus(page, "processing");
  await expect(output).toContainText("hello", { timeout: 30_000 });
  await expect(output).not.toContainText("slow");

  await expectPredictionStatus(page, "succeeded");
  await expect(output).toHaveAttribute("aria-busy", "false");
  await expect(output).toContainText("hello slow");
});

test("stops a running prediction", async ({ page }) => {
  await page.getByRole("textbox", { name: "text" }).fill("slow");
  await page.getByRole("textbox", { name: "Prediction ID" }).fill("playground-stop-id");
  await page.getByRole("button", { name: "Stream", exact: true }).click();
  await page.getByRole("button", { name: "Run" }).click();
  await expect(page.getByRole("button", { name: "Stop" })).toBeEnabled();
  await expect(predictionOutput(page)).toContainText("hello", { timeout: 30_000 });

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
  await page.getByRole("button", { name: "Async", exact: true }).click();
  await expect(page.getByText(/Webhook: .*\/webhook\/\.\.\./)).toBeVisible();
  await page.getByRole("checkbox", { name: "output" }).click();
  await page.getByRole("checkbox", { name: "logs" }).click();
  await expect(page.getByRole("checkbox", { name: "completed" })).toHaveAttribute(
    "aria-disabled",
    "true",
  );
  await page.getByRole("button", { name: "Run" }).click();

  await expectPredictionStatus(page, "succeeded");
  await expect(predictionOutput(page)).toContainText("hello webhook");
  await page.getByRole("tab", { name: "Request" }).click();
  const requestBody = page.getByLabel("Request body", { exact: true });
  await expect(requestBody).toContainText("start");
  await expect(requestBody).toContainText("completed");
  await expect(requestBody).not.toContainText('"logs"');
});

test("reconnects and opens the loaded schema", async ({ page }) => {
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

  const popupPromise = page.waitForEvent("popup");
  await page.getByRole("button", { name: "Schema" }).click();
  const popup = await popupPromise;
  await popup.waitForLoadState();
  const schema = JSON.parse((await popup.locator("body").textContent()) ?? "") as {
    openapi?: string;
  };
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
  await page.getByRole("button", { name: "Sync", exact: true }).click();
  const predictionRequest = page.waitForRequest(
    (request) =>
      request.method() === "PUT" &&
      new URL(request.url()).pathname === "/proxy/predictions/playground-e2e-id",
  );
  await page.getByRole("button", { name: "Run" }).click();
  await predictionRequest;
  await expectPredictionStatus(page, "succeeded");
  await expect(predictionOutput(page)).toContainText("hello identified");

  await page.getByRole("button", { name: "Reset" }).click();
  await expect(page.getByRole("textbox", { name: "text" })).toHaveValue("");
  await expect(page.getByRole("textbox", { name: "Prediction ID" })).toHaveValue("");
  await expect(page.getByText("Run a prediction to see its output.")).toBeVisible();
});

test("fits every primary control on a narrow viewport", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 800 });

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
  await expect(page.locator("#output-panel .response-title").getByRole("status")).toHaveText(
    status,
    { timeout: 60_000 },
  );
}

function predictionOutput(page: Page) {
  return page.getByRole("region", { name: "Prediction output" });
}

async function connectTarget(page: Page, target: string): Promise<void> {
  const schema = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/proxy/openapi.json" &&
      response.request().headers()["x-cog-target"] === target,
  );
  await page.getByRole("textbox", { name: "Target" }).fill(target);
  await page.getByRole("button", { name: "Connect" }).click();
  expect((await schema).ok()).toBe(true);
  await expect(page.getByRole("textbox", { name: "text" })).toBeVisible();
}

async function startTestModel(name: string): Promise<{ url: string; close: () => Promise<void> }> {
  const server = createServer((request, response) => {
    response.setHeader("Content-Type", "application/json");
    if (request.url === "/health-check") {
      response.end(JSON.stringify({ status: "READY" }));
      return;
    }
    if (request.url === "/openapi.json") {
      response.end(
        JSON.stringify({
          components: {
            schemas: {
              Input: {
                type: "object",
                required: ["text"],
                properties: { text: { type: "string" } },
              },
            },
          },
          paths: { "/predictions": { post: {} } },
        }),
      );
      return;
    }
    if (request.url === "/predictions" && request.method === "POST") {
      let body = "";
      request.setEncoding("utf8");
      request.on("data", (chunk) => (body += chunk));
      request.on("end", () => {
        const prediction = JSON.parse(body) as { input: { text: string } };
        response.end(
          JSON.stringify({ status: "succeeded", output: `${name}: ${prediction.input.text}` }),
        );
      });
      return;
    }
    response.statusCode = 404;
    response.end(JSON.stringify({ error: "not found" }));
  });
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Test model did not bind a port");
  return {
    url: `http://127.0.0.1:${address.port}`,
    close: () =>
      new Promise<void>((resolve, reject) =>
        server.close((error) => (error ? reject(error) : resolve())),
      ),
  };
}
