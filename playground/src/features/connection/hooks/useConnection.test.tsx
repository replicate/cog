import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useConnection } from "@/features/connection/hooks/useConnection";
import type { CogApi } from "@/services/cog";
import type { OpenAPIDocument } from "@/types/openapi";
import { deferred } from "@/test/deferred";

const document: OpenAPIDocument = {
  components: {
    schemas: {
      Input: { properties: { prompt: { type: "string" } } },
      Output: { type: "string" },
    },
  },
  paths: { "/predictions": { post: { "x-cog-streaming": true } } },
};

describe("useConnection", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("loads configuration, health, schema, and prediction capabilities", async () => {
    const api = fakeApi({
      config: vi.fn(async () => ({
        target: " http://localhost:9000 ",
        webhookBase: "https://hooks.example",
        cogVersion: "0.21.1-dev+g12345678",
      })),
      health: vi.fn(async () => ({ status: "healthy" })),
      schema: vi.fn(async () => document),
    });
    const { result } = renderHook(() => useConnection(api));

    await waitFor(() => expect(result.current.schema).toBe(document));

    expect(result.current).toMatchObject({
      target: "http://localhost:9000",
      targetDraft: "http://localhost:9000",
      health: { status: "healthy" },
      schemaError: "",
      webhookBase: "https://hooks.example",
      cogVersion: "0.21.1-dev+g12345678",
      capabilities: {
        endpoint: "/predictions",
        streaming: true,
        input: document.components?.schemas?.Input,
        output: document.components?.schemas?.Output,
      },
    });
    expect(api.setTarget).toHaveBeenCalledWith("http://localhost:9000");
  });

  it("trims manually entered targets and ignores empty targets", async () => {
    const api = fakeApi();
    const { result } = renderHook(() => useConnection(api));
    await waitFor(() => expect(result.current.target).toBe("http://localhost:8393"));

    act(() => result.current.setTargetDraft("  http://localhost:9001/  "));
    act(() => result.current.connect());
    await waitFor(() => expect(result.current.target).toBe("http://localhost:9001/"));
    expect(api.setTarget).toHaveBeenLastCalledWith("http://localhost:9001/");

    act(() => result.current.setTargetDraft("  "));
    act(() => result.current.connect());
    expect(result.current.target).toBe("http://localhost:9001/");
  });

  it("does not replace a manual target when initial configuration arrives late", async () => {
    const config = deferred<{ target: string; webhookBase: string }>();
    const api = fakeApi({ config: vi.fn(() => config.promise) });
    const { result } = renderHook(() => useConnection(api));

    act(() => result.current.setTargetDraft("http://manual.example"));
    act(() => result.current.connect());
    await waitFor(() => expect(api.setTarget).toHaveBeenCalledWith("http://manual.example"));

    config.resolve({
      target: "http://configured.example",
      webhookBase: "https://hooks.example",
    });
    await waitFor(() => expect(result.current.webhookBase).toBe("https://hooks.example"));

    expect(result.current.target).toBe("http://manual.example");
    expect(result.current.targetDraft).toBe("http://manual.example");
    expect(api.setTarget).not.toHaveBeenCalledWith("http://configured.example");
  });

  it("reports failures and retries schema and health requests", async () => {
    vi.useFakeTimers();
    const api = fakeApi({
      config: vi.fn(async () => {
        throw new Error("config unavailable");
      }),
      health: vi.fn(async () => {
        throw new Error("target unavailable");
      }),
      schema: vi.fn(async () => {
        throw new Error("schema unavailable");
      }),
    });
    const { result } = renderHook(() => useConnection(api));

    await flushPromises();
    expect(result.current).toMatchObject({
      target: "http://localhost:8393",
      health: { status: "unreachable", user_healthcheck_error: "target unreachable" },
      schemaError: "Waiting for schema... (schema unavailable)",
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(api.schema).toHaveBeenCalledTimes(2);
    expect(api.health).toHaveBeenCalledTimes(2);
  });
});

type ConnectionApi = Pick<CogApi, "config" | "health" | "schema" | "setTarget">;

function fakeApi(overrides: Partial<ConnectionApi> = {}): ConnectionApi {
  return {
    config: vi.fn(async () => ({})),
    health: vi.fn(async () => ({ status: "healthy" })),
    schema: vi.fn(async () => document),
    setTarget: vi.fn(),
    ...overrides,
  };
}

async function flushPromises(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}
