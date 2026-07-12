import { type RefObject, useCallback, useRef, useState } from "react";

import type { RunMode } from "@/features/predictions/types";
import { requestHeaders } from "@/features/predictions/utils/runtime";
import {
  boundedTraceData,
  enforceTraceBudget,
  MAX_TRACE_EVENTS,
} from "@/features/predictions/utils/trace";
import type { RequestTrace, TraceEventKind } from "@/types/prediction";

export type TraceRun = {
  token: string;
  startedAt: number;
};

type TraceStart = {
  token: string;
  endpoint: string;
  predictionId?: string;
  input: Record<string, unknown>;
  mode: RunMode;
};

/** Applies event and aggregate payload limits while rejecting mutations from stale run tokens. */
export function usePredictionTrace<T extends TraceRun>(activeRun: RefObject<T | undefined>) {
  const [trace, setTrace] = useState<RequestTrace>();
  const traceToken = useRef<string | undefined>(undefined);

  const updateTrace = useCallback((update: (current: RequestTrace) => RequestTrace) => {
    setTrace((current) => (current ? enforceTraceBudget(update(current)) : current));
  }, []);

  const beginTrace = useCallback((options: TraceStart) => {
    const requestBody = boundedTraceData({ input: options.input });
    traceToken.current = options.token;
    setTrace(
      enforceTraceBudget({
        startedAt: Date.now(),
        startedAtLabel: new Date().toLocaleTimeString(),
        method: options.predictionId ? "PUT" : "POST",
        endpoint: options.predictionId
          ? `${options.endpoint}/${encodeURIComponent(options.predictionId)}`
          : options.endpoint,
        requestHeaders: requestHeaders(options.mode),
        requestBody,
        events: [
          {
            id: crypto.randomUUID(),
            elapsedMs: 0,
            kind: "request",
            label: `${options.predictionId ? "PUT" : "POST"} ${options.endpoint}`,
            data: requestBody,
          },
        ],
      }),
    );
  }, []);

  const finishTrace = useCallback(
    (token: string) => {
      if (activeRun.current?.token !== token) return;
      updateTrace((current) => ({ ...current, finishedAt: Date.now() }));
    },
    [activeRun, updateTrace],
  );

  const recordTraceEvent = useCallback(
    (token: string, kind: TraceEventKind, label: string, data?: unknown) => {
      const active = activeRun.current;
      if (!active || active.token !== token) return;
      const event = {
        id: crypto.randomUUID(),
        elapsedMs: performance.now() - active.startedAt,
        kind,
        label,
        data: boundedTraceData(data),
      };
      updateTrace((current) => ({
        ...current,
        events: [...current.events, event].slice(-MAX_TRACE_EVENTS),
      }));
    },
    [activeRun, updateTrace],
  );

  const appendFinishedTraceEvent = useCallback(
    (run: TraceRun, kind: TraceEventKind, label: string, data?: unknown) => {
      if (traceToken.current !== run.token) return;
      const event = {
        id: crypto.randomUUID(),
        elapsedMs: performance.now() - run.startedAt,
        kind,
        label,
        data: boundedTraceData(data),
      };
      updateTrace((current) => ({
        ...current,
        events: [...current.events, event].slice(-MAX_TRACE_EVENTS),
      }));
    },
    [updateTrace],
  );

  const captureResponse = useCallback(
    (token: string, response: Response, label?: string) => {
      if (traceToken.current !== token) return;
      recordTraceEvent(
        token,
        "response",
        label ? `${label} (HTTP ${response.status})` : `HTTP ${response.status}`,
      );
      updateTrace((current) => ({
        ...current,
        responseStatus: response.status,
        responseHeaders: Object.fromEntries(response.headers.entries()),
      }));
    },
    [recordTraceEvent, updateTrace],
  );

  const setTraceBody = useCallback(
    (token: string, body: unknown, attachToResponseEvent = true) => {
      if (activeRun.current?.token !== token) return;
      const boundedBody = boundedTraceData(body);
      updateTrace((current) => {
        const events = [...current.events];
        if (attachToResponseEvent) {
          for (let index = events.length - 1; index >= 0; index -= 1) {
            if (events[index].kind === "response") {
              events[index] = { ...events[index], data: boundedBody };
              break;
            }
          }
        }
        return { ...current, responseBody: boundedBody, events };
      });
    },
    [activeRun, updateTrace],
  );

  const setTraceRequestBody = useCallback(
    (token: string, body: unknown) => {
      if (activeRun.current?.token !== token) return;
      updateTrace((current) => ({ ...current, requestBody: boundedTraceData(body) }));
    },
    [activeRun, updateTrace],
  );

  const resetTrace = useCallback(() => {
    setTrace(undefined);
    traceToken.current = undefined;
  }, []);

  const isCurrentTrace = useCallback((token: string) => traceToken.current === token, []);

  return {
    trace,
    beginTrace,
    finishTrace,
    recordTraceEvent,
    appendFinishedTraceEvent,
    captureResponse,
    setTraceBody,
    setTraceRequestBody,
    resetTrace,
    isCurrentTrace,
  };
}
