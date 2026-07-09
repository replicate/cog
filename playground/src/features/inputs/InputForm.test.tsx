import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/editor/LazyJsonEditor", () => ({
  LazyJsonEditor: ({
    label,
    onChange,
    value,
  }: {
    label: string;
    onChange: (value: string) => void;
    value: string;
  }) => (
    <textarea
      aria-label={label}
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
});
