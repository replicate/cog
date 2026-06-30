import { el, clear } from "./dom.js";
import { CogApi } from "./api.js";
import { buildForm } from "./form.js";
import { resolveRef, defaultInput } from "./schema.js";
import { toggleTheme, currentTheme } from "./theme.js";
import {
  createJSONEditor,
  destroyEditor,
  refreshEditorThemes,
  formatEditor,
} from "./editor.js";
import {
  setBadge,
  showError,
  clearError,
  renderValidationErrors,
  renderMetrics,
  renderOutput,
  renderText,
} from "./output.js";

const DEFAULT_TARGET = "http://localhost:8393";
const STORAGE_KEY = "cog-playground-target";
const TERMINAL = ["succeeded", "failed", "canceled"];

const api = new CogApi();

const state = {
  schema: null,
  inputSchema: null,
  outputSchema: null,
  supportsStreaming: false,
  supportsAsync: false,
  form: null,
  mode: "form", // "form" | "json"
  runMode: "sync", // "sync" | "stream" | "async"
  running: false,
  abort: null,
  eventSource: null,
  lastId: null,
  metrics: {},
  loadToken: 0,
  healthTimer: null,
  showLive: false, // a run is driving the toggled output view
  outputValue: null, // current output (array of chunks, scalar, or object)
  rawEvents: [], // raw frames/payloads exactly as received, for the Raw view
  outputView: "text", // "text" | "raw"
  webhookBase: "",
  jsonEditor: null, // Ace editor for the Form->JSON input view
  outputEditor: null, // read-only Ace editor for JSON/raw output
};

const dom = {};
const DOM_IDS = [
  "health-badge", "version-info", "target-url", "connect-btn", "target-status",
  "schema-link", "theme-toggle", "setup-panel", "setup-status", "setup-logs",
  "schema-error", "form-container", "json-container", "json-editor",
  "json-error", "json-format", "mode-form", "mode-json", "run-mode",
  "run-mode-sync", "run-mode-stream", "run-mode-async", "prediction-id",
  "webhook-options", "webhook-base-note", "stream-hint", "run-btn", "stop-btn",
  "reset-btn", "result-status", "output-view", "output-view-text",
  "output-view-raw", "metrics-container", "error-container", "output-container",
];

async function init() {
  for (const id of DOM_IDS) dom[id] = document.getElementById(id);

  state.jsonEditor = createJSONEditor(dom["json-editor"], { autosize: false });

  const params = new URLSearchParams(location.search);
  dom["target-url"].value =
    params.get("target") || localStorage.getItem(STORAGE_KEY) || DEFAULT_TARGET;
  refreshThemeLabel();

  const config = await api.getConfig();
  state.webhookBase = config.webhookBase || "";

  dom["connect-btn"].addEventListener("click", connect);
  dom["target-url"].addEventListener("keydown", (e) => {
    if (e.key === "Enter") connect();
  });
  dom["theme-toggle"].addEventListener("click", () => {
    toggleTheme();
    refreshThemeLabel();
    refreshEditorThemes();
  });
  dom["run-btn"].addEventListener("click", run);
  dom["stop-btn"].addEventListener("click", stop);
  dom["reset-btn"].addEventListener("click", reset);
  dom["mode-form"].addEventListener("click", () => setMode("form"));
  dom["mode-json"].addEventListener("click", () => setMode("json"));
  dom["json-format"].addEventListener("click", formatJSON);
  dom["run-mode-sync"].addEventListener("click", () => setRunMode("sync"));
  dom["run-mode-stream"].addEventListener("click", () => setRunMode("stream"));
  dom["run-mode-async"].addEventListener("click", () => setRunMode("async"));
  dom["output-view-text"].addEventListener("click", () => setOutputView("text"));
  dom["output-view-raw"].addEventListener("click", () => setOutputView("raw"));

  connect();
}

function refreshThemeLabel() {
  dom["theme-toggle"].textContent = currentTheme() === "dark" ? "Light" : "Dark";
}

function connect() {
  const url = dom["target-url"].value.trim();
  if (!url) return;
  api.setTarget(url);
  localStorage.setItem(STORAGE_KEY, url);
  dom["schema-link"].href =
    "/proxy/openapi.json?cog_target=" + encodeURIComponent(url);
  history.replaceState(null, "", "?target=" + encodeURIComponent(url));

  startHealthPolling();
  loadSchema();
}

function startHealthPolling() {
  if (state.healthTimer) clearInterval(state.healthTimer);
  pollHealth();
  state.healthTimer = setInterval(pollHealth, 5000);
}

async function pollHealth() {
  try {
    const data = await api.health();
    setBadge(dom["health-badge"], data.status);
    dom["target-status"].textContent = data.user_healthcheck_error || "";
    updateSetup(data.setup);
    updateVersion(data.version);
  } catch {
    setBadge(dom["health-badge"], "unreachable");
    dom["target-status"].textContent = "target unreachable";
  }
}

function updateSetup(setup) {
  if (!setup) {
    dom["setup-panel"].hidden = true;
    return;
  }
  dom["setup-panel"].hidden = false;
  setBadge(dom["setup-status"], setup.status);
  dom["setup-logs"].textContent = setup.logs || "";
}

function updateVersion(version) {
  if (!version) return;
  const parts = [];
  if (version.coglet) parts.push("coglet " + version.coglet);
  if (version.cog) parts.push("cog " + version.cog);
  if (version.python) parts.push("py " + version.python);
  dom["version-info"].textContent = parts.join(" · ");
}

async function loadSchema() {
  const token = ++state.loadToken;
  try {
    const schema = await api.schema();
    if (token !== state.loadToken) return; // superseded by a newer connect
    applySchema(schema);
    dom["schema-error"].classList.remove("visible");
  } catch (err) {
    if (token !== state.loadToken) return;
    showError(dom["schema-error"], "Waiting for schema… (" + err.message + ")");
    setTimeout(() => {
      if (token === state.loadToken) loadSchema();
    }, 3000);
  }
}

function applySchema(schema) {
  state.schema = schema;
  const schemas = (schema.components || {}).schemas || {};
  const paths = schema.paths || {};

  state.inputSchema = resolveRef(schema, schemas.Input || {});
  state.outputSchema = resolveRef(schema, schemas.Output || {});
  state.supportsStreaming =
    ((paths["/predictions"] || {}).post || {})["x-cog-streaming"] === true;
  // Async predictions are observed via webhooks and cancelled via the cancel
  // endpoint; treat the presence of that endpoint as the async-capable signal.
  state.supportsAsync = !!paths["/predictions/{prediction_id}/cancel"];

  state.runMode = state.supportsStreaming ? "stream" : "sync";
  configureRunModes();
  rebuildForm(defaultInput(schema, state.inputSchema));
}

function currentOutputSchema() {
  return state.outputSchema || {};
}

// configureRunModes shows only the run modes the model advertises.
function configureRunModes() {
  dom["run-mode-stream"].hidden = !state.supportsStreaming;
  dom["run-mode-async"].hidden = !state.supportsAsync;
  dom["run-mode"].hidden = !(state.supportsStreaming || state.supportsAsync);

  const available = { sync: true, stream: state.supportsStreaming, async: state.supportsAsync };
  if (!available[state.runMode]) state.runMode = "sync";

  const out = state.outputSchema || {};
  const isIterator =
    out["x-cog-array-type"] === "iterator" || out["x-cog-array-display"] === "concatenate";
  dom["stream-hint"].textContent =
    !state.supportsStreaming && isIterator
      ? "Add @cog.streaming to run() for real-time output"
      : "";

  updateRunModeButtons();
}

function updateRunModeButtons() {
  for (const m of ["sync", "stream", "async"]) {
    dom["run-mode-" + m].classList.toggle("active", state.runMode === m);
  }
  dom["webhook-options"].hidden = state.runMode !== "async";
  dom["webhook-base-note"].textContent = state.webhookBase
    ? "Webhook: " + state.webhookBase + "/webhook/…"
    : "No webhook host configured (set --webhook-host).";
}

function setRunMode(mode) {
  if (dom["run-mode-" + mode].hidden) return;
  state.runMode = mode;
  updateRunModeButtons();
}

// --- input mode toggle (Form vs JSON) ---
function setMode(mode) {
  if (mode === state.mode) return;
  if (mode === "json") {
    syncFormToJSON();
  } else {
    const parsed = parseEditor();
    if (parsed === undefined) return; // invalid JSON: stay in JSON mode
    rebuildForm(parsed);
  }
  state.mode = mode;
  dom["mode-form"].classList.toggle("active", mode === "form");
  dom["mode-json"].classList.toggle("active", mode === "json");
  dom["form-container"].hidden = mode !== "form";
  dom["json-container"].hidden = mode !== "json";
  if (mode === "json") state.jsonEditor.resize();
}

function rebuildForm(value) {
  state.form = buildForm(dom["form-container"], state.schema, state.inputSchema, value);
  if (state.mode === "json") syncFormToJSON();
}

function syncFormToJSON() {
  const value = state.form ? state.form.collect() : {};
  state.jsonEditor.setValue(JSON.stringify(value, null, 2), -1);
  dom["json-error"].textContent = "";
}

function parseEditor() {
  const raw = state.jsonEditor.getValue().trim();
  if (raw === "") {
    dom["json-error"].textContent = "";
    return {};
  }
  try {
    const parsed = JSON.parse(raw);
    dom["json-error"].textContent = "";
    return parsed;
  } catch (err) {
    dom["json-error"].textContent = "Invalid JSON: " + err.message;
    return undefined;
  }
}

function formatJSON() {
  if (!formatEditor(state.jsonEditor)) {
    dom["json-error"].textContent = "Invalid JSON";
  } else {
    dom["json-error"].textContent = "";
  }
}

// --- output view toggle (Text vs Raw) ---
function setOutputView(view) {
  state.outputView = view;
  dom["output-view-text"].classList.toggle("active", view === "text");
  dom["output-view-raw"].classList.toggle("active", view === "raw");
  if (state.showLive) renderLive();
}

function showOutputView(visible) {
  dom["output-view"].hidden = !visible;
}

// renderLive renders the current output in the selected view. Raw shows the
// exact frames/payloads in a read-only code editor; Text concatenates
// plain-string output, renders media, or shows structured JSON in a read-only
// code editor (with folding) for objects/arrays.
function renderLive() {
  // In Raw view the metrics are already part of the payload, so the separate
  // metrics table is redundant.
  dom["metrics-container"].hidden = state.outputView === "raw";
  if (state.outputView === "raw") {
    renderCode(state.rawEvents.join("\n\n"));
    return;
  }
  const value = state.outputValue;
  if (value == null) {
    resetOutputArea();
    renderText(dom["output-container"], "", state.running);
  } else if (isPlainText(value)) {
    resetOutputArea();
    renderText(dom["output-container"], value, state.running);
  } else if (Array.isArray(value) && value.length > 0 && value.every(isPlainText)) {
    resetOutputArea();
    renderText(dom["output-container"], value.join(""), state.running);
  } else if (hasMedia(value)) {
    resetOutputArea();
    renderOutput(dom["output-container"], value, currentOutputSchema());
  } else {
    renderCode(JSON.stringify(value, null, 2));
  }
}

// renderCode shows text in the read-only output editor, reusing the instance
// across updates (e.g. streaming Raw) rather than recreating it.
function renderCode(text) {
  if (!state.outputEditor) {
    clear(dom["output-container"]);
    const host = el("div", { class: "ace-json ace-output" });
    dom["output-container"].append(host);
    state.outputEditor = createJSONEditor(host, {
      readOnly: true,
      value: text,
      autosize: false,
    });
  } else {
    state.outputEditor.setValue(text, -1);
  }
  state.outputEditor.resize();
}

function resetOutputArea() {
  if (state.outputEditor) {
    destroyEditor(state.outputEditor);
    state.outputEditor = null;
  }
  clear(dom["output-container"]);
}

function isPlainText(x) {
  return typeof x === "string" && !x.startsWith("data:") && !/^https?:\/\//i.test(x);
}

function isMediaString(s) {
  return typeof s === "string" && (s.startsWith("data:") || /^https?:\/\//i.test(s));
}

function hasMedia(value) {
  return isMediaString(value) || (Array.isArray(value) && value.some(isMediaString));
}

// --- running ---
function activeInput() {
  return state.mode === "json" ? parseEditor() : state.form.collect();
}

function currentId() {
  const id = dom["prediction-id"].value.trim();
  return id || undefined;
}

function run() {
  if (state.running) return;
  const input = activeInput();
  if (input === undefined) return; // invalid JSON

  clearError(dom["error-container"]);
  resetOutputArea();
  renderMetrics(dom["metrics-container"], {});
  dom["result-status"].textContent = "";
  state.metrics = {};
  state.outputValue = null;
  state.rawEvents = [];
  state.lastId = currentId() || null;

  setRunning(true);
  state.abort = new AbortController();

  if (state.runMode === "async") runAsync(input);
  else if (state.runMode === "stream") runStream(input);
  else runSync(input);
}

async function runSync(input) {
  state.showLive = true;
  showOutputView(true);
  setBadge(dom["result-status"], "processing");
  try {
    const response = await api.submit({
      endpoint: "/predictions",
      id: currentId(),
      input,
      signal: state.abort.signal,
    });
    state.lastId = response.id || state.lastId;
    applyEnvelope(response);
    state.outputValue = response.error ? null : response.output ?? null;
    state.rawEvents = [JSON.stringify(response, null, 2)];
  } catch (err) {
    reportRunError(err);
  } finally {
    setRunning(false);
    renderLive();
  }
}

async function runStream(input) {
  state.showLive = true;
  state.outputValue = [];
  showOutputView(true);
  setBadge(dom["result-status"], "processing");
  try {
    for await (const event of api.stream({
      endpoint: "/predictions",
      id: currentId(),
      input,
      signal: state.abort.signal,
    })) {
      if (event.raw != null) state.rawEvents.push(event.raw);
      handleStreamEvent(event);
      renderLive();
    }
  } catch (err) {
    reportRunError(err);
  } finally {
    setRunning(false);
    renderLive(); // final render without the streaming cursor
  }
}

// runAsync submits with Prefer: respond-async and a webhook pointing at the
// playground server's sink, then observes delivered events over /events (SSE).
async function runAsync(input) {
  state.showLive = true;
  showOutputView(true);
  setBadge(dom["result-status"], "starting");

  const token = crypto.randomUUID();
  const webhook = state.webhookBase ? `${state.webhookBase}/webhook/${token}` : null;
  if (webhook) {
    const es = new EventSource("/events?token=" + token);
    state.eventSource = es;
    es.onmessage = (e) => {
      state.rawEvents.push(e.data);
      let data;
      try {
        data = JSON.parse(e.data);
      } catch {
        renderLive();
        return;
      }
      applyEnvelope(data);
      if (!data.error && data.output != null) {
        state.outputValue = data.output;
      }
      renderLive();
      if (TERMINAL.includes(data.status)) finishAsync();
    };
  }

  try {
    const response = await api.submit({
      endpoint: "/predictions",
      id: currentId(),
      input,
      asyncMode: true,
      webhook,
      webhookFilter: collectWebhookFilter(),
      signal: state.abort.signal,
    });
    state.lastId = response.id || state.lastId;
    setBadge(dom["result-status"], response.status || "starting");
    if (!webhook) finishAsync(); // nothing to observe; stop the spinner
  } catch (err) {
    reportRunError(err);
    finishAsync();
  }
}

function finishAsync() {
  if (state.eventSource) {
    state.eventSource.close();
    state.eventSource = null;
  }
  setRunning(false);
  renderLive();
}

function collectWebhookFilter() {
  return Array.from(document.querySelectorAll(".wh-filter:checked")).map((c) => c.value);
}

function handleStreamEvent(event) {
  const data = event.data;
  switch (event.type) {
    case "start":
      setBadge(dom["result-status"], data.status || "starting");
      break;
    case "output":
      if (!Array.isArray(state.outputValue)) state.outputValue = [];
      state.outputValue.push(data.chunk);
      break;
    case "metric":
      state.metrics[data.name] = data.value;
      renderMetrics(dom["metrics-container"], state.metrics);
      break;
    case "error":
      // Transport-level SSE error (e.g. replay truncated, broadcast lagged).
      showError(dom["error-container"], data.error || "stream error");
      break;
    case "completed":
      applyEnvelope(data);
      break;
  }
}

// applyEnvelope updates status/metrics/error from a prediction envelope (shared
// by sync responses, the streamed "completed" event, and webhooks).
function applyEnvelope(data) {
  if (!data) return;
  setBadge(dom["result-status"], data.status || "unknown");
  if (data.metrics) renderMetrics(dom["metrics-container"], data.metrics);
  if (data.error) showError(dom["error-container"], data.error);
}

function reportRunError(err) {
  if (err.name === "AbortError") {
    setBadge(dom["result-status"], "canceled");
    return;
  }
  // Surface the error in the Raw view too, not just the error banner.
  state.rawEvents = [
    JSON.stringify(err.detail ? { detail: err.detail } : { error: err.message }, null, 2),
  ];
  if (err.detail) {
    renderValidationErrors(dom["error-container"], err.detail);
  } else {
    showError(dom["error-container"], err.message);
  }
  setBadge(dom["result-status"], "failed");
}

function setRunning(running) {
  state.running = running;
  dom["run-btn"].disabled = running;
  dom["stop-btn"].disabled = !running;
  dom["reset-btn"].disabled = running;
}

// stop aborts the local request/stream and, if we know the prediction id, asks
// the model to cancel it (the only way to stop an async prediction).
function stop() {
  if (state.abort) state.abort.abort();
  if (state.lastId) api.cancel("/predictions", state.lastId).catch(() => {});
  finishAsync();
}

function reset() {
  if (state.running || !state.schema) return;
  clearError(dom["error-container"]);
  resetOutputArea();
  renderMetrics(dom["metrics-container"], {});
  dom["metrics-container"].hidden = false;
  dom["result-status"].textContent = "";
  state.showLive = false;
  state.outputValue = null;
  state.rawEvents = [];
  showOutputView(false);
  rebuildForm(defaultInput(state.schema, state.inputSchema));
}

document.addEventListener("DOMContentLoaded", init);
