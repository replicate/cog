import type { JsonObject } from "@/types/json";

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
