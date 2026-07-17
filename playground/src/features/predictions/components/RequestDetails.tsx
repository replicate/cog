import { useState } from "react";

import { LazyJsonEditor } from "@/components/editor/LazyJsonEditor";
import { formatDuration, formatValue } from "@/features/predictions/utils/format";
import type { PredictionEnvelope, RequestTrace } from "@/types/prediction";

/** Shows client and server timings with bodies expanded and headers collapsed by default. */
export function RequestDetails({
  trace,
  envelope,
}: {
  trace: RequestTrace;
  envelope?: PredictionEnvelope;
}) {
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
