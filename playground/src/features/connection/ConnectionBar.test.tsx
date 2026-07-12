import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ConnectionBar } from "./ConnectionBar";

describe("ConnectionBar", () => {
  it("edits and connects with both Enter and the button", () => {
    const onDraftChange = vi.fn();
    const onConnect = vi.fn();
    render(
      <ConnectionBar
        draft="http://localhost:8393"
        status="healthy"
        disabled={false}
        onDraftChange={onDraftChange}
        onConnect={onConnect}
      />,
    );
    const target = screen.getByRole("textbox", { name: "Target" });

    fireEvent.input(target, { target: { value: "http://localhost:9000" } });
    fireEvent.keyDown(target, { key: "Enter" });
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));

    expect(onDraftChange).toHaveBeenCalledWith("http://localhost:9000");
    expect(onConnect).toHaveBeenCalledTimes(2);
    expect(screen.getByText("healthy")).toBeVisible();
  });

  it("disables connection controls when unavailable", () => {
    const { rerender } = render(
      <ConnectionBar draft=" " disabled={false} onDraftChange={vi.fn()} onConnect={vi.fn()} />,
    );
    expect(screen.getByRole("button", { name: "Connect" })).toBeDisabled();

    rerender(
      <ConnectionBar
        draft="http://localhost:8393"
        disabled
        onDraftChange={vi.fn()}
        onConnect={vi.fn()}
      />,
    );
    expect(screen.getByRole("textbox", { name: "Target" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Connect" })).toBeDisabled();
  });
});
