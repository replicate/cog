import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { SegmentedTabs } from "@/components/SegmentedTabs";

describe("SegmentedTabs", () => {
  it("reports the selected Kumo tab", () => {
    const onChange = vi.fn();
    render(
      <SegmentedTabs
        id="modes"
        label="Modes"
        items={[
          { value: "form", label: "Form" },
          { value: "json", label: "JSON" },
        ]}
        value="form"
        onChange={onChange}
      />,
    );

    const form = screen.getByRole("tab", { name: "Form" });
    const json = screen.getByRole("tab", { name: "JSON" });
    expect(screen.getByRole("tablist", { name: "Modes" })).toBeVisible();
    expect(form).toHaveAttribute("aria-selected", "true");
    expect(form).toHaveAttribute("id", "modes-tab-form");
    expect(form).toHaveAttribute("aria-controls", "modes-panel-form");
    expect(json).toHaveAttribute("aria-selected", "false");

    fireEvent.click(json);
    expect(onChange).toHaveBeenCalledWith("json");
  });
});
