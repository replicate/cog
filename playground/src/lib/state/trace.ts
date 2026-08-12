import type { RequestTrace, TraceEvent, UnknownObject } from "../../types.js";

export const MAX_TRACE_EVENTS = 100;
export const MAX_TRACE_PAYLOAD_TEXT = 64 * 1024;
export const MAX_TRACE_TOTAL_TEXT = 1024 * 1024;
const MAX_TRACE_DEPTH = 10;
const OMITTED_PAYLOAD = "[payload omitted: trace budget exceeded]";
const OMITTED_OUTPUT = "[earlier output omitted]\n\n";

export function boundedTraceData(data: unknown): unknown {
  return boundValue(data, new WeakSet(), 0);
}

export function appendTraceEvent(events: TraceEvent[], event: TraceEvent): TraceEvent[] {
  const previous = events.at(-1);
  if (
    previous?.kind === "sse" &&
    previous.label === "output" &&
    event.kind === "sse" &&
    event.label === "output"
  ) {
    return [
      ...events.slice(0, -1),
      {
        ...previous,
        elapsedMs: event.elapsedMs,
        data: appendTraceOutput(previous.data, event.data),
        count: (previous.count ?? 1) + (event.count ?? 1),
      },
    ];
  }
  return [...events, event].slice(-MAX_TRACE_EVENTS);
}

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
  for (let index = trace.events.length - 1; index >= 0; index -= 1)
    events.push({ ...trace.events[index], data: retain(trace.events[index].data) });
  events.reverse();
  return { ...trace, requestBody, responseBody, events };
}

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
    const bounded: unknown[] = data.map((item: unknown) => boundValue(item, ancestors, depth + 1));
    ancestors.delete(data);
    return bounded;
  }
  const entries: [string, unknown][] = Object.entries(data);
  const bounded: UnknownObject = Object.fromEntries(
    entries.map(([key, value]): [string, unknown] => [
      key,
      boundValue(value, ancestors, depth + 1),
    ]),
  );
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

function formatTraceValue(value: unknown): string {
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value, null, 2) ?? String(value);
  } catch {
    return String(value);
  }
}

function appendTraceOutput(previous: unknown, next: unknown): string {
  const formatted = formatTraceValue(previous);
  const previousText = formatted.startsWith(OMITTED_OUTPUT)
    ? formatted.slice(OMITTED_OUTPUT.length)
    : formatted;
  const combined = `${previousText}\n\n${formatTraceValue(next)}`;
  return combined.length <= MAX_TRACE_PAYLOAD_TEXT
    ? combined
    : OMITTED_OUTPUT + combined.slice(-(MAX_TRACE_PAYLOAD_TEXT - OMITTED_OUTPUT.length));
}
