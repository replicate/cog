import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { WebhookOptions } from "@/features/predictions/components/WebhookOptions";

describe("WebhookOptions", () => {
  it("adds and removes optional webhook events", () => {
    const onChange = vi.fn();
    const { rerender } = render(
      <WebhookOptions value={["start", "logs"]} webhookBase="http://host" onChange={onChange} />,
    );

    expect(screen.getByRole("group", { name: "Webhook events" })).toBeVisible();
    fireEvent.click(screen.getByRole("checkbox", { name: "start" }));
    expect(onChange).toHaveBeenLastCalledWith(["logs"]);
    fireEvent.click(screen.getByRole("checkbox", { name: "output" }));
    expect(onChange).toHaveBeenLastCalledWith(["start", "logs", "output"]);
    expect(screen.getByRole("checkbox", { name: "completed" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    expect(screen.getByText("Webhook: http://host/webhook/...")).toBeVisible();

    rerender(<WebhookOptions value={[]} webhookBase="" disabled onChange={onChange} />);
    expect(screen.getByText("No webhook host configured")).toBeVisible();
    expect(screen.getByRole("checkbox", { name: "output" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });
});
