import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { HttpError } from "../../api/cog";
import type { CogApi } from "../../api/cog";
import { usePrediction } from "./usePrediction";

const options = {
  endpoint: "/predictions",
  input: { prompt: "hello" },
  mode: "sync" as const,
  webhookBase: "http://webhook.example",
  webhookEvents: ["start"],
};

describe("usePrediction", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("records a completed synchronous prediction and request trace", async () => {
    const response = { id: "p1", status: "succeeded", output: "hello" };
    const api = fakeApi({
      submit: vi.fn(async (request: { onResponse?: (response: Response) => void }) => {
        request.onResponse?.(new Response(JSON.stringify(response), { status: 200 }));
        return response;
      }),
    });
    const { result } = renderHook(() => usePrediction(api));

    await act(() => result.current.run(options));

    expect(result.current).toMatchObject({
      running: false,
      output: "hello",
      envelope: { id: "p1", status: "succeeded" },
      rawEvents: ['{\n  "id": "p1",\n  "status": "succeeded",\n  "output": "hello"\n}'],
    });
    expect(result.current.trace).toMatchObject({
      method: "POST",
      endpoint: "/predictions",
      responseBody: { id: "p1", status: "succeeded", output: "hello" },
      events: expect.arrayContaining([
        expect.objectContaining({ kind: "request" }),
        expect.objectContaining({ kind: "response" }),
      ]),
    });
  });

  it("collects stream output, metrics, logs, and raw frames", async () => {
    const api = fakeApi({
      stream: vi.fn(() =>
        eventStream([
          { type: "start", data: { id: "p1", status: "processing" }, raw: "event: start" },
          { type: "output", data: { chunk: "he" }, raw: "event: output\ndata: he" },
          { type: "output", data: { chunk: "llo" }, raw: "event: output\ndata: llo" },
          { type: "metric", data: { name: "predict_time", value: 0.1 }, raw: "event: metric" },
          { type: "log", data: { data: "ready" }, raw: "event: log" },
          { type: "completed", data: { status: "succeeded" }, raw: "event: completed" },
        ]),
      ),
    });
    const { result } = renderHook(() => usePrediction(api));

    await act(() => result.current.run({ ...options, mode: "stream" }));

    expect(result.current).toMatchObject({
      running: false,
      output: ["he", "llo"],
      envelope: { id: "p1", status: "succeeded", metrics: { predict_time: 0.1 }, logs: "ready" },
      rawEvents: [
        "event: start",
        "event: output\ndata: he",
        "event: output\ndata: llo",
        "event: metric",
        "event: log",
        "event: completed",
      ],
    });
    expect(result.current.trace?.responseBody).toBe(
      "event: start\n\nevent: output\ndata: he\n\nevent: output\ndata: llo\n\nevent: metric\n\nevent: log\n\nevent: completed",
    );
  });

  it("bounds the number of retained stream chunks", async () => {
    const events = Array.from({ length: 4100 }, () => ({
      type: "output",
      data: { chunk: "" },
      raw: "event: output",
    }));
    const api = fakeApi({
      stream: vi.fn(() =>
        eventStream([
          { type: "start", data: { id: "p1", status: "processing" }, raw: "event: start" },
          ...events,
          {
            type: "completed",
            data: {
              status: "succeeded",
              output: Array.from({ length: 5000 }, () => "terminal"),
              logs: "x".repeat(1024 * 1024 + 100),
              metrics: Object.fromEntries(
                Array.from({ length: 120 }, (_, index) => [`metric_${index}`, index]),
              ),
            },
            raw: "event: completed",
          },
        ]),
      ),
    });
    const { result } = renderHook(() => usePrediction(api));

    await act(() => result.current.run({ ...options, mode: "stream" }));

    expect(result.current.output).toHaveLength(4096);
    expect(result.current.rawEvents).toHaveLength(1000);
    expect(result.current.envelope?.logs).toHaveLength(1024 * 1024);
    expect(Object.keys(result.current.envelope?.metrics ?? {})).toHaveLength(100);
  });

  it("marks a stream without a terminal event as failed", async () => {
    const api = fakeApi({
      stream: vi.fn(() =>
        eventStream([
          { type: "start", data: { id: "p1", status: "processing" }, raw: "event: start" },
        ]),
      ),
    });
    const { result } = renderHook(() => usePrediction(api));

    await act(() => result.current.run({ ...options, mode: "stream" }));

    expect(result.current.running).toBe(false);
    expect(result.current.envelope?.status).toBe("failed");
    expect(result.current.error).toContain("ended before a terminal event");
  });

  it("surfaces validation errors and marks the run as failed", async () => {
    const api = fakeApi({
      submit: vi.fn().mockRejectedValue(new HttpError("invalid", 422, [{ msg: "required" }])),
    });
    const { result } = renderHook(() => usePrediction(api));

    await act(() => result.current.run(options));

    expect(result.current.error).toBe('{"msg":"required"}');
    expect(result.current.envelope?.status).toBe("failed");
    expect(result.current.trace?.events.at(-1)).toMatchObject({
      kind: "error",
      label: '{"msg":"required"}',
    });
  });

  it("cancels a submitted prediction", async () => {
    let releaseStream: () => void = () => undefined;
    const api = fakeApi({
      stream: vi.fn(() => pendingStream((release) => (releaseStream = release))),
      cancel: vi.fn().mockResolvedValue(undefined),
    });
    const { result } = renderHook(() => usePrediction(api));

    await act(async () => {
      void result.current.run({ ...options, mode: "stream" });
      await Promise.resolve();
    });
    await act(() => result.current.stop());
    releaseStream();

    expect(api.cancel).toHaveBeenCalledWith("/predictions", "p1", expect.any(AbortSignal));
    expect(result.current.envelope?.status).toBe("canceled");
    expect(result.current.running).toBe(false);
  });

  it("finishes locally when the cancellation request hangs", async () => {
    let releaseStream: () => void = () => undefined;
    const api = fakeApi({
      stream: vi.fn(() => pendingStream((release) => (releaseStream = release))),
      cancel: vi.fn(() => new Promise<void>(() => undefined)),
    });
    const { result } = renderHook(() => usePrediction(api));

    await act(async () => {
      void result.current.run({ ...options, mode: "stream" });
      await Promise.resolve();
    });
    act(() => result.current.stop());
    releaseStream();

    expect(result.current.running).toBe(false);
    expect(result.current.envelope?.status).toBe("canceled");
  });

  it("aborts active work when unmounted", async () => {
    let requestSignal: AbortSignal | undefined;
    const api = fakeApi({
      stream: vi.fn((request: { signal: AbortSignal }) => {
        requestSignal = request.signal;
        return pendingStream(() => undefined);
      }),
    });
    const { result, unmount } = renderHook(() => usePrediction(api));

    await act(async () => {
      void result.current.run({ ...options, mode: "stream" });
      await Promise.resolve();
    });
    unmount();

    expect(requestSignal?.aborted).toBe(true);
  });

  it("completes an asynchronous prediction from its webhook event stream", async () => {
    const sources: MockEventSource[] = [];
    vi.stubGlobal(
      "EventSource",
      class extends MockEventSource {
        constructor(url: string) {
          super(url);
          sources.push(this);
          queueMicrotask(() => this.onopen?.(new Event("open")));
        }
      },
    );
    const api = fakeApi({
      submit: vi.fn(async (request: { onResponse?: (response: Response) => void }) => {
        request.onResponse?.(new Response("{}", { status: 201 }));
        return { id: "p1", status: "starting" };
      }),
    });
    const { result } = renderHook(() => usePrediction(api));

    await act(async () => {
      void result.current.run({ ...options, mode: "async" });
      await Promise.resolve();
    });
    expect(sources).toHaveLength(1);
    await act(async () =>
      sources[0].onmessage?.(
        new MessageEvent("message", { data: '{"status":"succeeded","output":"done"}' }),
      ),
    );

    expect(result.current).toMatchObject({
      running: false,
      output: "done",
      envelope: { id: "p1", status: "succeeded", output: "done" },
      rawEvents: ['{"status":"succeeded","output":"done"}'],
    });
    expect(result.current.trace?.requestBody).toMatchObject({
      webhook: expect.stringMatching(/^http:\/\/webhook\.example\/webhook\//),
      webhook_events_filter: ["start", "completed"],
    });
  });

  it("closes an asynchronous event stream when connecting times out", async () => {
    vi.useFakeTimers();
    const sources: MockEventSource[] = [];
    const addAbortListener = vi.spyOn(AbortSignal.prototype, "addEventListener");
    const removeAbortListener = vi.spyOn(AbortSignal.prototype, "removeEventListener");
    vi.stubGlobal(
      "EventSource",
      class extends MockEventSource {
        constructor(url: string) {
          super(url);
          sources.push(this);
        }
      },
    );
    const api = fakeApi({ submit: vi.fn() });
    const { result } = renderHook(() => usePrediction(api));

    try {
      await act(async () => {
        const run = result.current.run({ ...options, mode: "async" });
        await vi.advanceTimersByTimeAsync(5000);
        await run;
      });

      expect(sources[0].close).toHaveBeenCalled();
      const abortListener = addAbortListener.mock.calls.find(([type]) => type === "abort")?.[1];
      expect(abortListener).toBeDefined();
      expect(removeAbortListener).toHaveBeenCalledWith("abort", abortListener);
      expect(api.submit).not.toHaveBeenCalled();
      expect(result.current).toMatchObject({
        running: false,
        error: "Webhook event connection timed out",
        envelope: { status: "failed" },
      });
    } finally {
      addAbortListener.mockRestore();
      removeAbortListener.mockRestore();
      vi.useRealTimers();
    }
  });
});

function fakeApi(overrides: Partial<Record<"submit" | "stream" | "cancel", unknown>>): CogApi {
  return {
    submit: vi.fn().mockResolvedValue({}),
    stream: vi.fn(() => eventStream([])),
    cancel: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  } as unknown as CogApi;
}

async function* eventStream(
  events: { type: string; data: Record<string, unknown>; raw: string }[],
) {
  yield* events;
}

async function* pendingStream(setRelease: (release: () => void) => void) {
  yield { type: "start", data: { id: "p1", status: "processing" }, raw: "event: start" };
  await new Promise<void>((resolve) => setRelease(resolve));
}

class MockEventSource {
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(readonly url: string) {}

  readonly close = vi.fn();
}
