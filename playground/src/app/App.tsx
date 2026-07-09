import { Button } from "@cloudflare/kumo/components/button";
import { useEffect, useMemo, useState } from "react";

import { CogApi } from "@/api/cog";
import { SegmentedTabs } from "@/components/SegmentedTabs";
import { StatusBadge } from "@/components/StatusBadge";
import { defaultInput } from "@/domain/schema";
import type { InputMode, RunMode } from "@/domain/types";
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
  const [theme, setThemeState] = useState<ThemeMode>(currentTheme());
  const [webhookEvents, setWebhookEvents] = useState(["start", "output", "logs", "completed"]);
  const { schema, capabilities } = connection;
  const resetPrediction = prediction.reset;

  useEffect(() => {
    if (!schema || !capabilities) return;
    const defaults = defaultInput(schema, capabilities.input);
    setInput(defaults);
    setJsonInput(JSON.stringify(defaults, null, 2));
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
    prediction.reset();
  };

  const changeInputMode = (next: string) => {
    if (next === inputMode) return;
    if (next === "json") {
      setJsonInput(JSON.stringify(input, null, 2));
      setInputMode("json");
      return;
    }
    try {
      setInput(parseInputObject(jsonInput));
      setJsonError("");
      setInputMode("form");
    } catch (error) {
      setJsonError(errorMessage(error));
    }
  };

  const toggleTheme = () => {
    const next = theme === "dark" ? "light" : "dark";
    setTheme(next);
    setThemeState(next);
  };

  const version = [
    connection.health.version?.coglet && `coglet ${connection.health.version.coglet}`,
    connection.health.version?.cog && `cog ${connection.health.version.cog}`,
    connection.health.version?.python && `py ${connection.health.version.python}`,
  ]
    .filter(Boolean)
    .join(" · ");
  const runnable = Boolean(
    connection.schema && connection.capabilities && !formBusy && formValid && !prediction.running,
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
        <a
          href={`/proxy/openapi.json?cog_target=${encodeURIComponent(connection.target)}`}
          target="_blank"
          rel="noreferrer"
        >
          Schema
        </a>
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
          {prediction.error && (
            <div className="error-container" role="alert">
              {prediction.error}
            </div>
          )}
          {connection.schema && connection.capabilities && inputMode === "form" && (
            <InputForm
              document={connection.schema}
              schema={connection.capabilities.input}
              value={input}
              onChange={setInput}
              onBusyChange={setFormBusy}
              onValidityChange={setFormValid}
            />
          )}
          {inputMode === "json" && (
            <div id="json-container">
              <LazyJsonEditor
                value={jsonInput}
                label="Prediction input JSON"
                onChange={setJsonInput}
              />
              {jsonError && <small className="field-error">{jsonError}</small>}
              <Button
                size="sm"
                variant="ghost"
                onClick={() => formatJSON(jsonInput, setJsonInput, setJsonError)}
              >
                Format
              </Button>
            </div>
          )}
        </section>
        <OutputPanel
          envelope={prediction.envelope}
          output={prediction.output}
          rawEvents={prediction.rawEvents}
          running={prediction.running}
          streaming={runMode !== "sync"}
          trace={prediction.trace}
        />
      </main>
    </>
  );
}

function formatJSON(
  value: string,
  onChange: (value: string) => void,
  onError: (error: string) => void,
) {
  try {
    onChange(JSON.stringify(JSON.parse(value), null, 2));
    onError("");
  } catch (error) {
    onError(errorMessage(error));
  }
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
