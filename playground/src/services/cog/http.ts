import type { JsonObject } from "@/types/json";
import type { PredictionEnvelope } from "@/types/prediction";

const MAX_JSON_RESPONSE_LENGTH = 16 * 1024 * 1024;
const MAX_ERROR_RESPONSE_LENGTH = 1024 * 1024;

export class HttpError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly detail?: unknown[],
  ) {
    super(message);
  }
}

export async function parsePredictionResponse(response: Response): Promise<PredictionEnvelope> {
  if (!response.ok) throw await responseError(response);
  return parseJSONResponse<PredictionEnvelope>(response);
}

export async function responseError(response: Response): Promise<HttpError> {
  const text = await readResponseText(response, MAX_ERROR_RESPONSE_LENGTH);
  let body: JsonObject = {};
  try {
    const parsed: unknown = JSON.parse(text);
    body = isJsonObject(parsed) ? parsed : {};
  } catch {
    // Use the response text below.
  }
  const detail = Array.isArray(body.detail) ? body.detail : undefined;
  const message =
    (typeof body.error === "string" && body.error) ||
    (typeof body.detail === "string" && body.detail) ||
    text ||
    `HTTP ${response.status}`;
  return new HttpError(message, response.status, detail);
}

export async function parseJSONResponse<T>(response: Response): Promise<T> {
  return JSON.parse(await readResponseText(response, MAX_JSON_RESPONSE_LENGTH)) as T;
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

function isJsonObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
