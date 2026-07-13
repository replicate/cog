import { useState } from "react";

import { LazyJsonEditor } from "@/components/editor/LazyJsonEditor";
import { useFollowTail } from "@/features/predictions/hooks/useFollowTail";
import { formatDuration, formatValue } from "@/features/predictions/utils/format";
import type { RequestTrace } from "@/types/prediction";

/** Renders bounded trace events while preserving payloads in an expandable viewer. */
export function PredictionTimeline({
  trace,
  running,
  active,
}: {
  trace: RequestTrace;
  running: boolean;
  active: boolean;
}) {
  const { ref, onScroll } = useFollowTail<HTMLOListElement>(running, trace.events.at(-1), active);
  return (
    <ol
      ref={ref}
      className="trace-timeline"
      // oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- scrollable timeline must be keyboard reachable
      tabIndex={0}
      onScroll={onScroll}
    >
      {trace.events.map((event) => (
        <li key={event.id} className={`trace-${event.kind}`}>
          <time>{formatDuration(event.elapsedMs)}</time>
          <span className="trace-kind">{event.kind}</span>
          <div>
            <strong>
              {event.label}
              {(event.count ?? 1) > 1 && ` × ${event.count}`}
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
        <LazyJsonEditor
          value={formatValue(value)}
          label="Timeline event payload"
          className="viewer-timeline"
          readOnly
          autoHeight
        />
      )}
    </details>
  );
}
