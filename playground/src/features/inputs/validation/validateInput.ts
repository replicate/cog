import type { OpenAPIDocument, OpenAPISchema } from "@/types/openapi";
import type { ValidationIssue } from "@/features/inputs/validation/inputValidation";

const VALIDATION_TIMEOUT_MS = 10_000;

export function validateInput(
  document: OpenAPIDocument,
  schema: OpenAPISchema,
  value: unknown,
  signal?: AbortSignal,
): Promise<ValidationIssue[]> {
  return new Promise((resolve) => {
    if (signal?.aborted) {
      resolve([]);
      return;
    }
    let active = true;
    let worker: Worker;
    try {
      worker = new Worker(new URL("./validation.worker.ts", import.meta.url), { type: "module" });
    } catch (error) {
      resolve([schemaIssue(error)]);
      return;
    }

    const finish = (issues: ValidationIssue[]) => {
      if (!active) return;
      active = false;
      window.clearTimeout(timeout);
      signal?.removeEventListener("abort", abort);
      worker.terminate();
      resolve(issues);
    };
    const abort = () => finish([]);
    const timeout = window.setTimeout(() => {
      finish([schemaIssue(new Error("validation timed out"))]);
    }, VALIDATION_TIMEOUT_MS);
    signal?.addEventListener("abort", abort, { once: true });
    worker.onmessage = (event: MessageEvent<{ issues: ValidationIssue[] }>) => {
      finish(event.data.issues);
    };
    worker.onerror = (event) => {
      finish([schemaIssue(event.message)]);
    };
    try {
      worker.postMessage({ document, schema, value });
    } catch (error) {
      finish([schemaIssue(error)]);
    }
  });
}

function schemaIssue(error: unknown): ValidationIssue {
  const detail = error instanceof Error ? error.message : String(error);
  return {
    keyword: "schema",
    message: `Cannot validate this OpenAPI schema: ${detail}`,
    path: "input",
  };
}
