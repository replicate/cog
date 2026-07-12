export function parseInputObject(value: string): Record<string, unknown> {
  const parsed: unknown = JSON.parse(value);
  if (!isInputObject(parsed)) throw new Error("Input must be a JSON object");
  return parsed;
}

export function serializeInput(input: Record<string, unknown>): string {
  return JSON.stringify(input, null, 2);
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function isInputObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
