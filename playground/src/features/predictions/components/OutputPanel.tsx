import { useEffect, useState } from "react";

import { SegmentedTabs, tabId, tabPanelId } from "@/components/SegmentedTabs";
import { StatusBadge } from "@/components/StatusBadge";
import { PredictionOutput } from "@/features/predictions/components/PredictionOutput";
import {
  type InspectorView,
  RequestInspector,
} from "@/features/predictions/components/RequestInspector";
import { RawResponse } from "@/features/predictions/components/RawResponse";
import type { OpenAPISchema } from "@/types/openapi";
import type { PredictionEnvelope, RequestTrace } from "@/types/prediction";

type PanelView = "output" | "raw" | InspectorView;

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
  trace,
}: Props) {
  const hasResult = Boolean(envelope || rawEvents.length > 0 || output !== undefined);
  const [panelView, setPanelView] = useState<PanelView>("output");
  const hasLogs = Boolean(envelope?.logs?.trim());
  const displayedError = error || envelope?.error;
  const panelItems: { value: PanelView; label: string }[] = [
    { value: "output", label: "Output" },
    { value: "raw", label: "Raw" },
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
          hasResult ? (
            <PredictionOutput metrics={envelope?.metrics} running={running} value={output} />
          ) : (
            <div className="empty-output">Run a prediction to see its output.</div>
          )
        ) : panelView === "raw" ? (
          hasResult ? (
            <RawResponse envelope={envelope} live={streaming} rawEvents={rawEvents} />
          ) : (
            <div className="empty-output">Run a prediction to see its raw response.</div>
          )
        ) : (
          <RequestInspector view={panelView} trace={trace} envelope={envelope} />
        )}
      </div>
    </section>
  );
}
