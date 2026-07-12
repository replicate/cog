import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { SetupPanel } from "@/features/connection/components/SetupPanel";

describe("SetupPanel", () => {
  it("renders nothing without setup and expands setup logs", () => {
    const { rerender } = render(<SetupPanel setup={undefined} />);
    expect(screen.queryByText("Setup")).not.toBeInTheDocument();

    rerender(<SetupPanel setup={{ status: "processing", logs: "downloading weights" }} />);
    const summary = screen.getByText("Setup");
    fireEvent.click(summary);

    expect(screen.getByText("processing")).toBeVisible();
    expect(screen.getByLabelText("Setup logs")).toHaveTextContent("downloading weights");
    expect(screen.getByLabelText("Setup logs")).toHaveAttribute("tabindex", "0");
  });
});
