// @ts-check

/** @param {import("../../types").OpenAPISchema} schema */
export function emptyInputValue(schema) {
  if (schema.type === "array") return [];
  if (schema.type === "object" || schema.properties) return {};
  if (schema.type === "boolean") return false;
  return "";
}

/** @param {string} value */
export function isDataURI(value) {
  return /^data:[^,]*,/i.test(value);
}

/** @param {string} value */
export function dataMediaKind(value) {
  if (/^data:image\//i.test(value)) return "image";
  if (/^data:audio\//i.test(value)) return "audio";
  if (/^data:video\//i.test(value)) return "video";
  return undefined;
}

/** @param {number} bytes */
export function formatBytes(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
