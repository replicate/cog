import { startTransition, useCallback, useEffect, useRef, useState } from "react";

import { type CogApi, HttpError } from "../../api/cog";
import type { PredictionEnvelope, RequestTrace, RunMode, TraceEventKind } from "../../domain/types";

const TERMINAL = new Set(["succeeded", "failed", "canceled"]);
const MAX_RAW_EVENT_TEXT = 1024 * 1024;
const MAX_RAW_EVENTS = 1000;
const MAX_STREAM_OUTPUT_TEXT = 4 * 1024 * 1024;
const MAX_STREAM_OUTPUT_ITEMS = 4096;
const MAX_LOG_TEXT = 1024 * 1024;
const MAX_METRICS = 100;
const MAX_TRACE_EVENTS = 100;
const MAX_TRACE_EVENT_TEXT = 64 * 1024;

type ActiveRun = {
  token: string;
  endpoint: string;
  predictionId?: string;
  abort: AbortController;
  events?: EventSource;
  startedAt: number;
};

type RunOptions = {
  endpoint: string;
  predictionId?: string;
  input: Record<string, unknown>;
  mode: RunMode;
  webhookBase: string;
  webhookEvents: string[];
};

type StreamBuffer = {
  rawEvents: string[];
  output: unknown[];
  outputLength: number;
  frame?: number;
};

export function usePrediction(api: CogApi) {
  const [running, setRunning] = useState(false);
  const [envelope, setEnvelope] = useState<PredictionEnvelope>();
  const [output, setOutput] = useState<unknown>();
  const [rawEvents, setRawEvents] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [trace, setTrace] = useState<RequestTrace>();
  const activeRun = useRef<ActiveRun | undefined>(undefined);
  const streamBuffer = useRef<StreamBuffer | undefined>(undefined);
  const traceToken = useRef<string | undefined>(undefined);
  const mounted = useRef(true);

  useEffect(
    () => () => {
      mounted.current = false;
      activeRun.current?.abort.abort();
      activeRun.current?.events?.close();
      const frame = streamBuffer.current?.frame;
      if (frame !== undefined) window.cancelAnimationFrame(frame);
      activeRun.current = undefined;
      streamBuffer.current = undefined;
    },
    [],
  );

  const flushStreamBuffer = useCallback((token: string) => {
    if (activeRun.current?.token !== token) return;
    const buffer = streamBuffer.current;
    if (!buffer) return;
    if (buffer.frame !== undefined) window.cancelAnimationFrame(buffer.frame);
    buffer.frame = undefined;
    if (buffer.rawEvents.length === 0 && buffer.output.length === 0) return;
    const rawEvents = buffer.rawEvents.splice(0);
    const output = buffer.output.slice();
    startTransition(() => {
      if (rawEvents.length)
        setRawEvents((current) => boundedTextItems([...current, ...rawEvents], MAX_RAW_EVENT_TEXT));
      if (output.length) setOutput(output);
    });
  }, []);

  const queueStreamRender = (token: string, raw: string, output?: unknown) => {
    const buffer = streamBuffer.current;
    if (activeRun.current?.token !== token || !buffer) return;
    buffer.rawEvents.push(raw);
    buffer.rawEvents = boundedTextItems(buffer.rawEvents, MAX_RAW_EVENT_TEXT);
    if (output !== undefined) {
      buffer.output.push(output);
      buffer.outputLength += valueLength(output);
      while (
        (buffer.outputLength > MAX_STREAM_OUTPUT_TEXT ||
          buffer.output.length > MAX_STREAM_OUTPUT_ITEMS) &&
        buffer.output.length > 1
      ) {
        buffer.outputLength -= valueLength(buffer.output.shift());
      }
    }
    if (buffer.frame === undefined) {
      buffer.frame = window.requestAnimationFrame(() => flushStreamBuffer(token));
    }
  };

  const finish = useCallback(
    (token: string) => {
      if (activeRun.current?.token !== token) return;
      flushStreamBuffer(token);
      setTrace((current) => (current ? { ...current, finishedAt: Date.now() } : current));
      activeRun.current.events?.close();
      activeRun.current = undefined;
      setRunning(false);
    },
    [flushStreamBuffer],
  );

  const applyEnvelope = useCallback((next: PredictionEnvelope) => {
    setEnvelope((current) => ({ ...current, ...next }));
    if (!next.error && next.output !== undefined) setOutput(next.output);
  }, []);

  const run = async (options: RunOptions) => {
    if (activeRun.current) return;
    const token = crypto.randomUUID();
    const abort = new AbortController();
    activeRun.current = {
      token,
      endpoint: options.endpoint,
      predictionId: options.predictionId,
      abort,
      startedAt: performance.now(),
    };
    streamBuffer.current = { rawEvents: [], output: [], outputLength: 0 };
    traceToken.current = token;
    setRunning(true);
    setError("");
    setEnvelope({ status: options.mode === "async" ? "starting" : "processing" });
    setOutput(options.mode === "stream" ? [] : undefined);
    setRawEvents([]);
    setTrace({
      startedAt: Date.now(),
      startedAtLabel: new Date().toLocaleTimeString(),
      method: options.predictionId ? "PUT" : "POST",
      endpoint: options.predictionId
        ? `${options.endpoint}/${encodeURIComponent(options.predictionId)}`
        : options.endpoint,
      requestHeaders: requestHeaders(options.mode),
      requestBody: { input: options.input },
      events: [
        {
          id: crypto.randomUUID(),
          elapsedMs: 0,
          kind: "request",
          label: `${options.predictionId ? "PUT" : "POST"} ${options.endpoint}`,
          data: { input: options.input },
        },
      ],
    });

    try {
      if (options.mode === "stream") await runStreaming(token, options, abort.signal);
      else if (options.mode === "async") await runAsync(token, options, abort.signal);
      else await runSync(token, options, abort.signal);
    } catch (runError) {
      if (activeRun.current?.token !== token) return;
      const canceled = runError instanceof DOMException && runError.name === "AbortError";
      setError(canceled ? "" : errorMessage(runError));
      recordTraceEvent(token, "error", canceled ? "Request aborted" : errorMessage(runError));
      setEnvelope((current) => ({ ...current, status: canceled ? "canceled" : "failed" }));
      finish(token);
    }
  };

  const runSync = async (token: string, options: RunOptions, signal: AbortSignal) => {
    const response = await api.submit({
      endpoint: options.endpoint,
      id: options.predictionId,
      input: options.input,
      signal,
      onResponse: (response) => captureResponse(token, response),
    });
    if (activeRun.current?.token !== token) return;
    activeRun.current.predictionId = response.id;
    applyEnvelope(response);
    setTraceBody(token, response);
    setRawEvents([JSON.stringify(response, null, 2)]);
    finish(token);
  };

  const runStreaming = async (token: string, options: RunOptions, signal: AbortSignal) => {
    const rawFrames: string[] = [];
    let terminal = false;
    for await (const event of api.stream({
      endpoint: options.endpoint,
      id: options.predictionId,
      input: options.input,
      signal,
      onResponse: (response) => captureResponse(token, response, "SSE connection opened"),
    })) {
      if (activeRun.current?.token !== token) return;
      rawFrames.push(event.raw);
      const retainedFrames = boundedTextItems(rawFrames, MAX_RAW_EVENT_TEXT);
      rawFrames.splice(0, rawFrames.length, ...retainedFrames);
      recordTraceEvent(token, "sse", event.type, event.raw);
      if (event.type === "start") {
        applyEnvelope(event.data);
        if (typeof event.data.id === "string") activeRun.current.predictionId = event.data.id;
        terminal = TERMINAL.has(String(event.data.status ?? ""));
      } else if (event.type === "output") {
        queueStreamRender(token, event.raw, event.data.chunk);
        continue;
      } else if (event.type === "metric") {
        const name = String(event.data.name);
        setEnvelope((current) => ({
          ...current,
          metrics:
            Object.hasOwn(current?.metrics ?? {}, name) ||
            Object.keys(current?.metrics ?? {}).length < MAX_METRICS
              ? { ...current?.metrics, [name]: Number(event.data.value) }
              : current?.metrics,
        }));
      } else if (event.type === "log") {
        setEnvelope((current) => ({
          ...current,
          logs: appendBoundedText(
            current?.logs ?? "",
            String(event.data.data ?? event.data.message ?? event.data.value ?? ""),
            MAX_LOG_TEXT,
          ),
        }));
      } else if (event.type === "error") {
        setError(String(event.data.error ?? "Stream error"));
        setEnvelope((current) => ({ ...current, ...event.data, status: "failed" }));
        terminal = true;
      } else if (event.type === "completed") {
        applyEnvelope(boundedTerminalEnvelope(event.data));
        terminal = true;
      }
      queueStreamRender(token, event.raw);
      if (terminal) break;
    }
    flushStreamBuffer(token);
    setTraceBody(token, rawFrames.join("\n\n"), false);
    if (!terminal) {
      setError("Prediction stream ended before a terminal event");
      setEnvelope((current) => ({ ...current, status: "failed" }));
      recordTraceEvent(token, "error", "Prediction stream ended before a terminal event");
    }
    finish(token);
  };

  const runAsync = async (token: string, options: RunOptions, signal: AbortSignal) => {
    if (!options.webhookBase) throw new Error("No webhook host is configured");
    const webhookToken = crypto.randomUUID();
    const events = new EventSource(`/events?token=${encodeURIComponent(webhookToken)}`);
    if (activeRun.current?.token !== token) return;
    activeRun.current.events = events;
    await eventSourceReady(events, signal);
    recordTraceEvent(token, "webhook", "Webhook event stream connected");
    const webhook = `${options.webhookBase}/webhook/${webhookToken}`;
    const filters = Array.from(new Set([...options.webhookEvents, "completed"]));
    setTrace((current) =>
      current
        ? {
            ...current,
            requestBody: {
              input: options.input,
              webhook,
              webhook_events_filter: filters,
            },
          }
        : current,
    );
    events.onmessage = (event) => {
      if (activeRun.current?.token !== token) return;
      setRawEvents((current) => boundedTextItems([...current, event.data], MAX_RAW_EVENT_TEXT));
      recordTraceEvent(token, "webhook", "Webhook delivery", event.data);
      try {
        const next = JSON.parse(event.data) as PredictionEnvelope;
        applyEnvelope(next);
        if (TERMINAL.has(next.status ?? "")) finish(token);
      } catch {
        setError("Received an invalid webhook payload");
      }
    };
    events.onerror = () => {
      if (activeRun.current?.token !== token) return;
      setError("Webhook event connection was interrupted");
      recordTraceEvent(token, "error", "Webhook event connection interrupted");
      setEnvelope((current) => ({ ...current, status: "failed" }));
      finish(token);
    };
    const response = await api.submit({
      endpoint: options.endpoint,
      id: options.predictionId,
      input: options.input,
      async: true,
      webhook,
      webhookEvents: filters,
      signal,
      onResponse: (response) => captureResponse(token, response),
    });
    if (activeRun.current?.token !== token) return;
    activeRun.current.predictionId = response.id;
    applyEnvelope(response);
    setTraceBody(token, response);
    if (TERMINAL.has(response.status ?? "")) finish(token);
  };

  const stop = () => {
    const current = activeRun.current;
    if (!current) return;
    current.abort.abort();
    current.events?.close();
    recordTraceEvent(current.token, "cancel", "Cancellation requested");
    setEnvelope((existing) => ({ ...existing, status: "canceled" }));
    finish(current.token);
    if (current.predictionId) {
      void api
        .cancel(current.endpoint, current.predictionId, AbortSignal.timeout(10_000))
        .catch((cancelError: unknown) => {
          if (!mounted.current || traceToken.current !== current.token) return;
          const message = `Cancellation failed: ${errorMessage(cancelError)}`;
          setError(message);
          appendFinishedTraceEvent(current, "error", message);
        });
    }
  };

  const reset = useCallback(() => {
    setEnvelope(undefined);
    setOutput(undefined);
    setRawEvents([]);
    setError("");
    setTrace(undefined);
    traceToken.current = undefined;
  }, []);

  const recordTraceEvent = (token: string, kind: TraceEventKind, label: string, data?: unknown) => {
    const active = activeRun.current;
    if (!active || active.token !== token) return;
    const event = {
      id: crypto.randomUUID(),
      elapsedMs: performance.now() - active.startedAt,
      kind,
      label,
      data: boundedTraceData(data),
    };
    setTrace((current) =>
      current
        ? { ...current, events: [...current.events, event].slice(-MAX_TRACE_EVENTS) }
        : current,
    );
  };

  const appendFinishedTraceEvent = (
    run: ActiveRun,
    kind: TraceEventKind,
    label: string,
    data?: unknown,
  ) => {
    const event = {
      id: crypto.randomUUID(),
      elapsedMs: performance.now() - run.startedAt,
      kind,
      label,
      data: boundedTraceData(data),
    };
    setTrace((current) =>
      current
        ? { ...current, events: [...current.events, event].slice(-MAX_TRACE_EVENTS) }
        : current,
    );
  };

  const captureResponse = (token: string, response: Response, label?: string) => {
    recordTraceEvent(
      token,
      "response",
      label ? `${label} (HTTP ${response.status})` : `HTTP ${response.status}`,
    );
    setTrace((current) =>
      current
        ? {
            ...current,
            responseStatus: response.status,
            responseHeaders: Object.fromEntries(response.headers.entries()),
          }
        : current,
    );
  };

  const setTraceBody = (token: string, body: unknown, attachToResponseEvent = true) => {
    if (activeRun.current?.token !== token) return;
    setTrace((current) => {
      if (!current) return current;
      const events = [...current.events];
      if (attachToResponseEvent) {
        for (let index = events.length - 1; index >= 0; index -= 1) {
          if (events[index].kind === "response") {
            events[index] = { ...events[index], data: body };
            break;
          }
        }
      }
      return { ...current, responseBody: body, events };
    });
  };

  return { running, envelope, output, rawEvents, error, trace, run, stop, reset };
}

function boundedTextItems(items: string[], maxLength: number): string[] {
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

function boundedTraceData(data: unknown): unknown {
  return typeof data === "string" && data.length > MAX_TRACE_EVENT_TEXT
    ? data.slice(0, MAX_TRACE_EVENT_TEXT) + "\n... truncated"
    : data;
}

function appendBoundedText(current: string, addition: string, maxLength: number): string {
  const combined = current + addition;
  return combined.length > maxLength ? combined.slice(-maxLength) : combined;
}

function boundedTerminalEnvelope(next: PredictionEnvelope): PredictionEnvelope {
  const { output: _output, logs, metrics, ...rest } = next;
  return {
    ...rest,
    ...(typeof logs === "string" ? { logs: logs.slice(-MAX_LOG_TEXT) } : {}),
    ...(metrics
      ? { metrics: Object.fromEntries(Object.entries(metrics).slice(0, MAX_METRICS)) }
      : {}),
  };
}

function valueLength(value: unknown): number {
  if (typeof value === "string") return value.length;
  try {
    return JSON.stringify(value)?.length ?? 0;
  } catch {
    return String(value).length;
  }
}

function requestHeaders(mode: RunMode): Record<string, string> {
  return {
    "Content-Type": "application/json",
    ...(mode === "stream" ? { Accept: "text/event-stream" } : {}),
    ...(mode === "async" ? { Prefer: "respond-async" } : {}),
  };
}

function eventSourceReady(events: EventSource, signal: AbortSignal): Promise<void> {
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

function errorMessage(error: unknown): string {
  if (error instanceof HttpError && error.detail) {
    return error.detail.map((item) => JSON.stringify(item)).join("\n");
  }
  return error instanceof Error ? error.message : String(error);
}
