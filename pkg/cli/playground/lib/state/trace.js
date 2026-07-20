// @ts-check

export const MAX_TRACE_EVENTS = 100;
export const MAX_TRACE_PAYLOAD_TEXT = 64 * 1024;
export const MAX_TRACE_TOTAL_TEXT = 1024 * 1024;
const MAX_TRACE_COLLECTION_ITEMS = 100;
const MAX_TRACE_DEPTH = 10;
const OMITTED_PAYLOAD = "[payload omitted: trace budget exceeded]";
const OMITTED_OUTPUT = "[earlier output omitted]\n\n";

/** @param {unknown} data */
export function boundedTraceData(data) {
  return boundValue(data, new WeakSet(), 0);
}

/** @param {import("../../types").TraceEvent[]} events @param {import("../../types").TraceEvent} event */
export function appendTraceEvent(events, event) {
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

/** @param {import("../../types").RequestTrace} trace */
export function enforceTraceBudget(trace) {
  let remaining = MAX_TRACE_TOTAL_TEXT;
  /** @param {unknown} value */
  const retain = (value) => {
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
  const events = [];
  for (let index = trace.events.length - 1; index >= 0; index -= 1)
    events.push({ ...trace.events[index], data: retain(trace.events[index].data) });
  events.reverse();
  return { ...trace, requestBody, responseBody, events };
}

/** @param {import("../../types").RequestTrace} trace */
export function tracePayloadLength(trace) {
  return (
    serialize(trace.requestBody).length +
    serialize(trace.responseBody).length +
    trace.events.reduce((length, event) => length + serialize(event.data).length, 0)
  );
}

/** @param {unknown} data @param {WeakSet<object>} ancestors @param {number} depth @returns {unknown} */
function boundValue(data, ancestors, depth) {
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
    /** @type {unknown[]} */
    const bounded = data
      .slice(0, MAX_TRACE_COLLECTION_ITEMS)
      .map((item) => boundValue(item, ancestors, depth + 1));
    if (data.length > MAX_TRACE_COLLECTION_ITEMS)
      bounded.push(`[${data.length - MAX_TRACE_COLLECTION_ITEMS} items omitted]`);
    ancestors.delete(data);
    return bounded;
  }
  const entries = Object.entries(data);
  /** @type {Record<string, unknown>} */
  const bounded = Object.fromEntries(
    entries
      .slice(0, MAX_TRACE_COLLECTION_ITEMS)
      .map(([key, value]) => [key, boundValue(value, ancestors, depth + 1)]),
  );
  if (entries.length > MAX_TRACE_COLLECTION_ITEMS)
    bounded["[truncated]"] = `${entries.length - MAX_TRACE_COLLECTION_ITEMS} properties omitted`;
  ancestors.delete(data);
  return bounded;
}

/** @param {unknown} value */
function serialize(value) {
  if (value === undefined) return "";
  try {
    return JSON.stringify(value) ?? "";
  } catch {
    return String(value);
  }
}
/** @param {unknown} value */
function formatTraceValue(value) {
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value, null, 2) ?? String(value);
  } catch {
    return String(value);
  }
}
/** @param {unknown} previous @param {unknown} next */
function appendTraceOutput(previous, next) {
  const formatted = formatTraceValue(previous);
  const previousText = formatted.startsWith(OMITTED_OUTPUT)
    ? formatted.slice(OMITTED_OUTPUT.length)
    : formatted;
  const combined = `${previousText}\n\n${formatTraceValue(next)}`;
  return combined.length <= MAX_TRACE_PAYLOAD_TEXT
    ? combined
    : OMITTED_OUTPUT + combined.slice(-(MAX_TRACE_PAYLOAD_TEXT - OMITTED_OUTPUT.length));
}
