import { isJsonObject, type JsonObject } from "@/types/json";
import { predictionEnvelope, type PredictionEnvelope } from "@/types/prediction";

const MAX_JSON_RESPONSE_LENGTH = 16 * 1024 * 1024;
const MAX_ERROR_RESPONSE_LENGTH = 1024 * 1024;

/** Preserves status and structured validation details from an unsuccessful HTTP response. */
export class HttpError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly detail?: unknown[],
  ) {
    super(message);
  }
}

/** Rejects unsuccessful responses and parses a size-bounded prediction envelope. */
export async function parsePredictionResponse(response: Response): Promise<PredictionEnvelope> {
  if (!response.ok) throw await responseError(response);
  const prediction = predictionEnvelope(await parseJSONResponse(response));
  if (!prediction) throw new Error("Invalid prediction response");
  return prediction;
}

/** Converts a bounded JSON or text error response into an `HttpError`. */
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

/** Reads JSON under the response-size limit as untrusted data. */
export async function parseJSONResponse(response: Response): Promise<unknown> {
  return JSON.parse(await readResponseText(response, MAX_JSON_RESPONSE_LENGTH));
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
