import { isJsonObject, type JsonObject } from "@/types/json";

export type PredictionEnvelope = {
  id?: string;
  status?: string;
  output?: unknown;
  error?: string;
  logs?: string;
  metrics?: Record<string, unknown>;
  [key: string]: unknown;
};

export type StreamEvent = {
  type: string;
  data: JsonObject;
  raw: string;
};

export type TraceEventKind = "request" | "response" | "sse" | "webhook" | "cancel" | "error";

export type TraceEvent = {
  id: string;
  elapsedMs: number;
  kind: TraceEventKind;
  label: string;
  data?: unknown;
  count?: number;
};

export type RequestTrace = {
  startedAtLabel: string;
  method: "POST" | "PUT";
  endpoint: string;
  requestHeaders: Record<string, string>;
  requestBody: unknown;
  responseStatus?: number;
  responseHeaders?: Record<string, string>;
  responseBody?: unknown;
  events: TraceEvent[];
};

/** Validates a prediction response and omits legacy nullable fields. */
export function predictionEnvelope(value: unknown): PredictionEnvelope | undefined {
  if (
    !isJsonObject(value) ||
    !["id", "status", "error", "logs"].every(
      (key) => value[key] === undefined || value[key] === null || typeof value[key] === "string",
    ) ||
    (value.metrics !== undefined && value.metrics !== null && !isJsonObject(value.metrics))
  ) {
    return undefined;
  }

  const { error, id, logs, metrics, status, ...other } = value;
  return {
    ...other,
    ...(typeof id === "string" && { id }),
    ...(typeof status === "string" && { status }),
    ...(typeof error === "string" && { error }),
    ...(typeof logs === "string" && { logs }),
    ...(isJsonObject(metrics) && { metrics }),
  };
}
