// @ts-check

import { HttpError } from "../transport/http.js";

export const TERMINAL = new Set(["succeeded", "failed", "canceled"]);
export const MAX_RAW_EVENT_TEXT = 1024 * 1024;
export const MAX_STREAM_OUTPUT_TEXT = 4 * 1024 * 1024;
export const MAX_STREAM_OUTPUT_ITEMS = 4096;
export const MAX_LOG_TEXT = 1024 * 1024;
export const MAX_METRICS = 100;
export const MAX_METRIC_ITEMS = 1000;
export const MAX_METRIC_ITEMS_TEXT = 64 * 1024;
export const MAX_RAW_EVENTS = 1000;

/** @param {string[]} items @param {number} maxLength */
export function boundedTextItems(items, maxLength) {
  const retained = [];
  let length = 0;
  for (let index = items.length - 1; index >= 0 && retained.length < MAX_RAW_EVENTS; index -= 1) {
    const item = items[index];
    if (length + item.length > maxLength) {
      if (!retained.length) retained.push(item.slice(-maxLength));
      break;
    }
    retained.push(item);
    length += item.length;
  }
  return retained.reverse();
}

/** @param {string} current @param {string} addition @param {number} maxLength */
export function appendBoundedText(current, addition, maxLength) {
  const combined = current + addition;
  return combined.length > maxLength ? combined.slice(-maxLength) : combined;
}

/** @param {import("../../types").PredictionEnvelope} next */
export function boundedTerminalEnvelope(next) {
  const { output: _output, logs, metrics, ...rest } = next;
  return {
    ...rest,
    ...(typeof logs === "string" ? { logs: logs.slice(-MAX_LOG_TEXT) } : {}),
    ...(metrics
      ? { metrics: Object.fromEntries(Object.entries(metrics).slice(0, MAX_METRICS)) }
      : {}),
  };
}

/** @param {Record<string, unknown> | undefined} metrics @param {string} name @param {unknown} value @param {unknown} mode */
export function applyMetric(metrics, name, value, mode) {
  const current = metrics ?? {};
  if (!Object.hasOwn(current, name) && Object.keys(current).length >= MAX_METRICS) return metrics;
  const next = { ...current };
  const metricMode = typeof mode === "string" ? mode : "replace";
  if (metricMode === "increment") {
    const previous = Number(next[name] ?? 0);
    const delta = Number(value);
    if (!Number.isFinite(previous) || !Number.isFinite(delta)) return metrics ?? current;
    next[name] = previous + delta;
  } else if (metricMode === "append") {
    if (valueLength(value) > MAX_METRIC_ITEMS_TEXT) return metrics ?? current;
    const existing = next[name];
    const values = Array.isArray(existing)
      ? [...existing, value]
      : existing === undefined
        ? [value]
        : [existing, value];
    next[name] = boundedMetricItems(values);
  } else if (value === null) delete next[name];
  else next[name] = value;
  return next;
}

/** @param {unknown[]} values */
function boundedMetricItems(values) {
  const retained = [];
  let length = 0;
  for (
    let index = values.length - 1;
    index >= 0 && retained.length < MAX_METRIC_ITEMS;
    index -= 1
  ) {
    const item = values[index];
    const itemLength = valueLength(item);
    if (itemLength > MAX_METRIC_ITEMS_TEXT - length) break;
    retained.push(item);
    length += itemLength;
  }
  return retained.reverse();
}

/** @param {unknown} value */
export function valueLength(value) {
  if (typeof value === "string") return value.length;
  try {
    return JSON.stringify(value)?.length ?? 0;
  } catch {
    return String(value).length;
  }
}

/** @param {import("../../types").RunMode} mode */
export function requestHeaders(mode) {
  return {
    "Content-Type": "application/json",
    ...(mode === "stream" ? { Accept: "text/event-stream" } : {}),
    ...(mode === "async" ? { Prefer: "respond-async" } : {}),
  };
}

/** @param {EventSource} events @param {AbortSignal} signal */
export function eventSourceReady(events, signal) {
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
      resolve(undefined);
    };
    events.onerror = () => {
      cleanup();
      events.close();
      reject(new Error("Could not establish webhook event connection"));
    };
  });
}

/** @param {unknown} error */
export function predictionErrorMessage(error) {
  return error instanceof HttpError && error.detail
    ? error.detail.map((item) => JSON.stringify(item)).join("\n")
    : error instanceof Error
      ? error.message
      : String(error);
}
