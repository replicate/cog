import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { deferred } from "../../test/deferred";

const { mockFileToDataURI } = vi.hoisted(() => ({ mockFileToDataURI: vi.fn() }));

vi.mock("../../api/cog", () => ({ fileToDataURI: mockFileToDataURI }));

vi.mock("@/editor/LazyJsonEditor", () => ({
  LazyJsonEditor: ({
    label,
    onChange,
    describedBy,
    disabled,
    invalid,
    readOnly,
    value,
  }: {
    label: string;
    onChange: (value: string) => void;
    describedBy?: string;
    disabled?: boolean;
    invalid?: boolean;
    readOnly?: boolean;
    value: string;
  }) => (
    <textarea
      aria-label={label}
      aria-describedby={describedBy}
      aria-invalid={invalid}
      disabled={disabled}
      readOnly={readOnly}
      value={value}
      onChange={(event) => onChange(event.currentTarget.value)}
    />
  ),
}));

import { InputForm } from "./InputForm";

const document = {
  components: { schemas: {} },
};

describe("InputForm", () => {
  beforeEach(() => {
    mockFileToDataURI.mockReset();
    mockFileToDataURI.mockImplementation(
      async (file: File) => `data:${file.type};base64,${file.name}`,
    );
  });

  it("preserves typed enum values and validates constrained numbers", () => {
    const onChange = vi.fn();
    const onValidityChange = vi.fn();
    render(
      <InputForm
        document={document}
        schema={{
          required: ["count", "choice"],
          properties: {
            count: { type: "integer", minimum: 1, maximum: 3 },
            choice: { enum: [1, 2] },
          },
        }}
        value={{ count: 1, choice: 1 }}
        onChange={onChange}
        onBusyChange={vi.fn()}
        onValidityChange={onValidityChange}
      />,
    );

    fireEvent.input(screen.getByRole("spinbutton", { name: "count" }), { target: { value: "4" } });
    expect(onChange).toHaveBeenLastCalledWith({ count: 4, choice: 1 });
    expect(onValidityChange).toHaveBeenLastCalledWith(false);
    fireEvent.change(screen.getByRole("combobox", { name: /choice/ }), { target: { value: "2" } });
    expect(onChange).toHaveBeenLastCalledWith({ count: 1, choice: 2 });
  });

  it("removes an optional field and does not retain its invalid state", () => {
    const onChange = vi.fn();
    const onValidityChange = vi.fn();
    const props = {
      document,
      schema: { properties: { optional: { type: "number", minimum: 1 } } },
      onChange,
      onBusyChange: vi.fn(),
      onValidityChange,
    };
    const { rerender } = render(<InputForm {...props} value={{ optional: 1 }} />);

    fireEvent.input(screen.getByRole("spinbutton", { name: "optional" }), {
      target: { value: "0" },
    });
    expect(onValidityChange).toHaveBeenLastCalledWith(false);
    fireEvent.click(screen.getByRole("checkbox", { name: "Include optional field optional" }));
    expect(onChange).toHaveBeenLastCalledWith({});
    rerender(<InputForm {...props} value={{}} />);
    expect(onValidityChange).toHaveBeenLastCalledWith(true);
  });

  it("refreshes structured editor values after a reset", () => {
    const props = {
      document,
      schema: { required: ["config"], properties: { config: { type: "object" } } },
      onChange: vi.fn(),
      onBusyChange: vi.fn(),
      onValidityChange: vi.fn(),
    };
    const { rerender } = render(<InputForm {...props} value={{ config: { version: 1 } }} />);
    const editor = screen.getByLabelText("config JSON") as HTMLTextAreaElement;
    expect(editor.value).toContain('"version": 1');

    rerender(<InputForm {...props} value={{ config: { version: 2 } }} />);
    expect(editor.value).toContain('"version": 2');
  });

  it("updates password, boolean, and optional string controls", () => {
    const onChange = vi.fn();
    const props = {
      document,
      schema: {
        required: ["secret", "enabled"],
        properties: {
          secret: { type: "string", format: "password" },
          enabled: { type: "boolean" },
          note: { type: "string" },
        },
      },
      onChange,
      onBusyChange: vi.fn(),
      onValidityChange: vi.fn(),
    };
    const { rerender } = render(<InputForm {...props} value={{ secret: "old", enabled: false }} />);

    fireEvent.input(screen.getByLabelText("secret"), { target: { value: "new" } });
    expect(onChange).toHaveBeenLastCalledWith({ secret: "new", enabled: false });
    fireEvent.change(screen.getByRole("combobox", { name: /enabled/ }), {
      target: { value: "true" },
    });
    expect(onChange).toHaveBeenLastCalledWith({ secret: "old", enabled: true });
    expect(screen.getByLabelText("note")).toBeDisabled();

    fireEvent.click(screen.getByRole("checkbox", { name: "Include optional field note" }));
    expect(onChange).toHaveBeenLastCalledWith({ secret: "old", enabled: false, note: "" });
    rerender(<InputForm {...props} value={{ secret: "old", enabled: false, note: "" }} />);
    expect(screen.getByLabelText("note")).toBeEnabled();
  });

  it("associates required controls with constraints and invalid state", () => {
    render(
      <InputForm
        document={document}
        schema={{
          required: ["prompt"],
          properties: {
            prompt: { type: "string", description: "Model prompt", minLength: 2 },
          },
        }}
        value={{ prompt: "" }}
        onChange={vi.fn()}
        onBusyChange={vi.fn()}
        onValidityChange={vi.fn()}
      />,
    );

    const prompt = screen.getByRole("textbox", { name: "prompt" });
    expect(prompt).toBeRequired();
    expect(prompt).toHaveAttribute("aria-invalid", "true");
    expect(prompt).toHaveAccessibleDescription("Model prompt · min 2 chars");
  });

  it("reports and recovers from structured JSON errors", () => {
    const onChange = vi.fn();
    const onValidityChange = vi.fn();
    render(
      <InputForm
        document={document}
        schema={{ required: ["config"], properties: { config: { type: "object" } } }}
        value={{ config: {} }}
        onChange={onChange}
        onBusyChange={vi.fn()}
        onValidityChange={onValidityChange}
      />,
    );
    const editor = screen.getByLabelText("config JSON");

    fireEvent.change(editor, { target: { value: "{" } });
    expect(screen.getByText(/Invalid JSON/)).toBeVisible();
    expect(onValidityChange).toHaveBeenLastCalledWith(false);

    fireEvent.change(editor, { target: { value: '{"version":2}' } });
    expect(onChange).toHaveBeenLastCalledWith({ config: { version: 2 } });
    expect(onValidityChange).toHaveBeenLastCalledWith(true);
  });

  it("loads a selected file as a data URI", async () => {
    const onChange = vi.fn();
    const onBusyChange = vi.fn();
    const { container } = render(
      <InputForm
        document={document}
        schema={{
          required: ["file"],
          properties: {
            file: { type: "string", format: "uri", "x-cog-accept": ["text/plain"] },
          },
        }}
        value={{ file: "" }}
        onChange={onChange}
        onBusyChange={onBusyChange}
        onValidityChange={vi.fn()}
      />,
    );
    const input = container.querySelector('input[type="file"]');
    expect(input).toHaveAttribute("accept", "text/plain");

    fireEvent.change(input!, {
      target: { files: [new File(["hello"], "hello.txt", { type: "text/plain" })] },
    });

    await waitFor(() =>
      expect(onChange).toHaveBeenCalledWith({ file: expect.stringMatching(/^data:/) }),
    );
    expect(onBusyChange).toHaveBeenCalledWith(true);
    await waitFor(() => expect(onBusyChange).toHaveBeenLastCalledWith(false));
    expect(screen.getByText("hello.txt (5 B)")).toBeVisible();
  });

  it("ignores a stale file read when a newer selection finishes later", async () => {
    const first = deferred<string>();
    const second = deferred<string>();
    mockFileToDataURI.mockImplementation((file: File) =>
      file.name === "first.txt" ? first.promise : second.promise,
    );
    const onChange = vi.fn();
    const onBusyChange = vi.fn();
    const { container } = render(
      <InputForm
        document={document}
        schema={{
          required: ["file"],
          properties: { file: { type: "string", format: "uri" } },
        }}
        value={{ file: "" }}
        onChange={onChange}
        onBusyChange={onBusyChange}
        onValidityChange={vi.fn()}
      />,
    );
    const input = container.querySelector('input[type="file"]')!;

    fireEvent.change(input, { target: { files: [new File(["a"], "first.txt")] } });
    fireEvent.change(input, { target: { files: [new File(["b"], "second.txt")] } });
    first.resolve("data:first");
    await first.promise;
    expect(onChange).not.toHaveBeenCalled();
    expect(onBusyChange).toHaveBeenLastCalledWith(true);

    second.resolve("data:second");
    await waitFor(() => expect(onChange).toHaveBeenCalledWith({ file: "data:second" }));
    expect(onBusyChange).toHaveBeenLastCalledWith(false);
  });

  it("renders the no-input and selected-image states", () => {
    const props = {
      document,
      onChange: vi.fn(),
      onBusyChange: vi.fn(),
      onValidityChange: vi.fn(),
    };
    const { rerender } = render(<InputForm {...props} schema={{ properties: {} }} value={{}} />);
    expect(screen.getByText("This model takes no inputs.")).toBeVisible();

    rerender(
      <InputForm
        {...props}
        schema={{
          required: ["image"],
          properties: { image: { type: "string", format: "uri" } },
        }}
        value={{ image: "data:image/png;base64,AA==" }}
      />,
    );
    expect(screen.getByRole("img", { name: "Selected input preview" })).toBeVisible();
  });
});
