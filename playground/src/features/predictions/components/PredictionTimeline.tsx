import { useState } from "react";

import { LazyJsonEditor } from "@/components/editor/LazyJsonEditor";
import { useFollowTail } from "@/features/predictions/hooks/useFollowTail";
import { formatDuration, formatValue } from "@/features/predictions/utils/format";
import type { RequestTrace, TraceEvent } from "@/types/prediction";

/** Compacts adjacent SSE output events while preserving their payloads in an expandable viewer. */
export function PredictionTimeline({
  trace,
  running,
  active,
}: {
  trace: RequestTrace;
  running: boolean;
  active: boolean;
}) {
  const events = compactEvents(trace.events);
  const { ref, onScroll } = useFollowTail<HTMLOListElement>(running, trace.events.at(-1), active);
  return (
    <ol
      ref={ref}
      className="trace-timeline"
      // oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- scrollable timeline must be keyboard reachable
      tabIndex={0}
      onScroll={onScroll}
    >
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
      previous.count += event.count ?? 1;
      previous.elapsedMs = event.elapsedMs;
      previous.data = `${formatValue(previous.data)}\n\n${formatValue(event.data)}`;
      continue;
    }
    compacted.push({ ...event, count: event.count ?? 1 });
  }
  return compacted;
}
