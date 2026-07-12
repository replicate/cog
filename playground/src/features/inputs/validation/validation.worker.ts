import type { OpenAPIDocument, OpenAPISchema } from "@/types/openapi";
import { createInputValidator } from "@/features/inputs/validation/inputValidation";

type ValidationRequest = {
  document: OpenAPIDocument;
  schema: OpenAPISchema;
  value: unknown;
};

self.onmessage = (event: MessageEvent<ValidationRequest>) => {
  const { document, schema, value } = event.data;
  self.postMessage({ issues: createInputValidator(document, schema)(value) });
};
