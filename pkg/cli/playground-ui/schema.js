// Pure OpenAPI-schema helpers (no DOM, no Lit) so they can be unit-reasoned
// and reused across components. These resolve the Cog-flavored OpenAPI 3.0
// document emitted by pkg/schema/openapi.go.

const REF_PREFIX = "#/components/schemas/";

// resolveRef follows a single $ref into components.schemas. Non-refs pass
// through unchanged.
export function resolveRef(root, obj) {
  if (!obj || !obj.$ref) return obj || {};
  if (obj.$ref.indexOf(REF_PREFIX) !== 0) return obj;
  const name = obj.$ref.slice(REF_PREFIX.length);
  const schemas = ((root || {}).components || {}).schemas || {};
  return schemas[name] || obj;
}

// resolveEnum returns the list of allowed values for a property, or null.
//
// Cog does NOT inline `enum` on choice fields. It emits
//   { "x-order": N, "allOf": [{ "$ref": "#/components/schemas/<name>" }] }
// with the actual values living in a sibling component. We follow allOf/$ref
// to recover them (this is the bug the old playground had — it only checked
// prop.enum and rendered choices as plain text).
export function resolveEnum(root, prop) {
  if (Array.isArray(prop.enum)) return prop.enum;
  if (Array.isArray(prop.allOf)) {
    for (const sub of prop.allOf) {
      const resolved = resolveRef(root, sub);
      if (Array.isArray(resolved.enum)) return resolved.enum;
    }
  }
  return null;
}

// effectiveProp unwraps a property to the schema that should drive the widget:
// resolves $ref, and for optional unions (anyOf with a null variant) picks the
// first non-null variant while preserving sibling metadata (default, x-order).
export function effectiveProp(root, prop) {
  let p = resolveRef(root, prop);
  if (Array.isArray(p.anyOf)) {
    for (const variant of p.anyOf) {
      const rv = resolveRef(root, variant);
      if (rv.type !== "null") {
        p = { ...p, ...rv };
        delete p.anyOf;
        break;
      }
    }
  }
  return p;
}

// fieldKind maps a (resolved) property to a widget kind plus the metadata the
// widget needs.
export function fieldKind(root, rawProp) {
  const prop = effectiveProp(root, rawProp);

  const choices = resolveEnum(root, prop);
  if (choices) {
    return { kind: "enum", choices, prop };
  }
  if (prop.type === "string" && prop.format === "uri") {
    return { kind: "file", prop };
  }
  if (prop.type === "string" && (prop.format === "password" || prop["x-cog-secret"])) {
    return { kind: "secret", prop };
  }
  if (prop.type === "string") {
    return { kind: "string", prop };
  }
  if (prop.type === "integer") {
    return { kind: "integer", prop };
  }
  if (prop.type === "number") {
    return { kind: "number", prop };
  }
  if (prop.type === "boolean") {
    return { kind: "boolean", prop };
  }
  if (prop.type === "array") {
    return { kind: "array", items: effectiveProp(root, prop.items || {}), prop };
  }
  // dict[str, V] / Any / object → free-form JSON
  return { kind: "object", prop };
}

// orderedInputs returns the Input properties sorted by x-order, each annotated
// with whether it is required.
export function orderedInputs(inputSchema) {
  const properties = (inputSchema || {}).properties || {};
  const required = (inputSchema || {}).required || [];
  return Object.keys(properties)
    .map((name) => ({
      name,
      prop: properties[name],
      required: required.indexOf(name) >= 0,
      order:
        properties[name] && properties[name]["x-order"] !== undefined
          ? properties[name]["x-order"]
          : 999,
    }))
    .sort((a, b) => a.order - b.order);
}

// defaultInput builds the initial input object from schema defaults.
export function defaultInput(root, inputSchema) {
  const out = {};
  for (const { name, prop } of orderedInputs(inputSchema)) {
    const p = effectiveProp(root, prop);
    if (p.default !== undefined) out[name] = p.default;
  }
  return out;
}

// coerceEnum converts a string <select> value back to the type of the matching
// choice (choices may be integers).
export function coerceEnum(choices, raw) {
  for (const choice of choices) {
    if (String(choice) === raw) return choice;
  }
  return raw;
}
