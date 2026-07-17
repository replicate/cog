export type JsonPrimitive = boolean | null | number | string;
export type JsonValue = JsonObject | JsonPrimitive | JsonValue[];
export type JsonObject = { [key: string]: JsonValue };

/** Narrows to a plain JSON object, excluding arrays and null. */
export function isJsonObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** Narrows values that can be represented by JSON. */
export function isJsonValue(value: unknown): value is JsonValue {
  if (typeof value === "number") return Number.isFinite(value);
  if (value === null || ["boolean", "string"].includes(typeof value)) return true;
  if (Array.isArray(value)) return value.every(isJsonValue);
  return isJsonObject(value) && Object.values(value).every(isJsonValue);
}
