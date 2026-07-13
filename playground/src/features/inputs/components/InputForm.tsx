import { useEffect, useMemo, useState } from "react";

import { InputField } from "@/features/inputs/components/InputField";
import { emptyInputValue } from "@/features/inputs/utils/inputControl";
import type { ValidationIssue } from "@/features/inputs/validation/inputValidation";
import type { OpenAPIDocument, OpenAPISchema } from "@/types/openapi";
import { effectiveSchema, enumValues, orderedProperties } from "@/utils/openapi";

type Props = {
  document: OpenAPIDocument;
  schema: OpenAPISchema;
  value: Record<string, unknown>;
  errors?: readonly ValidationIssue[];
  onChange: (value: Record<string, unknown>) => void;
  onBusyChange: (busy: boolean) => void;
  onValidityChange: (valid: boolean) => void;
};

/** Reports aggregate busy/valid state while adding or removing optional properties from the value. */
export function InputForm({
  document,
  schema,
  value,
  errors = [],
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

  useEffect(() => {
    const controlsValid = properties.every(([name]) => {
      const included = required.has(name) || Object.hasOwn(value, name);
      return !included || validity[name] !== false;
    });
    onValidityChange(controlsValid);
  }, [onValidityChange, properties, required, validity, value]);
  useEffect(() => onBusyChange(Object.values(busy).some(Boolean)), [busy, onBusyChange]);

  if (properties.length === 0) return <p className="muted">This model takes no inputs.</p>;

  const setField = (name: string, next: unknown) => onChange({ ...value, [name]: next });
  const setIncluded = (name: string, included: boolean) => {
    setValidity(({ [name]: _removed, ...current }) => current);
    setBusy(({ [name]: _removed, ...current }) => current);
    if (included) {
      const property = effectiveSchema(document, schema.properties?.[name] ?? {});
      setField(
        name,
        Object.hasOwn(property, "default")
          ? property.default
          : (enumValues(document, property)?.[0] ?? emptyInputValue(property)),
      );
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
          <InputField
            key={name}
            document={document}
            name={name}
            schema={property}
            required={required.has(name)}
            included={included}
            errors={errors.filter((error) => error.field === name)}
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
