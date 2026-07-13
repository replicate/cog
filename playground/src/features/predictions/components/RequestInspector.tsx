import { PredictionTimeline } from "@/features/predictions/components/PredictionTimeline";
import { RequestDetails } from "@/features/predictions/components/RequestDetails";
import { useFollowTail } from "@/features/predictions/hooks/useFollowTail";
import type { PredictionEnvelope, RequestTrace } from "@/types/prediction";

export type InspectorView = "logs" | "timeline" | "request";

type Props = {
  view: InspectorView;
  trace?: RequestTrace;
  envelope?: PredictionEnvelope;
  running: boolean;
  active?: boolean;
};

/** Handles missing trace/log data before delegating to the selected inspector view. */
export function RequestInspector({ view, trace, envelope, running, active = true }: Props) {
  if (view === "logs") return <Logs logs={envelope?.logs} running={running} active={active} />;
  if (!trace) {
    const message =
      view === "timeline"
        ? "Run a prediction to see its event timeline."
        : "Run a prediction to inspect its request and response metadata.";
    return <div className="empty-output">{message}</div>;
  }
  if (view === "timeline") {
    return <PredictionTimeline trace={trace} running={running} active={active} />;
  }
  return <RequestDetails trace={trace} envelope={envelope} />;
}

function Logs({ logs, running, active }: { logs?: string; running: boolean; active: boolean }) {
  const { ref, onScroll } = useFollowTail<HTMLPreElement>(running, logs, active);
  return logs ? (
    <pre
      ref={ref}
      className="inspector-logs"
      aria-label="Prediction logs"
      // oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- scrollable logs must be keyboard reachable
      tabIndex={0}
      onScroll={onScroll}
    >
      {logs}
    </pre>
  ) : (
    <div className="empty-output">No prediction logs were received.</div>
  );
}
