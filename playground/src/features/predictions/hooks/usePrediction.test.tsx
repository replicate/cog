import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { usePrediction } from "@/features/predictions/hooks/usePrediction";
import { HttpError, type CogApi } from "@/services/cog";
import { deferred } from "@/test/deferred";
import type { StreamEvent } from "@/types/prediction";

const options = {
  target: "http://localhost:5000",
  endpoint: "/predictions",
  input: { prompt: "hello" },
  mode: "sync" as const,
  webhookBase: "http://webhook.example",
  webhookEvents: ["start" as const],
};

describe("usePrediction", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("records a completed synchronous prediction and request trace", async () => {
    const response = { id: "p1", status: "succeeded", output: "hello" };
    const api = fakeApi({
      submit: vi.fn(async (request: { onResponse?: (response: Response) => void }) => {
        request.onResponse?.(
          new Response(JSON.stringify(response), {
            status: 200,
            headers: {
              "Content-Type": "application/json",
              "X-Cog-Upstream-Headers": upstreamHeaders(
                Object.fromEntries([
                  ["Content-Type", ["application/json"]],
                  ["X-Frame-Options", ["SAMEORIGIN"]],
                  ["X-Model", ["predictor"]],
                  ["__proto__", ["preserved"]],
                ]),
              ),
              "X-Model": "predictor",
              "X-Frame-Options": "DENY, SAMEORIGIN",
            },
          }),
        );
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
    expect(result.current.trace?.responseHeaders).toEqual(
      Object.fromEntries([
        ["content-type", "application/json"],
        ["x-frame-options", "SAMEORIGIN"],
        ["x-model", "predictor"],
        ["__proto__", "preserved"],
      ]),
    );
  });

  it("recursively bounds large values retained in request traces", async () => {
    const dataURI = `data:application/octet-stream;base64,${"A".repeat(70 * 1024)}`;
    const response = {
      status: "succeeded",
      metadata: { detail: "x".repeat(70 * 1024) },
    };
    const api = fakeApi({ submit: vi.fn().mockResolvedValue(response) });
    const { result } = renderHook(() => usePrediction(api));

    await act(() =>
      result.current.run({ ...options, input: { upload: dataURI, nested: { value: "ok" } } }),
    );

    const requestBody = result.current.trace?.requestBody as {
      input: { upload: string };
    };
    const responseBody = result.current.trace?.responseBody as {
      metadata: { detail: string };
    };
    expect(requestBody.input.upload).toContain("characters omitted");
    expect(requestBody.input.upload).not.toContain("A".repeat(1024));
    expect(responseBody.metadata.detail).toHaveLength(64 * 1024 + "\n... truncated".length);
    expect(responseBody.metadata.detail).toMatch(/\.\.\. truncated$/);
  });

  it("keeps model inputs visible in local request traces", async () => {
    const submit = vi.fn().mockResolvedValue({ status: "succeeded" });
    const api = fakeApi({ submit });
    const { result } = renderHook(() => usePrediction(api));

    await act(() =>
      result.current.run({
        ...options,
        input: { secret: "real value" },
      }),
    );

    expect(submit).toHaveBeenCalledWith(
      expect.objectContaining({ input: { secret: "real value" } }),
    );
    expect(result.current.trace?.requestBody).toEqual({ input: { secret: "real value" } });
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

  it("accumulates increment and append stream metrics", async () => {
    const api = fakeApi({
      stream: vi.fn(() =>
        eventStream([
          { type: "start", data: { id: "p1", status: "processing" }, raw: "event: start" },
          {
            type: "metric",
            data: { name: "tokens", value: 1, mode: "increment" },
            raw: "event: metric",
          },
          {
            type: "metric",
            data: { name: "tokens", value: 3, mode: "increment" },
            raw: "event: metric",
          },
          {
            type: "metric",
            data: { name: "steps", value: "a", mode: "append" },
            raw: "event: metric",
          },
          {
            type: "metric",
            data: { name: "steps", value: "b", mode: "append" },
            raw: "event: metric",
          },
          { type: "completed", data: { status: "succeeded" }, raw: "event: completed" },
        ]),
      ),
    });
    const { result } = renderHook(() => usePrediction(api));

    await act(() => result.current.run({ ...options, mode: "stream" }));

    expect(result.current.envelope?.metrics).toEqual({ tokens: 4, steps: ["a", "b"] });
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

  it("publishes streaming output before the terminal event", async () => {
    let release: () => void = () => undefined;
    const api = fakeApi({
      stream: vi.fn(() => progressiveStream((next) => (release = next))),
    });
    const { result } = renderHook(() => usePrediction(api));
    let run: Promise<void> | undefined;

    act(() => {
      run = result.current.run({ ...options, mode: "stream" });
    });
    await waitFor(() => expect(result.current.output).toEqual(["hello "]));
    expect(result.current.running).toBe(true);

    release();
    await act(async () => run);
    expect(result.current.output).toEqual(["hello ", "world"]);
    expect(result.current.running).toBe(false);
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

    expect(api.cancel).toHaveBeenCalledWith(
      "http://localhost:5000",
      "/predictions",
      "p1",
      expect.any(AbortSignal),
    );
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
    const sources = stubEventSources();
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
      webhook: "http://webhook.example/webhook/[redacted]",
      webhook_events_filter: ["start", "completed"],
    });
  });

  it("aborts a pending async submission after a terminal webhook", async () => {
    const sources = stubEventSources();
    let requestSignal: AbortSignal | undefined;
    const api = fakeApi({
      submit: vi.fn(
        (request: { signal: AbortSignal }) =>
          new Promise<never>((_, reject) => {
            requestSignal = request.signal;
            request.signal.addEventListener(
              "abort",
              () => reject(new DOMException("Aborted", "AbortError")),
              { once: true },
            );
          }),
      ),
    });
    const { result } = renderHook(() => usePrediction(api));
    let run: Promise<void> | undefined;

    act(() => {
      run = result.current.run({ ...options, mode: "async" });
    });
    await waitFor(() => expect(api.submit).toHaveBeenCalled());
    await act(async () =>
      sources[0].onmessage?.(
        new MessageEvent("message", { data: '{"status":"succeeded","output":"done"}' }),
      ),
    );
    await act(async () => run);

    expect(requestSignal?.aborted).toBe(true);
    expect(result.current).toMatchObject({
      running: false,
      error: "",
      envelope: { status: "succeeded", output: "done" },
    });
  });

  it("aborts a pending async submission when the event stream fails", async () => {
    const sources = stubEventSources();
    let requestSignal: AbortSignal | undefined;
    const api = fakeApi({
      submit: vi.fn(
        (request: { signal: AbortSignal }) =>
          new Promise<never>((_, reject) => {
            requestSignal = request.signal;
            request.signal.addEventListener(
              "abort",
              () => reject(new DOMException("Aborted", "AbortError")),
              { once: true },
            );
          }),
      ),
    });
    const { result } = renderHook(() => usePrediction(api));
    let run: Promise<void> | undefined;

    act(() => {
      run = result.current.run({ ...options, mode: "async" });
    });
    await waitFor(() => expect(api.submit).toHaveBeenCalled());
    await act(async () => sources[0].onerror?.(new Event("error")));
    await act(async () => run);

    expect(requestSignal?.aborted).toBe(true);
    expect(result.current).toMatchObject({
      running: false,
      error: "Webhook event connection was interrupted",
      envelope: { status: "failed" },
    });
  });

  it("fails and unblocks after an invalid webhook payload", async () => {
    const sources = stubEventSources();
    const api = fakeApi({
      submit: vi.fn(async () => ({ id: "p1", status: "starting" })),
    });
    const { result } = renderHook(() => usePrediction(api));

    await act(async () => {
      void result.current.run({ ...options, mode: "async" });
      await Promise.resolve();
    });
    await act(async () =>
      sources[0].onmessage?.(new MessageEvent("message", { data: "not-json" })),
    );

    expect(result.current).toMatchObject({
      running: false,
      error: "Received an invalid webhook payload",
      envelope: { status: "failed" },
    });
  });

  it("aborts an active run when reset", async () => {
    let requestSignal: AbortSignal | undefined;
    const api = fakeApi({
      stream: vi.fn((request: { signal: AbortSignal }) => {
        requestSignal = request.signal;
        return pendingStream(() => undefined);
      }),
    });
    const { result } = renderHook(() => usePrediction(api));

    await act(async () => {
      void result.current.run({ ...options, mode: "stream" });
      await Promise.resolve();
    });
    expect(result.current.running).toBe(true);

    act(() => result.current.reset());

    expect(requestSignal?.aborted).toBe(true);
    expect(result.current).toMatchObject({
      running: false,
      envelope: undefined,
      error: "",
    });
  });

  it("does not overwrite newer webhook state with a submit acknowledgement", async () => {
    const sources = stubEventSources();
    const response = deferred<{ id: string; status: string; output: null }>();
    const api = fakeApi({ submit: vi.fn(() => response.promise) });
    const { result } = renderHook(() => usePrediction(api));
    let run: Promise<void> | undefined;

    act(() => {
      run = result.current.run({ ...options, mode: "async" });
    });
    await waitFor(() => expect(api.submit).toHaveBeenCalled());
    await act(async () =>
      sources[0].onmessage?.(
        new MessageEvent("message", { data: '{"status":"processing","output":"webhook"}' }),
      ),
    );
    response.resolve({ id: "p1", status: "starting", output: null });
    await act(async () => run);

    expect(result.current).toMatchObject({
      running: true,
      output: "webhook",
      envelope: { id: "p1", status: "processing", output: "webhook" },
    });

    await act(async () =>
      sources[0].onmessage?.(
        new MessageEvent("message", { data: '{"status":"succeeded","output":"done"}' }),
      ),
    );
    expect(result.current.running).toBe(false);
  });

  it("ignores a late response from a finished run after a new run starts", async () => {
    const sources = stubEventSources();
    const firstResponse = deferred<{ id: string; status: string }>();
    let firstRequest: { onResponse?: (response: Response) => void } | undefined;
    const submit = vi
      .fn()
      .mockImplementationOnce((request: { onResponse?: (response: Response) => void }) => {
        firstRequest = request;
        return firstResponse.promise;
      })
      .mockImplementationOnce(async (request: { onResponse?: (response: Response) => void }) => {
        request.onResponse?.(
          new Response("{}", {
            status: 201,
            headers: {
              "X-Cog-Upstream-Headers": upstreamHeaders({ "X-Prediction": ["second"] }),
              "X-Prediction": "second",
            },
          }),
        );
        return { id: "p2", status: "succeeded", output: "second" };
      });
    const api = fakeApi({ submit });
    const { result } = renderHook(() => usePrediction(api));
    let firstRun: Promise<void> | undefined;

    act(() => {
      firstRun = result.current.run({ ...options, mode: "async" });
    });
    await waitFor(() => expect(firstRequest).toBeDefined());
    await act(async () =>
      sources[0].onmessage?.(new MessageEvent("message", { data: '{"status":"succeeded"}' })),
    );
    await act(() => result.current.run({ ...options, input: { prompt: "second" } }));
    expect(result.current.trace).toMatchObject({
      responseStatus: 201,
      responseHeaders: { "x-prediction": "second" },
    });

    act(() => {
      firstRequest?.onResponse?.(
        new Response("{}", {
          status: 202,
          headers: {
            "X-Cog-Upstream-Headers": upstreamHeaders({ "X-Prediction": ["first"] }),
            "X-Prediction": "first",
          },
        }),
      );
    });
    expect(result.current.trace).toMatchObject({
      responseStatus: 201,
      responseHeaders: { "x-prediction": "second" },
    });

    firstResponse.resolve({ id: "p1", status: "starting" });
    await act(async () => firstRun);
  });

  it("closes an asynchronous event stream when connecting times out", async () => {
    vi.useFakeTimers();
    const sources = stubEventSources({ autoOpen: false });
    const addAbortListener = vi.spyOn(AbortSignal.prototype, "addEventListener");
    const removeAbortListener = vi.spyOn(AbortSignal.prototype, "removeEventListener");
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

  it("closes an asynchronous event stream when stopped during setup", async () => {
    let stop: () => void = () => undefined;
    const sources = stubEventSources({ autoOpen: false, onCreate: () => stop() });
    const api = fakeApi({ submit: vi.fn() });
    const { result } = renderHook(() => usePrediction(api));
    stop = result.current.stop;

    await act(() => result.current.run({ ...options, mode: "async" }));

    expect(sources[0].close).toHaveBeenCalledOnce();
    expect(api.submit).not.toHaveBeenCalled();
    expect(result.current).toMatchObject({
      running: false,
      envelope: { status: "canceled" },
    });
  });
});

type PredictionApi = Pick<CogApi, "cancel" | "stream" | "submit">;

function fakeApi(overrides: Partial<PredictionApi>): PredictionApi {
  return {
    submit: vi.fn().mockResolvedValue({}),
    stream: vi.fn(() => eventStream([])),
    cancel: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}

function upstreamHeaders(headers: Record<string, string[]>): string {
  return btoa(JSON.stringify(headers)).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
}

async function* eventStream(events: StreamEvent[]): AsyncGenerator<StreamEvent> {
  yield* events;
}

async function* pendingStream(
  setRelease: (release: () => void) => void,
): AsyncGenerator<StreamEvent> {
  yield { type: "start", data: { id: "p1", status: "processing" }, raw: "event: start" };
  await new Promise<void>((resolve) => setRelease(resolve));
}

async function* progressiveStream(
  setRelease: (release: () => void) => void,
): AsyncGenerator<StreamEvent> {
  yield { type: "start", data: { id: "p1", status: "processing" }, raw: "event: start" };
  yield { type: "output", data: { chunk: "hello " }, raw: "event: output" };
  await new Promise<void>((resolve) => setRelease(resolve));
  yield { type: "output", data: { chunk: "world" }, raw: "event: output" };
  yield { type: "completed", data: { status: "succeeded" }, raw: "event: completed" };
}

class MockEventSource {
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(readonly url: string) {}

  readonly close = vi.fn();
}

function stubEventSources({
  autoOpen = true,
  onCreate,
}: { autoOpen?: boolean; onCreate?: (source: MockEventSource) => void } = {}): MockEventSource[] {
  const sources: MockEventSource[] = [];
  vi.stubGlobal(
    "EventSource",
    class extends MockEventSource {
      constructor(url: string) {
        super(url);
        sources.push(this);
        onCreate?.(this);
        if (autoOpen) queueMicrotask(() => this.onopen?.(new Event("open")));
      }
    },
  );
  return sources;
}
