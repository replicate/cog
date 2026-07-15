import { isHealthResponse, type HealthResponse } from "@/types/health";
import { isJsonObject, type JsonObject } from "@/types/json";
import { isOpenAPIDocument, type OpenAPIDocument } from "@/types/openapi";
import type { PredictionEnvelope, StreamEvent } from "@/types/prediction";
import { parseJSONResponse, parsePredictionResponse, responseError } from "@/services/cog/http";
import { readSSE } from "@/services/cog/sse";

const PROXY_PREFIX = "/proxy";

type SubmitOptions = {
  target: string;
  endpoint: string;
  id?: string;
  input: JsonObject;
  signal: AbortSignal;
  async?: boolean;
  webhook?: string;
  webhookEvents?: string[];
  onResponse?: (response: Response) => void;
};

type StreamOptions = Pick<
  SubmitOptions,
  "target" | "endpoint" | "id" | "input" | "signal" | "onResponse"
>;

type PlaygroundConfig = {
  target?: string;
  webhookBase?: string;
  cogVersion?: string;
};

/** Routes model requests through the same-origin proxy rather than exposing cross-origin access. */
export class CogApi {
  /** Loads the playground's optional target, webhook, and version configuration. */
  async config(signal?: AbortSignal): Promise<PlaygroundConfig> {
    const response = await fetch("/config", { credentials: "omit", signal });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const config = await parseJSONResponse(response);
    if (!isPlaygroundConfig(config)) throw new Error("Invalid playground configuration response");
    return config;
  }

  /** Reads size-limited health JSON from the configured target. */
  async health(target: string, signal?: AbortSignal): Promise<HealthResponse> {
    return this.#jsonRequest(target, "/health-check", signal, "health", isHealthResponse);
  }

  /** Reads the size-limited OpenAPI document from the configured target. */
  async schema(target: string, signal?: AbortSignal): Promise<OpenAPIDocument> {
    return this.#jsonRequest(target, "/openapi.json", signal, "OpenAPI", isOpenAPIDocument);
  }

  /** Uses PUT for caller-supplied IDs and adds `Prefer: respond-async` for async submissions. */
  async submit(options: SubmitOptions): Promise<PredictionEnvelope> {
    const headers = this.#headers(options.target, { "Content-Type": "application/json" });
    if (options.async) headers.set("Prefer", "respond-async");
    const response = await this.#proxyFetch(this.#url(options.endpoint, options.id), {
      method: options.id ? "PUT" : "POST",
      headers,
      body: JSON.stringify(this.#body(options.input, options.webhook, options.webhookEvents)),
      signal: options.signal,
    });
    options.onResponse?.(response);
    return parsePredictionResponse(response);
  }

  /** Requests SSE and exposes each frame only after bounded incremental parsing. */
  async *stream(options: StreamOptions): AsyncGenerator<StreamEvent> {
    const response = await this.#proxyFetch(this.#url(options.endpoint, options.id), {
      method: options.id ? "PUT" : "POST",
      headers: this.#headers(options.target, {
        "Content-Type": "application/json",
        Accept: "text/event-stream",
      }),
      body: JSON.stringify(this.#body(options.input)),
      signal: options.signal,
    });
    options.onResponse?.(response);
    yield* readSSE(response);
  }

  /** URL-encodes the run ID before posting to its cancellation endpoint. */
  async cancel(target: string, endpoint: string, id: string, signal?: AbortSignal): Promise<void> {
    const response = await this.#proxyFetch(
      `${PROXY_PREFIX}${endpoint}/${encodeURIComponent(id)}/cancel`,
      {
        method: "POST",
        headers: this.#headers(target),
        signal,
      },
    );
    if (!response.ok) throw await responseError(response);
  }

  #url(endpoint: string, id?: string): string {
    return id ? `${PROXY_PREFIX}${endpoint}/${encodeURIComponent(id)}` : PROXY_PREFIX + endpoint;
  }

  #headers(target: string, values?: HeadersInit): Headers {
    const headers = new Headers(values);
    headers.set("X-Cog-Target", target.trim().replace(/\/+$/, ""));
    return headers;
  }

  #body(
    input: JsonObject,
    webhook?: string,
    webhookEvents?: string[],
  ): { input: JsonObject; webhook?: string; webhook_events_filter?: string[] } {
    return webhook ? { input, webhook, webhook_events_filter: webhookEvents } : { input };
  }

  async #jsonRequest<T>(
    target: string,
    endpoint: string,
    signal: AbortSignal | undefined,
    responseName: string,
    validate: (value: unknown) => value is T,
  ): Promise<T> {
    const response = await this.#proxyFetch(PROXY_PREFIX + endpoint, {
      headers: this.#headers(target),
      signal,
    });
    if (!response.ok) throw await responseError(response);
    const body = await parseJSONResponse(response);
    if (!validate(body)) throw new Error(`Invalid ${responseName} response`);
    return body;
  }

  /** Never follows redirects so a hostile target cannot pivot the browser off-origin. */
  async #proxyFetch(url: string, init: RequestInit): Promise<Response> {
    const response = await fetch(url, {
      ...init,
      credentials: "omit",
      redirect: "manual",
    });
    if (response.type === "opaqueredirect" || (response.status >= 300 && response.status < 400)) {
      throw new Error(
        `Target API returned an unexpected redirect (${response.status || "opaque"})`,
      );
    }
    return response;
  }
}

function isPlaygroundConfig(value: unknown): value is PlaygroundConfig {
  return (
    isJsonObject(value) &&
    ["target", "webhookBase", "cogVersion"].every(
      (key) => value[key] === undefined || typeof value[key] === "string",
    )
  );
}
