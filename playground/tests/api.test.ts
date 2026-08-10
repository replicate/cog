import assert from "node:assert/strict";
import { test } from "vitest";

import { CogApi } from "../src/lib/transport/api.js";
import { isOpenAPIDocument } from "../src/lib/transport/guards.js";
import type { JsonObject } from "../src/types.js";
import {
  HttpError,
  parseJSONResponse,
  parsePredictionResponse,
  responseError,
} from "../src/lib/transport/http.js";

test("loads and validates playground configuration", async () => {
  await withFetch(
    async (input, init) => {
      assert.equal(input, "/config");
      assert.equal(init?.credentials, "omit");
      return jsonResponse({ target: "http://model", webhookBase: "http://hook", cogVersion: "1" });
    },
    async () => {
      assert.deepEqual(await new CogApi().config(), {
        target: "http://model",
        webhookBase: "http://hook",
        cogVersion: "1",
      });
    },
  );
});

test("rejects invalid configuration and model schema responses", async () => {
  await withFetch(
    async () => jsonResponse({ target: 123 }),
    async () => {
      await assert.rejects(new CogApi().config(), /Invalid playground configuration/);
    },
  );
  await withFetch(
    async () => jsonResponse([]),
    async () => {
      await assert.rejects(new CogApi().schema("http://model"), /Invalid OpenAPI response/);
    },
  );
});

test("normalizes the target and validates health responses", async () => {
  await withFetch(
    async (input, init) => {
      assert.equal(input, "/proxy/health-check");
      assert.equal(new Headers(init?.headers).get("X-Cog-Target"), "http://model");
      assert.equal(init?.redirect, "manual");
      return jsonResponse({ status: "READY" });
    },
    async () => {
      assert.deepEqual(await new CogApi().health(" http://model/// "), { status: "READY" });
    },
  );
});

test("submits POST predictions with plain input", async () => {
  await withFetch(
    async (input, init) => {
      assert.equal(input, "/proxy/predictions");
      assert.equal(init?.method, "POST");
      assert.deepEqual(JSON.parse(String(init?.body)), { input: { prompt: "hello" } });
      return jsonResponse({ id: "p1", status: "succeeded", output: "ok" });
    },
    async () => {
      const result = await new CogApi().submit({
        target: "http://model",
        endpoint: "/predictions",
        input: { prompt: "hello" },
        signal: new AbortController().signal,
      });
      assert.equal(result.output, "ok");
    },
  );
});

test("submits encoded custom IDs and async webhook filters", async () => {
  await withFetch(
    async (input, init) => {
      assert.equal(input, "/proxy/predictions/id%2Fwith%20spaces");
      assert.equal(init?.method, "PUT");
      const headers = new Headers(init?.headers);
      assert.equal(headers.get("Prefer"), "respond-async");
      assert.deepEqual(JSON.parse(String(init?.body)), {
        input: {},
        webhook: "http://hook/token",
        webhook_events_filter: ["start", "completed"],
      });
      return jsonResponse({ status: "starting" });
    },
    async () => {
      await new CogApi().submit({
        target: "http://model",
        endpoint: "/predictions",
        id: "id/with spaces",
        input: {},
        async: true,
        webhook: "http://hook/token",
        webhookEvents: ["start", "completed"],
        signal: new AbortController().signal,
      });
    },
  );
});

test("streams SSE with the required request headers", async () => {
  const body = [
    'event: start\ndata: {"status":"processing"}\n\n',
    'event: output\ndata: {"chunk":"hello"}\n\n',
    'event: completed\ndata: {"status":"succeeded"}\n\n',
  ].join("");
  await withFetch(
    async (input, init) => {
      assert.equal(input, "/proxy/predictions");
      assert.equal(new Headers(init?.headers).get("Accept"), "text/event-stream");
      return new Response(body, { status: 200 });
    },
    async () => {
      const events = [];
      for await (const event of new CogApi().stream({
        target: "http://model",
        endpoint: "/predictions",
        input: {},
        signal: new AbortController().signal,
      }))
        events.push(event);
      assert.deepEqual(
        events.map((event) => [event.type, event.data]),
        [
          ["start", { status: "processing" }],
          ["output", { chunk: "hello" }],
          ["completed", { status: "succeeded" }],
        ],
      );
    },
  );
});

test("rejects redirects instead of following target locations", async () => {
  await withFetch(
    async () => new Response(null, { status: 302 }),
    async () => {
      await assert.rejects(new CogApi().health("http://model"), /unexpected redirect \(302\)/);
    },
  );
});

test("sends cancellation to an encoded prediction endpoint", async () => {
  await withFetch(
    async (input, init) => {
      assert.equal(input, "/proxy/predictions/id%2F1/cancel");
      assert.equal(init?.method, "POST");
      return new Response(null, { status: 204 });
    },
    async () => {
      await new CogApi().cancel("http://model", "/predictions", "id/1");
    },
  );
});

test("surfaces structured and plain HTTP errors", async () => {
  const structured = await responseError(
    jsonResponse({ error: "bad input", detail: [{ loc: ["input"] }] }, 422),
  );
  assert.ok(structured instanceof HttpError);
  assert.equal(structured.message, "bad input");
  assert.equal(structured.status, 422);
  assert.deepEqual(structured.detail, [{ loc: ["input"] }]);
  assert.equal(
    (await responseError(new Response("plain failure", { status: 500 }))).message,
    "plain failure",
  );
});

test("rejects malformed and oversized JSON responses", async () => {
  await assert.rejects(parseJSONResponse(new Response("not json")), SyntaxError);
  await assert.rejects(
    parseJSONResponse(
      new Response("{}", { headers: { "Content-Length": String(16 * 1024 * 1024 + 1) } }),
    ),
    /Response body exceeds/,
  );
});

test("rejects successful HTTP responses with invalid prediction envelopes", async () => {
  await assert.rejects(parsePredictionResponse(jsonResponse([])), /Invalid prediction response/);
});

test("rejects invalid nested OpenAPI schemas", () => {
  assert.equal(
    isOpenAPIDocument({
      components: { schemas: { Input: { patternProperties: { "^x": 1 } } } },
      paths: { "/predictions": { post: {} } },
    }),
    false,
  );
});

test("bounds deeply nested OpenAPI schemas", () => {
  let schema: JsonObject = { type: "string" };
  for (let depth = 0; depth < 1000; depth += 1) schema = { not: schema };
  assert.equal(
    isOpenAPIDocument({
      components: { schemas: { Input: schema } },
      paths: { "/predictions": { post: {} } },
    }),
    false,
  );
});

async function withFetch(
  implementation: typeof globalThis.fetch,
  run: () => Promise<void>,
): Promise<void> {
  const original = globalThis.fetch;
  globalThis.fetch = implementation;
  try {
    await run();
  } finally {
    globalThis.fetch = original;
  }
}

function jsonResponse(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
