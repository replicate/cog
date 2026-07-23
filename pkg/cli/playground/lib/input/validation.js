// @ts-check

import { isJsonObject } from "../json.js";

const INVALID_URI_TEXT = /[\p{Cc}\s]|%(?![0-9a-f]{2})/iu;
const BASE64_DATA = /^[A-Za-z0-9+/]*={0,2}$/;
// RFC 3986 URI (absolute, scheme required). Inlined from ajv-formats' "uri"
// format so non-data URIs validate identically now that ajv-formats is no
// longer bundled.
const URI =
  /^(?:[a-z][a-z0-9+\-.]*:)(?:\/?\/(?:(?:[a-z0-9\-._~!$&'()*+,;=:]|%[0-9a-f]{2})*@)?(?:\[(?:(?:(?:(?:[0-9a-f]{1,4}:){6}|::(?:[0-9a-f]{1,4}:){5}|(?:[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){4}|(?:(?:[0-9a-f]{1,4}:){0,1}[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){3}|(?:(?:[0-9a-f]{1,4}:){0,2}[0-9a-f]{1,4})?::(?:[0-9a-f]{1,4}:){2}|(?:(?:[0-9a-f]{1,4}:){0,3}[0-9a-f]{1,4})?::[0-9a-f]{1,4}:|(?:(?:[0-9a-f]{1,4}:){0,4}[0-9a-f]{1,4})?::)(?:[0-9a-f]{1,4}:[0-9a-f]{1,4}|(?:(?:25[0-5]|2[0-4]\d|[01]?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d?\d))|(?:(?:[0-9a-f]{1,4}:){0,5}[0-9a-f]{1,4})?::[0-9a-f]{1,4}|(?:(?:[0-9a-f]{1,4}:){0,6}[0-9a-f]{1,4})?::)|[Vv][0-9a-f]+\.[a-z0-9\-._~!$&'()*+,;=:]+)\]|(?:(?:25[0-5]|2[0-4]\d|[01]?\d?\d)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d?\d)|(?:[a-z0-9\-._~!$&'()*+,;=]|%[0-9a-f]{2})*)(?::\d*)?(?:\/(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})*)*|\/(?:(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})+(?:\/(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})*)*)?|(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})+(?:\/(?:[a-z0-9\-._~!$&'()*+,;=:@]|%[0-9a-f]{2})*)*)(?:\?(?:[a-z0-9\-._~!$&'()*+,;=:@/?]|%[0-9a-f]{2})*)?(?:#(?:[a-z0-9\-._~!$&'()*+,;=:@/?]|%[0-9a-f]{2})*)?$/i;

/** @param {typeof import("../../cdn/ajv").Ajv} Ajv @param {import("../../types").OpenAPIDocument} document @param {import("../../types").OpenAPISchema} inputSchema */
export function createInputValidator(Ajv, document, inputSchema) {
  const root = normalizeOpenAPISchema({
    ...inputSchema,
    components: document.components,
  });
  const required = Array.isArray(inputSchema.required)
    ? inputSchema.required
    : [];
  let validate;
  try {
    const ajv = new Ajv({
      allErrors: true,
      formats: { password: true },
      logger: false,
      strictSchema: false,
      strictTypes: false,
    });
    ajv.addFormat("uri", validateURI);
    validate = ajv.compile(root);
  } catch (error) {
    const issue = schemaValidationIssue(error);
    return () => [issue];
  }
  return (/** @type {unknown} */ value) => {
    try {
      const valid = validate(value);
      const issues = valid ? [] : normalizeIssues(validate.errors ?? []);
      return withRequiredEmptyIssues(issues, required, value);
    } catch (error) {
      return [schemaValidationIssue(error)];
    }
  };
}

/** @param {import("../../types").ValidationIssue[]} issues @param {string[]} required @param {unknown} value */
function withRequiredEmptyIssues(issues, required, value) {
  if (!isJsonObject(value)) return issues;
  const result = [...issues];
  for (const name of required) {
    const item = value[name];
    const empty =
      typeof item === "string"
        ? item.trim() === ""
        : Array.isArray(item) && item.length === 0;
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

/** @param {unknown} error @returns {import("../../types").ValidationIssue} */
function schemaValidationIssue(error) {
  return {
    keyword: "schema",
    message: `Cannot validate this OpenAPI schema: ${error instanceof Error ? error.message : String(error)}`,
    path: "input",
  };
}

/** @param {unknown} value @returns {unknown} */
export function normalizeOpenAPISchema(value) {
  if (!isJsonObject(value)) return value;
  /** @type {Record<string, unknown>} */
  const normalized = { ...value };
  for (const keyword of ["allOf", "anyOf", "oneOf"])
    if (Array.isArray(value[keyword]))
      normalized[keyword] = value[keyword].map(normalizeOpenAPISchema);
  for (const keyword of ["properties", "patternProperties", "definitions"])
    if (isJsonObject(value[keyword]))
      normalized[keyword] = Object.fromEntries(
        Object.entries(value[keyword]).map(([key, schema]) => [
          key,
          normalizeOpenAPISchema(schema),
        ]),
      );
  for (const keyword of [
    "items",
    "additionalItems",
    "additionalProperties",
    "not",
  ]) {
    const child = value[keyword];
    if (Array.isArray(child))
      normalized[keyword] = child.map(normalizeOpenAPISchema);
    else if (isJsonObject(child))
      normalized[keyword] = normalizeOpenAPISchema(child);
  }
  if (isJsonObject(value.dependencies))
    normalized.dependencies = Object.fromEntries(
      Object.entries(value.dependencies).map(([key, dependency]) => [
        key,
        isJsonObject(dependency)
          ? normalizeOpenAPISchema(dependency)
          : dependency,
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
  for (const [exclusive, bound] of [
    ["exclusiveMinimum", "minimum"],
    ["exclusiveMaximum", "maximum"],
  ]) {
    if (typeof normalized[exclusive] !== "boolean") continue;
    if (normalized[exclusive] === true && typeof normalized[bound] === "number")
      normalized[exclusive] = normalized[bound];
    else delete normalized[exclusive];
  }
  if (normalized.nullable !== true) return normalized;
  if (typeof normalized.type === "string")
    normalized.type = [normalized.type, "null"];
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

/** @param {string} pattern */
export function normalizePythonPattern(pattern) {
  return pattern
    .replaceAll(/\(\?P<([A-Za-z_]\w*)>/g, "(?<$1>")
    .replaceAll(/\(\?P=([A-Za-z_]\w*)\)/g, String.raw`\k<$1>`)
    .replaceAll(String.raw`\A`, "^")
    .replaceAll(String.raw`\Z`, "$");
}

/** @param {string} value */
export function validateURI(value) {
  if (value.slice(0, 5).toLowerCase() !== "data:") {
    return URI.test(value);
  }
  const separator = value.indexOf(",", 5);
  if (separator < 0) return false;
  const metadata = value.slice(5, separator);
  if (INVALID_URI_TEXT.test(metadata)) return false;
  const data = value.slice(separator + 1);
  if (!metadata.toLowerCase().endsWith(";base64"))
    return !INVALID_URI_TEXT.test(data);
  try {
    const decoded = decodeURIComponent(data);
    return decoded.length % 4 === 0 && BASE64_DATA.test(decoded);
  } catch {
    return false;
  }
}

/** @param {import("../../cdn/ajv").ErrorObject[]} errors */
function normalizeIssues(errors) {
  const unions = errors.filter(
    (error) => error.keyword === "anyOf" || error.keyword === "oneOf",
  );
  const issues = errors
    .filter(
      (error) =>
        !unions.some(
          (union) =>
            error !== union &&
            error.schemaPath.startsWith(`${union.schemaPath}/`),
        ),
    )
    .map(toIssue);
  return issues.filter(
    (issue, index) =>
      issues.findIndex(
        (candidate) =>
          candidate.path === issue.path && candidate.message === issue.message,
      ) === index,
  );
}

/** @param {import("../../cdn/ajv").ErrorObject} error @returns {import("../../types").ValidationIssue} */
function toIssue(error) {
  const segments = pointerSegments(error.instancePath);
  const property =
    error.keyword === "required"
      ? error.params.missingProperty
      : error.keyword === "additionalProperties"
        ? error.params.additionalProperty
        : undefined;
  if (property !== undefined) segments.push(String(property));
  const path =
    segments.reduce(
      (result, segment) =>
        /^\d+$/.test(segment)
          ? `${result}[${segment}]`
          : result
            ? `${result}.${segment}`
            : segment,
      "",
    ) || "input";
  const message =
    error.keyword === "required"
      ? "This field is required."
      : error.keyword === "additionalProperties"
        ? "This field is not allowed."
        : error.keyword === "pattern"
          ? "Does not match the required pattern."
          : error.keyword === "anyOf" || error.keyword === "oneOf"
            ? "Must match one of the allowed schemas."
            : (error.message ?? "Invalid value.");
  return { field: segments[0], keyword: error.keyword, message, path };
}

/** @param {string} pointer */
function pointerSegments(pointer) {
  return pointer === "#" || pointer === ""
    ? []
    : pointer
        .replace(/^#\/?/, "")
        .split("/")
        .filter(Boolean)
        .map((segment) => segment.replaceAll("~1", "/").replaceAll("~0", "~"));
}
