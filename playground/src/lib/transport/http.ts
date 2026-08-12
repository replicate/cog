import type { PredictionEnvelope } from "../../types.js";

import { isObject } from "../json.js";
import { predictionEnvelope } from "./guards.js";

type ErrorResponse = { detail?: string | unknown[]; error?: string };

const MAX_JSON_RESPONSE_LENGTH = 16 * 1024 * 1024;
const MAX_ERROR_RESPONSE_LENGTH = 1024 * 1024;

export class HttpError extends Error {
  readonly status: number;
  readonly detail: unknown[] | undefined;

  constructor(message: string, status: number, detail: unknown[] | undefined) {
    super(message);
    this.status = status;
    this.detail = detail;
  }
}

export async function parsePredictionResponse(response: Response): Promise<PredictionEnvelope> {
  if (!response.ok) throw await responseError(response);
  const prediction = predictionEnvelope(await parseJSONResponse(response));
  if (!prediction) throw new Error("Invalid prediction response");
  return prediction;
}

export async function responseError(response: Response): Promise<HttpError> {
  const text = await readResponseText(response, MAX_ERROR_RESPONSE_LENGTH);
  let body: ErrorResponse = {};
  try {
    const parsed: unknown = JSON.parse(text);
    if (isErrorResponse(parsed)) body = parsed;
  } catch {
    // Use the response text.
  }
  const detail = Array.isArray(body.detail) ? body.detail : undefined;
  const message =
    (typeof body.error === "string" && body.error) ||
    (typeof body.detail === "string" && body.detail) ||
    text ||
    `HTTP ${response.status}`;
  return new HttpError(message, response.status, detail);
}

export async function parseJSONResponse(response: Response): Promise<unknown> {
  const parsed: unknown = JSON.parse(await readResponseText(response, MAX_JSON_RESPONSE_LENGTH));
  return parsed;
}

function isErrorResponse(value: unknown): value is ErrorResponse {
  return (
    isObject(value) &&
    (value.error === undefined || typeof value.error === "string") &&
    (value.detail === undefined || typeof value.detail === "string" || Array.isArray(value.detail))
  );
}

async function readResponseText(response: Response, maxLength: number): Promise<string> {
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
    while (true) {
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
