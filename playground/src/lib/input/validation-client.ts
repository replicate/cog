import type {
  OpenAPIDocument,
  OpenAPISchema,
  ValidationIssue,
  ValidationRequest,
  ValidationResponse,
} from "../../types.js";

import { isObject } from "../json.js";

const TIMEOUT = 10_000;
type PendingValidation = {
  resolve: (issues: ValidationIssue[]) => void;
  timeout: number;
};

let worker: Worker | undefined;
let requestId = 0;
const pending = new Map<number, PendingValidation>();

export function validateInput(
  schemaId: number,
  document: OpenAPIDocument,
  inputSchema: OpenAPISchema,
  input: unknown,
): Promise<ValidationIssue[]> {
  return new Promise<ValidationIssue[]>((resolve) => {
    const id = ++requestId;
    try {
      const target = ensureWorker();
      const timeout = window.setTimeout(
        () => disposeValidationWorker("validation timed out"),
        TIMEOUT,
      );
      pending.set(id, { resolve, timeout });
      const request: ValidationRequest = {
        id,
        nextSchemaId: schemaId,
        document,
        inputSchema,
        input,
      };
      target.postMessage(request);
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      const issues = validationFailure(reason);
      if (pending.has(id)) settle(id, issues);
      else resolve(issues);
    }
  });
}

function ensureWorker(): Worker {
  if (worker) return worker;
  const target = new Worker(new URL("../../worker/validation.js", import.meta.url), {
    type: "module",
  });
  worker = target;
  target.onmessage = (event: MessageEvent<unknown>) => {
    if (!isValidationResponse(event.data)) {
      if (worker === target) disposeValidationWorker("validation worker returned invalid data");
      return;
    }
    settle(event.data.id, event.data.issues);
  };
  target.onerror = () => {
    if (worker === target) disposeValidationWorker("validation worker failed");
  };
  return target;
}

function settle(id: number, issues: ValidationIssue[]): void {
  const request = pending.get(id);
  if (!request) return;
  pending.delete(id);
  clearTimeout(request.timeout);
  request.resolve(issues);
}

export function disposeValidationWorker(reason = "validation worker disposed"): void {
  worker?.terminate();
  worker = undefined;
  for (const id of pending.keys()) settle(id, validationFailure(reason));
}

function validationFailure(reason: string): ValidationIssue[] {
  return [
    {
      keyword: "schema",
      message: `Cannot validate this OpenAPI schema: ${reason}`,
      path: "input",
    },
  ];
}

function isValidationResponse(value: unknown): value is ValidationResponse {
  return (
    isObject(value) &&
    typeof value.id === "number" &&
    Array.isArray(value.issues) &&
    value.issues.every(isValidationIssue)
  );
}

function isValidationIssue(value: unknown): value is ValidationIssue {
  return (
    isObject(value) &&
    (value.field === undefined || typeof value.field === "string") &&
    typeof value.keyword === "string" &&
    typeof value.message === "string" &&
    typeof value.path === "string"
  );
}
