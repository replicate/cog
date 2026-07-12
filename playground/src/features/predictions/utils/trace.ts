import type { RequestTrace, TraceEvent } from "@/types/prediction";

export const MAX_TRACE_EVENTS = 100;
export const MAX_TRACE_PAYLOAD_TEXT = 64 * 1024;
export const MAX_TRACE_TOTAL_TEXT = 1024 * 1024;

const MAX_TRACE_COLLECTION_ITEMS = 100;
const MAX_TRACE_DEPTH = 10;
const OMITTED_PAYLOAD = "[payload omitted: trace budget exceeded]";

/** Recursively bounds trace strings, data URIs, collections, depth, and circular references. */
export function boundedTraceData(data: unknown): unknown {
  return boundValue(data, new WeakSet<object>(), 0);
}

/** Enforces one aggregate payload budget across request, response, and event data. */
export function enforceTraceBudget(trace: RequestTrace): RequestTrace {
  let remaining = MAX_TRACE_TOTAL_TEXT;
  const retain = (value: unknown): unknown => {
    if (value === undefined) return undefined;
    const length = serialize(value).length;
    if (length <= remaining) {
      remaining -= length;
      return value;
    }
    if (OMITTED_PAYLOAD.length <= remaining) {
      remaining -= OMITTED_PAYLOAD.length;
      return OMITTED_PAYLOAD;
    }
    return undefined;
  };

  const responseBody = retain(trace.responseBody);
  const requestBody = retain(trace.requestBody);
  const events: TraceEvent[] = [];
  for (let index = trace.events.length - 1; index >= 0; index -= 1) {
    const event = trace.events[index];
    events.push({ ...event, data: retain(event.data) });
  }
  events.reverse();
  return { ...trace, requestBody, responseBody, events };
}

/** Measures serialized trace payload fields for budget assertions. */
export function tracePayloadLength(trace: RequestTrace): number {
  return (
    serialize(trace.requestBody).length +
    serialize(trace.responseBody).length +
    trace.events.reduce((length, event) => length + serialize(event.data).length, 0)
  );
}

function boundValue(data: unknown, ancestors: WeakSet<object>, depth: number): unknown {
  if (typeof data === "string") {
    if (/^data:[^,]*,/i.test(data) && data.length > MAX_TRACE_PAYLOAD_TEXT) {
      const separator = data.indexOf(",");
      const prefix = data.slice(0, Math.min(separator + 1, 256));
      return `${prefix}[${data.length - prefix.length} characters omitted]`;
    }
    return data.length > MAX_TRACE_PAYLOAD_TEXT
      ? data.slice(0, MAX_TRACE_PAYLOAD_TEXT) + "\n... truncated"
      : data;
  }
  if (typeof data !== "object" || data === null) return data;
  if (depth >= MAX_TRACE_DEPTH) return "[maximum depth reached]";
  if (ancestors.has(data)) return "[circular reference]";

  ancestors.add(data);
  if (Array.isArray(data)) {
    const bounded = data
      .slice(0, MAX_TRACE_COLLECTION_ITEMS)
      .map((item) => boundValue(item, ancestors, depth + 1));
    if (data.length > MAX_TRACE_COLLECTION_ITEMS) {
      bounded.push(`[${data.length - MAX_TRACE_COLLECTION_ITEMS} items omitted]`);
    }
    ancestors.delete(data);
    return bounded;
  }

  const entries = Object.entries(data);
  const bounded = Object.fromEntries(
    entries
      .slice(0, MAX_TRACE_COLLECTION_ITEMS)
      .map(([key, value]) => [key, boundValue(value, ancestors, depth + 1)]),
  );
  if (entries.length > MAX_TRACE_COLLECTION_ITEMS) {
    bounded["[truncated]"] = `${entries.length - MAX_TRACE_COLLECTION_ITEMS} properties omitted`;
  }
  ancestors.delete(data);
  return bounded;
}

function serialize(value: unknown): string {
  if (value === undefined) return "";
  try {
    return JSON.stringify(value) ?? "";
  } catch {
    return String(value);
  }
}
