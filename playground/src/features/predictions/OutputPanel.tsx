import type { ReactNode } from "react";
import { useEffect, useRef, useState } from "react";

import { SegmentedTabs } from "../../components/SegmentedTabs";
import { StatusBadge } from "../../components/StatusBadge";
import type { OpenAPISchema, PredictionEnvelope, RequestTrace } from "../../domain/types";
import { LazyJsonEditor } from "@/editor/LazyJsonEditor";
import { type InspectorView, RequestInspector } from "./RequestInspector";

type PanelView = "output" | "response" | InspectorView;

type Props = {
  envelope?: PredictionEnvelope;
  output: unknown;
  rawEvents: string[];
  running: boolean;
  streaming: boolean;
  outputSchema?: OpenAPISchema;
  trace?: RequestTrace;
};

export function OutputPanel({
  envelope,
  output,
  rawEvents,
  running,
  streaming,
  outputSchema,
  trace,
}: Props) {
  const hasResult = envelope || rawEvents.length > 0 || output !== undefined;
  const [panelView, setPanelView] = useState<PanelView>("output");
  const hasLogs = Boolean(envelope?.logs?.trim());
  useEffect(() => {
    if (panelView === "logs" && !hasLogs) setPanelView("output");
  }, [hasLogs, panelView]);
  useEffect(() => {
    if (running) setPanelView("output");
  }, [running]);
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
            { value: "response", label: "Response" },
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
        <LiveOutput running={running}>
          {envelope?.metrics && <Metrics metrics={envelope.metrics} />}
          {hasResult ? (
            <RenderedOutput value={output} running={running} schema={outputSchema} />
          ) : (
            <div className="empty-output">Run a prediction to see its output.</div>
          )}
        </LiveOutput>
      ) : panelView === "response" ? (
        hasResult ? (
          <LazyJsonEditor
            value={responseDocument(rawEvents, envelope)}
            label="Prediction response"
            className="response-editor"
            readOnly
            followTail={streaming}
          />
        ) : (
          <div className="empty-output">Run a prediction to see its response.</div>
        )
      ) : (
        <RequestInspector view={panelView} trace={trace} envelope={envelope} />
      )}
    </section>
  );
}

function LiveOutput({ children, running }: { children: ReactNode; running: boolean }) {
  const viewport = useRef<HTMLDivElement>(null);
  const followTail = useRef(true);
  useEffect(() => {
    if (!running) return;
    followTail.current = true;
    const node = viewport.current;
    if (node) node.scrollTop = node.scrollHeight;
  }, [running]);
  useEffect(() => {
    const node = viewport.current;
    if (node && followTail.current) node.scrollTop = node.scrollHeight;
  }, [children, running]);
  return (
    <div
      ref={viewport}
      className="live-output"
      role="log"
      aria-live="polite"
      aria-busy={running}
      onScroll={(event) => {
        const node = event.currentTarget;
        followTail.current = node.scrollHeight - node.scrollTop - node.clientHeight < 48;
      }}
    >
      {children}
    </div>
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

function RenderedOutput({
  value,
  running,
  schema,
}: {
  value: unknown;
  running: boolean;
  schema?: OpenAPISchema;
}) {
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
    const concatenated = schema?.["x-cog-array-display"] === "concatenate";
    if (!concatenated) {
      const jsonValue = JSON.stringify(strings, null, 2);
      if (running) {
        return (
          <pre className="text-output">
            {jsonValue}
            <span className="streaming-cursor" />
          </pre>
        );
      }
      return (
        <LazyJsonEditor
          value={jsonValue}
          label="Structured prediction output"
          className="structured-output"
          readOnly
          followTail={running}
          autoHeight
        />
      );
    }
    return (
      <pre className="text-output">
        {strings.join("")}
        {running && <span className="streaming-cursor" />}
      </pre>
    );
  }
  const jsonValue = JSON.stringify(value, null, 2);
  if (running) {
    return (
      <pre className="text-output">
        {jsonValue}
        <span className="streaming-cursor" />
      </pre>
    );
  }
  return (
    <LazyJsonEditor
      value={jsonValue}
      label="Structured prediction output"
      className="structured-output"
      readOnly
      followTail={running}
      autoHeight
    />
  );
}

function responseDocument(rawEvents: string[], envelope?: PredictionEnvelope): string {
  const events = rawEvents.map(parseResponseEvent);
  const response = events.length === 0 ? envelope : events.length === 1 ? events[0] : events;
  return JSON.stringify(response ?? {}, null, 2);
}

function parseResponseEvent(raw: string): unknown {
  try {
    return JSON.parse(raw) as unknown;
  } catch {
    const lines = raw.split("\n");
    const event = lines
      .find((line) => line.startsWith("event:"))
      ?.slice(6)
      .trim();
    const data = lines
      .filter((line) => line.startsWith("data:"))
      .map((line) => line.slice(5).trimStart())
      .join("\n");
    if (!event && !data) return raw;
    let parsedData: unknown = data;
    try {
      parsedData = JSON.parse(data) as unknown;
    } catch {
      // Keep non-JSON event data as text.
    }
    return { event: event ?? "message", data: parsedData };
  }
}

function media(value: string): ReactNode {
  if (value.startsWith("data:image/")) {
    return <img src={value} alt="Prediction output" />;
  }
  if (value.startsWith("data:audio/")) {
    // oxlint-disable-next-line jsx-a11y/media-has-caption -- model outputs do not include caption tracks
    return <audio controls src={value} />;
  }
  if (value.startsWith("data:video/")) {
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
      <a href={value} target="_blank" rel="noopener noreferrer">
        {value}
      </a>
    );
  return null;
}
