import { type RefObject, startTransition, useCallback, useRef, useState } from "react";

import type { RunMode } from "@/features/predictions/types";
import {
  MAX_RAW_EVENT_TEXT,
  MAX_RAW_EVENTS,
  MAX_STREAM_OUTPUT_ITEMS,
  MAX_STREAM_OUTPUT_TEXT,
  valueLength,
} from "@/features/predictions/utils/runtime";
import type { TraceRun } from "@/features/predictions/hooks/usePredictionTrace";

type StreamBuffer = {
  rawEvents: string[];
  rawLength: number;
  output: unknown[];
  outputLength: number;
};

function emptyBuffer(): StreamBuffer {
  return { rawEvents: [], rawLength: 0, output: [], outputLength: 0 };
}

// Amortized O(1) trim: drop oldest items until within both the item and
// character limits, keeping at least the newest item.
function trim(items: unknown[], length: number, maxItems: number, maxText: number): number {
  while (items.length > 1 && (items.length > maxItems || length > maxText)) {
    length -= valueLength(items.shift());
  }
  return length;
}

/** Accumulates streamed output and raw frames bounded to the newest content, ignoring stale runs. */
export function usePredictionOutputState<T extends TraceRun>(activeRun: RefObject<T | undefined>) {
  const [output, setOutput] = useState<unknown>();
  const [rawEvents, setRawEvents] = useState<string[]>([]);
  const buffer = useRef<StreamBuffer>(emptyBuffer());

  const queueStreamRender = useCallback(
    (token: string, raw: string, nextOutput?: unknown) => {
      if (activeRun.current?.token !== token) return;
      const buf = buffer.current;
      buf.rawEvents.push(raw);
      buf.rawLength += raw.length;
      buf.rawLength = trim(buf.rawEvents, buf.rawLength, MAX_RAW_EVENTS, MAX_RAW_EVENT_TEXT);
      const nextRawEvents = buf.rawEvents.slice() as string[];

      let nextOutputItems: unknown[] | undefined;
      if (nextOutput !== undefined) {
        buf.output.push(nextOutput);
        buf.outputLength += valueLength(nextOutput);
        buf.outputLength = trim(
          buf.output,
          buf.outputLength,
          MAX_STREAM_OUTPUT_ITEMS,
          MAX_STREAM_OUTPUT_TEXT,
        );
        nextOutputItems = buf.output.slice();
      }

      startTransition(() => {
        setRawEvents(nextRawEvents);
        if (nextOutputItems) setOutput(nextOutputItems);
      });
    },
    [activeRun],
  );

  const startOutput = useCallback((mode: RunMode) => {
    buffer.current = emptyBuffer();
    setOutput(mode === "stream" ? [] : undefined);
    setRawEvents([]);
  }, []);

  const resetOutput = useCallback(() => {
    buffer.current = emptyBuffer();
    setOutput(undefined);
    setRawEvents([]);
  }, []);

  return {
    output,
    rawEvents,
    setOutput,
    setRawEvents,
    queueStreamRender,
    startOutput,
    resetOutput,
  };
}
