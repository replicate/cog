import type { JsonValue, OpenAPISchema } from "../../types.js";

export type DataMediaKind = "audio" | "image" | "video";

export function emptyInputValue(schema: OpenAPISchema): JsonValue {
  if (schema.type === "array") return [];
  if (schema.type === "object" || schema.properties) return {};
  if (schema.type === "boolean") return false;
  return "";
}

export function isDataURI(value: string): boolean {
  return /^data:[^,]*,/i.test(value);
}

export function dataMediaKind(value: string): DataMediaKind | undefined {
  if (/^data:image\//i.test(value)) return "image";
  if (/^data:audio\//i.test(value)) return "audio";
  if (/^data:video\//i.test(value)) return "video";
  return undefined;
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
