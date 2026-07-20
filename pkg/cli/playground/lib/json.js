// @ts-check

/** @param {unknown} value @returns {value is import("../types").JsonObject} */
export function isJsonObject(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** @param {unknown} value @returns {value is import("../types").JsonValue} */
export function isJsonValue(value) {
  if (typeof value === "number") return Number.isFinite(value);
  if (value === null || typeof value === "boolean" || typeof value === "string") return true;
  if (Array.isArray(value)) return value.every(isJsonValue);
  return isJsonObject(value) && Object.values(value).every(isJsonValue);
}

/** @param {string} value @returns {import("../types").JsonObject} */
export function parseInputObject(value) {
  const parsed = JSON.parse(value);
  if (!isJsonObject(parsed)) throw new Error("Prediction input must be a JSON object.");
  return parsed;
}

/** @param {import("../types").JsonObject} value */
export function serializeInput(value) {
  return JSON.stringify(value, null, 2);
}

/** @param {unknown} error */
export function errorMessage(error) {
  return error instanceof Error ? error.message : String(error);
}
