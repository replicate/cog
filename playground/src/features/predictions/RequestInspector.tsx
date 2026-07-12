import { useState } from "react";

import type { PredictionEnvelope, RequestTrace, TraceEvent } from "../../domain/types";
import { CodeViewer } from "@/editor/CodeViewer";

export type InspectorView = "logs" | "timeline" | "request";

type Props = {
  view: InspectorView;
  trace?: RequestTrace;
  envelope?: PredictionEnvelope;
};

export function RequestInspector({ view, trace, envelope }: Props) {
  if (!trace) {
    const message =
      view === "timeline"
        ? "Run a prediction to see its event timeline."
        : view === "logs"
          ? "Run an async prediction to see its logs."
          : "Run a prediction to inspect its request and response metadata.";
    return <div className="empty-output">{message}</div>;
  }
  if (view === "logs") return <Logs logs={envelope?.logs} />;
  if (view === "timeline") return <Timeline trace={trace} />;
  return <RequestDetails trace={trace} envelope={envelope} />;
}

function Logs({ logs }: { logs?: string }) {
  return logs ? (
    <pre className="inspector-logs">{logs}</pre>
  ) : (
    <div className="empty-output">No prediction logs were received.</div>
  );
}

function Timeline({ trace }: { trace: RequestTrace }) {
  const events = compactEvents(trace.events);
  return (
    <ol className="trace-timeline">
      {events.map((event) => (
        <li key={event.id} className={`trace-${event.kind}`}>
          <time>{formatDuration(event.elapsedMs)}</time>
          <span className="trace-kind">{event.kind}</span>
          <div>
            <strong>
              {event.label}
              {event.count > 1 && ` × ${event.count}`}
            </strong>
            {event.data !== undefined && <TimelinePayload value={event.data} />}
          </div>
        </li>
      ))}
    </ol>
  );
}

function TimelinePayload({ value }: { value: unknown }) {
  const [open, setOpen] = useState(false);
  return (
    <details open={open} onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary>Payload</summary>
      {open && (
        <CodeViewer
          value={formatValue(value)}
          label="Timeline event payload"
          className="viewer-timeline"
        />
      )}
    </details>
  );
}

type TimelineEvent = TraceEvent & { count: number };

function compactEvents(events: TraceEvent[]): TimelineEvent[] {
  const compacted: TimelineEvent[] = [];
  for (const event of events) {
    const previous = compacted.at(-1);
    if (
      previous &&
      event.kind === "sse" &&
      event.label === "output" &&
      previous.label === "output"
    ) {
      previous.count += 1;
      previous.elapsedMs = event.elapsedMs;
      previous.data = `${formatValue(previous.data)}\n\n${formatValue(event.data)}`;
      continue;
    }
    compacted.push({ ...event, count: 1 });
  }
  return compacted;
}

function RequestDetails({
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
      {open && <CodeViewer value={formatValue(value)} label={label} className="viewer-inspector" />}
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
