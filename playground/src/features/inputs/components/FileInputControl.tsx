import { Input } from "@cloudflare/kumo/components/input";
import { useEffect, useEffectEvent, useRef, useState } from "react";

import type { InputControlProps } from "@/features/inputs/components/InputField";
import { formatBytes, isDataURI } from "@/features/inputs/utils/inputControl";
import { fileToDataURI } from "@/services/cog";
import { constraintText } from "@/utils/openapi";

/**
 * Accepts uploaded files or URLs, reports loading and validity, and previews supported media
 * after converting local files to data URIs.
 */
export function FileInputControl(props: InputControlProps) {
  const { disabled } = props;
  const [fileName, setFileName] = useState("");
  const [error, setError] = useState("");
  const readToken = useRef(0);
  const descriptionId =
    props.schema.description || constraintText(props.schema)
      ? `field-${props.name}-description`
      : undefined;
  const errorId = `field-${props.name}-error`;
  const validationId = props.errors.length > 0 ? `field-${props.name}-validation` : undefined;
  const describedBy =
    [descriptionId, validationId, error ? errorId : undefined].filter(Boolean).join(" ") ||
    undefined;
  const clearBusy = useEffectEvent(() => props.onBusyChange(false));

  useEffect(() => {
    if (!disabled) return;
    readToken.current += 1;
    clearBusy();
  }, [disabled]);
  useEffect(
    () => () => {
      readToken.current += 1;
    },
    [],
  );

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
        disabled={disabled}
        aria-describedby={describedBy}
        aria-invalid={Boolean(error) || props.errors.length > 0 || undefined}
        onChange={async (event) => {
          const file = event.currentTarget.files?.[0];
          if (!file) return;
          const token = ++readToken.current;
          props.onBusyChange(true);
          setError("");
          try {
            const data = await fileToDataURI(file);
            if (readToken.current !== token) return;
            props.onChange(data);
            props.onValidityChange(true);
            setFileName(`${file.name} (${formatBytes(file.size)})`);
          } catch (readError) {
            if (readToken.current !== token) return;
            props.onValidityChange(false);
            setFileName("");
            setError(readError instanceof Error ? readError.message : "Could not read file");
          } finally {
            if (readToken.current === token) props.onBusyChange(false);
          }
        }}
      />
      {fileName && <span className="file-name">{fileName}</span>}
      <span className="muted">or URL</span>
      <Input
        aria-label={`${props.name} URL`}
        aria-describedby={describedBy}
        aria-invalid={Boolean(error) || props.errors.length > 0 || undefined}
        disabled={disabled}
        placeholder="https://..."
        value={typeof props.value === "string" && !isDataURI(props.value) ? props.value : ""}
        onInput={(event) => {
          readToken.current += 1;
          props.onBusyChange(false);
          setFileName("");
          setError("");
          props.onValidityChange(true);
          props.onChange(event.currentTarget.value);
        }}
      />
      {error && (
        <small id={errorId} className="field-error" role="alert">
          {error}
        </small>
      )}
      {typeof props.value === "string" && <MediaPreview value={props.value} />}
    </div>
  );
}

function MediaPreview({ value }: { value: string }) {
  if (!isDataURI(value)) return null;
  if (/^data:image\//i.test(value)) {
    return <img className="input-media" src={value} alt="Selected input preview" />;
  }
  if (/^data:audio\//i.test(value)) {
    // oxlint-disable-next-line jsx-a11y/media-has-caption -- model inputs do not include caption tracks
    return <audio controls src={value} />;
  }
  if (/^data:video\//i.test(value)) {
    // oxlint-disable-next-line jsx-a11y/media-has-caption -- model inputs do not include caption tracks
    return <video controls src={value} />;
  }
  return null;
}
