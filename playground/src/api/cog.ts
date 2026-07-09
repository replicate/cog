import type {
  HealthResponse,
  JsonObject,
  JsonValue,
  OpenAPIDocument,
  PredictionEnvelope,
  StreamEvent,
} from "../domain/types";

const PROXY_PREFIX = "/proxy";
const MAX_SSE_FRAME_LENGTH = 1024 * 1024;

type SubmitOptions = {
  endpoint: string;
  id?: string;
  input: Record<string, unknown>;
  signal: AbortSignal;
  async?: boolean;
  webhook?: string;
  webhookEvents?: string[];
  onResponse?: (response: Response) => void;
};

type StreamOptions = Pick<SubmitOptions, "endpoint" | "id" | "input" | "signal" | "onResponse">;

export class HttpError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly detail?: unknown[],
  ) {
    super(message);
  }
}

export class CogApi {
  #target = "";

  setTarget(target: string): void {
    this.#target = target.trim().replace(/\/+$/, "");
  }

  async config(): Promise<{ webhookBase?: string }> {
    const response = await fetch("/config");
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    return response.json() as Promise<{ webhookBase?: string }>;
  }

  async health(): Promise<HealthResponse> {
    return this.#jsonRequest<HealthResponse>("/health-check");
  }

  async schema(): Promise<OpenAPIDocument> {
    return this.#jsonRequest<OpenAPIDocument>("/openapi.json");
  }

  async submit(options: SubmitOptions): Promise<PredictionEnvelope> {
    const headers = this.#headers({ "Content-Type": "application/json" });
    if (options.async) headers.set("Prefer", "respond-async");
    const response = await fetch(this.#url(options.endpoint, options.id), {
      method: options.id ? "PUT" : "POST",
      headers,
      body: JSON.stringify(this.#body(options.input, options.webhook, options.webhookEvents)),
      signal: options.signal,
    });
    options.onResponse?.(response);
    return parseResponse(response);
  }

  async *stream(options: StreamOptions): AsyncGenerator<StreamEvent> {
    const response = await fetch(this.#url(options.endpoint, options.id), {
      method: options.id ? "PUT" : "POST",
      headers: this.#headers({ "Content-Type": "application/json", Accept: "text/event-stream" }),
      body: JSON.stringify(this.#body(options.input)),
      signal: options.signal,
    });
    options.onResponse?.(response);
    if (!response.ok) throw await responseError(response);
    if (!response.body) throw new Error("Streaming response has no body");

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let done = false;
    try {
      for (;;) {
        const chunk = await reader.read();
        done = chunk.done;
        buffer += decoder.decode(chunk.value, { stream: !done });
        buffer = normalizeSSELines(buffer, done);
        let separator = buffer.indexOf("\n\n");
        while (separator >= 0) {
          if (separator > MAX_SSE_FRAME_LENGTH) throw new Error("SSE event is too large");
          const raw = buffer.slice(0, separator);
          buffer = buffer.slice(separator + 2);
          const event = parseSSE(raw);
          if (event) yield event;
          separator = buffer.indexOf("\n\n");
        }
        if (buffer.length > MAX_SSE_FRAME_LENGTH) throw new Error("SSE event is too large");
        if (done) break;
      }
      if (buffer.trim()) {
        const event = parseSSE(buffer);
        if (event) yield event;
      }
    } finally {
      if (!done) await reader.cancel();
      reader.releaseLock();
    }
  }

  async cancel(endpoint: string, id: string, signal?: AbortSignal): Promise<void> {
    const response = await fetch(`${PROXY_PREFIX}${endpoint}/${encodeURIComponent(id)}/cancel`, {
      method: "POST",
      headers: this.#headers(),
      signal,
    });
    if (!response.ok) throw await responseError(response);
  }

  #url(endpoint: string, id?: string): string {
    return id ? `${PROXY_PREFIX}${endpoint}/${encodeURIComponent(id)}` : PROXY_PREFIX + endpoint;
  }

  #headers(values?: HeadersInit): Headers {
    const headers = new Headers(values);
    headers.set("X-Cog-Target", this.#target);
    return headers;
  }

  #body(
    input: Record<string, unknown>,
    webhook?: string,
    webhookEvents?: string[],
  ): { input: Record<string, unknown>; webhook?: string; webhook_events_filter?: string[] } {
    return webhook ? { input, webhook, webhook_events_filter: webhookEvents } : { input };
  }

  async #jsonRequest<T>(endpoint: string): Promise<T> {
    const response = await fetch(PROXY_PREFIX + endpoint, { headers: this.#headers() });
    if (!response.ok) throw await responseError(response);
    return response.json() as Promise<T>;
  }
}

function normalizeSSELines(buffer: string, done: boolean): string {
  const trailingCR = !done && buffer.endsWith("\r");
  const complete = trailingCR ? buffer.slice(0, -1) : buffer;
  return complete.replaceAll("\r\n", "\n").replaceAll("\r", "\n") + (trailingCR ? "\r" : "");
}

export function parseSSE(raw: string): StreamEvent | undefined {
  let type = "";
  const data: string[] = [];
  for (const line of raw.split("\n")) {
    if (line.startsWith("event:")) type = line.slice(6).trim();
    if (line.startsWith("data:")) data.push(line.slice(5).replace(/^ /, ""));
  }
  if (!type) return undefined;
  const joined = data.join("\n");
  let parsed: JsonObject = { value: joined };
  try {
    const value: unknown = JSON.parse(joined);
    parsed = isJsonObject(value) ? value : { value: isJsonValue(value) ? value : joined };
  } catch {
    // Preserve non-JSON event payloads.
  }
  return { type, data: parsed, raw };
}

function isJsonObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isJsonValue(value: unknown): value is JsonValue {
  if (value === null || ["boolean", "number", "string"].includes(typeof value)) return true;
  if (Array.isArray(value)) return value.every(isJsonValue);
  return isJsonObject(value) && Object.values(value).every(isJsonValue);
}

export function fileToDataURI(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error ?? new Error("Could not read file"));
    reader.readAsDataURL(file);
  });
}

async function parseResponse(response: Response): Promise<PredictionEnvelope> {
  if (!response.ok) throw await responseError(response);
  return response.json() as Promise<PredictionEnvelope>;
}

async function responseError(response: Response): Promise<HttpError> {
  const text = await response.text();
  let body: JsonObject = {};
  try {
    const parsed: unknown = JSON.parse(text);
    body = isJsonObject(parsed) ? parsed : {};
  } catch {
    // Use the response text below.
  }
  const detail = Array.isArray(body.detail) ? body.detail : undefined;
  const message =
    (typeof body.error === "string" && body.error) ||
    (typeof body.detail === "string" && body.detail) ||
    text ||
    `HTTP ${response.status}`;
  return new HttpError(message, response.status, detail);
}
