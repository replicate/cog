import type { ReactNode } from "react";
import { useEffect, useRef, useState } from "react";

import { SegmentedTabs, tabId, tabPanelId } from "../../components/SegmentedTabs";
import { StatusBadge } from "../../components/StatusBadge";
import type { OpenAPISchema, PredictionEnvelope, RequestTrace } from "../../domain/types";
import { LazyJsonEditor } from "@/editor/LazyJsonEditor";
import { type InspectorView, RequestInspector } from "./RequestInspector";

type PanelView = "output" | "response" | InspectorView;

type Props = {
  envelope?: PredictionEnvelope;
  error?: string;
  output: unknown;
  rawEvents: string[];
  running: boolean;
  streaming: boolean;
  outputSchema?: OpenAPISchema;
  trace?: RequestTrace;
};

export function OutputPanel({
  envelope,
  error,
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
  const displayedError = error || envelope?.error;
  const panelItems: { value: PanelView; label: string }[] = [
    { value: "output", label: "Output" },
    { value: "response", label: "Response" },
    ...(hasLogs ? [{ value: "logs" as const, label: "Logs" }] : []),
    { value: "timeline", label: "Timeline" },
    { value: "request", label: "Request" },
  ];
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
        <SegmentedTabs<PanelView>
          id="response-view"
          label="Response details"
          items={panelItems}
          value={panelView}
          onChange={setPanelView}
        />
      </div>
      <output className="sr-only" aria-live="polite">
        {running ? "Prediction running" : envelope?.status ? `Prediction ${envelope.status}` : ""}
      </output>
      {displayedError && (
        <div className="error-container" role="alert">
          {displayedError}
        </div>
      )}
      <div
        id={tabPanelId("response-view", panelView)}
        role="tabpanel"
        aria-labelledby={tabId("response-view", panelView)}
        tabIndex={0}
      >
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
      </div>
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
    <section
      ref={viewport}
      className="live-output"
      aria-label="Prediction output"
      // oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- scrollable output must be keyboard reachable
      tabIndex={0}
      onScroll={(event) => {
        const node = event.currentTarget;
        followTail.current = node.scrollHeight - node.scrollTop - node.clientHeight < 48;
      }}
    >
      {children}
    </section>
  );
}

function Metrics({ metrics }: { metrics: Record<string, number> }) {
  return (
    <table className="metrics-table">
      <caption className="sr-only">Prediction metrics</caption>
      <tbody>
        {Object.entries(metrics).map(([name, value]) => (
          <tr key={name}>
            <th scope="row">{name}</th>
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
