import type { OpenAPISchema } from "@/types/openapi";

/** Returns the schema-type-specific initial value for a newly included optional field. */
export function emptyInputValue(schema: OpenAPISchema): unknown {
  if (schema.type === "array") return [];
  if (schema.type === "object" || schema.properties) return {};
  if (schema.type === "boolean") return false;
  return "";
}

/** Checks whether a string has the basic metadata-and-payload shape of a data URI. */
export function isDataURI(value: string): boolean {
  return /^data:[^,]*,/i.test(value);
}

/** Formats a byte count using compact B, KB, or MB units. */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
