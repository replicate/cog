import { Button } from "@cloudflare/kumo/components/button";
import { useEffect, useMemo, useRef, useState } from "react";

import { CogApi } from "@/services/cog";
import { StatusBadge } from "@/components/StatusBadge";
import { ConnectionBar, SetupPanel, useConnection } from "@/features/connection";
import { InputPanel, usePlaygroundInput } from "@/features/inputs";
import {
  OutputPanel,
  type RunMode,
  RunToolbar,
  usePrediction,
  WEBHOOK_EVENTS,
  type WebhookEvent,
  WebhookOptions,
} from "@/features/predictions";
import { currentTheme, setTheme, type ThemeMode } from "@/config/theme";

/** Owns the shared API client and resets run state when the connected model schema changes. */
export function App() {
  const api = useMemo(() => new CogApi(), []);
  const connection = useConnection(api);
  const prediction = usePrediction(api);
  const input = usePlaygroundInput({
    target: connection.target,
    document: connection.schema,
    capabilities: connection.capabilities,
  });
  const [runMode, setRunMode] = useState<RunMode>("sync");
  const [predictionId, setPredictionId] = useState("");
  const [theme, setThemeState] = useState<ThemeMode>(currentTheme());
  const [webhookEvents, setWebhookEvents] = useState<WebhookEvent[]>([...WEBHOOK_EVENTS]);
  const schemaURL = useRef<string | undefined>(undefined);
  const resetPrediction = prediction.reset;

  useEffect(
    () => () => {
      if (schemaURL.current) URL.revokeObjectURL(schemaURL.current);
    },
    [],
  );

  useEffect(() => {
    if (connection.capabilities) {
      setRunMode(connection.capabilities.streaming ? "stream" : "sync");
    }
    resetPrediction();
  }, [connection.capabilities, resetPrediction]);

  const run = async () => {
    if (
      !connection.capabilities ||
      prediction.running ||
      input.validating ||
      input.formBusy ||
      (runMode === "async" && !connection.webhookBase)
    ) {
      return;
    }
    const validated = await input.validateForRun();
    if (!validated) return;

    void prediction.run({
      endpoint: connection.capabilities.endpoint,
      predictionId: predictionId.trim() || undefined,
      input: validated.input,
      mode: runMode,
      webhookBase: connection.webhookBase,
      webhookEvents,
    });
  };

  const reset = () => {
    if (!connection.schema || !connection.capabilities) return;
    input.reset();
    setPredictionId("");
    prediction.reset();
  };

  const toggleTheme = () => {
    const next = theme === "dark" ? "light" : "dark";
    setTheme(next);
    setThemeState(next);
  };

  const openSchema = () => {
    if (!connection.schema) return;
    if (schemaURL.current) URL.revokeObjectURL(schemaURL.current);
    const href = URL.createObjectURL(
      new Blob([JSON.stringify(connection.schema, null, 2)], { type: "application/json" }),
    );
    schemaURL.current = href;
    window.open(href, "_blank", "noopener,noreferrer");
  };

  const version = [
    connection.cogVersion && `cog ${connection.cogVersion}`,
    connection.health.version?.coglet && `coglet ${connection.health.version.coglet}`,
    connection.health.version?.python_sdk && `sdk ${connection.health.version.python_sdk}`,
    connection.health.version?.python && `py ${connection.health.version.python}`,
  ]
    .filter(Boolean)
    .join(" · ");
  const runnable = Boolean(
    connection.schema &&
    connection.capabilities &&
    !input.formBusy &&
    !input.validating &&
    !prediction.running &&
    (runMode !== "async" || connection.webhookBase),
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
        <Button size="sm" variant="ghost" disabled={!connection.schema} onClick={openSchema}>
          Schema
        </Button>
      </header>

      <ConnectionBar
        draft={connection.targetDraft}
        status={connection.health.user_healthcheck_error}
        disabled={prediction.running || input.validating}
        onDraftChange={connection.setTargetDraft}
        onConnect={connection.connect}
      />
      <RunToolbar
        runMode={runMode}
        predictionId={predictionId}
        streaming={connection.capabilities?.streaming ?? false}
        async={connection.capabilities?.async ?? false}
        running={prediction.running}
        validating={input.validating}
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
          disabled={prediction.running || input.validating}
          onChange={setWebhookEvents}
        />
      )}
      <SetupPanel setup={connection.health.setup} />

      <main>
        <InputPanel
          document={connection.schema}
          schema={connection.capabilities?.input}
          schemaError={connection.schemaError}
          state={input}
        />
        <OutputPanel
          envelope={prediction.envelope}
          error={prediction.error}
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
