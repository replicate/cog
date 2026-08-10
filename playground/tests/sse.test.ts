import assert from "node:assert/strict";
import { test } from "vitest";

import { parseSSE, readSSE } from "../src/lib/transport/sse.js";

test("parses multiline, primitive, and non-JSON SSE data", () => {
  assert.deepEqual(parseSSE('event: output\ndata: {"chunk":"a"}'), {
    type: "output",
    data: { chunk: "a" },
    raw: 'event: output\ndata: {"chunk":"a"}',
  });
  assert.deepEqual(parseSSE("event: output\ndata: 3"), {
    type: "output",
    data: { value: 3 },
    raw: "event: output\ndata: 3",
  });
  assert.deepEqual(parseSSE("event: log\ndata: first\ndata: second")?.data, {
    value: "first\nsecond",
  });
  assert.equal(parseSSE("data: ignored"), undefined);
});

test("reads frames split across chunks and accepts final unterminated frame", async () => {
  const encoder = new TextEncoder();
  const response = new Response(
    new ReadableStream({
      start(controller) {
        controller.enqueue(encoder.encode("event: start\r"));
        controller.enqueue(
          encoder.encode('\ndata: {}\r\n\r\nevent: completed\ndata: {"status":"succeeded"}'),
        );
        controller.close();
      },
    }),
  );
  const events = [];
  for await (const event of readSSE(response)) events.push(event);
  assert.deepEqual(
    events.map((event) => event.type),
    ["start", "completed"],
  );
});

test("rejects failed and bodyless streaming responses", async () => {
  await assert.rejects(async () => {
    for await (const _event of readSSE(new Response('{"error":"bad stream"}', { status: 500 }))) {
      // The generator must reject before yielding.
    }
  }, /bad stream/);
  await assert.rejects(async () => {
    for await (const _event of readSSE(new Response(null, { status: 200 }))) {
      // The generator must reject before yielding.
    }
  }, /Streaming response has no body/);
});

test("rejects oversized SSE frames", async () => {
  const response = new Response(`event: output\ndata: ${"x".repeat(1024 * 1024 + 1)}\n\n`);
  await assert.rejects(async () => {
    for await (const _event of readSSE(response)) {
      // The oversized frame must not be exposed.
    }
  }, /SSE event is too large/);
});

test("cancels the response reader when stream consumption stops early", async () => {
  const encoder = new TextEncoder();
  let canceled = false;
  const response = new Response(
    new ReadableStream({
      start(controller) {
        controller.enqueue(encoder.encode("event: output\ndata: first\n\n"));
      },
      cancel() {
        canceled = true;
      },
    }),
  );
  for await (const _event of readSSE(response)) break;
  assert.equal(canceled, true);
});
