import { describe, expect, it } from "vitest";

import {
  boundedTraceData,
  enforceTraceBudget,
  MAX_TRACE_PAYLOAD_TEXT,
  MAX_TRACE_TOTAL_TEXT,
  tracePayloadLength,
} from "@/features/predictions/utils/trace";
import type { RequestTrace } from "@/types/prediction";

describe("prediction trace bounding", () => {
  it("keeps ordinary data-prefixed text and small data URIs visible", () => {
    expect(boundedTraceData("data: analyze this model")).toBe("data: analyze this model");
    expect(boundedTraceData("data:text/plain,hello")).toBe("data:text/plain,hello");
  });

  it("omits only oversized data URI payloads", () => {
    const value = `data:application/octet-stream;base64,${"A".repeat(MAX_TRACE_PAYLOAD_TEXT)}`;
    const bounded = boundedTraceData(value);

    expect(bounded).toContain("characters omitted");
    expect(bounded).not.toContain("A".repeat(1024));
  });

  it("bounds circular and deeply nested values", () => {
    const circular: Record<string, unknown> = {};
    circular.self = circular;
    let deep: Record<string, unknown> = { value: "end" };
    for (let depth = 0; depth < 20; depth += 1) deep = { child: deep };

    expect(JSON.stringify(boundedTraceData(circular))).toContain("circular reference");
    expect(JSON.stringify(boundedTraceData(deep))).toContain("maximum depth reached");
  });

  it("enforces one aggregate budget while retaining newest event payloads", () => {
    const events = Array.from({ length: 100 }, (_, index) => ({
      id: String(index),
      elapsedMs: index,
      kind: "webhook" as const,
      label: "Webhook delivery",
      data: `${index}:${"x".repeat(MAX_TRACE_PAYLOAD_TEXT)}`,
    }));
    const trace: RequestTrace = {
      startedAt: 0,
      startedAtLabel: "now",
      method: "POST",
      endpoint: "/predictions",
      requestHeaders: {},
      requestBody: { input: {} },
      events,
    };

    const bounded = enforceTraceBudget(trace);

    expect(tracePayloadLength(bounded)).toBeLessThanOrEqual(MAX_TRACE_TOTAL_TEXT);
    expect(bounded.events.at(-1)?.data).toBe(events.at(-1)?.data);
    expect(bounded.events[0].data).toContain("trace budget exceeded");
  });
});
