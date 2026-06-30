import { el, clear } from "./dom.js";
import { mediaNode } from "./media.js";

// setBadge renders a status pill into `node`.
export function setBadge(node, status) {
  const label = status || "unknown";
  clear(node);
  node.append(el("span", { class: "badge badge-" + label.toLowerCase(), text: label }));
}

export function showError(node, message) {
  node.textContent = message;
  node.classList.add("visible");
}

export function clearError(node) {
  clear(node);
  node.classList.remove("visible");
}

// renderValidationErrors renders a 422 `detail` array as a field-by-field list.
export function renderValidationErrors(node, detail) {
  clear(node);
  node.append(el("div", { class: "error-title", text: "Validation error" }));
  const list = el("ul", { class: "error-list" });
  for (const item of detail) {
    const loc = Array.isArray(item.loc)
      ? item.loc.filter((p) => p !== "body").join(".")
      : "";
    const msg = item.msg || "invalid";
    list.append(el("li", { text: loc ? `${loc}: ${msg}` : msg }));
  }
  node.append(list);
  node.classList.add("visible");
}

export function renderMetrics(node, metrics) {
  clear(node);
  const keys = Object.keys(metrics || {});
  if (keys.length === 0) return;
  const table = el("table", { class: "metrics-table" });
  for (const key of keys) {
    table.append(
      el(
        "tr",
        {},
        el("td", { text: key }),
        el("td", { text: String(metrics[key]) }),
      ),
    );
  }
  node.append(table);
}

// renderOutput renders an arbitrary output value: data: URIs become media,
// http(s) strings become links, arrays recurse, objects render per-field
// (labelled by the Output schema's titles when available, else pretty-printed).
export function renderOutput(container, output, schema) {
  clear(container);
  appendOutput(container, output, schema);
}

function appendOutput(container, output, schema) {
  if (output === null || output === undefined) return;

  if (typeof output === "string") {
    container.append(wrap(stringNode(output)));
    return;
  }

  if (Array.isArray(output)) {
    const itemSchema = schema && schema.items;
    output.forEach((item, i) => {
      const itemDiv = el("div", { class: "output-item" });
      itemDiv.append(el("div", { class: "output-label", text: "[" + i + "]" }));
      appendOutput(itemDiv, item, itemSchema);
      container.append(itemDiv);
    });
    return;
  }

  if (typeof output === "object") {
    const props = schema && schema.properties;
    if (props) {
      for (const key of Object.keys(output)) {
        const fieldSchema = props[key] || {};
        const itemDiv = el("div", { class: "output-item" });
        itemDiv.append(
          el("div", { class: "output-label", text: fieldSchema.title || key }),
        );
        appendOutput(itemDiv, output[key], fieldSchema);
        container.append(itemDiv);
      }
      return;
    }
    container.append(wrap(el("pre", { text: JSON.stringify(output, null, 2) })));
    return;
  }

  container.append(wrap(el("pre", { text: String(output) })));
}

function stringNode(value) {
  const media = mediaNode(value);
  if (media) return media;
  if (value.startsWith("data:")) {
    return el("a", { href: value, download: "output", text: "Download file" });
  }
  if (value.startsWith("http://") || value.startsWith("https://")) {
    return el("a", { href: value, target: "_blank", text: value });
  }
  return el("pre", { text: value });
}

function wrap(node) {
  return el("div", { class: "output-item" }, node);
}

// renderText shows a text blob, optionally with a blinking cursor while a
// stream is still in flight. Used for the concatenated ("Text") stream view.
export function renderText(container, text, streaming = false) {
  clear(container);
  const pre = el("pre", { text });
  if (streaming) pre.append(el("span", { class: "streaming-cursor" }));
  container.append(wrap(pre));
  container.scrollTop = container.scrollHeight;
}

// renderRaw shows exactly what arrived over the wire (raw SSE frames / webhook
// payloads / the JSON response), unparsed.
export function renderRaw(container, frames, streaming = false) {
  clear(container);
  const pre = el("pre", { class: "mono", text: frames.join("\n\n") });
  if (streaming) pre.append(el("span", { class: "streaming-cursor" }));
  container.append(wrap(pre));
  container.scrollTop = container.scrollHeight;
}
