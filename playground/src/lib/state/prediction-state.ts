import type {
  JsonValue,
  PredictionApi,
  PredictionEnvelope,
  PredictionRunOptions,
  RequestTrace,
  RunMode,
  TraceEvent,
  TraceEventKind,
  WebhookEvent,
} from "../../types.js";
import { predictionEnvelope } from "../transport/guards.js";
import { isObject } from "../json.js";
import {
  appendBoundedText,
  applyMetric,
  boundedTerminalEnvelope,
  boundedTextItems,
  eventSourceReady,
  MAX_LOG_TEXT,
  MAX_RAW_EVENT_TEXT,
  MAX_RAW_EVENTS,
  MAX_STREAM_OUTPUT_ITEMS,
  MAX_STREAM_OUTPUT_TEXT,
  predictionErrorMessage,
  requestHeaders,
  TERMINAL,
  valueLength,
} from "./runtime.js";
import { appendTraceEvent, boundedTraceData, enforceTraceBudget } from "./trace.js";
import { Store } from "./store.js";

const UPSTREAM_HEADERS = "X-Cog-Upstream-Headers";
type ActiveRun = {
  token: string;
  target: string;
  endpoint: string;
  predictionId: string | undefined;
  abort: AbortController;
  startedAt: number;
  events: EventSource | undefined;
  webhookReceived: boolean;
};
type StreamBuffer = {
  rawEvents: string[];
  rawLength: number;
  output: JsonValue[];
  outputLength: number;
};

export class PredictionState extends Store {
  api: PredictionApi;
  running: boolean;
  envelope: PredictionEnvelope | undefined;
  output: JsonValue | undefined;
  mode: RunMode | undefined;
  rawEvents: string[];
  error: string;
  trace: RequestTrace | undefined;
  active: ActiveRun | undefined;
  traceToken: string | undefined;
  buffer: StreamBuffer;
  destroyed: boolean;

  constructor(api: PredictionApi) {
    super();
    this.api = api;
    this.running = false;
    this.envelope = undefined;
    this.output = undefined;
    this.mode = undefined;
    this.rawEvents = [];
    this.error = "";
    this.trace = undefined;
    this.active = undefined;
    this.traceToken = undefined;
    this.buffer = emptyBuffer();
    this.destroyed = false;
  }

  async run(options: PredictionRunOptions): Promise<void> {
    if (this.active) return;
    const token = crypto.randomUUID();
    const abort = new AbortController();
    this.active = {
      token,
      target: options.target,
      endpoint: options.endpoint,
      predictionId: options.predictionId,
      abort,
      startedAt: performance.now(),
      events: undefined,
      webhookReceived: false,
    };
    this.running = true;
    this.mode = options.mode;
    this.error = "";
    this.envelope = { status: options.mode === "async" ? "starting" : "processing" };
    this.output = options.mode === "stream" ? [] : undefined;
    this.rawEvents = [];
    this.buffer = emptyBuffer();
    this.beginTrace(token, options);
    this.emit();
    try {
      if (options.mode === "stream") await this.runStreaming(token, options, abort.signal);
      else if (options.mode === "async") await this.runAsync(token, options, abort.signal);
      else await this.runSync(token, options, abort.signal);
    } catch (error) {
      if (this.active?.token !== token) return;
      const canceled = error instanceof DOMException && error.name === "AbortError";
      this.error = canceled ? "" : predictionErrorMessage(error);
      this.record(token, "error", canceled ? "Request aborted" : this.error);
      this.envelope = { ...this.envelope, status: canceled ? "canceled" : "failed" };
      this.finish(token);
    }
  }

  async runSync(token: string, options: PredictionRunOptions, signal: AbortSignal): Promise<void> {
    const response = await this.api.submit({
      ...options,
      id: options.predictionId,
      signal,
      onResponse: (value: Response) => this.captureResponse(token, value),
    });
    if (this.active?.token !== token) return;
    this.active.predictionId = response.id;
    this.applyEnvelope(response);
    this.setTraceBody(token, response);
    this.rawEvents = [JSON.stringify(response, null, 2)];
    this.finish(token);
  }

  async runStreaming(
    token: string,
    options: PredictionRunOptions,
    signal: AbortSignal,
  ): Promise<void> {
    let rawFrames: string[] = [];
    let rawFrameLength = 0;
    let terminal = false;
    for await (const event of this.api.stream({
      ...options,
      id: options.predictionId,
      signal,
      onResponse: (value: Response) => this.captureResponse(token, value, "SSE connection opened"),
    })) {
      if (this.active?.token !== token) return;
      rawFrames.push(event.raw);
      rawFrameLength += event.raw.length;
      rawFrameLength = trim(rawFrames, rawFrameLength, MAX_RAW_EVENTS, MAX_RAW_EVENT_TEXT);
      this.record(token, "sse", event.type, event.data);
      if (event.type === "start") {
        this.applyEnvelope(event.data);
        if (typeof event.data.id === "string") this.active.predictionId = event.data.id;
        terminal = TERMINAL.has(String(event.data.status ?? ""));
      } else if (event.type === "output") {
        this.queueStream(token, event.raw, event.data.chunk);
        continue;
      } else if (event.type === "metric") {
        const metrics = applyMetric(
          this.envelope?.metrics,
          String(event.data.name),
          event.data.value,
          event.data.mode,
        );
        this.envelope = { ...this.envelope, metrics };
      } else if (event.type === "log") {
        this.envelope = {
          ...this.envelope,
          logs: appendBoundedText(
            this.envelope?.logs ?? "",
            String(event.data.data ?? event.data.message ?? event.data.value ?? ""),
            MAX_LOG_TEXT,
          ),
        };
      } else if (event.type === "error") {
        this.error = String(event.data.error ?? "Stream error");
        this.envelope = { ...this.envelope, ...event.data, status: "failed" };
        terminal = true;
      } else if (event.type === "completed") {
        this.applyEnvelope(boundedTerminalEnvelope(event.data));
        terminal = true;
      }
      this.queueStream(token, event.raw);
      if (terminal) break;
    }
    this.setTraceBody(token, boundedTextItems(rawFrames, MAX_RAW_EVENT_TEXT).join("\n\n"), false);
    if (!terminal) {
      this.error = "Prediction stream ended before a terminal event";
      this.envelope = { ...this.envelope, status: "failed" };
      this.record(token, "error", this.error);
    }
    this.finish(token);
  }

  async runAsync(token: string, options: PredictionRunOptions, signal: AbortSignal): Promise<void> {
    if (!options.webhookBase) throw new Error("No webhook host is configured");
    const webhookToken = crypto.randomUUID();
    const events = new EventSource(`/events?token=${encodeURIComponent(webhookToken)}`);
    if (this.active?.token !== token) {
      events.close();
      return;
    }
    this.active.events = events;
    await eventSourceReady(events, signal);
    this.record(token, "webhook", "Webhook event stream connected");
    const webhook = `${options.webhookBase}/webhook/${webhookToken}`;
    const filters: WebhookEvent[] = Array.from(
      new Set<WebhookEvent>([...options.webhookEvents, "completed"]),
    );
    this.setTraceRequestBody(token, {
      input: options.input,
      webhook: `${options.webhookBase}/webhook/[redacted]`,
      webhook_events_filter: filters,
    });
    events.onmessage = (event: MessageEvent<string>) => {
      if (this.active?.token !== token) return;
      this.rawEvents = boundedTextItems([...this.rawEvents, event.data], MAX_RAW_EVENT_TEXT);
      try {
        const parsed: unknown = JSON.parse(event.data);
        const next = predictionEnvelope(parsed);
        if (!next) throw new Error("Invalid prediction response");
        this.active.webhookReceived = true;
        this.record(token, "webhook", "Webhook delivery", next);
        this.applyEnvelope(next);
        if (TERMINAL.has(next.status ?? "")) {
          this.active.abort.abort();
          this.finish(token);
        }
      } catch {
        this.record(token, "webhook", "Webhook delivery", event.data);
        this.error = "Received an invalid webhook payload";
        this.envelope = { ...this.envelope, status: "failed" };
        this.active.abort.abort();
        this.finish(token);
      }
    };
    events.onerror = () => {
      if (this.active?.token !== token) return;
      this.error = "Webhook event connection was interrupted";
      this.record(token, "error", "Webhook event connection interrupted");
      this.envelope = { ...this.envelope, status: "failed" };
      this.active.abort.abort();
      this.finish(token);
    };
    const response = await this.api.submit({
      ...options,
      id: options.predictionId,
      async: true,
      webhook,
      webhookEvents: filters,
      signal,
      onResponse: (value: Response) => this.captureResponse(token, value),
    });
    if (this.active?.token !== token) return;
    this.active.predictionId = response.id;
    if (this.active.webhookReceived && !TERMINAL.has(response.status ?? ""))
      this.envelope = { ...response, ...this.envelope };
    else this.applyEnvelope(response);
    this.setTraceBody(token, response);
    if (TERMINAL.has(response.status ?? "")) this.finish(token);
    else this.emit();
  }

  stop(): void {
    const current = this.active;
    if (!current) return;
    current.abort.abort();
    current.events?.close();
    this.record(current.token, "cancel", "Cancellation requested");
    this.envelope = { ...this.envelope, status: "canceled" };
    this.finish(current.token);
    if (current.predictionId)
      void this.api
        .cancel(current.target, current.endpoint, current.predictionId, AbortSignal.timeout(10_000))
        .catch((error: unknown) => {
          if (this.destroyed || this.traceToken !== current.token) return;
          this.error = `Cancellation failed: ${predictionErrorMessage(error)}`;
          this.appendFinished(current, "error", this.error);
          this.emit();
        });
  }

  reset(): void {
    this.active?.abort.abort();
    this.active?.events?.close();
    this.active = undefined;
    this.running = false;
    this.envelope = undefined;
    this.output = undefined;
    this.mode = undefined;
    this.rawEvents = [];
    this.error = "";
    this.trace = undefined;
    this.traceToken = undefined;
    this.buffer = emptyBuffer();
    this.emit();
  }

  destroy(): void {
    this.destroyed = true;
    this.reset();
  }

  finish(token: string): void {
    if (this.active?.token !== token) return;
    this.active.events?.close();
    this.active = undefined;
    this.running = false;
    this.emit();
  }

  applyEnvelope(next: PredictionEnvelope): void {
    this.envelope = { ...this.envelope, ...next };
    if (!next.error && next.output !== undefined) this.output = next.output;
    this.emit();
  }

  queueStream(token: string, raw: string, nextOutput: JsonValue | undefined = undefined): void {
    if (this.active?.token !== token) return;
    const buffer = this.buffer;
    buffer.rawEvents.push(raw);
    buffer.rawLength += raw.length;
    buffer.rawLength = trim(buffer.rawEvents, buffer.rawLength, MAX_RAW_EVENTS, MAX_RAW_EVENT_TEXT);
    this.rawEvents = buffer.rawEvents.slice();
    if (nextOutput !== undefined) {
      buffer.output.push(nextOutput);
      buffer.outputLength += valueLength(nextOutput);
      buffer.outputLength = trim(
        buffer.output,
        buffer.outputLength,
        MAX_STREAM_OUTPUT_ITEMS,
        MAX_STREAM_OUTPUT_TEXT,
      );
      this.output = buffer.output.slice();
    }
    this.emit();
  }

  beginTrace(token: string, options: PredictionRunOptions): void {
    const body = boundedTraceData({ input: options.input });
    this.traceToken = token;
    this.trace = enforceTraceBudget({
      startedAtLabel: new Date().toLocaleTimeString(),
      method: options.predictionId ? "PUT" : "POST",
      endpoint: options.predictionId
        ? `${options.endpoint}/${encodeURIComponent(options.predictionId)}`
        : options.endpoint,
      requestHeaders: requestHeaders(options.mode),
      requestBody: body,
      events: [
        {
          id: crypto.randomUUID(),
          elapsedMs: 0,
          kind: "request",
          label: `${options.predictionId ? "PUT" : "POST"} ${options.endpoint}`,
          data: body,
        },
      ],
    });
  }

  record(token: string, kind: TraceEventKind, label: string, data: unknown = undefined): void {
    if (this.active?.token !== token || !this.trace) return;
    const event: TraceEvent = {
      id: crypto.randomUUID(),
      elapsedMs: performance.now() - this.active.startedAt,
      kind,
      label,
      data: boundedTraceData(data),
    };
    this.trace = enforceTraceBudget({
      ...this.trace,
      events: appendTraceEvent(this.trace.events, event),
    });
  }

  appendFinished(
    run: ActiveRun,
    kind: TraceEventKind,
    label: string,
    data: unknown = undefined,
  ): void {
    if (this.traceToken !== run.token || !this.trace) return;
    this.trace = enforceTraceBudget({
      ...this.trace,
      events: appendTraceEvent(this.trace.events, {
        id: crypto.randomUUID(),
        elapsedMs: performance.now() - run.startedAt,
        kind,
        label,
        data: boundedTraceData(data),
      }),
    });
  }

  captureResponse(token: string, response: Response, label?: string): void {
    if (this.traceToken !== token || !this.trace) return;
    this.record(
      token,
      "response",
      label ? `${label} (HTTP ${response.status})` : `HTTP ${response.status}`,
    );
    this.trace = enforceTraceBudget({
      ...this.trace,
      responseStatus: response.status,
      responseHeaders: modelResponseHeaders(response),
    });
  }

  setTraceBody(token: string, body: unknown, attach = true): void {
    if (this.active?.token !== token || !this.trace) return;
    const bounded = boundedTraceData(body);
    const events = this.trace.events.map((event) => ({ ...event }));
    if (attach)
      for (let index = events.length - 1; index >= 0; index -= 1)
        if (events[index].kind === "response") {
          events[index].data = bounded;
          break;
        }
    this.trace = enforceTraceBudget({ ...this.trace, responseBody: bounded, events });
  }

  setTraceRequestBody(token: string, body: unknown): void {
    if (this.active?.token === token && this.trace)
      this.trace = enforceTraceBudget({ ...this.trace, requestBody: boundedTraceData(body) });
  }
}

function emptyBuffer(): StreamBuffer {
  return { rawEvents: [], rawLength: 0, output: [], outputLength: 0 };
}

function trim<T>(items: T[], length: number, maxItems: number, maxText: number): number {
  while (items.length > 1 && (items.length > maxItems || length > maxText))
    length -= valueLength(items.shift());
  return length;
}

export function modelResponseHeaders(response: Response): RequestTrace["requestHeaders"] {
  const encoded = response.headers.get(UPSTREAM_HEADERS);
  if (!encoded) return {};
  try {
    const base64 = encoded.replaceAll("-", "+").replaceAll("_", "/");
    const padded = base64.padEnd(Math.ceil(base64.length / 4) * 4, "=");
    const bytes = Uint8Array.from(atob(padded), (character) => character.charCodeAt(0));
    const parsed: unknown = JSON.parse(new TextDecoder().decode(bytes));
    if (!isObject(parsed)) return {};
    const headers: [string, string][] = [];
    for (const [name, value] of Object.entries(parsed))
      if (Array.isArray(value) && value.every((item) => typeof item === "string")) {
        const normalized = name.trim().toLowerCase();
        if (normalized && normalized !== UPSTREAM_HEADERS.toLowerCase())
          headers.push([normalized, value.join(", ")]);
      }
    return Object.fromEntries(headers);
  } catch {
    return {};
  }
}
