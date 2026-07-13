import type { JsonEditorProps } from "@/components/editor/JsonEditor";

export function FakeJsonEditor({
  label,
  onChange,
  value,
  describedBy,
  disabled,
  invalid,
  readOnly,
  autoHeight,
  followTail,
  active,
}: JsonEditorProps) {
  if (onChange) {
    return (
      <textarea
        aria-label={label}
        aria-describedby={describedBy}
        aria-invalid={invalid}
        disabled={disabled}
        readOnly={readOnly}
        value={value}
        onChange={(event) => onChange(event.currentTarget.value)}
      />
    );
  }
  return (
    <pre
      aria-label={label}
      aria-readonly={readOnly}
      data-auto-height={autoHeight}
      data-follow-tail={followTail}
      data-active={active}
    >
      {value}
    </pre>
  );
}
