// @ts-check

import { Ajv } from "../cdn/ajv.js";
import { createInputValidator } from "../lib/input/validation.js";

let schemaId = -1;
/** @type {((value: unknown) => import("../types").ValidationIssue[]) | undefined} */
let validator;

self.onmessage = (event) => {
  const { id, document, input, inputSchema, nextSchemaId } = event.data;
  try {
    if (!validator || schemaId !== nextSchemaId) {
      validator = createInputValidator(Ajv, document, inputSchema);
      schemaId = nextSchemaId;
    }
    self.postMessage({ id, issues: validator(input) });
  } catch (error) {
    validator = undefined;
    self.postMessage({
      id,
      issues: [
        {
          keyword: "schema",
          message: `Cannot validate this OpenAPI schema: ${error instanceof Error ? error.message : String(error)}`,
          path: "input",
        },
      ],
    });
  }
};
