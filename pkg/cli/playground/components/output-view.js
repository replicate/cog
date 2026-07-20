// @ts-check

import { formatDuration, formatValue, renderTerminalText } from "../lib/format.js";
import { JsonEditor } from "./json-editor.js";
import { element, followTail, segmentedTabs, statusBadge, tabId, tabPanelId } from "./dom.js";

export class OutputView {
  /** @param {HTMLElement} root @param {import("../lib/state/prediction-state.js").PredictionState} state */
  constructor(root, state) {
    this.root = root;
    this.state = state;
    this.view = "output";
    this.mounted = new Set(["output"]);
    /** @type {Map<string, HTMLElement>} */ this.panels = new Map();
    /** @type {Map<string, Set<JsonEditor>>} */ this.editors = new Map();
    /** @type {JsonEditor | undefined} */ this.rawEditor = undefined;
    /** @type {HTMLOListElement | undefined} */ this.timelineList = undefined;
    /** @type {Map<string, {node:HTMLLIElement,time:HTMLTimeElement,kind:HTMLSpanElement,label:HTMLElement,details?:HTMLDetailsElement,editor?:JsonEditor,value?:unknown}>} */
    this.timelineItems = new Map();
    /** @type {boolean | undefined} */ this.lastRunning = undefined;
    /** @type {HTMLElement | undefined} */ this.outputRegion = undefined;
    /** @type {HTMLElement | undefined} */ this.outputBody = undefined;
    this.outputKind = "";
    /** @type {string[]} */ this.outputItemKinds = [];
    this.metricsValue = "";
    /** @type {HTMLTableElement | undefined} */ this.metricsTable = undefined;
    /** @type {HTMLPreElement | undefined} */ this.logsNode = undefined;
    this.renderFrame = 0;
    /** @type {Map<string, boolean>} */ this.following = new Map([
      ["output", true],
      ["logs", true],
      ["timeline", true],
    ]);
    /** @type {Map<string, boolean>} */ this.requestOpen = new Map();
    this.unsubscribe = state.subscribe(() => this.scheduleRender());
    this.render();
  }
  scheduleRender() {
    if (this.renderFrame) return;
    this.renderFrame = requestAnimationFrame(() => {
      this.renderFrame = 0;
      this.render();
    });
  }
  render() {
    const focusedId = this.root.contains(document.activeElement)
      ? document.activeElement?.id
      : undefined;
    const envelope = this.state.envelope;
    const hasLogs = Boolean(envelope?.logs?.trim());
    if (this.view === "logs" && !hasLogs) {
      this.view = "output";
      this.destroyPanel("logs");
    }
    const items = [
      { value: "output", label: "Output" },
      { value: "raw", label: "Raw" },
      ...(hasLogs ? [{ value: "logs", label: "Logs" }] : []),
      { value: "timeline", label: "Timeline" },
      { value: "request", label: "Request" },
    ];
    const section = element("section", { id: "output-panel" });
    const title = element(
      "div",
      { className: "response-title" },
      element("h2", { text: "Response" }),
    );
    if (envelope?.status) title.append(statusBadge(envelope.status));
    section.append(
      element(
        "div",
        { className: "panel-head" },
        title,
        segmentedTabs("response-view", "Response details", items, this.view, (view) => {
          if (view === this.view) return;
          this.view = view;
          this.mounted.add(view);
          this.render();
        }),
      ),
    );
    section.append(
      element("output", {
        className: "sr-only",
        "aria-live": "polite",
        text: this.state.running
          ? "Prediction running"
          : envelope?.status
            ? `Prediction ${envelope.status}`
            : "",
      }),
    );
    const displayedError = this.state.error || envelope?.error;
    if (displayedError)
      section.append(
        element("div", { className: "error-container", role: "alert", text: displayedError }),
      );
    const runningChanged = this.lastRunning !== this.state.running;
    if (this.state.running && runningChanged)
      for (const view of ["output", "logs", "timeline"]) this.following.set(view, true);
    for (const item of items) {
      if (!this.mounted.has(item.value)) continue;
      let panel = this.panels.get(item.value);
      if (!panel) {
        panel = element("div", {
          id: tabPanelId("response-view", item.value),
          role: "tabpanel",
          "aria-labelledby": tabId("response-view", item.value),
        });
        this.panels.set(item.value, panel);
      }
      const active = item.value === this.view;
      panel.hidden = !active;
      panel.tabIndex = active ? 0 : -1;
      if (active || runningChanged || !panel.dataset.rendered) this.updatePanel(item.value, panel);
      section.append(panel);
    }
    this.root.replaceChildren(section);
    const focused = focusedId ? this.root.querySelector(`#${CSS.escape(focusedId)}`) : null;
    if (focused instanceof HTMLElement) focused.focus({ preventScroll: true });
    this.lastRunning = this.state.running;
    if (this.state.running && this.following.get(this.view) !== false)
      requestAnimationFrame(() => {
        const scroll = this.panels
          .get(this.view)
          ?.querySelector(".live-output, .inspector-logs, .trace-timeline");
        if (scroll) scroll.scrollTop = scroll.scrollHeight;
      });
  }
  /** @param {string} view @param {HTMLElement} panel */
  updatePanel(view, panel) {
    if (view === "output") {
      const hasResult = Boolean(
        this.state.envelope || this.state.rawEvents.length || this.state.output !== undefined,
      );
      if (!hasResult) {
        this.resetOutput();
        panel.replaceChildren(
          element("div", {
            className: "empty-output",
            text: "Run a prediction to see its output.",
          }),
        );
      } else if (!this.outputRegion) {
        this.outputRegion = this.renderOutput();
        panel.replaceChildren(this.outputRegion);
      } else {
        this.updateOutput();
      }
      panel.dataset.rendered = "true";
      return;
    }
    if (view === "raw") {
      const hasResult = Boolean(
        this.state.envelope || this.state.rawEvents.length || this.state.output !== undefined,
      );
      if (!hasResult) {
        this.destroyEditors(view);
        panel.replaceChildren(
          element("div", {
            className: "empty-output",
            text: "Run a prediction to see its raw response.",
          }),
        );
      } else {
        const streaming = this.state.mode !== "sync";
        const value = streaming
          ? this.state.rawEvents.join("\n\n")
          : (this.state.rawEvents[0] ?? JSON.stringify(this.state.envelope ?? {}, null, 2));
        if (this.rawEditor)
          this.rawEditor.update({
            value,
            followTail: streaming && this.state.running,
            active: view === this.view,
          });
        else {
          panel.replaceChildren(
            this.editor(value, "Raw prediction response", "response-editor", view, {
              followTail: streaming && this.state.running,
              active: view === this.view,
            }),
          );
        }
      }
      panel.dataset.rendered = "true";
      return;
    }
    if (view === "timeline") {
      this.updateTimeline(panel);
      panel.dataset.rendered = "true";
      return;
    }
    if (view === "logs") {
      if (!this.logsNode) {
        this.logsNode = element("pre", {
          id: "response-logs",
          className: "inspector-logs",
          "aria-label": "Prediction logs",
          tabindex: "0",
        });
        this.logsNode.addEventListener("scroll", () => {
          if (this.logsNode) this.following.set("logs", followTail(this.logsNode));
        });
      }
      this.logsNode.textContent = renderTerminalText(this.state.envelope?.logs ?? "");
      panel.replaceChildren(this.logsNode);
      panel.dataset.rendered = "true";
      return;
    }
    if (view === "request")
      for (const details of panel.querySelectorAll("details.inspector-document")) {
        if (!(details instanceof HTMLDetailsElement)) continue;
        const summary = details.querySelector("summary")?.textContent;
        if (summary) this.requestOpen.set(summary, details.open);
      }
    this.destroyEditors(view);
    panel.replaceChildren(this.renderView(view));
    panel.dataset.rendered = "true";
  }
  /** @param {string} view */
  renderView(view) {
    const hasResult = Boolean(
      this.state.envelope || this.state.rawEvents.length || this.state.output !== undefined,
    );
    if (view === "output")
      return hasResult
        ? this.renderOutput()
        : element("div", {
            className: "empty-output",
            text: "Run a prediction to see its output.",
          });
    if (view === "logs") return this.renderLogs();
    if (!this.state.trace)
      return element("div", {
        className: "empty-output",
        text:
          view === "timeline"
            ? "Run a prediction to see its event timeline."
            : "Run a prediction to inspect its request and response metadata.",
      });
    return view === "timeline"
      ? this.renderTimeline(this.state.trace)
      : this.renderRequest(this.state.trace);
  }
  renderOutput() {
    const region = element("section", {
      className: "live-output",
      "aria-label": "Prediction output",
      "aria-busy": String(this.state.running),
      tabindex: "0",
    });
    region.addEventListener("scroll", () => {
      this.following.set("output", followTail(region));
    });
    this.outputBody = element("div", { className: "output-body" });
    region.append(this.outputBody);
    this.outputRegion = region;
    this.updateOutput();
    return region;
  }
  updateOutput() {
    if (!this.outputRegion || !this.outputBody) return;
    this.outputRegion.setAttribute("aria-busy", String(this.state.running));
    const metrics = this.state.envelope?.metrics;
    const metricsValue = metrics ? safeJSON(metrics) : "";
    if (metricsValue !== this.metricsValue) {
      this.metricsValue = metricsValue;
      this.metricsTable?.remove();
      this.metricsTable = metrics ? this.renderMetrics(metrics) : undefined;
      if (this.metricsTable) this.outputRegion.insertBefore(this.metricsTable, this.outputBody);
    }
    const value =
      Array.isArray(this.state.output) &&
      this.state.output.every((item) => typeof item === "string") &&
      !this.state.output.some((item) => isMediaValue(item))
        ? this.state.output.join("")
        : this.state.output;
    if (value === undefined || value === null) {
      this.updateEmptyOutput();
      return;
    }
    if (Array.isArray(value) && value.every((item) => typeof item === "string")) {
      this.updateOutputItems(value);
      return;
    }
    if (typeof value === "string" && isMediaValue(value)) {
      this.updateOutputItems([value]);
      return;
    }
    this.updateTextOutput(
      typeof value === "string" ? value : (JSON.stringify(value, null, 2) ?? String(value)),
      this.state.running,
    );
  }
  updateEmptyOutput() {
    if (!this.outputBody) return;
    if (this.outputKind !== "empty") {
      this.outputKind = "empty";
      this.outputItemKinds = [];
      this.outputBody.replaceChildren(element("div", { className: "empty-output" }));
    }
    const empty = this.outputBody.firstElementChild;
    if (empty)
      empty.textContent = this.state.running
        ? "Waiting for output..."
        : "Prediction returned no output.";
  }
  /** @param {string} value @param {boolean} running */
  updateTextOutput(value, running) {
    if (!this.outputBody) return;
    if (this.outputKind !== "text") {
      this.outputKind = "text";
      this.outputItemKinds = [];
      const text = document.createTextNode(value);
      this.outputBody.replaceChildren(element("pre", { className: "text-output" }, text));
    } else {
      const text = this.outputBody.querySelector(".text-output")?.firstChild;
      if (text instanceof Text && text.data !== value) text.data = value;
    }
    const output = this.outputBody.querySelector(".text-output");
    if (!output) return;
    const cursor = output.querySelector(".streaming-cursor");
    if (running && !cursor) output.append(element("span", { className: "streaming-cursor" }));
    else if (!running) cursor?.remove();
  }
  /** @param {string[]} values */
  updateOutputItems(values) {
    if (!this.outputBody) return;
    if (this.outputKind !== "items") {
      this.outputKind = "items";
      this.outputItemKinds = [];
      this.outputBody.replaceChildren();
    }
    values.forEach((value, index) => {
      const kind = mediaKind(value) ?? "text";
      let item = this.outputBody?.children[index];
      if (!(item instanceof HTMLElement) || this.outputItemKinds[index] !== kind) {
        const next = element("div", { className: "output-item" });
        if (item) item.replaceWith(next);
        else this.outputBody?.append(next);
        item = next;
        this.outputItemKinds[index] = kind;
      }
      updateOutputItem(item, kind, value);
    });
    while (this.outputBody.children.length > values.length)
      this.outputBody.lastElementChild?.remove();
    this.outputItemKinds.length = values.length;
  }
  resetOutput() {
    this.outputRegion = undefined;
    this.outputBody = undefined;
    this.outputKind = "";
    this.outputItemKinds = [];
    this.metricsValue = "";
    this.metricsTable = undefined;
  }
  /** @param {Record<string, unknown>} metrics */
  renderMetrics(metrics) {
    const body = element("tbody");
    for (const [name, value] of Object.entries(metrics))
      body.append(
        element(
          "tr",
          {},
          element("th", { scope: "row", text: name }),
          element("td", { text: typeof value === "string" ? value : safeJSON(value) }),
        ),
      );
    return element(
      "table",
      { className: "metrics-table" },
      element("caption", { className: "sr-only", text: "Prediction metrics" }),
      body,
    );
  }
  renderLogs() {
    const logs = element("pre", {
      className: "inspector-logs",
      "aria-label": "Prediction logs",
      tabindex: "0",
      text: renderTerminalText(this.state.envelope?.logs ?? ""),
    });
    logs.addEventListener("scroll", () => {
      this.following.set("logs", followTail(logs));
    });
    return logs;
  }
  /** @param {import("../types").RequestTrace} trace */
  renderTimeline(trace) {
    const list = element("ol", { className: "trace-timeline", tabindex: "0" });
    list.addEventListener("scroll", () => {
      this.following.set("timeline", followTail(list));
    });
    for (const event of trace.events) {
      const detail = element(
        "div",
        {},
        element("strong", {
          text: `${event.label}${(event.count ?? 1) > 1 ? ` × ${event.count}` : ""}`,
        }),
      );
      if (event.data !== undefined)
        detail.append(
          this.lazyDocument(
            "Payload",
            event.data,
            "Timeline event payload",
            "viewer-timeline",
            false,
            "timeline",
          ),
        );
      list.append(
        element(
          "li",
          { className: `trace-${event.kind}` },
          element("time", { text: formatDuration(event.elapsedMs) }),
          element("span", { className: "trace-kind", text: event.kind }),
          detail,
        ),
      );
    }
    return list;
  }
  /** @param {HTMLElement} panel */
  updateTimeline(panel) {
    const trace = this.state.trace;
    if (!trace) {
      this.destroyTimeline();
      panel.replaceChildren(
        element("div", {
          className: "empty-output",
          text: "Run a prediction to see its event timeline.",
        }),
      );
      return;
    }
    if (!this.timelineList) {
      this.timelineList = element("ol", { className: "trace-timeline", tabindex: "0" });
      this.timelineList.addEventListener("scroll", () => {
        if (this.timelineList) this.following.set("timeline", followTail(this.timelineList));
      });
    }
    if (!panel.contains(this.timelineList)) panel.replaceChildren(this.timelineList);
    const retained = new Set();
    for (const event of trace.events) {
      retained.add(event.id);
      let item = this.timelineItems.get(event.id);
      if (!item) {
        const time = element("time");
        const kind = element("span", { className: "trace-kind" });
        const label = element("strong");
        const detail = element("div", {}, label);
        const node = element("li", {}, time, kind, detail);
        item = { node, time, kind, label };
        this.timelineItems.set(event.id, item);
      }
      item.node.className = `trace-${event.kind}`;
      item.time.textContent = formatDuration(event.elapsedMs);
      item.kind.textContent = event.kind;
      item.label.textContent = `${event.label}${(event.count ?? 1) > 1 ? ` × ${event.count}` : ""}`;
      item.value = event.data;
      if (event.data === undefined && item.details) {
        if (item.editor) this.removeEditor("timeline", item.editor);
        item.details.remove();
        item.details = undefined;
        item.editor = undefined;
      } else if (event.data !== undefined) {
        if (!item.details) {
          const details = element("details", {}, element("summary", { text: "Payload" }));
          details.addEventListener("toggle", () => {
            if (!details.open) {
              if (item?.editor) this.removeEditor("timeline", item.editor);
              if (item) item.editor = undefined;
              details.querySelector(".json-editor")?.remove();
              return;
            }
            if (item?.editor) return;
            const wrapper = element("div");
            item.editor = this.createEditor(
              wrapper,
              formatValue(item?.value),
              "Timeline event payload",
              "viewer-timeline",
              "timeline",
            );
            details.append(wrapper);
          });
          item.details = details;
          item.label.parentElement?.append(details);
        }
        item.editor?.update({ value: formatValue(event.data) });
      }
      this.timelineList.append(item.node);
    }
    for (const [id, item] of this.timelineItems)
      if (!retained.has(id)) {
        if (item.editor) this.removeEditor("timeline", item.editor);
        item.node.remove();
        this.timelineItems.delete(id);
      }
  }
  /** @param {import("../types").RequestTrace} trace */
  renderRequest(trace) {
    const metrics = this.state.envelope?.metrics;
    const predictTime =
      typeof metrics?.predict_time === "number" ? metrics.predict_time * 1000 : undefined;
    const summary = element(
      "dl",
      { className: "request-summary" },
      summaryItem(
        "Request",
        element(
          "span",
          {},
          element("span", { className: "method-badge", text: trace.method }),
          ` ${trace.endpoint}`,
        ),
      ),
      summaryItem("Started", trace.startedAtLabel),
      summaryItem("Status", String(trace.responseStatus ?? "Waiting")),
    );
    if (predictTime !== undefined)
      summary.append(summaryItem("Prediction time", formatDuration(predictTime)));
    const root = element(
      "div",
      { className: "request-inspector" },
      summary,
      this.lazyDocument(
        "Request headers",
        trace.requestHeaders,
        "Request headers",
        "viewer-inspector",
        false,
        "request",
      ),
      this.lazyDocument(
        "Request body",
        trace.requestBody,
        "Request body",
        "viewer-inspector",
        true,
        "request",
      ),
    );
    if (trace.responseHeaders)
      root.append(
        this.lazyDocument(
          "Response headers",
          trace.responseHeaders,
          "Response headers",
          "viewer-inspector",
          false,
          "request",
        ),
      );
    if (trace.responseBody !== undefined)
      root.append(
        this.lazyDocument(
          "Response body",
          trace.responseBody,
          "Response body",
          "viewer-inspector viewer-response-body",
          true,
          "request",
          { autoHeight: false },
        ),
      );
    return root;
  }
  /** @param {string} summary @param {unknown} value @param {string} label @param {string} className @param {boolean} open @param {string} view @param {{autoHeight?:boolean}} [options] */
  lazyDocument(summary, value, label, className, open, view, options = {}) {
    const resolvedOpen = view === "request" ? (this.requestOpen.get(summary) ?? open) : open;
    const details = element(
      "details",
      { className: "inspector-document", open: resolvedOpen },
      element("summary", {
        id: `inspector-${summary.toLowerCase().replaceAll(" ", "-")}`,
        text: summary,
      }),
    );
    let mounted = false;
    const mount = () => {
      if (!details.open || mounted) return;
      mounted = true;
      details.append(this.editor(formatValue(value), label, className, view, options));
    };
    details.addEventListener("toggle", () => {
      if (view === "request") this.requestOpen.set(summary, details.open);
      mount();
    });
    if (resolvedOpen) mount();
    return details;
  }
  /** @param {string} value @param {string} label @param {string} className @param {string} view @param {{followTail?:boolean,active?:boolean,autoHeight?:boolean}} options */
  editor(value, label, className, view, options) {
    const wrapper = element("div", { className: `json-editor ${className}` });
    this.createEditor(wrapper, value, label, className, view, options);
    return wrapper;
  }
  /** @param {HTMLElement} wrapper @param {string} value @param {string} label @param {string} className @param {string} view @param {{followTail?:boolean,active?:boolean,autoHeight?:boolean}} [options] */
  createEditor(wrapper, value, label, className, view, options = {}) {
    wrapper.className = `json-editor ${className}`;
    const host = element("div");
    const editor = new JsonEditor(host, {
      value,
      label,
      readOnly: true,
      ...options,
      autoHeight: options.autoHeight ?? true,
    });
    let editors = this.editors.get(view);
    if (!editors) {
      editors = new Set();
      this.editors.set(view, editors);
    }
    editors.add(editor);
    if (view === "raw") this.rawEditor = editor;
    wrapper.append(
      host,
      element("button", {
        type: "button",
        className: "editor-copy",
        "aria-label": `Copy ${label}`,
        text: "Copy",
        onclick: () => editor.copy(),
      }),
    );
    return editor;
  }
  /** @param {string} view @param {JsonEditor} editor */
  removeEditor(view, editor) {
    editor.destroy();
    const editors = this.editors.get(view);
    editors?.delete(editor);
    if (!editors?.size) this.editors.delete(view);
  }
  /** @param {string} view */
  destroyEditors(view) {
    for (const editor of this.editors.get(view) ?? []) editor.destroy();
    this.editors.delete(view);
    if (view === "raw") this.rawEditor = undefined;
  }
  /** @param {string} view */
  destroyPanel(view) {
    if (view === "timeline") this.destroyTimeline();
    this.destroyEditors(view);
    this.panels.get(view)?.remove();
    this.panels.delete(view);
  }
  destroyTimeline() {
    this.destroyEditors("timeline");
    this.timelineItems.clear();
    this.timelineList?.remove();
    this.timelineList = undefined;
  }
  destroy() {
    this.unsubscribe();
    cancelAnimationFrame(this.renderFrame);
    for (const view of this.editors.keys()) this.destroyEditors(view);
  }
}

/** @param {unknown} value */
function safeJSON(value) {
  try {
    return JSON.stringify(value) ?? String(value);
  } catch {
    return String(value);
  }
}
/** @param {string} value */
function isMediaValue(value) {
  return mediaKind(value) !== undefined;
}
/** @param {string} value */
function mediaKind(value) {
  if (/^data:image\//i.test(value)) return "image";
  if (/^data:audio\//i.test(value)) return "audio";
  if (/^data:video\//i.test(value)) return "video";
  if (/^data:[^,]*,/i.test(value)) return "download";
  if (/^https?:\/\//i.test(value)) return "url";
  return undefined;
}
/** @param {Element} item @param {string} kind @param {string} value */
function updateOutputItem(item, kind, value) {
  if (kind === "text") {
    let output = item.querySelector(".text-output");
    if (!output) {
      output = element("pre", { className: "text-output" }, document.createTextNode(value));
      item.replaceChildren(output);
      return;
    }
    const text = output.firstChild;
    if (text instanceof Text && text.data !== value) text.data = value;
    return;
  }
  let output = item.firstElementChild;
  const expectedTag =
    kind === "image" ? "IMG" : kind === "audio" ? "AUDIO" : kind === "video" ? "VIDEO" : "A";
  if (!output || output.tagName !== expectedTag) {
    const next = media(value);
    if (next) item.replaceChildren(next);
    return;
  }
  const attribute = output instanceof HTMLAnchorElement ? "href" : "src";
  if (output.getAttribute(attribute) !== value) output.setAttribute(attribute, value);
  if (output instanceof HTMLAnchorElement && kind === "url") output.textContent = value;
}
/** @param {string} value */
function media(value) {
  if (!isMediaValue(value)) return null;
  if (/^data:image\//i.test(value)) return element("img", { src: value, alt: "Prediction output" });
  if (/^data:audio\//i.test(value)) return element("audio", { controls: true, src: value });
  if (/^data:video\//i.test(value)) return element("video", { controls: true, src: value });
  if (/^data:/i.test(value))
    return element("a", { href: value, download: "output", text: "Download file" });
  return element("a", { href: value, target: "_blank", rel: "noopener noreferrer", text: value });
}
/** @param {string} label @param {string|Node} value */
function summaryItem(label, value) {
  return element("div", {}, element("dt", { text: label }), element("dd", {}, value));
}
