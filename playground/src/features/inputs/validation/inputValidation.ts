import type { ErrorObject, SchemaObject } from "ajv";
import Ajv from "ajv-draft-04";
import addFormats from "ajv-formats";

import type { OpenAPIDocument, OpenAPISchema } from "@/types/openapi";

export type ValidationIssue = {
  field?: string;
  keyword: string;
  message: string;
  path: string;
};

export type InputValidator = (value: unknown) => ValidationIssue[];

const INVALID_URI_TEXT = /[\p{Cc}\s]|%(?![0-9a-f]{2})/iu;
const BASE64_DATA = /^[A-Za-z0-9+/]*={0,2}$/;

/**
 * Normalizes an OpenAPI schema and compiles a synchronous AJV validator that returns
 * field-oriented issues instead of throwing schema or validation errors.
 */
export function createInputValidator(
  document: OpenAPIDocument,
  inputSchema: OpenAPISchema,
): InputValidator {
  const root = normalizeOpenAPISchema({
    ...inputSchema,
    components: document.components,
  }) as SchemaObject;
  const required = Array.isArray(inputSchema.required) ? inputSchema.required : [];
  let validate: ReturnType<Ajv["compile"]>;
  try {
    const ajv = new Ajv({
      allErrors: true,
      formats: { password: true },
      logger: false,
      strictSchema: false,
      strictTypes: false,
    });
    addFormats(ajv);
    ajv.addFormat("uri", validateURI);
    validate = ajv.compile(root);
  } catch (error) {
    const issue = schemaValidationIssue(error);
    return () => [issue];
  }

  return (value) => {
    try {
      const valid = validate(value);
      const issues = valid ? [] : normalizeIssues(validate.errors ?? []);
      return withRequiredEmptyIssues(issues, required, value);
    } catch (error) {
      return [schemaValidationIssue(error)];
    }
  };
}

/**
 * Flags required fields that are present but empty. AJV's `required` keyword only checks for
 * a key's presence, so a blank string (e.g. a cleared text field) would otherwise pass.
 */
function withRequiredEmptyIssues(
  issues: ValidationIssue[],
  required: string[],
  value: unknown,
): ValidationIssue[] {
  if (!isObject(value)) return issues;
  const result = [...issues];
  for (const name of required) {
    if (!isEmptyRequiredValue(value[name])) continue;
    if (result.some((issue) => issue.path === name)) continue;
    result.push({
      field: name,
      keyword: "required",
      message: "This field is required.",
      path: name,
    });
  }
  return result;
}

function isEmptyRequiredValue(value: unknown): boolean {
  if (typeof value === "string") return value.trim() === "";
  return Array.isArray(value) && value.length === 0;
}

function schemaValidationIssue(error: unknown): ValidationIssue {
  return {
    keyword: "schema",
    message: `Cannot validate this OpenAPI schema: ${errorMessage(error)}`,
    path: "input",
  };
}

function normalizeOpenAPISchema(value: unknown): unknown {
  if (!isObject(value) || Array.isArray(value)) return value;

  const normalized = { ...value };
  for (const keyword of ["allOf", "anyOf", "oneOf"] as const) {
    if (Array.isArray(value[keyword])) {
      normalized[keyword] = value[keyword].map(normalizeOpenAPISchema);
    }
  }
  for (const keyword of ["properties", "patternProperties", "definitions"] as const) {
    if (isObject(value[keyword])) {
      normalized[keyword] = Object.fromEntries(
        Object.entries(value[keyword]).map(([key, schema]) => [
          key,
          normalizeOpenAPISchema(schema),
        ]),
      );
    }
  }
  for (const keyword of ["items", "additionalItems", "additionalProperties", "not"] as const) {
    const child = value[keyword];
    if (Array.isArray(child)) {
      normalized[keyword] = child.map(normalizeOpenAPISchema);
    } else if (isObject(child)) {
      normalized[keyword] = normalizeOpenAPISchema(child);
    }
  }
  if (isObject(value.dependencies)) {
    normalized.dependencies = Object.fromEntries(
      Object.entries(value.dependencies).map(([key, dependency]) => [
        key,
        isObject(dependency) && !Array.isArray(dependency)
          ? normalizeOpenAPISchema(dependency)
          : dependency,
      ]),
    );
  }
  if (isObject(value.components) && isObject(value.components.schemas)) {
    normalized.components = {
      ...value.components,
      schemas: Object.fromEntries(
        Object.entries(value.components.schemas).map(([key, schema]) => [
          key,
          normalizeOpenAPISchema(schema),
        ]),
      ),
    };
  }
  if (typeof normalized.pattern === "string") {
    const pattern = normalizePythonPattern(normalized.pattern);
    try {
      new RegExp(pattern, "u");
      normalized.pattern = pattern;
    } catch {
      // Cog accepts Python regex syntax that browsers cannot execute. The
      // server remains authoritative for those patterns.
      delete normalized.pattern;
    }
  }
  if (normalized.nullable !== true) return normalized;

  if (typeof normalized.type === "string") {
    normalized.type = [normalized.type, "null"];
  } else if (Array.isArray(normalized.type) && !normalized.type.includes("null")) {
    normalized.type = [...normalized.type, "null"];
  } else {
    const components = normalized.components;
    delete normalized.components;
    delete normalized.nullable;
    return {
      ...(components ? { components } : {}),
      anyOf: [normalized, { type: "null" }],
    };
  }
  delete normalized.nullable;
  return normalized;
}

function normalizePythonPattern(pattern: string): string {
  return pattern
    .replaceAll(/\(\?P<([A-Za-z_]\w*)>/g, "(?<$1>")
    .replaceAll(/\(\?P=([A-Za-z_]\w*)\)/g, String.raw`\k<$1>`)
    .replaceAll(String.raw`\A`, "^")
    .replaceAll(String.raw`\Z`, "$");
}

function validateURI(value: string): boolean {
  if (value.slice(0, 5).toLowerCase() !== "data:") {
    const standardURI = addFormats.get("uri");
    return typeof standardURI === "function" && standardURI(value);
  }

  const separator = value.indexOf(",", 5);
  if (separator < 0) return false;
  const metadata = value.slice(5, separator);
  if (hasInvalidURIText(metadata)) return false;

  const data = value.slice(separator + 1);
  if (!metadata.toLowerCase().endsWith(";base64")) return !hasInvalidURIText(data);
  try {
    const decoded = decodeURIComponent(data);
    return decoded.length % 4 === 0 && BASE64_DATA.test(decoded);
  } catch {
    return false;
  }
}

function hasInvalidURIText(value: string): boolean {
  return INVALID_URI_TEXT.test(value);
}

function normalizeIssues(errors: ErrorObject[]): ValidationIssue[] {
  const unionErrors = errors.filter(
    (error) => error.keyword === "anyOf" || error.keyword === "oneOf",
  );
  const issues = errors
    .filter(
      (error) =>
        !unionErrors.some(
          (union) => error !== union && error.schemaPath.startsWith(`${union.schemaPath}/`),
        ),
    )
    .map(toIssue);

  return issues.filter(
    (issue, index) =>
      issues.findIndex(
        (candidate) => candidate.path === issue.path && candidate.message === issue.message,
      ) === index,
  );
}

function toIssue(error: ErrorObject): ValidationIssue {
  const segments = pointerSegments(error.instancePath);
  const property = errorProperty(error);
  if (property) segments.push(property);
  const path = formatPath(segments);
  return {
    field: segments[0],
    keyword: error.keyword,
    message: issueMessage(error),
    path,
  };
}

function issueMessage(error: ErrorObject): string {
  if (error.keyword === "required") return "This field is required.";
  if (error.keyword === "additionalProperties") return "This field is not allowed.";
  if (error.keyword === "pattern") return "Does not match the required pattern.";
  if (error.keyword === "anyOf" || error.keyword === "oneOf") {
    return "Must match one of the allowed schemas.";
  }
  return error.message ?? "Invalid value.";
}

function pointerSegments(pointer: string): string[] {
  if (pointer === "#" || pointer === "") return [];
  return pointer
    .replace(/^#\/?/, "")
    .split("/")
    .filter(Boolean)
    .map((segment) => segment.replaceAll("~1", "/").replaceAll("~0", "~"));
}

function errorProperty(error: ErrorObject): string | undefined {
  if (error.keyword === "required" && "missingProperty" in error.params) {
    return String(error.params.missingProperty);
  }
  if (error.keyword === "additionalProperties" && "additionalProperty" in error.params) {
    return String(error.params.additionalProperty);
  }
  return undefined;
}

function formatPath(segments: string[]): string {
  if (segments.length === 0) return "input";
  return segments.reduce(
    (path, segment) =>
      /^\d+$/.test(segment) ? `${path}[${segment}]` : path ? `${path}.${segment}` : segment,
    "",
  );
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
