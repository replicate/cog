import { LazyJsonEditor } from "@/components/editor/LazyJsonEditor";
import type { PredictionEnvelope } from "@/types/prediction";

/** Shows exact live transport frames or a pretty-printed synchronous terminal response. */
export function RawResponse({
  envelope,
  live,
  rawEvents,
}: {
  envelope?: PredictionEnvelope;
  live: boolean;
  rawEvents: string[];
}) {
  return (
    <LazyJsonEditor
      value={rawResponseText(rawEvents, envelope, live)}
      label="Raw prediction response"
      className="response-editor"
      readOnly
      followTail={live}
      autoHeight
    />
  );
}

export function rawResponseText(
  rawEvents: string[],
  envelope: PredictionEnvelope | undefined,
  live: boolean,
): string {
  if (live) return rawEvents.join("\n\n");
  const raw = rawEvents[0];
  if (raw !== undefined) {
    try {
      return JSON.stringify(JSON.parse(raw), null, 2);
    } catch {
      return raw;
    }
  }
  return JSON.stringify(envelope ?? {}, null, 2);
}
