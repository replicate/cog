import type { JsonObject } from "@/types/json";

export type OpenAPISchema = {
  $ref?: string;
  type?: string;
  format?: string;
  title?: string;
  description?: string;
  default?: unknown;
  enum?: unknown[];
  allOf?: OpenAPISchema[];
  anyOf?: OpenAPISchema[];
  oneOf?: OpenAPISchema[];
  items?: OpenAPISchema;
  properties?: Record<string, OpenAPISchema>;
  additionalProperties?: boolean | OpenAPISchema;
  required?: string[];
  nullable?: boolean;
  minimum?: number;
  maximum?: number;
  exclusiveMinimum?: boolean;
  exclusiveMaximum?: boolean;
  multipleOf?: number;
  minLength?: number;
  maxLength?: number;
  pattern?: string;
  minItems?: number;
  maxItems?: number;
  uniqueItems?: boolean;
  minProperties?: number;
  maxProperties?: number;
  deprecated?: boolean;
  [key: `x-${string}`]: unknown;
};

export type OpenAPIDocument = {
  components?: { schemas?: Record<string, OpenAPISchema> };
  paths?: Record<string, OpenAPIPathItem>;
};

export type OpenAPIOperation = JsonObject & { "x-cog-streaming"?: boolean };
export type OpenAPIPathItem = Record<string, OpenAPIOperation>;
