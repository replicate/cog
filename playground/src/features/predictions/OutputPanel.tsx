import type { ReactNode } from "react";
import { useEffect, useState } from "react";

import { SegmentedTabs } from "../../components/SegmentedTabs";
import { StatusBadge } from "../../components/StatusBadge";
import type { PredictionEnvelope, RequestTrace } from "../../domain/types";
import { LazyJsonEditor } from "@/editor/LazyJsonEditor";
import { type InspectorView, RequestInspector } from "./RequestInspector";

type PanelView = "output" | "raw" | InspectorView;

type Props = {
  envelope?: PredictionEnvelope;
  output: unknown;
  rawEvents: string[];
  running: boolean;
  streaming: boolean;
  trace?: RequestTrace;
};

export function OutputPanel({ envelope, output, rawEvents, running, streaming, trace }: Props) {
  const hasResult = envelope || rawEvents.length > 0 || output !== undefined;
  const [panelView, setPanelView] = useState<PanelView>("output");
  const hasLogs = Boolean(envelope?.logs?.trim());
  useEffect(() => {
    if (panelView === "logs" && !hasLogs) setPanelView("output");
  }, [hasLogs, panelView]);
  return (
    <section id="output-panel">
      <div className="panel-head">
        <div className="response-title">
          <h2>Response</h2>
          {envelope?.status && <StatusBadge status={envelope.status} />}
        </div>
        <SegmentedTabs
          label="Response details"
          items={[
            { value: "output", label: "Output" },
            { value: "raw", label: "Raw" },
            ...(hasLogs ? [{ value: "logs", label: "Logs" }] : []),
            { value: "timeline", label: "Timeline" },
            { value: "request", label: "Request" },
          ]}
          value={panelView}
          onChange={(next) => setPanelView(next as PanelView)}
        />
      </div>
      {envelope?.error && (
        <div className="error-container" role="alert">
          {envelope.error}
        </div>
      )}
      {panelView === "output" ? (
        <>
          {envelope?.metrics && <Metrics metrics={envelope.metrics} />}
          {hasResult ? (
            <RenderedOutput value={output} running={running} />
          ) : (
            <div className="empty-output">Run a prediction to see its output.</div>
          )}
        </>
      ) : panelView === "raw" ? (
        hasResult ? (
          <LazyJsonEditor
            value={rawEvents.join("\n\n")}
            label="Raw prediction events"
            className="ace-output"
            readOnly
            followTail={streaming}
          />
        ) : (
          <div className="empty-output">Run a prediction to see its raw response.</div>
        )
      ) : (
        <RequestInspector view={panelView} trace={trace} envelope={envelope} />
      )}
    </section>
  );
}

function Metrics({ metrics }: { metrics: Record<string, number> }) {
  return (
    <table className="metrics-table">
      <tbody>
        {Object.entries(metrics).map(([name, value]) => (
          <tr key={name}>
            <td>{name}</td>
            <td>{String(value)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function RenderedOutput({ value, running }: { value: unknown; running: boolean }) {
  if (value === undefined || value === null) {
    return (
      <div className="empty-output">
        {running ? "Waiting for output..." : "Prediction returned no output."}
      </div>
    );
  }
  if (typeof value === "string") {
    const mediaNode = media(value);
    if (mediaNode) return <div className="output-item">{mediaNode}</div>;
    return (
      <pre className="text-output">
        {value}
        {running && <span className="streaming-cursor" />}
      </pre>
    );
  }
  if (Array.isArray(value) && value.every((item) => typeof item === "string")) {
    const strings = value as string[];
    if (strings.some((item) => media(item))) {
      return (
        <div>
          {strings.map((item, index) => (
            <div className="output-item" key={index}>
              {media(item) ?? <pre>{item}</pre>}
            </div>
          ))}
        </div>
      );
    }
    return (
      <pre className="text-output">
        {strings.join("")}
        {running && <span className="streaming-cursor" />}
      </pre>
    );
  }
  return (
    <LazyJsonEditor
      value={JSON.stringify(value, null, 2)}
      label="Structured prediction output"
      className="ace-output"
      readOnly
      followTail={running}
    />
  );
}

function media(value: string): ReactNode {
  if (value.startsWith("data:image/") || /\.(png|jpe?g|gif|webp)(?:[?#]|$)/i.test(value)) {
    return <img src={value} alt="Prediction output" />;
  }
  if (value.startsWith("data:audio/") || /\.(mp3|wav|ogg|flac|m4a)(?:[?#]|$)/i.test(value)) {
    // oxlint-disable-next-line jsx-a11y/media-has-caption -- model outputs do not include caption tracks
    return <audio controls src={value} />;
  }
  if (value.startsWith("data:video/") || /\.(mp4|webm|mov)(?:[?#]|$)/i.test(value)) {
    // oxlint-disable-next-line jsx-a11y/media-has-caption -- model outputs do not include caption tracks
    return <video controls src={value} />;
  }
  if (value.startsWith("data:"))
    return (
      <a href={value} download="output">
        Download file
      </a>
    );
  if (/^https?:\/\//i.test(value))
    return (
      <a href={value} target="_blank" rel="noreferrer">
        {value}
      </a>
    );
  return null;
}
