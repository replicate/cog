import type { OpenAPIDocument, OpenAPISchema } from "@/types/openapi";
import {
  createInputValidator,
  type InputValidator,
  type ValidationIssue,
} from "@/features/inputs/validation/inputValidation";

type ValidationRequest = {
  requestId: number;
  schemaId: number;
  document: OpenAPIDocument;
  schema: OpenAPISchema;
  value: unknown;
};

// Compile the validator once per schema and reuse it: a connected model's
// schema is immutable, so subsequent runs only re-run the cached validator.
let cached: { schemaId: number; validate: InputValidator } | undefined;

self.onmessage = (event: MessageEvent<ValidationRequest>) => {
  const { requestId, schemaId, document, schema, value } = event.data;
  try {
    if (!cached || cached.schemaId !== schemaId) {
      cached = { schemaId, validate: createInputValidator(document, schema) };
    }
    self.postMessage({ requestId, issues: cached.validate(value) });
  } catch (error) {
    cached = undefined;
    const detail = error instanceof Error ? error.message : String(error);
    const issue: ValidationIssue = {
      keyword: "schema",
      message: `Cannot validate this OpenAPI schema: ${detail}`,
      path: "input",
    };
    self.postMessage({ requestId, issues: [issue] });
  }
};
