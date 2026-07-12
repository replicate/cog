import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { JsonEditor } from "@/components/editor/JsonEditor";

describe("JsonEditor", () => {
  it("renders highlighted read-only JSON and follows controlled updates", async () => {
    const { container, rerender } = render(
      <JsonEditor value={'{"answer":42}'} label="Response JSON" readOnly />,
    );

    const editor = screen.getByRole("textbox", { name: "Response JSON" });
    expect(editor).toHaveAttribute("aria-readonly", "true");
    expect(editor).toHaveAttribute("contenteditable", "false");
    expect(container.querySelector(".cm-gutters")).toBeInTheDocument();
    expect(container.querySelector(".cm-content span")).toBeInTheDocument();

    rerender(<JsonEditor value={'{"answer":43}'} label="Response JSON" readOnly />);
    await waitFor(() => expect(editor).toHaveTextContent('"answer":43'));
  });

  it("copies the complete document", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    render(<JsonEditor value={'{"answer":42}'} label="Copy JSON" />);

    fireEvent.click(screen.getByRole("button", { name: "Copy Copy JSON" }));

    await waitFor(() => expect(writeText).toHaveBeenCalledWith('{"answer":42}'));
  });

  it("removes disabled editors and copy controls from keyboard navigation", () => {
    render(
      <JsonEditor value="{}" label="Disabled JSON" disabled invalid describedBy="disabled-help" />,
    );

    const editor = screen.getByRole("textbox", { name: "Disabled JSON" });
    expect(editor).toHaveAttribute("tabindex", "-1");
    expect(editor).toHaveAttribute("aria-disabled", "true");
    expect(editor).toHaveAttribute("aria-invalid", "true");
    expect(editor).toHaveAttribute("aria-describedby", "disabled-help");
    expect(screen.getByRole("button", { name: "Copy Disabled JSON" })).toBeDisabled();
  });

  it("leaves Tab available for keyboard navigation", () => {
    render(<JsonEditor value="{}" label="Input JSON" />);
    const editor = screen.getByRole("textbox", { name: "Input JSON" });
    editor.focus();

    expect(fireEvent.keyDown(editor, { key: "Tab", code: "Tab" })).toBe(true);
  });

  it("does not add controlled updates to undo history", async () => {
    const { rerender } = render(<JsonEditor value={'{"answer":42}'} label="Updated JSON" />);
    const editor = screen.getByRole("textbox", { name: "Updated JSON" });

    rerender(<JsonEditor value={'{"answer":43}'} label="Updated JSON" />);
    await waitFor(() => expect(editor).toHaveTextContent('"answer":43'));
    editor.focus();
    fireEvent.keyDown(editor, { key: "z", code: "KeyZ", metaKey: true });

    expect(editor).toHaveTextContent('"answer":43');
  });
});
