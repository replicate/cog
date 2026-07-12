import { Checkbox } from "@cloudflare/kumo/components/checkbox";
import { Input, InputArea } from "@cloudflare/kumo/components/input";
import { useEffect, useMemo, useState } from "react";

import { fileToDataURI } from "../../api/cog";
import {
  constraintText,
  effectiveSchema,
  enumValues,
  orderedProperties,
} from "../../domain/schema";
import type { OpenAPIDocument, OpenAPISchema } from "../../domain/types";
import { LazyJsonEditor } from "@/editor/LazyJsonEditor";

type Props = {
  document: OpenAPIDocument;
  schema: OpenAPISchema;
  value: Record<string, unknown>;
  onChange: (value: Record<string, unknown>) => void;
  onBusyChange: (busy: boolean) => void;
  onValidityChange: (valid: boolean) => void;
};

export function InputForm({
  document,
  schema,
  value,
  onChange,
  onBusyChange,
  onValidityChange,
}: Props) {
  const properties = useMemo(
    () =>
      orderedProperties(schema).map(
        ([name, property]) => [name, effectiveSchema(document, property)] as const,
      ),
    [document, schema],
  );
  const required = useMemo(() => new Set(schema.required ?? []), [schema.required]);
  const [validity, setValidity] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState<Record<string, boolean>>({});

  const schemaValid = properties.every(([name, property]) => {
    const included = required.has(name) || Object.hasOwn(value, name);
    return !included || validValue(property, value[name], required.has(name));
  });
  useEffect(() => {
    const controlsValid = properties.every(([name]) => {
      const included = required.has(name) || Object.hasOwn(value, name);
      return !included || validity[name] !== false;
    });
    onValidityChange(schemaValid && controlsValid);
  }, [onValidityChange, properties, required, schemaValid, validity, value]);
  useEffect(() => onBusyChange(Object.values(busy).some(Boolean)), [busy, onBusyChange]);

  if (properties.length === 0) return <p className="muted">This model takes no inputs.</p>;

  const setField = (name: string, next: unknown) => onChange({ ...value, [name]: next });
  const setIncluded = (name: string, included: boolean) => {
    if (included) {
      const property = effectiveSchema(document, schema.properties?.[name] ?? {});
      setField(name, property.default ?? emptyValue(property));
      return;
    }
    const next = { ...value };
    delete next[name];
    onChange(next);
  };

  return (
    <div>
      {properties.map(([name, property]) => {
        const included = required.has(name) || Object.hasOwn(value, name);
        return (
          <Field
            key={name}
            document={document}
            name={name}
            schema={property}
            required={required.has(name)}
            included={included}
            value={value[name]}
            onIncludedChange={(next) => setIncluded(name, next)}
            onChange={(next) => setField(name, next)}
            onBusyChange={(next) => setBusy((current) => ({ ...current, [name]: next }))}
            onValidityChange={(next) => setValidity((current) => ({ ...current, [name]: next }))}
          />
        );
      })}
    </div>
  );
}

type FieldProps = {
  document: OpenAPIDocument;
  name: string;
  schema: OpenAPISchema;
  required: boolean;
  included: boolean;
  value: unknown;
  onIncludedChange: (included: boolean) => void;
  onChange: (value: unknown) => void;
  onBusyChange: (busy: boolean) => void;
  onValidityChange: (valid: boolean) => void;
};

function Field(props: FieldProps) {
  const { schema, name, required, included } = props;
  const description = [schema.description, constraintText(schema)].filter(Boolean).join(" · ");
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
      {description && <small className="desc">{description}</small>}
      <div aria-disabled={!included} className={!included ? "field-disabled" : undefined}>
        <FieldControl {...props} disabled={!included} />
      </div>
    </div>
  );
}

function FieldControl(props: FieldProps & { disabled: boolean }) {
  const choices = enumValues(props.document, props.schema);
  const common = { id: `field-${props.name}`, disabled: props.disabled };

  if (choices) {
    return (
      <select
        {...common}
        value={String(props.value ?? "")}
        onChange={(event) => {
          const raw = event.currentTarget.value;
          props.onChange(choices.find((choice) => String(choice) === raw) ?? raw);
        }}
      >
        {!props.required && <option value="">Select a value</option>}
        {choices.map((choice) => (
          <option key={String(choice)} value={String(choice)}>
            {String(choice)}
          </option>
        ))}
      </select>
    );
  }

  if (props.schema.type === "string" && props.schema.format === "uri") {
    return <FileControl {...props} />;
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
          props.onValidityChange(event.currentTarget.checkValidity());
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
  return <StructuredControl {...props} />;
}

function StructuredControl(props: FieldProps & { disabled: boolean }) {
  const initial = props.value === undefined ? emptyValue(props.schema) : props.value;
  const [raw, setRaw] = useState(JSON.stringify(initial, null, 2));
  const [error, setError] = useState("");
  useEffect(() => {
    setRaw(
      JSON.stringify(props.value === undefined ? emptyValue(props.schema) : props.value, null, 2),
    );
    setError("");
  }, [props.schema, props.value]);
  return (
    <>
      <LazyJsonEditor
        value={raw}
        label={`${props.name} JSON`}
        className="ace-field"
        readOnly={props.disabled}
        onChange={(next) => {
          setRaw(next);
          try {
            props.onChange(JSON.parse(next));
            setError("");
            props.onValidityChange(true);
          } catch (parseError) {
            setError(parseError instanceof Error ? parseError.message : "Invalid JSON");
            props.onValidityChange(false);
          }
        }}
      />
      {error && <small className="field-error">Invalid JSON: {error}</small>}
    </>
  );
}

function FileControl(props: FieldProps & { disabled: boolean }) {
  const [fileName, setFileName] = useState("");
  const [error, setError] = useState("");
  const accept = Array.isArray(props.schema["x-cog-accept"])
    ? props.schema["x-cog-accept"].join(",")
    : typeof props.schema["x-cog-accept"] === "string"
      ? props.schema["x-cog-accept"]
      : undefined;
  return (
    <div className="file-widget">
      <input
        id={`field-${props.name}`}
        type="file"
        accept={accept}
        disabled={props.disabled}
        onChange={async (event) => {
          const file = event.currentTarget.files?.[0];
          if (!file) return;
          props.onBusyChange(true);
          setError("");
          try {
            props.onChange(await fileToDataURI(file));
            setFileName(`${file.name} (${formatBytes(file.size)})`);
          } catch (readError) {
            setError(readError instanceof Error ? readError.message : "Could not read file");
          } finally {
            props.onBusyChange(false);
          }
        }}
      />
      {fileName && <span className="file-name">{fileName}</span>}
      <span className="muted">or URL</span>
      <Input
        aria-label={`${props.name} URL`}
        disabled={props.disabled}
        placeholder="https://..."
        value={
          typeof props.value === "string" && !props.value.startsWith("data:") ? props.value : ""
        }
        onInput={(event) => props.onChange(event.currentTarget.value)}
      />
      {error && <small className="field-error">{error}</small>}
      {typeof props.value === "string" && <MediaPreview value={props.value} />}
    </div>
  );
}

function MediaPreview({ value }: { value: string }) {
  if (value.startsWith("data:image/")) {
    return <img className="input-media" src={value} alt="Selected input preview" />;
  }
  if (value.startsWith("data:audio/")) {
    // oxlint-disable-next-line jsx-a11y/media-has-caption -- model inputs do not include caption tracks
    return <audio controls src={value} />;
  }
  if (value.startsWith("data:video/")) {
    // oxlint-disable-next-line jsx-a11y/media-has-caption -- model inputs do not include caption tracks
    return <video controls src={value} />;
  }
  return null;
}

function emptyValue(schema: OpenAPISchema): unknown {
  if (schema.type === "array") return [];
  if (schema.type === "object" || schema.properties) return {};
  if (schema.type === "boolean") return false;
  return "";
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function validValue(schema: OpenAPISchema, value: unknown, required: boolean): boolean {
  if (value === undefined || value === null || value === "") return !required;
  if (typeof value === "string") {
    if (schema.minLength !== undefined && value.length < schema.minLength) return false;
    if (schema.maxLength !== undefined && value.length > schema.maxLength) return false;
  }
  if (typeof value === "number") {
    if (schema.minimum !== undefined && value < schema.minimum) return false;
    if (schema.maximum !== undefined && value > schema.maximum) return false;
  }
  return true;
}
