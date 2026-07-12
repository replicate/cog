import type { HealthResponse } from "@/types/health";
import type { OpenAPIDocument } from "@/types/openapi";
import type { PredictionEnvelope, StreamEvent } from "@/types/prediction";
import { parseJSONResponse, parsePredictionResponse, responseError } from "@/services/cog/http";
import { readSSE } from "@/services/cog/sse";

const PROXY_PREFIX = "/proxy";

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

export class CogApi {
  #target = "";

  setTarget(target: string): void {
    this.#target = target.trim().replace(/\/+$/, "");
  }

  async config(
    signal?: AbortSignal,
  ): Promise<{ target?: string; webhookBase?: string; cogVersion?: string }> {
    const response = await fetch("/config", { credentials: "omit", signal });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    return parseJSONResponse<{ target?: string; webhookBase?: string; cogVersion?: string }>(
      response,
    );
  }

  async health(signal?: AbortSignal): Promise<HealthResponse> {
    return this.#jsonRequest<HealthResponse>("/health-check", signal);
  }

  async schema(signal?: AbortSignal): Promise<OpenAPIDocument> {
    return this.#jsonRequest<OpenAPIDocument>("/openapi.json", signal);
  }

  async submit(options: SubmitOptions): Promise<PredictionEnvelope> {
    const headers = this.#headers({ "Content-Type": "application/json" });
    if (options.async) headers.set("Prefer", "respond-async");
    const response = await fetch(this.#url(options.endpoint, options.id), {
      method: options.id ? "PUT" : "POST",
      headers,
      body: JSON.stringify(this.#body(options.input, options.webhook, options.webhookEvents)),
      credentials: "omit",
      signal: options.signal,
    });
    options.onResponse?.(response);
    return parsePredictionResponse(response);
  }

  async *stream(options: StreamOptions): AsyncGenerator<StreamEvent> {
    const response = await fetch(this.#url(options.endpoint, options.id), {
      method: options.id ? "PUT" : "POST",
      headers: this.#headers({ "Content-Type": "application/json", Accept: "text/event-stream" }),
      body: JSON.stringify(this.#body(options.input)),
      credentials: "omit",
      signal: options.signal,
    });
    options.onResponse?.(response);
    yield* readSSE(response);
  }

  async cancel(endpoint: string, id: string, signal?: AbortSignal): Promise<void> {
    const response = await fetch(`${PROXY_PREFIX}${endpoint}/${encodeURIComponent(id)}/cancel`, {
      method: "POST",
      headers: this.#headers(),
      credentials: "omit",
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

  async #jsonRequest<T>(endpoint: string, signal?: AbortSignal): Promise<T> {
    const response = await fetch(PROXY_PREFIX + endpoint, {
      headers: this.#headers(),
      credentials: "omit",
      signal,
    });
    if (!response.ok) throw await responseError(response);
    return parseJSONResponse<T>(response);
  }
}
