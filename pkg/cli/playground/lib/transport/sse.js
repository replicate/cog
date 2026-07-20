// @ts-check

import { responseError } from "./http.js";
import { isJsonObject, isJsonValue } from "../json.js";

const MAX_SSE_FRAME_LENGTH = 1024 * 1024;

/** @param {Response} response @returns {AsyncGenerator<import("../../types").StreamEvent>} */
export async function* readSSE(response) {
  if (!response.ok) throw await responseError(response);
  if (!response.body) throw new Error("Streaming response has no body");
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let done = false;
  try {
    for (;;) {
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

/** @param {string} raw @returns {import("../../types").StreamEvent | undefined} */
export function parseSSE(raw) {
  let type = "";
  const data = [];
  for (const line of raw.replaceAll("\r\n", "\n").replaceAll("\r", "\n").split("\n")) {
    if (line.startsWith("event:")) type = line.slice(6).trim();
    if (line.startsWith("data:")) data.push(line.slice(5).replace(/^ /, ""));
  }
  if (!type) return undefined;
  const joined = data.join("\n");
  /** @type {import("../../types").JsonObject} */
  let parsed = { value: joined };
  try {
    const value = JSON.parse(joined);
    parsed = isJsonObject(value) ? value : { value: isJsonValue(value) ? value : joined };
  } catch {
    /* Preserve non-JSON event payloads. */
  }
  return { type, data: parsed, raw };
}

/** @param {string} buffer */
function sseSeparator(buffer) {
  for (let index = 0; index < buffer.length; index += 1) {
    const first = lineBreakLength(buffer, index);
    if (!first) continue;
    const second = lineBreakLength(buffer, index + first);
    if (second) return { index, length: first + second };
    index += first - 1;
  }
  return undefined;
}

/** @param {string} value @param {number} index */
function lineBreakLength(value, index) {
  if (value[index] === "\n") return 1;
  if (value[index] !== "\r" || index + 1 === value.length) return 0;
  return value[index + 1] === "\n" ? 2 : 1;
}
