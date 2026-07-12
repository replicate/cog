import { useEffect, useState } from "react";

import { LazyJsonEditor } from "@/components/editor/LazyJsonEditor";
import type { InputControlProps } from "@/features/inputs/components/InputField";
import { emptyInputValue } from "@/features/inputs/utils/inputControl";
import { constraintText } from "@/utils/openapi";

/** Retains invalid JSON locally and updates the form value only after parsing succeeds. */
export function StructuredInputControl(props: InputControlProps) {
  const initial = props.value === undefined ? emptyInputValue(props.schema) : props.value;
  const [raw, setRaw] = useState(JSON.stringify(initial, null, 2));
  const [error, setError] = useState("");
  const descriptionId =
    props.schema.description || constraintText(props.schema)
      ? `field-${props.name}-description`
      : undefined;
  const errorId = `field-${props.name}-error`;
  const validationId = props.errors.length > 0 ? `field-${props.name}-validation` : undefined;

  useEffect(() => {
    setRaw(
      JSON.stringify(
        props.value === undefined ? emptyInputValue(props.schema) : props.value,
        null,
        2,
      ),
    );
    setError("");
  }, [props.schema, props.value]);

  return (
    <>
      <LazyJsonEditor
        value={raw}
        label={`${props.name} JSON`}
        className="json-field"
        readOnly={props.disabled}
        disabled={props.disabled}
        invalid={Boolean(error) || props.errors.length > 0}
        describedBy={
          [descriptionId, validationId, error ? errorId : undefined].filter(Boolean).join(" ") ||
          undefined
        }
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
      {error && (
        <small id={errorId} className="field-error" role="alert">
          Invalid JSON: {error}
        </small>
      )}
    </>
  );
}
