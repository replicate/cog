// @ts-check

import { isHealthResponse, isOpenAPIDocument } from "./guards.js";
import { isJsonObject } from "../json.js";
import { parseJSONResponse, parsePredictionResponse, responseError } from "./http.js";
import { readSSE } from "./sse.js";

const PROXY_PREFIX = "/proxy";

export class CogApi {
  /** @param {AbortSignal} [signal] */
  async config(signal) {
    const response = await fetch("/config", { credentials: "omit", signal });
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const config = await parseJSONResponse(response);
    if (
      !isJsonObject(config) ||
      !["target", "webhookBase", "cogVersion"].every(
        (key) => config[key] === undefined || typeof config[key] === "string",
      )
    )
      throw new Error("Invalid playground configuration response");
    return config;
  }

  /** @param {string} target @param {AbortSignal} [signal] */
  health(target, signal) {
    return this.#jsonRequest(target, "/health-check", signal, "health", isHealthResponse);
  }
  /** @param {string} target @param {AbortSignal} [signal] */
  schema(target, signal) {
    return this.#jsonRequest(target, "/openapi.json", signal, "OpenAPI", isOpenAPIDocument);
  }

  /** @param {{target:string, endpoint:string, id?:string, input:import("../../types").JsonObject, signal:AbortSignal, async?:boolean, webhook?:string, webhookEvents?:string[], onResponse?:(response:Response)=>void}} options */
  async submit(options) {
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

  /** @param {{target:string, endpoint:string, id?:string, input:import("../../types").JsonObject, signal:AbortSignal, onResponse?:(response:Response)=>void}} options */
  async *stream(options) {
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

  /** @param {string} target @param {string} endpoint @param {string} id @param {AbortSignal} [signal] */
  async cancel(target, endpoint, id, signal) {
    const response = await this.#proxyFetch(
      `${PROXY_PREFIX}${endpoint}/${encodeURIComponent(id)}/cancel`,
      { method: "POST", headers: this.#headers(target), signal },
    );
    if (!response.ok) throw await responseError(response);
  }

  /** @param {string} endpoint @param {string} [id] */
  #url(endpoint, id) {
    return id ? `${PROXY_PREFIX}${endpoint}/${encodeURIComponent(id)}` : PROXY_PREFIX + endpoint;
  }
  /** @param {string} target @param {HeadersInit} [values] */
  #headers(target, values) {
    const headers = new Headers(values);
    headers.set("X-Cog-Target", target.trim().replace(/\/+$/, ""));
    return headers;
  }
  /** @template T @param {string} target @param {string} endpoint @param {AbortSignal | undefined} signal @param {string} name @param {(value:unknown)=>value is T} validate */
  async #jsonRequest(target, endpoint, signal, name, validate) {
    const response = await this.#proxyFetch(PROXY_PREFIX + endpoint, {
      headers: this.#headers(target),
      signal,
    });
    if (!response.ok) throw await responseError(response);
    const body = await parseJSONResponse(response);
    if (!validate(body)) throw new Error(`Invalid ${name} response`);
    return body;
  }
  /** @param {string} url @param {RequestInit} init */
  async #proxyFetch(url, init) {
    const response = await fetch(url, { ...init, credentials: "omit", redirect: "manual" });
    if (response.type === "opaqueredirect" || (response.status >= 300 && response.status < 400))
      throw new Error(
        `Target API returned an unexpected redirect (${response.status || "opaque"})`,
      );
    return response;
  }
}
