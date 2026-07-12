import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { validateInput } from "@/features/inputs/validation/validateInput";
import type { ValidationIssue } from "@/features/inputs/validation/inputValidation";

const document = { components: { schemas: {} } };
const schema = { type: "object", properties: { prompt: { type: "string" } } };

class FakeWorker {
  static instances: FakeWorker[] = [];

  onerror: ((event: ErrorEvent) => void) | null = null;
  onmessage: ((event: MessageEvent<{ issues: ValidationIssue[] }>) => void) | null = null;
  postMessage = vi.fn();
  terminate = vi.fn();

  constructor() {
    FakeWorker.instances.push(this);
  }

  respond(issues: ValidationIssue[]): void {
    this.onmessage?.({ data: { issues } } as MessageEvent<{ issues: ValidationIssue[] }>);
  }
}

describe("validateInput", () => {
  beforeEach(() => {
    FakeWorker.instances = [];
    vi.stubGlobal("Worker", FakeWorker);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("resolves with the worker result", async () => {
    const value = { prompt: "hello" };
    const result = validateInput(document, schema, value);

    expect(FakeWorker.instances[0].postMessage).toHaveBeenCalledWith({ document, schema, value });
    FakeWorker.instances[0].respond([]);

    await expect(result).resolves.toEqual([]);
    expect(FakeWorker.instances[0].terminate).toHaveBeenCalled();
  });

  it("terminates validation that exceeds the worker deadline", async () => {
    vi.useFakeTimers();
    const result = validateInput(document, schema, { prompt: "hello" });

    vi.advanceTimersByTime(10_000);

    expect(FakeWorker.instances[0].terminate).toHaveBeenCalled();
    await expect(result).resolves.toEqual([
      expect.objectContaining({ keyword: "schema", message: expect.stringContaining("timed out") }),
    ]);
  });

  it("turns synchronous worker posting failures into validation issues", async () => {
    class ThrowingWorker extends FakeWorker {
      override postMessage = vi.fn(() => {
        throw new DOMException("could not clone", "DataCloneError");
      });
    }
    vi.stubGlobal("Worker", ThrowingWorker);
    const failed = validateInput(document, schema, { prompt: "hello" });

    await expect(failed).resolves.toEqual([
      expect.objectContaining({
        keyword: "schema",
        message: expect.stringContaining("could not clone"),
      }),
    ]);
    expect(FakeWorker.instances[0].terminate).toHaveBeenCalled();
  });

  it("terminates validation when canceled", async () => {
    const controller = new AbortController();
    const result = validateInput(document, schema, { prompt: "hello" }, controller.signal);

    controller.abort();

    await expect(result).resolves.toEqual([]);
    expect(FakeWorker.instances[0].terminate).toHaveBeenCalled();
  });
});
