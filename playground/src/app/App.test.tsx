import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useEffect } from "react";
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
  InputForm: ({
    onChange,
    onValidityChange,
    value,
  }: {
    onChange: (value: Record<string, unknown>) => void;
    onValidityChange: (valid: boolean) => void;
    value: Record<string, unknown>;
  }) => {
    const prompt = String(value.prompt ?? "");
    useEffect(() => onValidityChange(prompt.length > 0), [onValidityChange, prompt]);
    return (
      <textarea
        aria-label="Form prompt"
        value={prompt}
        onChange={(event) => onChange({ ...value, prompt: event.currentTarget.value })}
      />
    );
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
    document.documentElement.dataset.mode = "dark";
    localStorage.clear();
  });

  it("uses JSON validity instead of stale Form validity in JSON mode", async () => {
    render(<App />);
    const runButton = screen.getByRole("button", { name: "Run" });
    await waitFor(() => expect(runButton).toBeDisabled());
    expect(screen.getByRole("tablist", { name: "Input editor mode" })).toBeVisible();
    expect(screen.getByRole("tabpanel", { name: "Form" })).toHaveAttribute(
      "aria-labelledby",
      "input-mode-tab-form",
    );

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

  it("keeps Form and JSON input synchronized", async () => {
    render(<App />);
    const form = screen.getByLabelText("Form prompt");
    fireEvent.change(form, { target: { value: "from form" } });

    fireEvent.click(screen.getByRole("tab", { name: "JSON" }));
    expect(screen.getByLabelText("Prediction input JSON")).toHaveValue(
      '{\n  "prompt": "from form"\n}',
    );

    fireEvent.change(screen.getByLabelText("Prediction input JSON"), {
      target: { value: '{"prompt":"from json"}' },
    });
    fireEvent.click(screen.getByRole("tab", { name: "Form" }));
    expect(screen.getByLabelText("Form prompt")).toHaveValue("from json");
  });

  it("allows leaving invalid JSON and restores the last valid input", async () => {
    render(<App />);
    fireEvent.click(screen.getByRole("tab", { name: "JSON" }));
    const editor = screen.getByLabelText("Prediction input JSON");
    fireEvent.change(editor, { target: { value: '{"prompt":"last valid"}' } });
    fireEvent.change(editor, { target: { value: "{" } });
    expect(screen.getByRole("button", { name: "Run" })).toBeDisabled();

    fireEvent.click(screen.getByRole("tab", { name: "Form" }));
    expect(screen.getByLabelText("Form prompt")).toHaveValue("last valid");
    fireEvent.click(screen.getByRole("tab", { name: "JSON" }));
    expect(screen.getByLabelText("Prediction input JSON")).toHaveValue(
      '{\n  "prompt": "last valid"\n}',
    );
    expect(screen.queryByText(/Expected property name|Unexpected end/)).not.toBeInTheDocument();
  });

  it("formats valid JSON and reports malformed JSON", () => {
    render(<App />);
    fireEvent.click(screen.getByRole("tab", { name: "JSON" }));
    const editor = screen.getByLabelText("Prediction input JSON");
    fireEvent.change(editor, { target: { value: '{"prompt":"formatted"}' } });
    fireEvent.click(screen.getByRole("button", { name: "Format" }));
    expect(editor).toHaveValue('{\n  "prompt": "formatted"\n}');

    fireEvent.change(editor, { target: { value: "{" } });
    fireEvent.click(screen.getByRole("button", { name: "Format" }));
    expect(screen.getByText(/Expected property name|Unexpected end/)).toBeVisible();
  });

  it("resets form input and the prediction ID", () => {
    render(<App />);
    fireEvent.change(screen.getByLabelText("Form prompt"), { target: { value: "changed" } });
    fireEvent.input(screen.getByRole("textbox", { name: "Prediction ID" }), {
      target: { value: "custom-id" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Reset" }));

    expect(screen.getByLabelText("Form prompt")).toHaveValue("");
    expect(screen.getByRole("textbox", { name: "Prediction ID" })).toHaveValue("");
    expect(reset).toHaveBeenCalled();
  });

  it("toggles the Kumo theme", () => {
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: "Light" }));

    expect(document.documentElement).toHaveAttribute("data-mode", "light");
    expect(localStorage.getItem("cog-playground-theme")).toBe("light");
    expect(screen.getByRole("button", { name: "Dark" })).toBeVisible();
  });

  it("downloads the loaded OpenAPI schema", () => {
    const createObjectURL = vi.fn(() => "blob:schema");
    const revokeObjectURL = vi.fn();
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => undefined);
    vi.stubGlobal("URL", { ...URL, createObjectURL, revokeObjectURL });
    try {
      render(<App />);
      fireEvent.click(screen.getByRole("button", { name: "Schema" }));

      expect(createObjectURL).toHaveBeenCalledWith(expect.any(Blob));
      expect(click).toHaveBeenCalled();
      expect(revokeObjectURL).toHaveBeenCalledWith("blob:schema");
    } finally {
      click.mockRestore();
      vi.unstubAllGlobals();
    }
  });
});
