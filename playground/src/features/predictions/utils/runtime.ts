import type { RunMode } from "@/features/predictions/types";
import { HttpError } from "@/services/cog";
import type { PredictionEnvelope } from "@/types/prediction";

export const TERMINAL = new Set(["succeeded", "failed", "canceled"]);
export const MAX_RAW_EVENT_TEXT = 1024 * 1024;
export const MAX_STREAM_OUTPUT_TEXT = 4 * 1024 * 1024;
export const MAX_STREAM_OUTPUT_ITEMS = 4096;
export const MAX_LOG_TEXT = 1024 * 1024;
export const MAX_METRICS = 100;
export const MAX_RAW_EVENTS = 1000;

/** Retains the newest text items within both character and item-count limits. */
export function boundedTextItems(items: string[], maxLength: number): string[] {
  const retained: string[] = [];
  let length = 0;
  for (let index = items.length - 1; index >= 0 && retained.length < MAX_RAW_EVENTS; index -= 1) {
    const item = items[index];
    if (length + item.length > maxLength) {
      if (retained.length === 0) retained.push(item.slice(-maxLength));
      break;
    }
    retained.push(item);
    length += item.length;
  }
  return retained.reverse();
}

/** Concatenates text while preserving only the newest characters under the limit. */
export function appendBoundedText(current: string, addition: string, maxLength: number): string {
  const combined = current + addition;
  return combined.length > maxLength ? combined.slice(-maxLength) : combined;
}

/** Removes terminal output and bounds retained logs and metric count. */
export function boundedTerminalEnvelope(next: PredictionEnvelope): PredictionEnvelope {
  const { output: _output, logs, metrics, ...rest } = next;
  return {
    ...rest,
    ...(typeof logs === "string" ? { logs: logs.slice(-MAX_LOG_TEXT) } : {}),
    ...(metrics
      ? { metrics: Object.fromEntries(Object.entries(metrics).slice(0, MAX_METRICS)) }
      : {}),
  };
}

/** Applies a streamed metric event using replace, increment, or append semantics. */
export function applyMetric(
  metrics: Record<string, unknown> | undefined,
  name: string,
  value: unknown,
  mode: unknown,
): Record<string, unknown> | undefined {
  const current = metrics ?? {};
  if (!Object.hasOwn(current, name) && Object.keys(current).length >= MAX_METRICS) {
    return metrics;
  }
  const next = { ...current };
  const metricMode = typeof mode === "string" ? mode : "replace";
  if (metricMode === "increment") {
    const previous = Number(next[name] ?? 0);
    const delta = Number(value);
    if (!Number.isFinite(previous) || !Number.isFinite(delta)) return metrics ?? current;
    next[name] = previous + delta;
    return next;
  }
  if (metricMode === "append") {
    const existing = next[name];
    if (Array.isArray(existing)) next[name] = [...existing, value];
    else if (existing === undefined) next[name] = [value];
    else next[name] = [existing, value];
    return next;
  }
  if (value === null) delete next[name];
  else next[name] = value;
  return next;
}

/** Estimates an arbitrary value's serialized character length without throwing. */
export function valueLength(value: unknown): number {
  if (typeof value === "string") return value.length;
  try {
    return JSON.stringify(value)?.length ?? 0;
  } catch {
    return String(value).length;
  }
}

/** Returns the trace-visible request headers implied by a prediction mode. */
export function requestHeaders(mode: RunMode): Record<string, string> {
  return {
    "Content-Type": "application/json",
    ...(mode === "stream" ? { Accept: "text/event-stream" } : {}),
    ...(mode === "async" ? { Prefer: "respond-async" } : {}),
  };
}

/** Waits for an EventSource to open, closing it on timeout, abort, or connection failure. */
export function eventSourceReady(events: EventSource, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const timeout = window.setTimeout(() => {
      cleanup();
      events.close();
      reject(new Error("Webhook event connection timed out"));
    }, 5000);
    const cleanup = () => {
      clearTimeout(timeout);
      signal.removeEventListener("abort", abort);
    };
    const abort = () => {
      cleanup();
      events.close();
      reject(new DOMException("Aborted", "AbortError"));
    };
    signal.addEventListener("abort", abort, { once: true });
    events.onopen = () => {
      cleanup();
      resolve();
    };
    events.onerror = () => {
      cleanup();
      events.close();
      reject(new Error("Could not establish webhook event connection"));
    };
  });
}

/** Formats structured HTTP validation details or falls back to a normal error message. */
export function predictionErrorMessage(error: unknown): string {
  if (error instanceof HttpError && error.detail) {
    return error.detail.map((item) => JSON.stringify(item)).join("\n");
  }
  return error instanceof Error ? error.message : String(error);
}
