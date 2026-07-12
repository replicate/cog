/** Parses JSON and requires its root value to be a non-array object. */
export function parseInputObject(value: string): Record<string, unknown> {
  const parsed: unknown = JSON.parse(value);
  if (!isInputObject(parsed)) throw new Error("Input must be a JSON object");
  return parsed;
}

/** Returns the canonical pretty-printed JSON representation used by the input editor. */
export function serializeInput(input: Record<string, unknown>): string {
  return JSON.stringify(input, null, 2);
}

/** Preserves `Error.message` and stringifies non-Error thrown values. */
export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function isInputObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
