// CogApi talks to the target model only through the playground's own reverse
// proxy (same origin). Every request carries the chosen target base URL in the
// X-Cog-Target header; the Go server forwards it. This avoids CORS (Cog sets
// none) and keeps SSE streaming working.

const PROXY_PREFIX = "/proxy";

export class CogApi {
  constructor() {
    this.target = "";
  }

  setTarget(url) {
    this.target = (url || "").trim().replace(/\/+$/, "");
  }

  _headers(extra) {
    return Object.assign({ "X-Cog-Target": this.target }, extra || {});
  }

  _url(endpoint, id) {
    return id
      ? `${PROXY_PREFIX}${endpoint}/${encodeURIComponent(id)}`
      : PROXY_PREFIX + endpoint;
  }

  _body(input, webhook, webhookFilter) {
    const body = { input };
    if (webhook) {
      body.webhook = webhook;
      body.webhook_events_filter = webhookFilter;
    }
    return body;
  }

  // getConfig returns playground server config (e.g. the webhook base URL).
  async getConfig() {
    try {
      const r = await fetch("/config");
      if (r.ok) return r.json();
    } catch {
      /* ignore */
    }
    return {};
  }

  async health() {
    const r = await fetch(PROXY_PREFIX + "/health-check", {
      headers: this._headers(),
    });
    if (!r.ok) throw new Error("HTTP " + r.status);
    return r.json();
  }

  async schema() {
    const r = await fetch(PROXY_PREFIX + "/openapi.json", {
      headers: this._headers(),
    });
    if (!r.ok) throw new Error("HTTP " + r.status);
    return r.json();
  }

  // submit runs a prediction/training in blocking (sync) or async mode. A
  // non-empty `id` makes the request idempotent (PUT). Returns the response
  // envelope (the 202 acknowledgement in async mode).
  async submit({ endpoint, id, input, asyncMode, webhook, webhookFilter, signal }) {
    const headers = this._headers({ "Content-Type": "application/json" });
    if (asyncMode) headers["Prefer"] = "respond-async";
    const r = await fetch(this._url(endpoint, id), {
      method: id ? "PUT" : "POST",
      headers,
      body: JSON.stringify(this._body(input, webhook, webhookFilter)),
      signal,
    });
    const body = await r.json().catch(() => ({}));
    if (!r.ok) throw httpError(r.status, body);
    return body;
  }

  // stream runs a prediction in SSE mode, yielding parsed { type, data } events.
  async *stream({ endpoint, id, input, webhook, webhookFilter, signal }) {
    const resp = await fetch(this._url(endpoint, id), {
      method: id ? "PUT" : "POST",
      headers: this._headers({
        "Content-Type": "application/json",
        Accept: "text/event-stream",
      }),
      body: JSON.stringify(this._body(input, webhook, webhookFilter)),
      signal,
    });
    if (!resp.ok) {
      const text = await resp.text();
      let body = {};
      try {
        body = JSON.parse(text);
      } catch {
        /* not JSON */
      }
      throw httpError(resp.status, body, text);
    }

    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      let sep;
      while ((sep = buffer.indexOf("\n\n")) >= 0) {
        const raw = buffer.slice(0, sep);
        buffer = buffer.slice(sep + 2);
        const event = parseSSEEvent(raw);
        if (event) {
          event.raw = raw;
          yield event;
        }
      }
    }
    if (buffer.trim()) {
      const event = parseSSEEvent(buffer);
      if (event) {
        event.raw = buffer;
        yield event;
      }
    }
  }

  // cancel requests cancellation of an in-flight prediction/training by id.
  async cancel(endpoint, id) {
    await fetch(`${PROXY_PREFIX}${endpoint}/${encodeURIComponent(id)}/cancel`, {
      method: "POST",
      headers: this._headers(),
    });
  }
}

// httpError builds an Error from a non-2xx response, attaching the structured
// `detail` array (422 validation errors) when present so callers can render
// field-level messages.
function httpError(status, body, fallbackText) {
  const detail = body && Array.isArray(body.detail) ? body.detail : null;
  const message =
    (body && (body.error || (typeof body.detail === "string" ? body.detail : null))) ||
    fallbackText ||
    "HTTP " + status;
  const err = new Error(message);
  err.status = status;
  if (detail) err.detail = detail;
  return err;
}

// parseSSEEvent parses one "event: ...\ndata: ..." block. The data payload is
// JSON-decoded when possible.
export function parseSSEEvent(block) {
  let eventType = "";
  const dataLines = [];
  for (const line of block.split("\n")) {
    if (line.startsWith("event:")) {
      eventType = line.slice(6).trim();
    } else if (line.startsWith("data:")) {
      dataLines.push(line.slice(5).replace(/^ /, ""));
    }
  }
  if (!eventType) return null;
  const dataStr = dataLines.join("\n");
  let data = dataStr;
  try {
    data = JSON.parse(dataStr);
  } catch {
    /* keep raw string */
  }
  return { type: eventType, data };
}

// fileToDataURI reads a File into a base64 data: URI suitable for a cog.Path
// input.
export function fileToDataURI(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result);
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });
}

export function formatBytes(bytes) {
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1048576) return (bytes / 1024).toFixed(1) + " KB";
  return (bytes / 1048576).toFixed(1) + " MB";
}
