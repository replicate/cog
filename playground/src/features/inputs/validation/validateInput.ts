import type { OpenAPIDocument, OpenAPISchema } from "@/types/openapi";
import type { ValidationIssue } from "@/features/inputs/validation/inputValidation";

const VALIDATION_TIMEOUT_MS = 10_000;

type PendingRequest = {
  resolve: (issues: ValidationIssue[]) => void;
  timeout: ReturnType<typeof setTimeout>;
};

type WorkerResponse = { requestId: number; issues: ValidationIssue[] };

// A single long-lived worker is shared across runs. The worker compiles the
// validator once per schemaId and reuses it: a connected model's schema is
// immutable for the life of its server, so it never needs recompiling.
let worker: Worker | undefined;
let nextRequestId = 1;
const pending = new Map<number, PendingRequest>();

function settle(requestId: number, issues: ValidationIssue[]): void {
  const request = pending.get(requestId);
  if (!request) return;
  pending.delete(requestId);
  clearTimeout(request.timeout);
  request.resolve(issues);
}

function ensureWorker(): Worker {
  if (worker) return worker;
  const created = new Worker(new URL("./validation.worker.ts", import.meta.url), {
    type: "module",
  });
  created.onmessage = (event: MessageEvent<WorkerResponse>) => {
    settle(event.data.requestId, event.data.issues);
  };
  created.onerror = (event) => {
    // The worker is unusable; fail everything in flight and rebuild lazily.
    const issue = schemaIssue(event.message);
    for (const requestId of pending.keys()) settle(requestId, [issue]);
    created.terminate();
    if (worker === created) worker = undefined;
  };
  worker = created;
  return created;
}

/**
 * Validates input against a model's schema in the shared worker. `schemaId` identifies the
 * connected schema so the worker compiles its validator once and reuses it across runs.
 */
export function validateInput(
  document: OpenAPIDocument,
  schema: OpenAPISchema,
  value: unknown,
  schemaId: number,
): Promise<ValidationIssue[]> {
  return new Promise((resolve) => {
    let active: Worker;
    try {
      active = ensureWorker();
    } catch (error) {
      resolve([schemaIssue(error)]);
      return;
    }
    const requestId = nextRequestId++;
    const timeout = setTimeout(
      () => settle(requestId, [schemaIssue(new Error("validation timed out"))]),
      VALIDATION_TIMEOUT_MS,
    );
    pending.set(requestId, { resolve, timeout });
    try {
      active.postMessage({ requestId, schemaId, document, schema, value });
    } catch (error) {
      settle(requestId, [schemaIssue(error)]);
    }
  });
}

/** Tears down the shared validation worker, resolving any in-flight requests. */
export function disposeValidationWorker(): void {
  worker?.terminate();
  worker = undefined;
  for (const requestId of pending.keys()) settle(requestId, []);
}

function schemaIssue(error: unknown): ValidationIssue {
  const detail = error instanceof Error ? error.message : String(error);
  return {
    keyword: "schema",
    message: `Cannot validate this OpenAPI schema: ${detail}`,
    path: "input",
  };
}
