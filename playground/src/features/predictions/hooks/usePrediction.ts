import { useCallback, useEffect, useRef, useState } from "react";

import type { RunMode, WebhookEvent } from "@/features/predictions/types";
import {
  appendBoundedText,
  applyMetric,
  boundedTerminalEnvelope,
  boundedTextItems,
  eventSourceReady,
  MAX_LOG_TEXT,
  MAX_RAW_EVENT_TEXT,
  predictionErrorMessage as errorMessage,
  TERMINAL,
} from "@/features/predictions/utils/runtime";
import { usePredictionOutputState } from "@/features/predictions/hooks/usePredictionOutputState";
import { type TraceRun, usePredictionTrace } from "@/features/predictions/hooks/usePredictionTrace";
import type { CogApi } from "@/services/cog";
import type { PredictionEnvelope } from "@/types/prediction";

type ActiveRun = TraceRun & {
  target: string;
  endpoint: string;
  predictionId?: string;
  abort: AbortController;
  events?: EventSource;
  webhookReceived?: boolean;
};

type RunOptions = {
  target: string;
  endpoint: string;
  predictionId?: string;
  input: Record<string, unknown>;
  mode: RunMode;
  webhookBase: string;
  webhookEvents: WebhookEvent[];
};

type PredictionApi = Pick<CogApi, "cancel" | "stream" | "submit">;

/**
 * Enforces one active prediction, bounds retained transport data, and ignores late updates from
 * superseded synchronous, streaming, or webhook runs.
 */
export function usePrediction(api: PredictionApi) {
  const [running, setRunning] = useState(false);
  const [envelope, setEnvelope] = useState<PredictionEnvelope>();
  const [error, setError] = useState("");
  const activeRun = useRef<ActiveRun | undefined>(undefined);
  const mounted = useRef(true);
  const {
    output,
    rawEvents,
    setOutput,
    setRawEvents,
    queueStreamRender,
    startOutput,
    resetOutput,
  } = usePredictionOutputState(activeRun);
  const {
    trace,
    beginTrace,
    recordTraceEvent,
    appendFinishedTraceEvent,
    captureResponse,
    setTraceBody,
    setTraceRequestBody,
    resetTrace,
    isCurrentTrace,
  } = usePredictionTrace(activeRun);

  useEffect(
    () => () => {
      mounted.current = false;
      activeRun.current?.abort.abort();
      activeRun.current?.events?.close();
      activeRun.current = undefined;
    },
    [],
  );

  const finish = useCallback((token: string) => {
    if (activeRun.current?.token !== token) return;
    activeRun.current.events?.close();
    activeRun.current = undefined;
    setRunning(false);
  }, []);

  const applyEnvelope = useCallback(
    (next: PredictionEnvelope) => {
      setEnvelope((current) => ({ ...current, ...next }));
      if (!next.error && next.output !== undefined) setOutput(next.output);
    },
    [setOutput],
  );

  const run = async (options: RunOptions) => {
    if (activeRun.current) return;
    const token = crypto.randomUUID();
    const abort = new AbortController();
    activeRun.current = {
      token,
      target: options.target,
      endpoint: options.endpoint,
      predictionId: options.predictionId,
      abort,
      startedAt: performance.now(),
    };
    setRunning(true);
    setError("");
    setEnvelope({ status: options.mode === "async" ? "starting" : "processing" });
    startOutput(options.mode);
    beginTrace({ token, ...options });

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
      target: options.target,
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
      target: options.target,
      endpoint: options.endpoint,
      id: options.predictionId,
      input: options.input,
      signal,
      onResponse: (response) => captureResponse(token, response, "SSE connection opened"),
    })) {
      if (activeRun.current?.token !== token) return;
      rawFrames.push(event.raw);
      recordTraceEvent(token, "sse", event.type, event.data);
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
          metrics: applyMetric(current?.metrics, name, event.data.value, event.data.mode),
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
    setTraceBody(token, boundedTextItems(rawFrames, MAX_RAW_EVENT_TEXT).join("\n\n"), false);
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
    if (activeRun.current?.token !== token) {
      events.close();
      return;
    }
    activeRun.current.events = events;
    await eventSourceReady(events, signal);
    recordTraceEvent(token, "webhook", "Webhook event stream connected");
    const webhook = `${options.webhookBase}/webhook/${webhookToken}`;
    const traceWebhook = `${options.webhookBase}/webhook/[redacted]`;
    const filters = Array.from(new Set([...options.webhookEvents, "completed"]));
    setTraceRequestBody(token, {
      input: options.input,
      webhook: traceWebhook,
      webhook_events_filter: filters,
    });
    events.onmessage = (event) => {
      if (activeRun.current?.token !== token) return;
      setRawEvents((current) => boundedTextItems([...current, event.data], MAX_RAW_EVENT_TEXT));
      try {
        const next = JSON.parse(event.data) as PredictionEnvelope;
        activeRun.current.webhookReceived = true;
        recordTraceEvent(token, "webhook", "Webhook delivery", next);
        applyEnvelope(next);
        if (TERMINAL.has(next.status ?? "")) {
          activeRun.current?.abort.abort();
          finish(token);
        }
      } catch {
        recordTraceEvent(token, "webhook", "Webhook delivery", event.data);
        setError("Received an invalid webhook payload");
        setEnvelope((current) => ({ ...current, status: "failed" }));
        activeRun.current.abort.abort();
        finish(token);
      }
    };
    events.onerror = () => {
      if (activeRun.current?.token !== token) return;
      setError("Webhook event connection was interrupted");
      recordTraceEvent(token, "error", "Webhook event connection interrupted");
      setEnvelope((current) => ({ ...current, status: "failed" }));
      activeRun.current.abort.abort();
      finish(token);
    };
    const response = await api.submit({
      target: options.target,
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
    if (activeRun.current.webhookReceived && !TERMINAL.has(response.status ?? "")) {
      setEnvelope((current) => ({ ...response, ...current }));
    } else {
      applyEnvelope(response);
    }
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
        .cancel(current.target, current.endpoint, current.predictionId, AbortSignal.timeout(10_000))
        .catch((cancelError: unknown) => {
          if (!mounted.current || !isCurrentTrace(current.token)) return;
          const message = `Cancellation failed: ${errorMessage(cancelError)}`;
          setError(message);
          appendFinishedTraceEvent(current, "error", message);
        });
    }
  };

  const reset = useCallback(() => {
    const current = activeRun.current;
    if (current) {
      current.abort.abort();
      current.events?.close();
      activeRun.current = undefined;
      setRunning(false);
    }
    setEnvelope(undefined);
    resetOutput();
    setError("");
    resetTrace();
  }, [resetOutput, resetTrace]);

  return { running, envelope, output, rawEvents, error, trace, run, stop, reset };
}
