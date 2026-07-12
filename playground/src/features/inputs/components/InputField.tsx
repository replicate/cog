import { Checkbox } from "@cloudflare/kumo/components/checkbox";
import { Input, InputArea } from "@cloudflare/kumo/components/input";

import { FileInputControl } from "@/features/inputs/components/FileInputControl";
import { StructuredInputControl } from "@/features/inputs/components/StructuredInputControl";
import type { ValidationIssue } from "@/features/inputs/validation/inputValidation";
import type { OpenAPIDocument, OpenAPISchema } from "@/types/openapi";
import { constraintText, enumValues } from "@/utils/openapi";

export type InputFieldProps = {
  document: OpenAPIDocument;
  name: string;
  schema: OpenAPISchema;
  required: boolean;
  included: boolean;
  errors: readonly ValidationIssue[];
  value: unknown;
  onIncludedChange: (included: boolean) => void;
  onChange: (value: unknown) => void;
  onBusyChange: (busy: boolean) => void;
  onValidityChange: (valid: boolean) => void;
};

export type InputControlProps = InputFieldProps & { disabled: boolean };

/** Keeps omitted optional fields inert and delegates included values to a schema-specific control. */
export function InputField(props: InputFieldProps) {
  const { schema, name, required, included } = props;
  const description = [schema.description, constraintText(schema)].filter(Boolean).join(" · ");
  const descriptionId = `field-${name}-description`;
  const validationId = `field-${name}-validation`;
  return (
    <div className="field">
      <div className="field-heading">
        {!required && (
          <Checkbox
            aria-label={`Include optional field ${name}`}
            checked={included}
            onCheckedChange={(checked) => props.onIncludedChange(checked === true)}
          />
        )}
        <label htmlFor={`field-${name}`}>
          {name}
          {required && <span className="req"> *</span>}
          {schema.deprecated && <span className="deprecated-tag"> (deprecated)</span>}
        </label>
      </div>
      {description && (
        <small id={descriptionId} className="desc">
          {description}
        </small>
      )}
      <div
        inert={!included}
        aria-disabled={!included}
        className={!included ? "field-disabled" : undefined}
      >
        <InputControl {...props} disabled={!included} />
      </div>
      {props.errors.length > 0 && (
        <ul id={validationId} className="field-errors">
          {props.errors.map((error) => (
            <li key={`${error.path}:${error.keyword}:${error.message}`}>{error.message}</li>
          ))}
        </ul>
      )}
    </div>
  );
}

function InputControl(props: InputControlProps) {
  const choices = enumValues(props.document, props.schema);
  const describedBy =
    props.schema.description || constraintText(props.schema)
      ? `field-${props.name}-description`
      : undefined;
  const validationId = props.errors.length > 0 ? `field-${props.name}-validation` : undefined;
  const common = {
    id: `field-${props.name}`,
    disabled: props.disabled,
    required: props.required,
    "aria-describedby": [describedBy, validationId].filter(Boolean).join(" ") || undefined,
    "aria-invalid": props.errors.length > 0 || undefined,
  };

  if (choices) {
    return (
      <select
        {...common}
        value={String(choices.findIndex((choice) => Object.is(choice, props.value)))}
        onChange={(event) => props.onChange(choices[Number(event.currentTarget.value)])}
      >
        {!props.required && <option value="-1">Select a value</option>}
        {choices.map((choice, index) => (
          <option key={index} value={String(index)}>
            {String(choice)}
          </option>
        ))}
      </select>
    );
  }
  if (props.schema.type === "string" && props.schema.format === "uri") {
    return <FileInputControl {...props} />;
  }
  if (props.schema.type === "string" && props.schema.format === "password") {
    return (
      <Input
        {...common}
        type="password"
        aria-label={props.name}
        value={String(props.value ?? "")}
        onInput={(event) => props.onChange(event.currentTarget.value)}
      />
    );
  }
  if (props.schema.type === "string") {
    return (
      <InputArea
        {...common}
        aria-label={props.name}
        value={String(props.value ?? "")}
        onInput={(event) => props.onChange(event.currentTarget.value)}
      />
    );
  }
  if (props.schema.type === "integer" || props.schema.type === "number") {
    return (
      <Input
        {...common}
        type="number"
        aria-label={props.name}
        min={props.schema.minimum}
        max={props.schema.maximum}
        step={props.schema.type === "integer" ? 1 : "any"}
        value={props.value === undefined ? "" : String(props.value)}
        onInput={(event) => {
          const raw = event.currentTarget.value;
          props.onChange(raw === "" ? "" : Number(raw));
        }}
      />
    );
  }
  if (props.schema.type === "boolean") {
    return (
      <select
        {...common}
        value={String(props.value ?? false)}
        onChange={(event) => props.onChange(event.currentTarget.value === "true")}
      >
        <option value="true">true</option>
        <option value="false">false</option>
      </select>
    );
  }
  return <StructuredInputControl {...props} />;
}
