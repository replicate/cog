import assert from "node:assert/strict";
import { test } from "vitest";

import {
  appendBoundedText,
  applyMetric,
  boundedTextItems,
  MAX_METRIC_ITEMS,
  MAX_METRIC_ITEMS_TEXT,
} from "../src/lib/state/runtime.js";
import {
  appendTraceEvent,
  boundedTraceData,
  enforceTraceBudget,
  MAX_TRACE_TOTAL_TEXT,
  tracePayloadLength,
} from "../src/lib/state/trace.js";
import type { JsonValue, MetricMap, TraceEvent } from "../src/types.js";

test("retains newest bounded transport text", () => {
  assert.equal(appendBoundedText("abcdef", "gh", 5), "defgh");
  assert.deepEqual(boundedTextItems(["aaa", "bbb", "ccc"], 7), ["bbb", "ccc"]);
});

test("applies metric modes", () => {
  assert.deepEqual(applyMetric({ count: 2 }, "count", 3, "increment"), { count: 5 });
  assert.deepEqual(applyMetric({ values: [1] }, "values", 2, "append"), { values: [1, 2] });
  assert.deepEqual(applyMetric({ old: true }, "old", null, "replace"), {});
  assert.equal(applyMetric(undefined, "count", "invalid", "increment"), undefined);
  assert.equal(
    applyMetric(undefined, "values", "x".repeat(MAX_METRIC_ITEMS_TEXT + 1), "append"),
    undefined,
  );
});

test("bounds appended metric values", () => {
  let metrics: MetricMap = {};
  for (let index = 0; index < MAX_METRIC_ITEMS + 2; index += 1)
    metrics = applyMetric(metrics, "samples", index, "append") ?? {};
  const samples = metrics.samples;
  assert.ok(isJsonArray(samples));
  assert.equal(samples.length, MAX_METRIC_ITEMS);
  assert.deepEqual(samples.slice(0, 2), [2, 3]);

  const firstLarge = "x".repeat(Math.floor(MAX_METRIC_ITEMS_TEXT * 0.75));
  const secondLarge = "y".repeat(Math.floor(MAX_METRIC_ITEMS_TEXT * 0.75));
  const bounded = applyMetric(
    { samples: [firstLarge, secondLarge] },
    "samples",
    "newest",
    "append",
  );
  assert.deepEqual(bounded?.samples, [secondLarge, "newest"]);
  assert.deepEqual(
    applyMetric(
      { samples: ["existing"] },
      "samples",
      "x".repeat(MAX_METRIC_ITEMS_TEXT + 1),
      "append",
    ),
    { samples: ["existing"] },
  );
});

test("bounds circular trace data and compacts output events", () => {
  const circular: { self?: object } = {};
  circular.self = circular;
  assert.deepEqual(boundedTraceData(circular), { self: "[circular reference]" });
  const first: TraceEvent = { id: "1", elapsedMs: 1, kind: "sse", label: "output", data: "a" };
  const second: TraceEvent = { id: "2", elapsedMs: 2, kind: "sse", label: "output", data: "b" };
  const events = appendTraceEvent([first], second);
  assert.equal(events.length, 1);
  assert.equal(events[0].count, 2);
  assert.match(String(events[0].data), /a\n\nb/);
});

function isJsonArray(value: JsonValue | undefined): value is JsonValue[] {
  return Array.isArray(value);
}

test("preserves complete trace collections", () => {
  const collection = Array.from({ length: 164 }, (_, index) => index);
  assert.deepEqual(boundedTraceData(collection), collection);
  const object = Object.fromEntries(collection.map((index) => [`key-${index}`, index]));
  assert.deepEqual(boundedTraceData(object), object);
});

test("enforces aggregate trace budget", () => {
  const trace = enforceTraceBudget({
    startedAtLabel: "now",
    method: "POST",
    endpoint: "/predictions",
    requestHeaders: {},
    requestBody: "x".repeat(MAX_TRACE_TOTAL_TEXT),
    responseBody: "y".repeat(MAX_TRACE_TOTAL_TEXT),
    events: [],
  });
  assert.ok(tracePayloadLength(trace) <= MAX_TRACE_TOTAL_TEXT);
});
