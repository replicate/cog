import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";

import { RUN_MODES } from "@/features/predictions/constants";
import type { RunMode } from "@/features/predictions/types";

type Props = {
  runMode: RunMode;
  predictionId: string;
  streaming: boolean;
  async: boolean;
  running: boolean;
  validating?: boolean;
  runnable: boolean;
  schemaLoaded: boolean;
  onRunModeChange: (mode: RunMode) => void;
  onPredictionIdChange: (id: string) => void;
  onRun: () => void;
  onStop: () => void;
  onReset: () => void;
};

/** Shows supported transport modes and locks mode and ID controls while running or validating. */
export function RunToolbar(props: Props) {
  const controlsDisabled = props.running || props.validating;
  const availableModes = RUN_MODES.filter(
    (mode) =>
      mode === "sync" ||
      (mode === "stream" && props.streaming) ||
      (mode === "async" && props.async),
  );
  return (
    <fieldset id="action-bar">
      <legend className="sr-only">Prediction controls</legend>
      {(props.streaming || props.async) && (
        <fieldset className="playground-options" disabled={controlsDisabled}>
          <legend className="sr-only">Prediction mode</legend>
          {availableModes.map((mode) => (
            <button
              key={mode}
              type="button"
              aria-pressed={props.runMode === mode}
              onClick={() => props.onRunModeChange(mode)}
            >
              {mode[0].toUpperCase() + mode.slice(1)}
            </button>
          ))}
        </fieldset>
      )}
      <Input
        aria-label="Prediction ID"
        placeholder="id (optional)"
        value={props.predictionId}
        disabled={controlsDisabled}
        onInput={(event) => props.onPredictionIdChange(event.currentTarget.value)}
      />
      <div className="run-actions">
        <Button
          variant="primary"
          disabled={!props.runnable}
          loading={props.running}
          onClick={props.onRun}
        >
          Run
        </Button>
        <Button variant="secondary-destructive" disabled={!props.running} onClick={props.onStop}>
          Stop
        </Button>
        <Button
          variant="ghost"
          disabled={props.running || !props.schemaLoaded}
          onClick={props.onReset}
        >
          Reset
        </Button>
      </div>
    </fieldset>
  );
}
