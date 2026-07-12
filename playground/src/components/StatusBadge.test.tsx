import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { StatusBadge } from "@/components/StatusBadge";

describe("StatusBadge", () => {
  it.each([
    ["READY", "ready"],
    ["SETUP_FAILED", "setup_failed"],
    ["canceled", "canceled"],
    [undefined, "unknown"],
  ])("normalizes %s", (status, label) => {
    render(<StatusBadge status={status} />);
    expect(screen.getByText(label)).toBeVisible();
  });
});
