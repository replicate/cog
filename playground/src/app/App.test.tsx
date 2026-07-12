import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useEffect } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { createInputValidator } from "@/features/inputs/validation/inputValidation";
import type { ValidationIssue } from "@/features/inputs/validation/inputValidation";
import { deferred } from "@/test/deferred";

const run = vi.fn();
const reset = vi.fn();
const { mockValidateInput } = vi.hoisted(() => ({ mockValidateInput: vi.fn() }));

vi.mock("@/features/connection/hooks/useConnection", () => {
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
            properties: {
              prompt: { type: "string" },
            },
          },
          Output: { type: "string" },
        },
      },
      paths: { "/predictions": { post: {} } },
    },
    schemaError: "",
    webhookBase: "",
    cogVersion: "0.21.1-dev+g0db4bffa",
    capabilities: {
      endpoint: "/predictions",
      input: {
        type: "object",
        required: ["prompt"],
        properties: {
          prompt: { type: "string" },
        },
      },
      output: { type: "string" },
      streaming: false,
      async: false,
    },
  };
  return { useConnection: () => connection };
});

vi.mock("@/features/predictions/hooks/usePrediction", () => ({
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

vi.mock("@/features/inputs/validation/validateInput", () => ({ validateInput: mockValidateInput }));

vi.mock("@/features/inputs/components/InputForm", () => ({
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
    useEffect(() => onValidityChange(true), [onValidityChange]);
    return (
      <textarea
        aria-label="Form prompt"
        value={prompt}
        onChange={(event) => onChange({ ...value, prompt: event.currentTarget.value })}
      />
    );
  },
}));

vi.mock("@/components/editor/LazyJsonEditor", () => ({
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

import { App } from "@/app/App";

describe("App", () => {
  beforeEach(() => {
    run.mockReset();
    reset.mockReset();
    mockValidateInput.mockReset();
    mockValidateInput.mockImplementation(
      async (
        document: Parameters<typeof createInputValidator>[0],
        schema: Parameters<typeof createInputValidator>[1],
        value: unknown,
      ) => createInputValidator(document, schema)(value),
    );
    document.documentElement.dataset.mode = "dark";
    localStorage.clear();
  });

  it("runs valid JSON after validating it on demand", async () => {
    render(<App />);
    const runButton = screen.getByRole("button", { name: "Run" });
    expect(runButton).toBeEnabled();
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
    await waitFor(() =>
      expect(run).toHaveBeenCalledWith(
        expect.objectContaining({ input: { prompt: "hello" }, mode: "sync" }),
      ),
    );
  });

  it("does not run non-object JSON", () => {
    render(<App />);
    const runButton = screen.getByRole("button", { name: "Run" });
    fireEvent.click(screen.getByRole("tab", { name: "JSON" }));
    fireEvent.change(screen.getByLabelText("Prediction input JSON"), {
      target: { value: "[]" },
    });

    expect(runButton).toBeEnabled();
    fireEvent.click(screen.getByRole("button", { name: "Format" }));

    fireEvent.click(runButton);
    expect(run).not.toHaveBeenCalled();
    expect(screen.getByText("Input must be a JSON object")).toBeVisible();
  });

  it("does not run input that fails the OpenAPI schema", async () => {
    render(<App />);
    const runButton = screen.getByRole("button", { name: "Run" });
    fireEvent.click(screen.getByRole("tab", { name: "JSON" }));
    fireEvent.change(screen.getByLabelText("Prediction input JSON"), {
      target: { value: '{"prompt":42}' },
    });

    expect(runButton).toBeEnabled();
    expect(screen.queryByText("Input does not match the OpenAPI schema.")).not.toBeInTheDocument();

    fireEvent.click(runButton);
    const heading = await screen.findByText("Input does not match the OpenAPI schema.");
    const summary = heading.parentElement;
    expect(summary).toBeVisible();
    expect(summary).toHaveTextContent(/prompt.*string/i);
    expect(run).not.toHaveBeenCalled();
  });

  it("prevents reconnecting while input validation is pending", async () => {
    const validation = deferred<ValidationIssue[]>();
    mockValidateInput.mockReturnValueOnce(validation.promise);
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: "Run" }));
    await waitFor(() => expect(mockValidateInput).toHaveBeenCalled());
    expect(screen.getByRole("textbox", { name: "Target" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Connect" })).toBeDisabled();

    validation.resolve([]);
    await waitFor(() => expect(run).toHaveBeenCalled());
    expect(screen.getByRole("textbox", { name: "Target" })).toBeEnabled();
  });

  it("cancels pending validation when input changes", async () => {
    const validation = deferred<ValidationIssue[]>();
    mockValidateInput.mockReturnValueOnce(validation.promise);
    render(<App />);

    fireEvent.click(screen.getByRole("button", { name: "Run" }));
    await waitFor(() => expect(mockValidateInput).toHaveBeenCalled());
    const signal = mockValidateInput.mock.calls[0][3] as AbortSignal;

    fireEvent.change(screen.getByLabelText("Form prompt"), { target: { value: "changed" } });
    expect(signal.aborted).toBe(true);
    validation.resolve([]);
    await waitFor(() => expect(screen.getByRole("button", { name: "Run" })).toBeEnabled());
    expect(run).not.toHaveBeenCalled();
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
    expect(screen.getByRole("button", { name: "Run" })).toBeEnabled();

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

  it("opens the loaded OpenAPI schema in a new tab", () => {
    const originalURL = URL;
    const createObjectURL = vi.fn(() => "blob:schema");
    const revokeObjectURL = vi.fn();
    const open = vi.spyOn(window, "open").mockImplementation(() => null);
    vi.stubGlobal("URL", { ...URL, createObjectURL, revokeObjectURL });
    try {
      const { unmount } = render(<App />);
      fireEvent.click(screen.getByRole("button", { name: "Schema" }));

      expect(createObjectURL).toHaveBeenCalledWith(expect.any(Blob));
      expect(open).toHaveBeenCalledWith("blob:schema", "_blank", "noopener,noreferrer");
      unmount();
      expect(revokeObjectURL).toHaveBeenCalledWith("blob:schema");
    } finally {
      open.mockRestore();
      vi.stubGlobal("URL", originalURL);
    }
  });

  it("shows the complete Cog development version", () => {
    render(<App />);

    expect(screen.getByText(/cog 0\.21\.1-dev\+g0db4bffa/)).toBeVisible();
  });
});
