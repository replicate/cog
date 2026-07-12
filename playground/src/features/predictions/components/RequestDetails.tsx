import { useState } from "react";

import { LazyJsonEditor } from "@/components/editor/LazyJsonEditor";
import type { PredictionEnvelope, RequestTrace } from "@/types/prediction";

export function RequestDetails({
  trace,
  envelope,
}: {
  trace: RequestTrace;
  envelope?: PredictionEnvelope;
}) {
  const duration = trace.finishedAt
    ? trace.finishedAt - trace.startedAt
    : trace.events.at(-1)?.elapsedMs;
  const predictTime =
    typeof envelope?.metrics?.predict_time === "number"
      ? envelope.metrics.predict_time * 1000
      : undefined;
  return (
    <div className="request-inspector">
      <dl className="request-summary">
        <div>
          <dt>Request</dt>
          <dd>
            <span className="method-badge">{trace.method}</span> {trace.endpoint}
          </dd>
        </div>
        <div>
          <dt>Started</dt>
          <dd>{trace.startedAtLabel}</dd>
        </div>
        <div>
          <dt>Status</dt>
          <dd>{trace.responseStatus ?? "Waiting"}</dd>
        </div>
        <div>
          <dt>Total duration</dt>
          <dd>{duration === undefined ? "Waiting" : formatDuration(duration)}</dd>
        </div>
        {predictTime !== undefined && (
          <div>
            <dt>Prediction time</dt>
            <dd>{formatDuration(predictTime)}</dd>
          </div>
        )}
      </dl>
      <InspectorDocument label="Request headers" value={trace.requestHeaders} />
      <InspectorDocument label="Request body" value={trace.requestBody} />
      {trace.responseHeaders && (
        <InspectorDocument label="Response headers" value={trace.responseHeaders} />
      )}
      {trace.responseBody !== undefined && (
        <InspectorDocument label="Response body" value={trace.responseBody} />
      )}
    </div>
  );
}

function InspectorDocument({ label, value }: { label: string; value: unknown }) {
  const [open, setOpen] = useState(label.includes("body"));
  return (
    <details
      className="inspector-document"
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary>{label}</summary>
      {open && (
        <LazyJsonEditor
          value={formatValue(value)}
          label={label}
          className="viewer-inspector"
          readOnly
          autoHeight
        />
      )}
    </details>
  );
}

function formatValue(value: unknown): string {
  return typeof value === "string" ? value : JSON.stringify(value, null, 2);
}

function formatDuration(milliseconds: number): string {
  return milliseconds < 1000
    ? `${Math.round(milliseconds)} ms`
    : `${(milliseconds / 1000).toFixed(2)} s`;
}
