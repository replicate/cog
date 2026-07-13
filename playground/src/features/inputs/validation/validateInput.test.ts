import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { disposeValidationWorker, validateInput } from "@/features/inputs/validation/validateInput";
import type { ValidationIssue } from "@/features/inputs/validation/inputValidation";

const document = { components: { schemas: {} } };
const schema = { type: "object", properties: { prompt: { type: "string" } } };

type Request = { requestId: number; schemaId: number; value: unknown };
type Response = { requestId: number; issues: ValidationIssue[] };

class FakeWorker {
  static instances: FakeWorker[] = [];

  onerror: ((event: ErrorEvent) => void) | null = null;
  onmessage: ((event: MessageEvent<Response>) => void) | null = null;
  postMessage = vi.fn();
  terminate = vi.fn();

  constructor() {
    FakeWorker.instances.push(this);
  }

  get lastRequest(): Request {
    return this.postMessage.mock.lastCall?.[0] as Request;
  }

  respond(requestId: number, issues: ValidationIssue[]): void {
    this.onmessage?.({ data: { requestId, issues } } as MessageEvent<Response>);
  }
}

describe("validateInput", () => {
  beforeEach(() => {
    vi.stubGlobal("Worker", FakeWorker);
    disposeValidationWorker();
    FakeWorker.instances = [];
  });

  afterEach(() => {
    disposeValidationWorker();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("resolves with the worker result", async () => {
    const value = { prompt: "hello" };
    const result = validateInput(document, schema, value, 1);

    const worker = FakeWorker.instances[0];
    expect(worker.lastRequest).toMatchObject({ schemaId: 1, document, schema, value });
    worker.respond(worker.lastRequest.requestId, []);

    await expect(result).resolves.toEqual([]);
  });

  it("reuses a single shared worker across runs", async () => {
    const first = validateInput(document, schema, { prompt: "a" }, 1);
    const second = validateInput(document, schema, { prompt: "b" }, 1);

    expect(FakeWorker.instances).toHaveLength(1);
    const worker = FakeWorker.instances[0];
    for (const call of worker.postMessage.mock.calls) worker.respond(call[0].requestId, []);

    await expect(Promise.all([first, second])).resolves.toEqual([[], []]);
  });

  it("resolves with a timeout issue when the worker exceeds the deadline", async () => {
    vi.useFakeTimers();
    const result = validateInput(document, schema, { prompt: "hello" }, 1);

    vi.advanceTimersByTime(10_000);

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
    disposeValidationWorker();
    const failed = validateInput(document, schema, { prompt: "hello" }, 1);

    await expect(failed).resolves.toEqual([
      expect.objectContaining({
        keyword: "schema",
        message: expect.stringContaining("could not clone"),
      }),
    ]);
  });

  it("resolves empty when canceled", async () => {
    const controller = new AbortController();
    const result = validateInput(document, schema, { prompt: "hello" }, 1, controller.signal);

    controller.abort();

    await expect(result).resolves.toEqual([]);
  });
});
