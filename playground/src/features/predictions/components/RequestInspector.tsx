import { PredictionTimeline } from "@/features/predictions/components/PredictionTimeline";
import { RequestDetails } from "@/features/predictions/components/RequestDetails";
import type { PredictionEnvelope, RequestTrace } from "@/types/prediction";

export type InspectorView = "logs" | "timeline" | "request";

type Props = {
  view: InspectorView;
  trace?: RequestTrace;
  envelope?: PredictionEnvelope;
};

export function RequestInspector({ view, trace, envelope }: Props) {
  if (view === "logs") return <Logs logs={envelope?.logs} />;
  if (!trace) {
    const message =
      view === "timeline"
        ? "Run a prediction to see its event timeline."
        : "Run a prediction to inspect its request and response metadata.";
    return <div className="empty-output">{message}</div>;
  }
  if (view === "timeline") return <PredictionTimeline trace={trace} />;
  return <RequestDetails trace={trace} envelope={envelope} />;
}

function Logs({ logs }: { logs?: string }) {
  return logs ? (
    // oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- scrollable logs must be keyboard reachable
    <pre className="inspector-logs" aria-label="Prediction logs" tabIndex={0}>
      {logs}
    </pre>
  ) : (
    <div className="empty-output">No prediction logs were received.</div>
  );
}
