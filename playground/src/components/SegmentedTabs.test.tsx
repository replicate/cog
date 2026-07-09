import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { SegmentedTabs } from "./SegmentedTabs";

describe("SegmentedTabs", () => {
  it("reports the selected Kumo tab", () => {
    const onChange = vi.fn();
    render(
      <SegmentedTabs
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
    expect(form).toHaveAttribute("aria-selected", "true");
    expect(json).toHaveAttribute("aria-selected", "false");

    fireEvent.click(json);
    expect(onChange).toHaveBeenCalledWith("json");
  });
});
