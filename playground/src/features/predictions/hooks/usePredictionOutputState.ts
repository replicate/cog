import { type RefObject, startTransition, useCallback, useEffect, useRef, useState } from "react";

import type { RunMode } from "@/features/predictions/types";
import {
  boundedTextItems,
  MAX_RAW_EVENT_TEXT,
  MAX_STREAM_OUTPUT_ITEMS,
  MAX_STREAM_OUTPUT_TEXT,
  valueLength,
} from "@/features/predictions/utils/runtime";
import type { TraceRun } from "@/features/predictions/hooks/usePredictionTrace";

type StreamBuffer = {
  rawEvents: string[];
  output: unknown[];
  outputLength: number;
  frame?: number;
};

export function usePredictionOutputState<T extends TraceRun>(activeRun: RefObject<T | undefined>) {
  const [output, setOutput] = useState<unknown>();
  const [rawEvents, setRawEvents] = useState<string[]>([]);
  const streamBuffer = useRef<StreamBuffer | undefined>(undefined);

  useEffect(
    () => () => {
      const frame = streamBuffer.current?.frame;
      if (frame !== undefined) window.cancelAnimationFrame(frame);
      streamBuffer.current = undefined;
    },
    [],
  );

  const flushStreamBuffer = useCallback(
    (token: string) => {
      if (activeRun.current?.token !== token) return;
      const buffer = streamBuffer.current;
      if (!buffer) return;
      if (buffer.frame !== undefined) window.cancelAnimationFrame(buffer.frame);
      buffer.frame = undefined;
      if (buffer.rawEvents.length === 0 && buffer.output.length === 0) return;
      const nextRawEvents = buffer.rawEvents.splice(0);
      const nextOutput = buffer.output.slice();
      startTransition(() => {
        if (nextRawEvents.length) {
          setRawEvents((current) =>
            boundedTextItems([...current, ...nextRawEvents], MAX_RAW_EVENT_TEXT),
          );
        }
        if (nextOutput.length) setOutput(nextOutput);
      });
    },
    [activeRun],
  );

  const queueStreamRender = useCallback(
    (token: string, raw: string, nextOutput?: unknown) => {
      const buffer = streamBuffer.current;
      if (activeRun.current?.token !== token || !buffer) return;
      buffer.rawEvents.push(raw);
      buffer.rawEvents = boundedTextItems(buffer.rawEvents, MAX_RAW_EVENT_TEXT);
      if (nextOutput !== undefined) {
        buffer.output.push(nextOutput);
        buffer.outputLength += valueLength(nextOutput);
        while (
          (buffer.outputLength > MAX_STREAM_OUTPUT_TEXT ||
            buffer.output.length > MAX_STREAM_OUTPUT_ITEMS) &&
          buffer.output.length > 1
        ) {
          buffer.outputLength -= valueLength(buffer.output.shift());
        }
      }
      if (buffer.frame === undefined) {
        buffer.frame = window.requestAnimationFrame(() => flushStreamBuffer(token));
      }
    },
    [activeRun, flushStreamBuffer],
  );

  const startOutput = useCallback((mode: RunMode) => {
    streamBuffer.current = { rawEvents: [], output: [], outputLength: 0 };
    setOutput(mode === "stream" ? [] : undefined);
    setRawEvents([]);
  }, []);

  const resetOutput = useCallback(() => {
    setOutput(undefined);
    setRawEvents([]);
  }, []);

  return {
    output,
    rawEvents,
    setOutput,
    setRawEvents,
    flushStreamBuffer,
    queueStreamRender,
    startOutput,
    resetOutput,
  };
}
