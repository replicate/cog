import type { JsonObject, StreamEvent } from "../../types.js";

import { isJsonObject, isJsonValue } from "../json.js";
import { responseError } from "./http.js";

const MAX_SSE_FRAME_LENGTH = 1024 * 1024;
type SSESeparator = { index: number; length: number };

export async function* readSSE(response: Response): AsyncGenerator<StreamEvent, void, unknown> {
  if (!response.ok) throw await responseError(response);
  if (!response.body) throw new Error("Streaming response has no body");
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let done = false;
  try {
    while (true) {
      const chunk = await reader.read();
      done = chunk.done;
      buffer += decoder.decode(chunk.value, { stream: !done });
      let separator = sseSeparator(buffer);
      while (separator) {
        if (separator.index > MAX_SSE_FRAME_LENGTH) throw new Error("SSE event is too large");
        const raw = buffer.slice(0, separator.index);
        buffer = buffer.slice(separator.index + separator.length);
        const event = parseSSE(raw);
        if (event) yield event;
        separator = sseSeparator(buffer);
      }
      if (buffer.length > MAX_SSE_FRAME_LENGTH) throw new Error("SSE event is too large");
      if (done) break;
    }
    if (buffer.trim()) {
      const event = parseSSE(buffer);
      if (event) yield event;
    }
  } finally {
    try {
      if (!done) await reader.cancel();
    } finally {
      reader.releaseLock();
    }
  }
}

export function parseSSE(raw: string): StreamEvent | undefined {
  let type = "";
  const data: string[] = [];
  for (const line of raw.replaceAll("\r\n", "\n").replaceAll("\r", "\n").split("\n")) {
    if (line.startsWith("event:")) type = line.slice(6).trim();
    if (line.startsWith("data:")) data.push(line.slice(5).replace(/^ /, ""));
  }
  if (!type) return undefined;
  const joined = data.join("\n");
  let parsed: JsonObject = { value: joined };
  try {
    const value: unknown = JSON.parse(joined);
    if (isJsonObject(value)) parsed = value;
    else if (isJsonValue(value)) parsed = { value };
  } catch {
    // Preserve non-JSON event payloads.
  }
  return { type, data: parsed, raw };
}

function sseSeparator(buffer: string): SSESeparator | undefined {
  for (let index = 0; index < buffer.length; index += 1) {
    const first = lineBreakLength(buffer, index);
    if (!first) continue;
    const second = lineBreakLength(buffer, index + first);
    if (second) return { index, length: first + second };
    index += first - 1;
  }
  return undefined;
}

function lineBreakLength(value: string, index: number): number {
  if (value[index] === "\n") return 1;
  if (value[index] !== "\r" || index + 1 === value.length) return 0;
  return value[index + 1] === "\n" ? 2 : 1;
}
