import type { ErrorObject, ValidateFunction } from "ajv";
import Ajv from "ajv-draft-04";
import addFormats from "ajv-formats";

import type {
  InputValidator,
  OpenAPIDocument,
  OpenAPISchema,
  ValidationIssue,
} from "../../types.js";
import { isJsonObject } from "../json.js";

const INVALID_URI_TEXT = /[\p{Cc}\s]|%(?![0-9a-f]{2})/iu;
const BASE64_DATA = /^[A-Za-z0-9+/]*={0,2}$/;
// RFC 3986 URI (absolute, scheme required). This preserves ajv-formats behavior
// for normal URIs while allowing the large data URIs used for file inputs.
const URI =
  /^(?:[a-z][a-z0-9+\-.]*:)(?:\/?\/(?:(?:[a-z0-9\-._~!$&'()*+,;=:]|%[0-9a-f]{2})*@)?(?:\[(?:(?:(?:(?:[0-9a-f]{1,4}:){6}|::(?:[0-9a-f]{1,4}:){5}|(?:[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){4}|(?:(?:[0-9a-f]{1,4}:){0,1}[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){3}|(?:(?:[0-9a-f]{1,4}:){0,2}[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){2}|(?:(?:[0-9a-f]{1,4}:){0,3}[0-9a-f]{1,4})?::[0-9a-f]{1,4}:|(?:(?:[0-9a-f]{1,4}:){0,4}[0-9a-f]{1,4})?::)(?:[0-9a-f]{1,4}:[0-9a-f]{1,4}|(?:(?:25[0-5]|2[0-4]\d|[01]?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d?\d))|(?:(?:[0-9a-f]{1,4}:){0,5}[0-9a-f]{1,4})?::[0-9a-f]{1,4}|(?:(?:[0-9a-f]{1,4}:){0,6}[0-9a-f]{1,4})?::)|[Vv][0-9a-f]+\.[a-z0-9\-._~!$&'()*+,;=:]+)\]|(?:(?:25[0-5]|2[0-4]\d|[01]?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d?\d)|(?:[a-z0-9\-._~!$&'()*+,;=]|%[0-9a-f]{2})*)(?::\d*)?(?:\/(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})*)*|\/(?:(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})+(?:\/(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})*)*)?|(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})+(?:\/(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})*)*)(?:\?(?:[a-z0-9\-._~!$&'()*+,;=:@/?]|%[0-9a-f]{2})*)?(?:#(?:[a-z0-9\-._~!$&'()*+,;=:@/?]|%[0-9a-f]{2})*)?$/i;

export function createInputValidator(
  document: OpenAPIDocument,
  inputSchema: OpenAPISchema,
): InputValidator {
  const root = normalizeOpenAPISchema({
    ...inputSchema,
    components: document.components,
  });
  const required = Array.isArray(inputSchema.required) ? inputSchema.required : [];
  let validate: ValidateFunction<unknown>;
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
    validate = ajv.compile<unknown>(root);
  } catch (error) {
    const issue = schemaValidationIssue(error);
    return () => [issue];
  }
  return (value: unknown): ValidationIssue[] => {
    try {
      const valid = validate(value);
      const issues = valid ? [] : normalizeIssues(validate.errors ?? []);
      return withRequiredEmptyIssues(issues, required, value);
    } catch (error) {
      return [schemaValidationIssue(error)];
    }
  };
}

function withRequiredEmptyIssues(
  issues: ValidationIssue[],
  required: string[],
  value: unknown,
): ValidationIssue[] {
  if (!isJsonObject(value)) return issues;
  const result = [...issues];
  for (const name of required) {
    const item = value[name];
    const empty =
      typeof item === "string" ? item.trim() === "" : Array.isArray(item) && item.length === 0;
    if (empty && !result.some((issue) => issue.path === name))
      result.push({
        field: name,
        keyword: "required",
        message: "This field is required.",
        path: name,
      });
  }
  return result;
}

function schemaValidationIssue(error: unknown): ValidationIssue {
  return {
    keyword: "schema",
    message: `Cannot validate this OpenAPI schema: ${error instanceof Error ? error.message : String(error)}`,
    path: "input",
  };
}

export function normalizeOpenAPISchema(value: OpenAPISchema): OpenAPISchema {
  const normalized: OpenAPISchema = { ...value };
  for (const keyword of ["allOf", "anyOf", "oneOf"] as const)
    if (Array.isArray(value[keyword]))
      normalized[keyword] = value[keyword].map(normalizeOpenAPISchema);
  for (const keyword of ["properties", "patternProperties", "definitions"] as const)
    if (value[keyword])
      normalized[keyword] = Object.fromEntries(
        Object.entries(value[keyword]).map(([key, schema]) => [
          key,
          normalizeOpenAPISchema(schema),
        ]),
      );
  const items = value.items;
  if (Array.isArray(items)) normalized.items = items.map(normalizeOpenAPISchema);
  else if (isJsonObject(items)) normalized.items = normalizeOpenAPISchema(items);
  for (const keyword of ["additionalItems", "additionalProperties", "not"] as const) {
    const child = value[keyword];
    if (isJsonObject(child)) normalized[keyword] = normalizeOpenAPISchema(child);
  }
  if (isJsonObject(value.dependencies))
    normalized.dependencies = Object.fromEntries(
      Object.entries(value.dependencies).map(([key, dependency]) => [
        key,
        isJsonObject(dependency) ? normalizeOpenAPISchema(dependency) : dependency,
      ]),
    );
  if (isJsonObject(value.components) && isJsonObject(value.components.schemas))
    normalized.components = {
      ...value.components,
      schemas: Object.fromEntries(
        Object.entries(value.components.schemas).map(([key, schema]) => [
          key,
          normalizeOpenAPISchema(schema),
        ]),
      ),
    };
  if (typeof normalized.pattern === "string") {
    const pattern = normalizePythonPattern(normalized.pattern);
    try {
      new RegExp(pattern, "u");
      normalized.pattern = pattern;
    } catch {
      delete normalized.pattern;
    }
  }
  if (normalized.nullable !== true) return normalized;
  if (typeof normalized.type === "string") normalized.type = [normalized.type, "null"];
  else if (Array.isArray(normalized.type) && !normalized.type.includes("null"))
    normalized.type = [...normalized.type, "null"];
  else {
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

export function normalizePythonPattern(pattern: string): string {
  return pattern
    .replaceAll(/\(\?P<([A-Za-z_]\w*)>/g, "(?<$1>")
    .replaceAll(/\(\?P=([A-Za-z_]\w*)\)/g, String.raw`\k<$1>`)
    .replaceAll(String.raw`\A`, "^")
    .replaceAll(String.raw`\Z`, "$");
}

export function validateURI(value: string): boolean {
  if (value.slice(0, 5).toLowerCase() !== "data:") {
    return URI.test(value);
  }
  const separator = value.indexOf(",", 5);
  if (separator < 0) return false;
  const metadata = value.slice(5, separator);
  if (INVALID_URI_TEXT.test(metadata)) return false;
  const data = value.slice(separator + 1);
  if (!metadata.toLowerCase().endsWith(";base64")) return !INVALID_URI_TEXT.test(data);
  try {
    const decoded = decodeURIComponent(data);
    return decoded.length % 4 === 0 && BASE64_DATA.test(decoded);
  } catch {
    return false;
  }
}

function normalizeIssues(errors: ErrorObject[]): ValidationIssue[] {
  const unions = errors.filter((error) => error.keyword === "anyOf" || error.keyword === "oneOf");
  const issues = errors
    .filter(
      (error) =>
        !unions.some(
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
  const property = issueProperty(error);
  if (property !== undefined) segments.push(String(property));

  const path =
    segments.reduce((result, segment) => appendPathSegment(result, segment), "") || "input";
  const message = issueMessage(error);
  return { field: segments[0], keyword: error.keyword, message, path };
}

function appendPathSegment(path: string, segment: string): string {
  if (/^\d+$/.test(segment)) return `${path}[${segment}]`;
  return path ? `${path}.${segment}` : segment;
}

function issueProperty(error: ErrorObject): unknown {
  if (error.keyword === "required") return error.params.missingProperty;
  if (error.keyword === "additionalProperties") return error.params.additionalProperty;
  return undefined;
}

function issueMessage(error: ErrorObject): string {
  switch (error.keyword) {
    case "required":
      return "This field is required.";
    case "additionalProperties":
      return "This field is not allowed.";
    case "pattern":
      return "Does not match the required pattern.";
    case "anyOf":
    case "oneOf":
      return "Must match one of the allowed schemas.";
    default:
      return error.message ?? "Invalid value.";
  }
}

function pointerSegments(pointer: string): string[] {
  return pointer === "#" || pointer === ""
    ? []
    : pointer
        .replace(/^#\/?/, "")
        .split("/")
        .filter(Boolean)
        .map((segment) => segment.replaceAll("~1", "/").replaceAll("~0", "~"));
}
