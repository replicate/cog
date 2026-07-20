export type ErrorObject = {
  instancePath: string;
  keyword: string;
  message?: string;
  params: Record<string, unknown>;
  schemaPath: string;
};

export type ValidateFunction = {
  (value: unknown): boolean;
  errors?: ErrorObject[] | null;
};

export class Ajv {
  constructor(options?: Record<string, unknown>);
  addFormat(name: string, format: (value: string) => boolean): void;
  compile(schema: unknown): ValidateFunction;
}

export function addFormats(ajv: Ajv): Ajv;

export namespace addFormats {
  function get(name: string): ((value: string) => boolean) | undefined;
}
