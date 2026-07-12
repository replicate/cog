import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useEffect, useRef } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const run = vi.fn();
const reset = vi.fn();

vi.mock("@/features/connection/useConnection", () => {
  const connection = {
    target: "http://localhost:8393",
    targetDraft: "http://localhost:8393",
    setTargetDraft: vi.fn(),
    connect: vi.fn(),
    health: { status: "ready", version: { python_sdk: "0.21.0" } },
    schema: {
      components: {
        schemas: {
          Input: {
            type: "object",
            required: ["prompt"],
            properties: { prompt: { type: "string" } },
          },
          Output: { type: "string" },
        },
      },
      paths: { "/predictions": { post: {} } },
    },
    schemaError: "",
    webhookBase: "",
    capabilities: {
      endpoint: "/predictions",
      input: {
        type: "object",
        required: ["prompt"],
        properties: { prompt: { type: "string" } },
      },
      output: { type: "string" },
      streaming: false,
      async: false,
    },
  };
  return { useConnection: () => connection };
});

vi.mock("@/features/predictions/usePrediction", () => ({
  usePrediction: () => ({
    running: false,
    envelope: undefined,
    output: undefined,
    rawEvents: [],
    error: "",
    trace: undefined,
    run,
    stop: vi.fn(),
    reset,
  }),
}));

vi.mock("@/features/inputs/InputForm", () => ({
  InputForm: ({ onValidityChange }: { onValidityChange: (valid: boolean) => void }) => {
    const initialValidityChange = useRef(onValidityChange);
    useEffect(() => initialValidityChange.current(false), []);
    return <div>Form input</div>;
  },
}));

vi.mock("@/editor/LazyJsonEditor", () => ({
  LazyJsonEditor: ({
    label,
    onChange,
    value,
  }: {
    label: string;
    onChange?: (value: string) => void;
    value: string;
  }) => (
    <textarea
      aria-label={label}
      value={value}
      onChange={(event) => onChange?.(event.currentTarget.value)}
    />
  ),
}));

import { App } from "./App";

describe("App", () => {
  beforeEach(() => {
    run.mockReset();
    reset.mockReset();
  });

  it("uses JSON validity instead of stale Form validity in JSON mode", async () => {
    render(<App />);
    const runButton = screen.getByRole("button", { name: "Run" });
    await waitFor(() => expect(runButton).toBeDisabled());

    fireEvent.click(screen.getByRole("tab", { name: "JSON" }));
    fireEvent.change(screen.getByLabelText("Prediction input JSON"), {
      target: { value: '{"prompt":"hello"}' },
    });

    expect(runButton).toBeEnabled();
    fireEvent.click(runButton);
    expect(run).toHaveBeenCalledWith(
      expect.objectContaining({ input: { prompt: "hello" }, mode: "sync" }),
    );
  });

  it("does not enable Run when formatting non-object JSON", async () => {
    render(<App />);
    const runButton = screen.getByRole("button", { name: "Run" });
    fireEvent.click(screen.getByRole("tab", { name: "JSON" }));
    fireEvent.change(screen.getByLabelText("Prediction input JSON"), {
      target: { value: "[]" },
    });

    expect(runButton).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Format" }));

    expect(runButton).toBeDisabled();
    expect(screen.getByText("Input must be a JSON object")).toBeVisible();
  });
});
