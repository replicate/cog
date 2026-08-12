import type {
  JsonObject,
  JsonValue,
  OpenAPIDocument,
  OpenAPISchema,
  OpenAPISchemaMap,
  PlaygroundCapabilities,
  PlaygroundEndpoint,
} from "../../types.js";

import { emptyInputValue } from "./input.js";

const REF_PREFIX = "#/components/schemas/";
type EndpointDescriptor = {
  endpoint: PlaygroundEndpoint;
  inputSchema: string;
  resource: string;
};

const ENDPOINTS: { prediction: EndpointDescriptor; training: EndpointDescriptor } = {
  prediction: { endpoint: "/predictions", inputSchema: "Input", resource: "prediction" },
  training: { endpoint: "/trainings", inputSchema: "TrainingInput", resource: "training" },
};

export function resolveRef(root: OpenAPIDocument, value: OpenAPISchema): OpenAPISchema {
  if (!value.$ref?.startsWith(REF_PREFIX)) return value;
  return root.components?.schemas?.[value.$ref.slice(REF_PREFIX.length)] ?? value;
}

export function effectiveSchema(root: OpenAPIDocument, value: OpenAPISchema): OpenAPISchema {
  const schema = resolveRef(root, value);
  if (!schema.anyOf) return schema;
  const nonNull = schema.anyOf
    .map((item) => resolveRef(root, item))
    .filter((item) => !hasOnlyType(item, "null"));
  if (nonNull.length !== 1) return schema;
  const merged: OpenAPISchema = { ...schema, ...nonNull[0] };
  delete merged.anyOf;
  return merged;
}

export function enumValues(root: OpenAPIDocument, value: OpenAPISchema): JsonValue[] | undefined {
  const schema = effectiveSchema(root, value);
  if (schema.enum) return schema.enum;
  for (const item of schema.allOf ?? []) {
    const resolved = resolveRef(root, item);
    if (resolved.enum) return resolved.enum;
  }
  return undefined;
}

export function initialInputValue(root: OpenAPIDocument, schema: OpenAPISchema): JsonValue {
  if (Object.hasOwn(schema, "default")) return schema.default ?? null;
  return enumValues(root, schema)?.[0] ?? emptyInputValue(schema);
}

export function defaultInput(root: OpenAPIDocument, schema: OpenAPISchema): JsonObject {
  const result: JsonObject = {};
  const required = new Set(schema.required ?? []);

  for (const [name, raw] of orderedProperties(schema)) {
    const property = effectiveSchema(root, raw);
    if (!required.has(name)) {
      if (Object.hasOwn(property, "default")) continue;
      result[name] = initialInputValue(root, property);
      continue;
    }

    if (property.default !== undefined) {
      result[name] = property.default;
      continue;
    }

    const choices = enumValues(root, property);
    if (choices?.length) result[name] = choices[0];
    else if (hasType(property, "boolean")) result[name] = false;
  }

  return result;
}

export function orderedProperties(schema: OpenAPISchema): [string, OpenAPISchema][] {
  const properties: OpenAPISchemaMap = schema.properties ?? {};
  return Object.entries(properties).sort(
    ([, left], [, right]) =>
      (typeof left["x-order"] === "number" ? left["x-order"] : 999) -
      (typeof right["x-order"] === "number" ? right["x-order"] : 999),
  );
}

export function constraintText(schema: OpenAPISchema): string {
  const parts: string[] = [];
  if (Object.hasOwn(schema, "default"))
    parts.push(
      `default ${typeof schema.default === "string" ? schema.default : JSON.stringify(schema.default)}`,
    );
  if (schema.minimum !== undefined) parts.push(`min ${schema.minimum}`);
  if (schema.maximum !== undefined) parts.push(`max ${schema.maximum}`);
  if (schema.minLength !== undefined) parts.push(`min ${schema.minLength} chars`);
  if (schema.maxLength !== undefined) parts.push(`max ${schema.maxLength} chars`);
  if (schema.pattern) parts.push(`pattern: ${schema.pattern}`);
  return parts.join(" · ");
}

export function playgroundCapabilities(document: OpenAPIDocument): PlaygroundCapabilities {
  const schemas: OpenAPISchemaMap = document.components?.schemas ?? {};
  const mode = document.paths?.["/trainings"] ? "training" : "prediction";
  const descriptor = ENDPOINTS[mode];
  const input = resolveRef(document, schemas[descriptor.inputSchema] ?? {});

  return {
    endpoint: descriptor.endpoint,
    input,
    streaming: document.paths?.[descriptor.endpoint]?.post?.["x-cog-streaming"] === true,
    async: Boolean(document.paths?.[`${descriptor.endpoint}/{${descriptor.resource}_id}/cancel`]),
  };
}

export function hasType(schema: OpenAPISchema, type: string): boolean {
  return schema.type === type || (Array.isArray(schema.type) && schema.type.includes(type));
}

function hasOnlyType(schema: OpenAPISchema, type: string): boolean {
  return (
    schema.type === type ||
    (Array.isArray(schema.type) && schema.type.length === 1 && schema.type[0] === type)
  );
}
