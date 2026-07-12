import type { OpenAPISchema } from "@/types/openapi";

export function emptyInputValue(schema: OpenAPISchema): unknown {
  if (schema.type === "array") return [];
  if (schema.type === "object" || schema.properties) return {};
  if (schema.type === "boolean") return false;
  return "";
}

export function isDataURI(value: string): boolean {
  return /^data:[^,]*,/i.test(value);
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
