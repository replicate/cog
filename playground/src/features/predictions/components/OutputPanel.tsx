import { memo, useEffect, useState } from "react";

import { SegmentedTabs, tabId, tabPanelId } from "@/components/SegmentedTabs";
import { StatusBadge } from "@/components/StatusBadge";
import { PredictionOutput } from "@/features/predictions/components/PredictionOutput";
import {
  type InspectorView,
  RequestInspector,
} from "@/features/predictions/components/RequestInspector";
import { RawResponse } from "@/features/predictions/components/RawResponse";
import type { PredictionEnvelope, RequestTrace } from "@/types/prediction";

type PanelView = "output" | "raw" | InspectorView;

type Props = {
  envelope?: PredictionEnvelope;
  error?: string;
  output: unknown;
  rawEvents: string[];
  running: boolean;
  streaming: boolean;
  trace?: RequestTrace;
};

type PanelContentProps = Omit<Props, "error"> & {
  active: boolean;
  hasResult: boolean;
  view: PanelView;
};

/** Retains visited response views and exposes Logs only when the response contains them. */
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
  const [mountedViews, setMountedViews] = useState<ReadonlySet<PanelView>>(
    () => new Set(["output"]),
  );
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

  const selectPanelView = (view: PanelView) => {
    setMountedViews((current) => (current.has(view) ? current : new Set(current).add(view)));
    setPanelView(view);
  };

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
          onChange={selectPanelView}
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
      {panelItems.map(({ value: view }) => {
        if (!mountedViews.has(view)) return null;
        const active = panelView === view;
        return (
          <div
            key={view}
            id={tabPanelId("response-view", view)}
            role="tabpanel"
            aria-labelledby={tabId("response-view", view)}
            tabIndex={active ? 0 : -1}
            hidden={!active}
          >
            <PanelContent
              view={view}
              active={active}
              hasResult={hasResult}
              envelope={envelope}
              output={output}
              rawEvents={rawEvents}
              running={running}
              streaming={streaming}
              trace={trace}
            />
          </div>
        );
      })}
    </section>
  );
}

// Inactive panels are frozen: they hold their last content while hidden and
// re-sync when reactivated. This avoids re-rendering the heavyweight (hidden)
// CodeMirror editors on every stream event.
const PanelContent = memo(
  function PanelContent({
    view,
    active,
    hasResult,
    envelope,
    output,
    rawEvents,
    running,
    streaming,
    trace,
  }: PanelContentProps) {
    if (view === "output") {
      return hasResult ? (
        <PredictionOutput metrics={envelope?.metrics} running={running} value={output} />
      ) : (
        <div className="empty-output">Run a prediction to see its output.</div>
      );
    }
    if (view === "raw") {
      return hasResult ? (
        <RawResponse
          envelope={envelope}
          live={streaming}
          rawEvents={rawEvents}
          running={running}
          active={active}
        />
      ) : (
        <div className="empty-output">Run a prediction to see its raw response.</div>
      );
    }
    return (
      <RequestInspector
        view={view}
        trace={trace}
        envelope={envelope}
        running={running}
        active={active}
      />
    );
  },
  (previous, next) => !previous.active && !next.active && previous.running === next.running,
);
