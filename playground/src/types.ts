export type StringMap<T> = { [key: string]: T };
export type UnknownObject = StringMap<unknown>;

export type JsonPrimitive = boolean | null | number | string;
export type JsonValue = JsonObject | JsonPrimitive | JsonValue[];
export interface JsonObject {
  [key: string]: JsonValue;
}

export type HealthResponse = {
  status?: string;
  user_healthcheck_error?: string;
  setup?: { status?: string; logs?: string };
  version?: { coglet?: string; python_sdk?: string; python?: string };
};

export type PlaygroundConfig = {
  target?: string;
  webhookBase?: string;
  cogVersion?: string;
};

export type OpenAPISchemaMap = StringMap<OpenAPISchema>;
export type OpenAPISchema = {
  $ref?: string;
  type?: string | string[];
  format?: string;
  title?: string;
  description?: string;
  default?: JsonValue;
  const?: JsonValue;
  enum?: JsonValue[];
  allOf?: OpenAPISchema[];
  anyOf?: OpenAPISchema[];
  oneOf?: OpenAPISchema[];
  not?: OpenAPISchema;
  items?: boolean | OpenAPISchema | OpenAPISchema[];
  additionalItems?: boolean | OpenAPISchema;
  properties?: OpenAPISchemaMap;
  patternProperties?: OpenAPISchemaMap;
  definitions?: OpenAPISchemaMap;
  dependencies?: StringMap<OpenAPISchema | string[]>;
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
  components?: OpenAPIComponents;
  [key: `x-${string}`]: JsonValue;
};

export type OpenAPIComponents = { schemas?: OpenAPISchemaMap };
export type OpenAPIOperation = { "x-cog-streaming"?: boolean };
export type OpenAPIPathItem = { post?: OpenAPIOperation };
export type OpenAPIDocument = {
  components?: OpenAPIComponents;
  paths?: StringMap<OpenAPIPathItem>;
};

export type PlaygroundEndpoint = "/predictions" | "/trainings";
export type PlaygroundCapabilities = {
  endpoint: PlaygroundEndpoint;
  input: OpenAPISchema;
  streaming: boolean;
  async: boolean;
};

export type PredictionEnvelope = {
  id?: string;
  status?: string;
  output?: JsonValue;
  error?: string;
  logs?: string;
  metrics?: MetricMap;
  [key: string]: unknown;
};

export type MetricMap = StringMap<JsonValue | undefined>;
export type StreamEvent = { type: string; data: JsonObject; raw: string };
export type InputMode = "form" | "json";
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
  requestHeaders: StringMap<string>;
  requestBody: unknown;
  responseStatus?: number;
  responseHeaders?: StringMap<string>;
  responseBody?: unknown;
  events: TraceEvent[];
};

export type ValidationIssue = { field?: string; keyword: string; message: string; path: string };
export type InputValidator = (value: unknown) => ValidationIssue[];
export type ValidationRequest = {
  id: number;
  document: OpenAPIDocument;
  input: unknown;
  inputSchema: OpenAPISchema;
  nextSchemaId: number;
};
export type ValidationResponse = { id: number; issues: ValidationIssue[] };

export type PredictionRunOptions = {
  target: string;
  endpoint: PlaygroundEndpoint;
  predictionId?: string;
  input: JsonObject;
  mode: RunMode;
  webhookBase: string;
  webhookEvents: WebhookEvent[];
};

export type SubmitOptions = {
  target: string;
  endpoint: PlaygroundEndpoint;
  id?: string;
  input: JsonObject;
  signal: AbortSignal;
  async?: boolean;
  webhook?: string;
  webhookEvents?: WebhookEvent[];
  onResponse?: (response: Response) => void;
};

export type StreamOptions = Omit<SubmitOptions, "async" | "webhook" | "webhookEvents">;

export interface ConnectionApi {
  config(signal?: AbortSignal): Promise<PlaygroundConfig>;
  health(target: string, signal?: AbortSignal): Promise<HealthResponse>;
  schema(target: string, signal?: AbortSignal): Promise<OpenAPIDocument>;
}

export interface PredictionApi {
  submit(options: SubmitOptions): Promise<PredictionEnvelope>;
  stream(options: StreamOptions): AsyncGenerator<StreamEvent, void, unknown>;
  cancel(target: string, endpoint: string, id: string, signal?: AbortSignal): Promise<void>;
}
