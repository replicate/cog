import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { RunToolbar } from "@/features/predictions/components/RunToolbar";

describe("RunToolbar", () => {
  it("wires prediction modes, ID, and action buttons", () => {
    const onRunModeChange = vi.fn();
    const onPredictionIdChange = vi.fn();
    const onRun = vi.fn();
    const onStop = vi.fn();
    const onReset = vi.fn();
    const { rerender } = render(
      <RunToolbar
        runMode="sync"
        predictionId=""
        streaming
        async
        running={false}
        runnable
        schemaLoaded
        onRunModeChange={onRunModeChange}
        onPredictionIdChange={onPredictionIdChange}
        onRun={onRun}
        onStop={onStop}
        onReset={onReset}
      />,
    );

    expect(screen.getByRole("group", { name: "Prediction mode" })).toBeVisible();
    expect(screen.getByRole("button", { name: "Sync" })).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(screen.getByRole("button", { name: "Stream" }));
    fireEvent.click(screen.getByRole("button", { name: "Async" }));
    fireEvent.input(screen.getByRole("textbox", { name: "Prediction ID" }), {
      target: { value: "prediction-1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Run" }));
    fireEvent.click(screen.getByRole("button", { name: "Reset" }));

    expect(onRunModeChange).toHaveBeenNthCalledWith(1, "stream");
    expect(onRunModeChange).toHaveBeenNthCalledWith(2, "async");
    expect(onPredictionIdChange).toHaveBeenCalledWith("prediction-1");
    expect(onRun).toHaveBeenCalled();
    expect(onReset).toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Stop" })).toBeDisabled();

    rerender(
      <RunToolbar
        runMode="sync"
        predictionId="prediction-1"
        streaming
        async
        running={false}
        validating
        runnable={false}
        schemaLoaded
        onRunModeChange={onRunModeChange}
        onPredictionIdChange={onPredictionIdChange}
        onRun={onRun}
        onStop={onStop}
        onReset={onReset}
      />,
    );
    expect(screen.getByRole("button", { name: "Sync" })).toBeDisabled();
    expect(screen.getByRole("textbox", { name: "Prediction ID" })).toBeDisabled();

    rerender(
      <RunToolbar
        runMode="stream"
        predictionId="prediction-1"
        streaming
        async
        running
        runnable={false}
        schemaLoaded
        onRunModeChange={onRunModeChange}
        onPredictionIdChange={onPredictionIdChange}
        onRun={onRun}
        onStop={onStop}
        onReset={onReset}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Stop" }));
    expect(onStop).toHaveBeenCalled();
    expect(screen.getByRole("textbox", { name: "Prediction ID" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Sync" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Async" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Reset" })).toBeDisabled();
  });

  it("hides unsupported prediction modes", () => {
    render(
      <RunToolbar
        runMode="sync"
        predictionId=""
        streaming={false}
        async={false}
        running={false}
        runnable={false}
        schemaLoaded={false}
        onRunModeChange={vi.fn()}
        onPredictionIdChange={vi.fn()}
        onRun={vi.fn()}
        onStop={vi.fn()}
        onReset={vi.fn()}
      />,
    );
    expect(screen.queryByRole("button", { name: "Stream" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Async" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Run" })).toBeDisabled();
  });
});
