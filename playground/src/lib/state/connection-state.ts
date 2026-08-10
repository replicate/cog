import type {
  ConnectionApi,
  HealthResponse,
  OpenAPIDocument,
  PlaygroundCapabilities,
} from "../../types.js";

import { playgroundCapabilities } from "../input/openapi.js";
import { errorMessage } from "../json.js";
import { Store } from "./store.js";

const FALLBACK_TARGET = "http://localhost:8393";

export class ConnectionState extends Store {
  api: ConnectionApi;
  target: string;
  targetDraft: string;
  targetTouched: boolean;
  webhookBase: string;
  cogVersion: string;
  schema: OpenAPIDocument | undefined;
  schemaError: string;
  health: HealthResponse;
  capabilities: PlaygroundCapabilities | undefined;
  configAbort: AbortController | undefined;
  schemaAbort: AbortController | undefined;
  healthAbort: AbortController | undefined;
  retry: number;
  poll: number;
  hasConnected: boolean;
  destroyed: boolean;

  constructor(api: ConnectionApi) {
    super();
    this.api = api;
    this.target = FALLBACK_TARGET;
    this.targetDraft = FALLBACK_TARGET;
    this.targetTouched = false;
    this.webhookBase = "";
    this.cogVersion = "";
    this.schema = undefined;
    this.schemaError = "";
    this.health = { status: "unknown" };
    this.capabilities = undefined;
    this.configAbort = undefined;
    this.schemaAbort = undefined;
    this.healthAbort = undefined;
    this.retry = 0;
    this.poll = 0;
    this.hasConnected = false;
    this.destroyed = false;
  }

  async start(): Promise<void> {
    if (this.destroyed) return;
    this.configAbort = new AbortController();
    try {
      const signal = AbortSignal.any([this.configAbort.signal, AbortSignal.timeout(10_000)]);
      const config = await this.api.config(signal);
      if (this.destroyed) return;
      this.webhookBase = typeof config.webhookBase === "string" ? config.webhookBase : "";
      this.cogVersion = typeof config.cogVersion === "string" ? config.cogVersion : "";
      if (!this.targetTouched && !this.hasConnected) {
        const target =
          typeof config.target === "string" && config.target.trim()
            ? config.target.trim()
            : FALLBACK_TARGET;
        this.target = target;
        this.targetDraft = target;
      }
    } catch (error) {
      if (this.destroyed) return;
      if (!isAbort(error) && !this.targetTouched && !this.hasConnected) {
        this.target = FALLBACK_TARGET;
        this.targetDraft = FALLBACK_TARGET;
      }
    }
    this.emit();
    if (!this.hasConnected) this.connect(this.targetTouched ? this.targetDraft : this.target);
  }

  setDraft(draft: string): void {
    this.targetTouched = true;
    this.targetDraft = draft;
  }

  connect(value: string = this.targetDraft): void {
    if (this.destroyed) return;
    const target = value.trim();
    if (!target) return;
    this.hasConnected = true;
    this.target = target;
    this.targetDraft = target;
    this.schema = undefined;
    this.capabilities = undefined;
    this.schemaError = "Loading schema...";
    this.health = { status: "unknown" };
    this.stopRequests();
    this.emit();
    this.loadSchema();
    this.pollHealth();
  }

  async loadSchema(): Promise<void> {
    if (this.destroyed) return;
    this.schemaAbort?.abort();
    this.schemaAbort = new AbortController();
    const signal = AbortSignal.any([this.schemaAbort.signal, AbortSignal.timeout(10_000)]);
    const target = this.target;
    try {
      const schema = await this.api.schema(target, signal);
      if (this.destroyed || target !== this.target) return;
      this.schema = schema;
      this.capabilities = playgroundCapabilities(schema);
      this.schemaError = "";
      this.emit();
    } catch (error) {
      if (this.destroyed || isAbort(error) || target !== this.target) return;
      this.schemaError = `Waiting for schema... (${errorMessage(error)})`;
      this.emit();
      this.retry = window.setTimeout(() => this.loadSchema(), 3000);
    }
  }

  async pollHealth(): Promise<void> {
    if (this.destroyed) return;
    this.healthAbort?.abort();
    this.healthAbort = new AbortController();
    const signal = AbortSignal.any([this.healthAbort.signal, AbortSignal.timeout(10_000)]);
    const target = this.target;
    try {
      const health = await this.api.health(target, signal);
      if (this.destroyed || target !== this.target) return;
      this.health = health;
      this.emit();
    } catch (error) {
      if (this.destroyed || target !== this.target) return;
      if (!isAbort(error)) {
        this.health = { status: "unreachable", user_healthcheck_error: "target unreachable" };
        this.emit();
      }
    }
    this.poll = window.setTimeout(() => this.pollHealth(), 5000);
  }

  stopRequests(): void {
    this.schemaAbort?.abort();
    this.healthAbort?.abort();
    clearTimeout(this.retry);
    clearTimeout(this.poll);
  }

  destroy(): void {
    this.destroyed = true;
    this.configAbort?.abort();
    this.stopRequests();
  }
}

function isAbort(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}
