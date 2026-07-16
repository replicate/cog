import { isJsonObject } from "@/types/json";

export type HealthResponse = {
  status?: string;
  user_healthcheck_error?: string;
  setup?: { status?: string; logs?: string };
  version?: { coglet?: string; python_sdk?: string; python?: string };
};

/** Validates the health response fields the playground consumes. */
export function isHealthResponse(value: unknown): value is HealthResponse {
  return (
    isJsonObject(value) &&
    isOptionalString(value.status) &&
    isOptionalString(value.user_healthcheck_error) &&
    isOptionalObject(value.setup, isSetup) &&
    isOptionalObject(value.version, isVersion)
  );
}

function isOptionalString(value: unknown): boolean {
  return value === undefined || typeof value === "string";
}

function isOptionalObject<T>(value: unknown, validate: (value: unknown) => value is T): boolean {
  return value === undefined || validate(value);
}

function isSetup(value: unknown): value is NonNullable<HealthResponse["setup"]> {
  return isJsonObject(value) && isOptionalString(value.status) && isOptionalString(value.logs);
}

function isVersion(value: unknown): value is NonNullable<HealthResponse["version"]> {
  return (
    isJsonObject(value) &&
    isOptionalString(value.coglet) &&
    isOptionalString(value.python_sdk) &&
    isOptionalString(value.python)
  );
}
