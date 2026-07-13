import type { OpenAPIDocument, OpenAPISchema } from "@/types/openapi";

const REF_PREFIX = "#/components/schemas/";

/** Resolves a local component-schema reference, leaving unsupported references unchanged. */
export function resolveRef(root: OpenAPIDocument, value: OpenAPISchema): OpenAPISchema {
  if (!value.$ref?.startsWith(REF_PREFIX)) return value;
  return root.components?.schemas?.[value.$ref.slice(REF_PREFIX.length)] ?? value;
}

/** Resolves local references and unwraps a nullable `anyOf` with one non-null branch. */
export function effectiveSchema(root: OpenAPIDocument, value: OpenAPISchema): OpenAPISchema {
  let schema = resolveRef(root, value);
  if (!schema.anyOf) return schema;
  const nonNull = schema.anyOf
    .map((item) => resolveRef(root, item))
    .filter((item) => item.type !== "null");
  if (nonNull.length !== 1) return schema;
  const merged: OpenAPISchema = { ...schema, ...nonNull[0] };
  delete merged.anyOf;
  return merged;
}

/** Finds enum choices declared directly or through a referenced `allOf` branch. */
export function enumValues(root: OpenAPIDocument, value: OpenAPISchema): unknown[] | undefined {
  const schema = effectiveSchema(root, value);
  if (schema.enum) return schema.enum;
  for (const item of schema.allOf ?? []) {
    const resolved = resolveRef(root, item);
    if (resolved.enum) return resolved.enum;
  }
  return undefined;
}

/** Builds initial input from explicit defaults and required enum or Boolean properties. */
export function defaultInput(
  root: OpenAPIDocument,
  schema: OpenAPISchema,
): Record<string, unknown> {
  const result: Record<string, unknown> = {};
  const required = new Set(schema.required ?? []);
  for (const [name, raw] of orderedProperties(schema)) {
    const property = effectiveSchema(root, raw);
    if (property.default !== undefined && property.default !== null) {
      result[name] = property.default;
      continue;
    }
    if (!required.has(name)) continue;
    const choices = enumValues(root, property);
    if (choices?.length) result[name] = choices[0];
    else if (property.type === "boolean") result[name] = false;
  }
  return result;
}

/** Orders object properties by Cog's `x-order` extension, with unspecified entries last. */
export function orderedProperties(schema: OpenAPISchema): [string, OpenAPISchema][] {
  return Object.entries(schema.properties ?? {}).sort(([, left], [, right]) => {
    const leftOrder = typeof left["x-order"] === "number" ? left["x-order"] : 999;
    const rightOrder = typeof right["x-order"] === "number" ? right["x-order"] : 999;
    return leftOrder - rightOrder;
  });
}

/** Formats supported numeric, length, and pattern constraints as field guidance. */
export function constraintText(schema: OpenAPISchema): string {
  const parts: string[] = [];
  if (schema.minimum !== undefined) parts.push(`min ${schema.minimum}`);
  if (schema.maximum !== undefined) parts.push(`max ${schema.maximum}`);
  if (schema.minLength !== undefined) parts.push(`min ${schema.minLength} chars`);
  if (schema.maxLength !== undefined) parts.push(`max ${schema.maxLength} chars`);
  if (schema.pattern) parts.push(`pattern: ${schema.pattern}`);
  return parts.join(" · ");
}

export type PlaygroundCapabilities = {
  endpoint: "/predictions" | "/trainings";
  input: OpenAPISchema;
  streaming: boolean;
  async: boolean;
};

/** Derives the model endpoint, input schema, streaming support, and cancellation capability. */
export function playgroundCapabilities(document: OpenAPIDocument): PlaygroundCapabilities {
  const schemas = document.components?.schemas ?? {};
  const training = Boolean(document.paths?.["/trainings"]);
  const endpoint = training ? "/trainings" : "/predictions";
  const input = resolveRef(document, schemas[training ? "TrainingInput" : "Input"] ?? {});
  const operation = document.paths?.[endpoint]?.post;
  return {
    endpoint,
    input,
    streaming: operation?.["x-cog-streaming"] === true,
    async: Boolean(
      document.paths?.[`${endpoint}/{${training ? "training" : "prediction"}_id}/cancel`],
    ),
  };
}
