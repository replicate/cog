// @ts-check

import { predictionEnvelope } from "./guards.js";
import { isJsonObject } from "../json.js";

const MAX_JSON_RESPONSE_LENGTH = 16 * 1024 * 1024;
const MAX_ERROR_RESPONSE_LENGTH = 1024 * 1024;

export class HttpError extends Error {
  /** @param {string} message @param {number} status @param {unknown[] | undefined} detail */
  constructor(message, status, detail) {
    super(message);
    this.status = status;
    this.detail = detail;
  }
}

/** @param {Response} response */
export async function parsePredictionResponse(response) {
  if (!response.ok) throw await responseError(response);
  const prediction = predictionEnvelope(await parseJSONResponse(response));
  if (!prediction) throw new Error("Invalid prediction response");
  return prediction;
}

/** @param {Response} response */
export async function responseError(response) {
  const text = await readResponseText(response, MAX_ERROR_RESPONSE_LENGTH);
  /** @type {import("../../types").JsonObject} */
  let body = {};
  try {
    const parsed = JSON.parse(text);
    if (isJsonObject(parsed)) body = parsed;
  } catch {
    /* Use the response text. */
  }
  const detail = Array.isArray(body.detail) ? body.detail : undefined;
  const message =
    (typeof body.error === "string" && body.error) ||
    (typeof body.detail === "string" && body.detail) ||
    text ||
    `HTTP ${response.status}`;
  return new HttpError(message, response.status, detail);
}

/** @param {Response} response */
export async function parseJSONResponse(response) {
  return JSON.parse(await readResponseText(response, MAX_JSON_RESPONSE_LENGTH));
}

/** @param {Response} response @param {number} maxLength */
async function readResponseText(response, maxLength) {
  const contentLength = Number(response.headers.get("Content-Length"));
  if (Number.isFinite(contentLength) && contentLength > maxLength) {
    await response.body?.cancel();
    throw new Error(`Response body exceeds ${maxLength} bytes`);
  }
  if (!response.body) return "";
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let length = 0;
  let text = "";
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) return text + decoder.decode();
      length += value.byteLength;
      if (length > maxLength) {
        await reader.cancel();
        throw new Error(`Response body exceeds ${maxLength} bytes`);
      }
      text += decoder.decode(value, { stream: true });
    }
  } finally {
    reader.releaseLock();
  }
}
