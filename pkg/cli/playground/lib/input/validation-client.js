// @ts-check

const TIMEOUT = 10_000;
/** @type {Worker | undefined} */
let worker;
let requestId = 0;
/** @type {Map<number, {resolve:(issues:import("../../types").ValidationIssue[])=>void, timeout:number}>} */
const pending = new Map();

/** @param {number} schemaId @param {import("../../types").OpenAPIDocument} document @param {import("../../types").OpenAPISchema} inputSchema @param {unknown} input */
export function validateInput(schemaId, document, inputSchema, input) {
  return new Promise((resolve) => {
    const id = ++requestId;
    try {
      const target = ensureWorker();
      const timeout = window.setTimeout(
        () => disposeValidationWorker("validation timed out"),
        TIMEOUT,
      );
      pending.set(id, { resolve, timeout });
      target.postMessage({ id, nextSchemaId: schemaId, document, inputSchema, input });
    } catch (error) {
      const reason = error instanceof Error ? error.message : String(error);
      if (pending.has(id))
        settle(id, [
          {
            keyword: "schema",
            message: `Cannot validate this OpenAPI schema: ${reason}`,
            path: "input",
          },
        ]);
      else
        resolve([
          {
            keyword: "schema",
            message: `Cannot validate this OpenAPI schema: ${reason}`,
            path: "input",
          },
        ]);
    }
  });
}

function ensureWorker() {
  if (worker) return worker;
  const target = new Worker("/worker/validation.js", { type: "module" });
  worker = target;
  target.onmessage = (event) => settle(event.data.id, event.data.issues);
  target.onerror = () => {
    if (worker === target) disposeValidationWorker("validation worker failed");
  };
  return target;
}

/** @param {number} id @param {import("../../types").ValidationIssue[]} issues */
function settle(id, issues) {
  const request = pending.get(id);
  if (!request) return;
  pending.delete(id);
  clearTimeout(request.timeout);
  request.resolve(issues);
}

/** @param {string} [reason] */
export function disposeValidationWorker(reason = "validation worker disposed") {
  worker?.terminate();
  worker = undefined;
  for (const id of pending.keys())
    settle(id, [
      {
        keyword: "schema",
        message: `Cannot validate this OpenAPI schema: ${reason}`,
        path: "input",
      },
    ]);
}
