import assert from "node:assert/strict";
import { test } from "vitest";

import { PredictionState } from "../src/lib/state/prediction-state.js";
import { MAX_STREAM_OUTPUT_ITEMS } from "../src/lib/state/runtime.js";
import { CogApi } from "../src/lib/transport/api.js";
import type {
  JsonObject,
  PredictionApi,
  PredictionRunOptions,
  StreamEvent,
  StreamOptions,
} from "../src/types.js";

test("applies start, output, metric, log, unknown, and completed stream events", async () => {
  const events = [
    streamEvent("start", { id: "p1", status: "processing" }),
    streamEvent("output", { chunk: "hello " }),
    streamEvent("metric", { name: "tokens", value: 2, mode: "increment" }),
    streamEvent("metric", { name: "samples", value: 1, mode: "append" }),
    streamEvent("log", { data: "loading\n" }),
    streamEvent("custom", { value: "preserved" }),
    streamEvent("output", { chunk: "world" }),
    streamEvent("completed", {
      status: "succeeded",
      output: "terminal output must not replace chunks",
      logs: "done\n",
      metrics: { predict_time: 1.25 },
    }),
  ];
  const api = streamingApi(events);
  const state = new PredictionState(api);

  await state.run(runOptions());

  assert.equal(state.running, false);
  assert.equal(state.envelope?.status, "succeeded");
  assert.equal(state.envelope?.logs, "done\n");
  assert.deepEqual(state.envelope?.metrics, { predict_time: 1.25 });
  assert.deepEqual(state.output, ["hello ", "world"]);
  assert.deepEqual(
    state.rawEvents,
    events.map((event) => event.raw),
  );
  assert.ok(state.trace?.events.some((event) => event.label === "custom"));
});

test("fails a stream that ends without a terminal event", async () => {
  const state = new PredictionState(streamingApi([streamEvent("start", { status: "processing" })]));

  await state.run(runOptions());

  assert.equal(state.envelope?.status, "failed");
  assert.match(state.error, /ended before a terminal event/);
});

test("reports stream error events and stops consuming later output", async () => {
  const state = new PredictionState(
    streamingApi([
      streamEvent("start", { status: "processing" }),
      streamEvent("error", { error: "model exploded" }),
      streamEvent("output", { chunk: "late" }),
    ]),
  );

  await state.run(runOptions());

  assert.equal(state.envelope?.status, "failed");
  assert.equal(state.error, "model exploded");
  assert.deepEqual(state.output, []);
});

test("retains only the newest bounded number of streamed output chunks", async () => {
  const events = [streamEvent("start", { status: "processing" })];
  for (let index = 0; index < MAX_STREAM_OUTPUT_ITEMS + 2; index += 1)
    events.push(streamEvent("output", { chunk: String(index) }));
  events.push(streamEvent("completed", { status: "succeeded" }));
  const state = new PredictionState(streamingApi(events));

  await state.run(runOptions());

  assert.ok(Array.isArray(state.output));
  assert.equal(state.output.length, MAX_STREAM_OUTPUT_ITEMS);
  assert.equal(state.output[0], "2");
  assert.equal(state.output.at(-1), String(MAX_STREAM_OUTPUT_ITEMS + 1));
});

test("stop aborts streaming immediately and requests API cancellation", async () => {
  let canceled = "";
  const api = predictionApi({
    async *stream(options: StreamOptions) {
      yield streamEvent("start", { id: "prediction-id", status: "processing" });
      yield streamEvent("output", { chunk: "partial" });
      await new Promise((_, reject) =>
        options.signal.addEventListener(
          "abort",
          () => reject(new DOMException("Aborted", "AbortError")),
          { once: true },
        ),
      );
    },
    async cancel(_target: string, _endpoint: string, id: string) {
      canceled = id;
    },
  });
  const state = new PredictionState(api);
  const running = state.run(runOptions());
  await waitFor(() => Array.isArray(state.output) && state.output.length === 1);

  state.stop();
  await running;
  await waitFor(() => canceled !== "");

  assert.equal(state.envelope?.status, "canceled");
  assert.equal(state.running, false);
  assert.equal(canceled, "prediction-id");
});

test("reset ignores output emitted by a superseded stream", async () => {
  let release: () => void = () => {};
  const gate = new Promise<void>((resolve) => {
    release = resolve;
  });
  const api = predictionApi({
    async *stream() {
      yield streamEvent("start", { status: "processing" });
      await gate;
      yield streamEvent("output", { chunk: "late" });
      yield streamEvent("completed", { status: "succeeded" });
    },
  });
  const state = new PredictionState(api);
  const running = state.run(runOptions());
  await waitFor(() => state.envelope?.status === "processing");

  state.reset();
  release();
  await running;

  assert.equal(state.envelope, undefined);
  assert.equal(state.output, undefined);
  assert.deepEqual(state.rawEvents, []);
});

test("CogApi and PredictionState consume incrementally chunked SSE end to end", async () => {
  const encoder = new TextEncoder();
  const chunks = [
    'event: start\r\ndata: {"id":"p1","status":"processing"}\r',
    '\n\r\nevent: output\ndata: {"chunk":"hel',
    'lo "}\n\nevent: output\ndata: {"chunk":"wørld"}\n\nevent: compl',
    'eted\ndata: {"status":"succeeded","metrics":{"tokens":2}}',
  ];
  const requests: Array<{ url: string; init: RequestInit }> = [];
  await withFetch(
    async (url, init) => {
      requests.push({ url: String(url), init: init ?? {} });
      let index = 0;
      return new Response(
        new ReadableStream({
          pull(controller) {
            if (index < chunks.length) controller.enqueue(encoder.encode(chunks[index++]));
            else controller.close();
          },
        }),
        { status: 200, headers: { "Content-Type": "text/event-stream" } },
      );
    },
    async () => {
      const state = new PredictionState(new CogApi());
      await state.run(runOptions());

      assert.deepEqual(state.output, ["hello ", "wørld"]);
      assert.equal(state.envelope?.status, "succeeded");
      assert.deepEqual(state.envelope?.metrics, { tokens: 2 });
      state.destroy();
    },
  );
  const request = requests[0];
  assert.ok(request);
  assert.equal(request.url, "/proxy/predictions");
  assert.equal(request.init.method, "POST");
  assert.equal(new Headers(request.init.headers).get("Accept"), "text/event-stream");
  assert.equal(new Headers(request.init.headers).get("X-Cog-Target"), "http://model");
  assert.deepEqual(JSON.parse(String(request.init.body)), { input: {} });
});

function streamEvent(type: string, data: JsonObject): StreamEvent {
  return { type, data, raw: `event: ${type}\ndata: ${JSON.stringify(data)}` };
}

function streamingApi(events: StreamEvent[]): PredictionApi {
  return predictionApi({
    async *stream(options: StreamOptions) {
      options.onResponse?.(new Response(null, { status: 200 }));
      for (const event of events) yield event;
    },
  });
}

function runOptions(): PredictionRunOptions {
  return {
    target: "http://model",
    endpoint: "/predictions",
    input: {},
    mode: "stream",
    webhookBase: "",
    webhookEvents: [],
  };
}

async function waitFor(condition: () => boolean): Promise<void> {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    if (condition()) return;
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
  assert.fail("condition was not reached");
}

async function withFetch(
  fetcher: typeof globalThis.fetch,
  run: () => Promise<void>,
): Promise<void> {
  const original = globalThis.fetch;
  globalThis.fetch = fetcher;
  try {
    await run();
  } finally {
    globalThis.fetch = original;
  }
}

function predictionApi(overrides: Partial<PredictionApi>): PredictionApi {
  return {
    submit: async () => ({ status: "succeeded" }),
    stream: async function* () {},
    cancel: async () => {},
    ...overrides,
  };
}
