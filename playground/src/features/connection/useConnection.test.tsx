import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { CogApi } from "../../api/cog";
import type { OpenAPIDocument } from "../../domain/types";
import { useConnection } from "./useConnection";

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

function fakeApi(overrides: Partial<CogApi> = {}): CogApi {
  return {
    config: vi.fn(async () => ({})),
    health: vi.fn(async () => ({ status: "healthy" })),
    schema: vi.fn(async () => document),
    setTarget: vi.fn(),
    ...overrides,
  } as unknown as CogApi;
}

async function flushPromises(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}
