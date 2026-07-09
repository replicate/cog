import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";

import { SegmentedTabs } from "../../components/SegmentedTabs";
import { RUN_MODES, type RunMode } from "../../domain/types";

type Props = {
  runMode: RunMode;
  predictionId: string;
  streaming: boolean;
  async: boolean;
  running: boolean;
  runnable: boolean;
  schemaLoaded: boolean;
  onRunModeChange: (mode: RunMode) => void;
  onPredictionIdChange: (id: string) => void;
  onRun: () => void;
  onStop: () => void;
  onReset: () => void;
};

export function RunToolbar(props: Props) {
  const availableModes = RUN_MODES.filter(
    (mode) =>
      mode === "sync" ||
      (mode === "stream" && props.streaming) ||
      (mode === "async" && props.async),
  );
  return (
    <div id="action-bar">
      {(props.streaming || props.async) && (
        <SegmentedTabs
          label="Prediction mode"
          items={availableModes.map((mode) => ({
            value: mode,
            label: mode[0].toUpperCase() + mode.slice(1),
          }))}
          value={props.runMode}
          onChange={(next) => props.onRunModeChange(next as RunMode)}
        />
      )}
      <Input
        aria-label="Prediction ID"
        placeholder="id (optional)"
        value={props.predictionId}
        disabled={props.running}
        onInput={(event) => props.onPredictionIdChange(event.currentTarget.value)}
      />
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
  );
}
