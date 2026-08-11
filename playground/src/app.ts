import "./app.css";

import { element, replaceChildrenPreservingFocus, statusBadge } from "@/components/dom.js";
import { InputView } from "@/components/input-view.js";
import { OutputView } from "@/components/output-view.js";
import { renderTerminalText } from "@/lib/format.js";
import { ConnectionState } from "@/lib/state/connection-state.js";
import { InputState } from "@/lib/state/input-state.js";
import { PredictionState } from "@/lib/state/prediction-state.js";
import { CogApi } from "@/lib/transport/api.js";
import type { PlaygroundCapabilities, RunMode, WebhookEvent } from "@/types.js";

type EditableWebhookEvent = Exclude<WebhookEvent, "completed">;

const EDITABLE_WEBHOOK_EVENTS: readonly EditableWebhookEvent[] = ["start", "output", "logs"];

export class App {
  root: HTMLElement;
  api: CogApi;
  connection: ConnectionState;
  input: InputState;
  prediction: PredictionState;
  runMode: RunMode;
  predictionId: string;
  webhookEvents: Set<EditableWebhookEvent>;
  lastCapabilities: PlaygroundCapabilities | undefined;
  schemaURL: string | undefined;
  header: HTMLElement;
  toolbar: HTMLDivElement;
  webhooks: HTMLDivElement;
  inputHost: HTMLDivElement;
  outputHost: HTMLDivElement;
  inputView: InputView;
  outputView: OutputView;
  unsubscribeConnection: () => void;
  unsubscribeInput: () => void;
  unsubscribePrediction: () => void;

  constructor(root: HTMLElement) {
    this.root = root;
    this.api = new CogApi();
    this.connection = new ConnectionState(this.api);
    this.input = new InputState();
    this.prediction = new PredictionState(this.api);
    this.runMode = "sync";
    this.predictionId = "";
    this.webhookEvents = new Set(EDITABLE_WEBHOOK_EVENTS);
    this.lastCapabilities = undefined;
    this.schemaURL = undefined;
    this.header = element("header");
    this.toolbar = element("div", { id: "playground-toolbar" });
    this.webhooks = element("div");
    this.inputHost = element("div");
    this.outputHost = element("div");
    this.root.replaceChildren(
      this.header,
      this.toolbar,
      this.webhooks,
      element("main", {}, this.inputHost, this.outputHost),
    );
    this.inputView = new InputView(this.inputHost, this.input, this.connection);
    this.outputView = new OutputView(this.outputHost, this.prediction);
    this.unsubscribeConnection = this.connection.subscribe(() => this.connectionChanged());
    this.unsubscribeInput = this.input.subscribe(() => this.renderToolbar());
    this.unsubscribePrediction = this.prediction.subscribe(() => this.renderToolbar());
    this.renderHeader();
    this.renderToolbar();
    this.connection.start();
  }

  connectionChanged(): void {
    this.input.connect(
      this.connection.target,
      this.connection.schema,
      this.connection.capabilities,
    );
    if (this.connection.capabilities !== this.lastCapabilities) {
      this.lastCapabilities = this.connection.capabilities;
      if (this.connection.capabilities)
        this.runMode = this.connection.capabilities.streaming ? "stream" : "sync";
      this.prediction.reset();
    }
    this.renderHeader();
    this.renderToolbar();
    this.inputView.updateConnection();
  }

  renderHeader(): void {
    const currentSetup = this.header.querySelector("#setup-panel");
    const setupOpen = currentSetup instanceof HTMLDetailsElement && currentSetup.open;
    const currentLogs = this.header.querySelector("#setup-logs");
    const setupScroll = currentLogs instanceof HTMLElement ? currentLogs.scrollTop : 0;
    const health = this.connection.health;

    const version = [
      this.connection.cogVersion && `cog ${this.connection.cogVersion}`,
      health.version?.coglet && `coglet ${health.version.coglet}`,
      health.version?.python_sdk && `sdk ${health.version.python_sdk}`,
      health.version?.python && `py ${health.version.python}`,
    ]
      .filter(Boolean)
      .join(" · ");
    const nodes = [
      element("h1", { text: "Cog Playground" }),
      statusBadge(health.status),
      element("span", { className: "spacer" }),
      element("span", { className: "muted", text: version }),
    ];

    if (health.setup) {
      const details = element(
        "details",
        { id: "setup-panel", open: setupOpen },
        element(
          "summary",
          { id: "setup-summary" },
          element("span", { className: "setup-label", text: "Setup " }),
          statusBadge(health.setup.status),
        ),
      );
      if (health.setup.logs !== undefined)
        details.append(
          element("pre", {
            id: "setup-logs",
            "aria-label": "Setup logs",
            tabindex: "0",
            text: renderTerminalText(health.setup.logs),
          }),
        );
      nodes.push(details);
    }

    const theme = document.documentElement.dataset.mode === "light" ? "light" : "dark";
    nodes.push(
      element("button", {
        id: "theme-toggle",
        type: "button",
        className: "ghost",
        text: theme === "dark" ? "Light" : "Dark",
        onclick: () => {
          const next = theme === "dark" ? "light" : "dark";
          document.documentElement.dataset.mode = next;
          localStorage.setItem("cog-playground-theme", next);
          this.renderHeader();
        },
      }),
    );
    nodes.push(
      element("button", {
        id: "open-schema",
        type: "button",
        className: "ghost",
        text: "Schema",
        disabled: !this.connection.schema,
        onclick: () => this.openSchema(),
      }),
    );

    replaceChildrenPreservingFocus(this.header, ...nodes);
    const nextLogs = this.header.querySelector("#setup-logs");
    if (nextLogs instanceof HTMLElement) nextLogs.scrollTop = setupScroll;
  }

  renderToolbar(): void {
    const controlsDisabled = this.prediction.running || this.input.validating;
    const capabilities = this.connection.capabilities;
    const target = element("input", {
      className: "target-input",
      type: "url",
      value: this.connection.targetDraft,
      disabled: controlsDisabled,
      "aria-label": "Target",
    });
    target.addEventListener("input", () => {
      this.connection.setDraft(target.value);
      connect.disabled = controlsDisabled || !target.value.trim();
    });
    target.addEventListener("keydown", (event: KeyboardEvent) => {
      if (event.key === "Enter") this.connection.connect(target.value);
    });

    const connect = element("button", {
      id: "connect-target",
      type: "button",
      className: "primary",
      text: "Connect",
      disabled: controlsDisabled || !this.connection.targetDraft.trim(),
      onclick: () => this.connection.connect(target.value),
    });
    const targetBar = element(
      "fieldset",
      { id: "target-bar" },
      element("legend", { className: "sr-only", text: "Target connection" }),
      element("label", { htmlFor: "target", text: "Target" }),
      target,
      connect,
      element("output", {
        className: "muted",
        text: this.connection.health.user_healthcheck_error ?? "",
      }),
    );
    target.id = "target";

    const modeOptions = element("fieldset", { className: "playground-options" });
    modeOptions.append(element("legend", { className: "sr-only", text: "Prediction mode" }));
    const modes: Array<{ value: RunMode; label: string }> = [{ value: "sync", label: "Sync" }];
    if (capabilities?.streaming) modes.push({ value: "stream", label: "Stream" });
    if (capabilities?.async) modes.push({ value: "async", label: "Async" });
    for (const { value, label } of modes)
      modeOptions.append(
        element("button", {
          id: `run-mode-${value}`,
          type: "button",
          text: label,
          "aria-pressed": this.runMode === value,
          disabled: controlsDisabled,
          onclick: () => {
            this.runMode = value;
            this.renderToolbar();
          },
        }),
      );

    const predictionId = element("input", {
      id: "prediction-id",
      value: this.predictionId,
      placeholder: "id (optional)",
      "aria-label": "Prediction ID",
      disabled: controlsDisabled,
    });
    predictionId.addEventListener("input", () => {
      this.predictionId = predictionId.value;
    });

    const runnable = Boolean(this.runnableCapabilities());
    const actions = element(
      "div",
      { className: "run-actions" },
      element("button", {
        id: "run-prediction",
        type: "button",
        className: "primary",
        text: "Run",
        disabled: !runnable,
        "aria-busy": String(this.prediction.running),
        onclick: () => this.run(),
      }),
      element("button", {
        id: "stop-prediction",
        type: "button",
        className: "secondary-destructive",
        text: "Stop",
        disabled: !this.prediction.running,
        onclick: () => this.prediction.stop(),
      }),
      element("button", {
        id: "reset-playground",
        type: "button",
        className: "ghost",
        text: "Reset",
        disabled: !capabilities || this.prediction.running,
        onclick: () => {
          this.input.reset();
          this.predictionId = "";
          this.prediction.reset();
          this.renderToolbar();
        },
      }),
    );
    const actionBar = element(
      "fieldset",
      { id: "action-bar" },
      element("legend", { className: "sr-only", text: "Prediction controls" }),
      capabilities?.streaming || capabilities?.async ? modeOptions : undefined,
      element(
        "label",
        { className: "prediction-id" },
        element("span", { className: "sr-only", text: "Prediction ID" }),
        predictionId,
      ),
      actions,
    );

    replaceChildrenPreservingFocus(this.toolbar, targetBar, actionBar);
    this.renderWebhookOptions();
  }

  renderWebhookOptions(): void {
    if (this.runMode !== "async") {
      this.webhooks.replaceChildren();
      return;
    }
    const disabled = this.prediction.running || this.input.validating;
    const options = element(
      "fieldset",
      { id: "webhook-options" },
      element("legend", { className: "sr-only", text: "Webhook events" }),
      element("span", {
        className: "webhook-title",
        "aria-hidden": "true",
        text: "Webhook events",
      }),
    );
    for (const event of EDITABLE_WEBHOOK_EVENTS) {
      const checkbox = element("input", {
        id: `webhook-event-${event}`,
        type: "checkbox",
        checked: this.webhookEvents.has(event),
        disabled,
      });
      checkbox.addEventListener("change", () =>
        checkbox.checked ? this.webhookEvents.add(event) : this.webhookEvents.delete(event),
      );
      options.append(element("label", {}, checkbox, event));
    }
    options.append(
      element(
        "label",
        {},
        element("input", {
          id: "webhook-event-completed",
          type: "checkbox",
          checked: true,
          disabled: true,
          "aria-disabled": "true",
        }),
        "completed",
      ),
    );
    options.append(
      element("span", {
        className: "muted",
        text: this.connection.webhookBase
          ? `Webhook: ${this.connection.webhookBase}/webhook/...`
          : "No webhook host configured",
      }),
    );
    replaceChildrenPreservingFocus(this.webhooks, options);
  }

  runnableCapabilities(): PlaygroundCapabilities | undefined {
    const capabilities = this.connection.capabilities;
    if (!capabilities) return undefined;
    if (this.input.formBusy || this.input.validating || this.prediction.running) return undefined;
    if (this.runMode === "async" && !this.connection.webhookBase) return undefined;
    return capabilities;
  }

  async run(): Promise<void> {
    const capabilities = this.runnableCapabilities();
    if (!capabilities) return;

    const input = await this.input.validateForRun();
    if (!input) return;

    void this.prediction.run({
      target: this.connection.target,
      endpoint: capabilities.endpoint,
      predictionId: this.predictionId.trim() || undefined,
      input,
      mode: this.runMode,
      webhookBase: this.connection.webhookBase,
      webhookEvents: [...this.webhookEvents],
    });
  }

  openSchema(): void {
    if (!this.connection.schema) return;
    if (this.schemaURL) URL.revokeObjectURL(this.schemaURL);
    this.schemaURL = URL.createObjectURL(
      new Blob([JSON.stringify(this.connection.schema, null, 2)], { type: "application/json" }),
    );
    window.open(this.schemaURL, "_blank", "noopener,noreferrer");
  }

  destroy(): void {
    this.unsubscribeConnection();
    this.unsubscribeInput();
    this.unsubscribePrediction();
    this.connection.destroy();
    this.input.destroy();
    this.prediction.destroy();
    this.inputView.destroy();
    this.outputView.destroy();
    if (this.schemaURL) URL.revokeObjectURL(this.schemaURL);
  }
}

const root = document.querySelector("#root");
if (!(root instanceof HTMLElement)) throw new Error("Missing playground root");
const app = new App(root);
window.addEventListener("pagehide", (event) => {
  if (!event.persisted) app.destroy();
});
