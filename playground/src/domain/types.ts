export type JsonPrimitive = boolean | null | number | string;
export type JsonValue = JsonObject | JsonPrimitive | JsonValue[];
export type JsonObject = { [key: string]: JsonValue };

export type OpenAPISchema = {
  $ref?: string;
  type?: string;
  format?: string;
  title?: string;
  description?: string;
  default?: unknown;
  enum?: unknown[];
  allOf?: OpenAPISchema[];
  anyOf?: OpenAPISchema[];
  items?: OpenAPISchema;
  properties?: Record<string, OpenAPISchema>;
  required?: string[];
  minimum?: number;
  maximum?: number;
  minLength?: number;
  maxLength?: number;
  pattern?: string;
  deprecated?: boolean;
  [key: `x-${string}`]: unknown;
};

export type OpenAPIDocument = {
  components?: { schemas?: Record<string, OpenAPISchema> };
  paths?: Record<string, OpenAPIPathItem>;
};

export type OpenAPIOperation = JsonObject & { "x-cog-streaming"?: boolean };
export type OpenAPIPathItem = Record<string, OpenAPIOperation>;

export type HealthResponse = {
  status?: string;
  user_healthcheck_error?: string;
  setup?: { status?: string; logs?: string };
  version?: { coglet?: string; python_sdk?: string; python?: string };
};

export type PredictionEnvelope = {
  id?: string;
  status?: string;
  output?: unknown;
  error?: string;
  logs?: string;
  metrics?: Record<string, number>;
  [key: string]: unknown;
};

export type StreamEvent = {
  type: string;
  data: JsonObject;
  raw: string;
};

export const RUN_MODES = ["sync", "stream", "async"] as const;
export type RunMode = (typeof RUN_MODES)[number];

export const INPUT_MODES = ["form", "json"] as const;
export type InputMode = (typeof INPUT_MODES)[number];

export const WEBHOOK_EVENTS = ["start", "output", "logs"] as const;
export type WebhookEvent = (typeof WEBHOOK_EVENTS)[number];

export type TraceEventKind = "request" | "response" | "sse" | "webhook" | "cancel" | "error";

export type TraceEvent = {
  id: string;
  elapsedMs: number;
  kind: TraceEventKind;
  label: string;
  data?: unknown;
};

export type RequestTrace = {
  startedAt: number;
  startedAtLabel: string;
  finishedAt?: number;
  method: "POST" | "PUT";
  endpoint: string;
  requestHeaders: Record<string, string>;
  requestBody: unknown;
  responseStatus?: number;
  responseHeaders?: Record<string, string>;
  responseBody?: unknown;
  events: TraceEvent[];
};
