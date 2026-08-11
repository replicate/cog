import type {
  InputValidator,
  ValidationIssue,
  ValidationRequest,
  ValidationResponse,
} from "../types.js";

import { createInputValidator } from "../lib/input/validation.js";
import { isObject } from "../lib/json.js";
import { isOpenAPIDocument, isOpenAPISchema } from "../lib/transport/guards.js";

const workerScope = self as DedicatedWorkerGlobalScope;
let schemaId = -1;
let validator: InputValidator | undefined;

workerScope.onmessage = (event: MessageEvent<unknown>): void => {
  const request = validationRequest(event.data);
  if (!request) {
    const id = isObject(event.data) && typeof event.data.id === "number" ? event.data.id : -1;
    postIssues(id, [
      {
        keyword: "schema",
        message: "Cannot validate this OpenAPI schema: invalid worker request",
        path: "input",
      },
    ]);
    return;
  }
  try {
    if (!validator || schemaId !== request.nextSchemaId) {
      validator = createInputValidator(request.document, request.inputSchema);
      schemaId = request.nextSchemaId;
    }
    postIssues(request.id, validator(request.input));
  } catch (error) {
    validator = undefined;
    postIssues(request.id, [
      {
        keyword: "schema",
        message: `Cannot validate this OpenAPI schema: ${error instanceof Error ? error.message : String(error)}`,
        path: "input",
      },
    ]);
  }
};

function validationRequest(value: unknown): ValidationRequest | undefined {
  if (
    !isObject(value) ||
    typeof value.id !== "number" ||
    typeof value.nextSchemaId !== "number" ||
    !isOpenAPIDocument(value.document) ||
    !isOpenAPISchema(value.inputSchema)
  )
    return undefined;
  return {
    id: value.id,
    nextSchemaId: value.nextSchemaId,
    document: value.document,
    inputSchema: value.inputSchema,
    input: value.input,
  };
}

function postIssues(id: number, issues: ValidationIssue[]): void {
  const response: ValidationResponse = { id, issues };
  workerScope.postMessage(response);
}
