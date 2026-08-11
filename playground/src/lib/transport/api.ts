import type {
  HealthResponse,
  OpenAPIDocument,
  PlaygroundConfig,
  ConnectionApi,
  PredictionApi,
  PredictionEnvelope,
  StreamEvent,
  StreamOptions,
  SubmitOptions,
} from "../../types.js";

import { isObject } from "../json.js";
import { isHealthResponse, isOpenAPIDocument } from "./guards.js";
import { parseJSONResponse, parsePredictionResponse, responseError } from "./http.js";
import { readSSE } from "./sse.js";

const PROXY_PREFIX = "/proxy";
type ValueGuard<T> = (value: unknown) => value is T;

export class CogApi implements ConnectionApi, PredictionApi {
  async config(signal?: AbortSignal): Promise<PlaygroundConfig> {
    const response = await fetch("/config", { credentials: "omit", signal });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const config = await parseJSONResponse(response);
    if (!isPlaygroundConfig(config)) throw new Error("Invalid playground configuration response");
    return config;
  }

  health(target: string, signal?: AbortSignal): Promise<HealthResponse> {
    return this.#jsonRequest(target, "/health-check", signal, "health", isHealthResponse);
  }

  schema(target: string, signal?: AbortSignal): Promise<OpenAPIDocument> {
    return this.#jsonRequest(target, "/openapi.json", signal, "OpenAPI", isOpenAPIDocument);
  }

  async submit(options: SubmitOptions): Promise<PredictionEnvelope> {
    const headers = this.#headers(options.target, { "Content-Type": "application/json" });
    if (options.async) headers.set("Prefer", "respond-async");
    const response = await this.#proxyFetch(this.#url(options.endpoint, options.id), {
      method: options.id ? "PUT" : "POST",
      headers,
      body: JSON.stringify(
        options.webhook
          ? {
              input: options.input,
              webhook: options.webhook,
              webhook_events_filter: options.webhookEvents,
            }
          : { input: options.input },
      ),
      signal: options.signal,
    });
    options.onResponse?.(response);
    return parsePredictionResponse(response);
  }

  async *stream(options: StreamOptions): AsyncGenerator<StreamEvent, void, unknown> {
    const response = await this.#proxyFetch(this.#url(options.endpoint, options.id), {
      method: options.id ? "PUT" : "POST",
      headers: this.#headers(options.target, {
        "Content-Type": "application/json",
        Accept: "text/event-stream",
      }),
      body: JSON.stringify({ input: options.input }),
      signal: options.signal,
    });
    options.onResponse?.(response);
    yield* readSSE(response);
  }

  async cancel(target: string, endpoint: string, id: string, signal?: AbortSignal): Promise<void> {
    const response = await this.#proxyFetch(
      `${PROXY_PREFIX}${endpoint}/${encodeURIComponent(id)}/cancel`,
      { method: "POST", headers: this.#headers(target), signal },
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

  async #jsonRequest<T>(
    target: string,
    endpoint: string,
    signal: AbortSignal | undefined,
    name: string,
    validate: ValueGuard<T>,
  ): Promise<T> {
    const response = await this.#proxyFetch(PROXY_PREFIX + endpoint, {
      headers: this.#headers(target),
      signal,
    });
    if (!response.ok) throw await responseError(response);
    const body = await parseJSONResponse(response);
    if (!validate(body)) throw new Error(`Invalid ${name} response`);
    return body;
  }

  async #proxyFetch(url: string, init: RequestInit): Promise<Response> {
    const response = await fetch(url, { ...init, credentials: "omit", redirect: "manual" });
    if (response.type === "opaqueredirect" || (response.status >= 300 && response.status < 400))
      throw new Error(
        `Target API returned an unexpected redirect (${response.status || "opaque"})`,
      );
    return response;
  }
}

function isPlaygroundConfig(value: unknown): value is PlaygroundConfig {
  return (
    isObject(value) &&
    ["target", "webhookBase", "cogVersion"].every(
      (key) => value[key] === undefined || typeof value[key] === "string",
    )
  );
}
