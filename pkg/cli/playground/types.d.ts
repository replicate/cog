export type JsonPrimitive = boolean | null | number | string;
export type JsonValue = JsonObject | JsonPrimitive | JsonValue[];
export type JsonObject = { [key: string]: JsonValue };

export type HealthResponse = {
  status?: string;
  user_healthcheck_error?: string;
  setup?: { status?: string; logs?: string };
  version?: { coglet?: string; python_sdk?: string; python?: string };
};

export type OpenAPISchema = {
  $ref?: string;
  type?: string | string[];
  format?: string;
  title?: string;
  description?: string;
  default?: JsonValue;
  enum?: JsonValue[];
  allOf?: OpenAPISchema[];
  anyOf?: OpenAPISchema[];
  oneOf?: OpenAPISchema[];
  items?: boolean | OpenAPISchema;
  properties?: Record<string, OpenAPISchema>;
  additionalProperties?: boolean | OpenAPISchema;
  required?: string[];
  nullable?: boolean;
  minimum?: number;
  maximum?: number;
  exclusiveMinimum?: boolean | number;
  exclusiveMaximum?: boolean | number;
  multipleOf?: number;
  minLength?: number;
  maxLength?: number;
  pattern?: string;
  minItems?: number;
  maxItems?: number;
  uniqueItems?: boolean;
  minProperties?: number;
  maxProperties?: number;
  deprecated?: boolean;
  [key: `x-${string}`]: JsonValue;
};

export type OpenAPIOperation = JsonObject & { "x-cog-streaming"?: boolean };
export type OpenAPIPathItem = JsonObject & { post?: OpenAPIOperation };
export type OpenAPIDocument = {
  components?: { schemas?: Record<string, OpenAPISchema> };
  paths?: Record<string, OpenAPIPathItem>;
};

export type PlaygroundCapabilities = {
  endpoint: "/predictions" | "/trainings";
  input: OpenAPISchema;
  streaming: boolean;
  async: boolean;
};

export type PredictionEnvelope = {
  id?: string;
  status?: string;
  output?: unknown;
  error?: string;
  logs?: string;
  metrics?: Record<string, unknown>;
  [key: string]: unknown;
};

export type StreamEvent = { type: string; data: JsonObject; raw: string };
export type RunMode = "sync" | "stream" | "async";
export type WebhookEvent = "start" | "output" | "logs" | "completed";
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

export type ValidationIssue = { field?: string; keyword: string; message: string; path: string };
