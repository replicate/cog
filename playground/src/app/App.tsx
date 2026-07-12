import { Button } from "@cloudflare/kumo/components/button";
import { useEffect, useMemo, useState } from "react";

import { CogApi } from "@/api/cog";
import { SegmentedTabs, tabId, tabPanelId } from "@/components/SegmentedTabs";
import { StatusBadge } from "@/components/StatusBadge";
import { defaultInput } from "@/domain/schema";
import { WEBHOOK_EVENTS, type InputMode, type RunMode, type WebhookEvent } from "@/domain/types";
import { LazyJsonEditor } from "@/editor/LazyJsonEditor";
import { ConnectionBar } from "@/features/connection/ConnectionBar";
import { SetupPanel } from "@/features/connection/SetupPanel";
import { useConnection } from "@/features/connection/useConnection";
import { InputForm } from "@/features/inputs/InputForm";
import { OutputPanel } from "@/features/predictions/OutputPanel";
import { RunToolbar } from "@/features/predictions/RunToolbar";
import { usePrediction } from "@/features/predictions/usePrediction";
import { WebhookOptions } from "@/features/predictions/WebhookOptions";
import { currentTheme, setTheme, type ThemeMode } from "@/theme";

export function App() {
  const api = useMemo(() => new CogApi(), []);
  const connection = useConnection(api);
  const prediction = usePrediction(api);
  const [input, setInput] = useState<Record<string, unknown>>({});
  const [jsonInput, setJsonInput] = useState("{}");
  const [jsonError, setJsonError] = useState("");
  const [inputMode, setInputMode] = useState<InputMode>("form");
  const [runMode, setRunMode] = useState<RunMode>("sync");
  const [predictionId, setPredictionId] = useState("");
  const [formBusy, setFormBusy] = useState(false);
  const [formValid, setFormValid] = useState(true);
  const [formRevision, setFormRevision] = useState(0);
  const [theme, setThemeState] = useState<ThemeMode>(currentTheme());
  const [webhookEvents, setWebhookEvents] = useState<WebhookEvent[]>([...WEBHOOK_EVENTS]);
  const { schema, capabilities } = connection;
  const resetPrediction = prediction.reset;

  useEffect(() => {
    if (!schema || !capabilities) return;
    const defaults = defaultInput(schema, capabilities.input);
    setInput(defaults);
    setJsonInput(JSON.stringify(defaults, null, 2));
    setJsonError("");
    setFormBusy(false);
    setFormValid(true);
    setFormRevision((current) => current + 1);
    setRunMode(capabilities.streaming ? "stream" : "sync");
    resetPrediction();
  }, [capabilities, resetPrediction, schema]);

  const run = () => {
    if (!connection.capabilities) return;
    let nextInput = input;
    if (inputMode === "json") {
      try {
        nextInput = parseInputObject(jsonInput);
        setJsonError("");
      } catch (error) {
        setJsonError(errorMessage(error));
        return;
      }
    }
    void prediction.run({
      endpoint: connection.capabilities.endpoint,
      predictionId: predictionId.trim() || undefined,
      input: nextInput,
      mode: runMode,
      webhookBase: connection.webhookBase,
      webhookEvents,
    });
  };

  const reset = () => {
    if (!connection.schema || !connection.capabilities) return;
    const defaults = defaultInput(connection.schema, connection.capabilities.input);
    setInput(defaults);
    setJsonInput(JSON.stringify(defaults, null, 2));
    setPredictionId("");
    setJsonError("");
    setFormBusy(false);
    setFormValid(true);
    setFormRevision((current) => current + 1);
    prediction.reset();
  };

  const changeJsonInput = (next: string) => {
    setJsonInput(next);
    try {
      const parsed = parseInputObject(next);
      setInput(parsed);
      setJsonError("");
    } catch (error) {
      setJsonError(errorMessage(error));
    }
  };

  const changeFormInput = (next: Record<string, unknown>) => {
    setInput(next);
    setJsonInput(JSON.stringify(next, null, 2));
    setJsonError("");
  };

  const changeInputMode = (next: InputMode) => {
    if (next === inputMode) return;
    if (next === "json") {
      setJsonInput(JSON.stringify(input, null, 2));
      setJsonError("");
      setInputMode("json");
      return;
    }
    try {
      setInput(parseInputObject(jsonInput));
      setJsonError("");
    } catch {
      setJsonInput(JSON.stringify(input, null, 2));
      setJsonError("");
    }
    setInputMode("form");
  };

  const formatJsonInput = () => {
    try {
      const parsed = parseInputObject(jsonInput);
      setInput(parsed);
      setJsonInput(JSON.stringify(parsed, null, 2));
      setJsonError("");
    } catch (error) {
      setJsonError(errorMessage(error));
    }
  };

  const toggleTheme = () => {
    const next = theme === "dark" ? "light" : "dark";
    setTheme(next);
    setThemeState(next);
  };

  const downloadSchema = () => {
    if (!connection.schema) return;
    const href = URL.createObjectURL(
      new Blob([JSON.stringify(connection.schema, null, 2)], { type: "application/json" }),
    );
    const link = document.createElement("a");
    link.href = href;
    link.download = "openapi.json";
    link.click();
    URL.revokeObjectURL(href);
  };

  const version = [
    connection.health.version?.coglet && `coglet ${connection.health.version.coglet}`,
    connection.health.version?.python_sdk && `cog ${connection.health.version.python_sdk}`,
    connection.health.version?.python && `py ${connection.health.version.python}`,
  ]
    .filter(Boolean)
    .join(" · ");
  const activeInputValid = inputMode === "json" ? !jsonError : !formBusy && formValid;
  const runnable = Boolean(
    connection.schema && connection.capabilities && activeInputValid && !prediction.running,
  );

  return (
    <>
      <header>
        <h1>Cog Playground</h1>
        <StatusBadge status={connection.health.status} />
        <span className="spacer" />
        <span className="muted">{version}</span>
        <Button size="sm" variant="ghost" onClick={toggleTheme}>
          {theme === "dark" ? "Light" : "Dark"}
        </Button>
        <Button size="sm" variant="ghost" disabled={!connection.schema} onClick={downloadSchema}>
          Schema
        </Button>
      </header>

      <ConnectionBar
        draft={connection.targetDraft}
        status={connection.health.user_healthcheck_error}
        disabled={prediction.running}
        onDraftChange={connection.setTargetDraft}
        onConnect={connection.connect}
      />
      <RunToolbar
        runMode={runMode}
        predictionId={predictionId}
        streaming={connection.capabilities?.streaming ?? false}
        async={connection.capabilities?.async ?? false}
        running={prediction.running}
        runnable={runnable}
        schemaLoaded={Boolean(connection.schema)}
        onRunModeChange={setRunMode}
        onPredictionIdChange={setPredictionId}
        onRun={run}
        onStop={() => void prediction.stop()}
        onReset={reset}
      />
      {runMode === "async" && (
        <WebhookOptions
          value={webhookEvents}
          webhookBase={connection.webhookBase}
          onChange={setWebhookEvents}
        />
      )}
      <SetupPanel setup={connection.health.setup} />

      <main>
        <section id="input-panel">
          <div className="panel-head">
            <h2>Input</h2>
            <SegmentedTabs
              id="input-mode"
              label="Input editor mode"
              items={[
                { value: "form", label: "Form" },
                { value: "json", label: "JSON" },
              ]}
              value={inputMode}
              onChange={changeInputMode}
            />
          </div>
          {connection.schemaError && (
            <output className="notice visible">{connection.schemaError}</output>
          )}
          <div
            id={tabPanelId("input-mode", inputMode)}
            role="tabpanel"
            aria-labelledby={tabId("input-mode", inputMode)}
            tabIndex={0}
          >
            {connection.schema && connection.capabilities && inputMode === "form" && (
              <InputForm
                key={formRevision}
                document={connection.schema}
                schema={connection.capabilities.input}
                value={input}
                onChange={changeFormInput}
                onBusyChange={setFormBusy}
                onValidityChange={setFormValid}
              />
            )}
            {inputMode === "json" && (
              <div id="json-container">
                <LazyJsonEditor
                  value={jsonInput}
                  label="Prediction input JSON"
                  className="json-input"
                  describedBy={jsonError ? "json-input-error" : undefined}
                  invalid={Boolean(jsonError)}
                  onChange={changeJsonInput}
                />
                {jsonError && (
                  <small id="json-input-error" className="field-error" role="alert">
                    {jsonError}
                  </small>
                )}
                <Button size="sm" variant="ghost" onClick={formatJsonInput}>
                  Format
                </Button>
              </div>
            )}
          </div>
        </section>
        <OutputPanel
          envelope={prediction.envelope}
          error={prediction.error}
          output={prediction.output}
          rawEvents={prediction.rawEvents}
          running={prediction.running}
          streaming={runMode !== "sync"}
          outputSchema={connection.capabilities?.output}
          trace={prediction.trace}
        />
      </main>
    </>
  );
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function parseInputObject(value: string): Record<string, unknown> {
  const parsed: unknown = JSON.parse(value);
  if (!isInputObject(parsed)) {
    throw new Error("Input must be a JSON object");
  }
  return parsed;
}

function isInputObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
