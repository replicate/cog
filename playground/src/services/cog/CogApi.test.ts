import { afterEach, describe, expect, it, vi } from "vitest";

import { CogApi } from "@/services/cog/CogApi";
import { parseSSE } from "@/services/cog/sse";

const signal = new AbortController().signal;

describe("CogApi", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("submits asynchronous predictions through the proxy", async () => {
    const fetch = vi.fn().mockResolvedValue(jsonResponse({ id: "p1", status: "starting" }));
    vi.stubGlobal("fetch", fetch);
    const api = new CogApi();
    api.setTarget("http://localhost:5000/");

    await expect(
      api.submit({
        endpoint: "/predictions",
        id: "custom/id",
        input: { prompt: "hello" },
        signal,
        async: true,
        webhook: "http://callback/webhook/token",
        webhookEvents: ["start", "completed"],
      }),
    ).resolves.toMatchObject({ id: "p1" });

    const [url, request] = fetch.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/proxy/predictions/custom%2Fid");
    expect(request.method).toBe("PUT");
    expect(new Headers(request.headers).get("X-Cog-Target")).toBe("http://localhost:5000");
    expect(new Headers(request.headers).get("Prefer")).toBe("respond-async");
    expect(JSON.parse(String(request.body))).toEqual({
      input: { prompt: "hello" },
      webhook: "http://callback/webhook/token",
      webhook_events_filter: ["start", "completed"],
    });
  });

  it.each([
    [jsonResponse({ error: "model unavailable" }, 503), "model unavailable"],
    [jsonResponse({ detail: "bad input" }, 422), "bad input"],
    [new Response("proxy failed", { status: 502 }), "proxy failed"],
    [new Response(null, { status: 500 }), "HTTP 500"],
  ])("preserves useful HTTP errors", async (response, message) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
    const api = new CogApi();

    await expect(api.health()).rejects.toMatchObject({ status: response.status, message });
  });

  it("preserves validation details", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(jsonResponse({ detail: [{ loc: ["input"] }] }, 422)),
    );
    const api = new CogApi();

    await expect(api.schema()).rejects.toMatchObject({ detail: [{ loc: ["input"] }] });
  });

  it("parses chunked CRLF SSE frames and preserves raw frames", async () => {
    const body = streamBody([
      "event: start\r",
      '\ndata: {"id":"p1"}\r\n\r',
      '\nevent: output\r\ndata: {"chunk":"hello"}\r\n\r\nevent: completed\r\ndata: {"status":"succeeded"}',
    ]);
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(body, { headers: { "Content-Type": "text/event-stream" } }),
        ),
    );
    const api = new CogApi();
    const events = [];

    for await (const event of api.stream({ endpoint: "/predictions", input: {}, signal }))
      events.push(event);

    expect(events).toEqual([
      { type: "start", data: { id: "p1" }, raw: 'event: start\r\ndata: {"id":"p1"}' },
      {
        type: "output",
        data: { chunk: "hello" },
        raw: 'event: output\r\ndata: {"chunk":"hello"}',
      },
      {
        type: "completed",
        data: { status: "succeeded" },
        raw: 'event: completed\r\ndata: {"status":"succeeded"}',
      },
    ]);
  });

  it("rejects failed and bodyless streams", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("busy", { status: 409 })));
    const api = new CogApi();
    await expect(
      collect(api.stream({ endpoint: "/predictions", input: {}, signal })),
    ).rejects.toMatchObject({
      status: 409,
      message: "busy",
    });

    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, body: null }));
    await expect(
      collect(api.stream({ endpoint: "/predictions", input: {}, signal })),
    ).rejects.toThrow("Streaming response has no body");
  });

  it("rejects oversized SSE events", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(streamBody([`event: output\ndata: ${"x".repeat(1024 * 1024)}`])),
        ),
    );
    const api = new CogApi();

    await expect(
      collect(api.stream({ endpoint: "/predictions", input: {}, signal })),
    ).rejects.toThrow("SSE event is too large");
  });

  it("rejects oversized JSON responses before reading them", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response("{}", {
          headers: { "Content-Length": String(16 * 1024 * 1024 + 1) },
        }),
      ),
    );
    const api = new CogApi();

    await expect(api.schema()).rejects.toThrow("Response body exceeds 16777216 bytes");
  });
});

describe("parseSSE", () => {
  it("joins non-JSON data lines", () => {
    expect(parseSSE("event: log\ndata: first\ndata: second")).toMatchObject({
      type: "log",
      data: { value: "first\nsecond" },
    });
  });

  it("preserves primitive JSON data", () => {
    expect(parseSSE("event: output\ndata: 42")).toMatchObject({
      type: "output",
      data: { value: 42 },
    });
  });

  it("ignores frames without an event type", () => {
    expect(parseSSE("data: no-event")).toBeUndefined();
  });
});

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function streamBody(chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  return new ReadableStream({
    start(controller) {
      chunks.forEach((chunk) => controller.enqueue(encoder.encode(chunk)));
      controller.close();
    },
  });
}

async function collect<T>(stream: AsyncIterable<T>): Promise<T[]> {
  const events: T[] = [];
  for await (const event of stream) events.push(event);
  return events;
}
