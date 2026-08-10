import type { JsonObject, JsonValue, UnknownObject } from "../types.js";

export function isObject(value: unknown): value is UnknownObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function isJsonObject(value: unknown): value is JsonObject {
  return isObject(value) && Object.values(value).every(isJsonValue);
}

export function isJsonValue(value: unknown): value is JsonValue {
  if (typeof value === "number") return Number.isFinite(value);
  if (value === null || typeof value === "boolean" || typeof value === "string") return true;
  if (Array.isArray(value)) return value.every(isJsonValue);
  return isJsonObject(value);
}

export function parseInputObject(value: string): JsonObject {
  const parsed: unknown = JSON.parse(value);
  if (!isJsonObject(parsed)) throw new Error("Prediction input must be a JSON object.");
  return parsed;
}

export function serializeInput(value: JsonObject): string {
  const serialized = JSON.stringify(value, null, 2);
  if (serialized === undefined) throw new Error("Could not serialize prediction input.");
  return serialized;
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
