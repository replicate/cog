import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/components/editor/LazyJsonEditor", () => ({
  LazyJsonEditor: ({
    label,
    readOnly,
    value,
  }: {
    label: string;
    readOnly?: boolean;
    value: string;
  }) => (
    <pre aria-label={label} aria-readonly={readOnly} data-highlighted="true">
      {value}
    </pre>
  ),
}));

import { RequestInspector } from "@/features/predictions/components/RequestInspector";
import type { RequestTrace } from "@/types/prediction";

const trace: RequestTrace = {
  startedAt: 100,
  startedAtLabel: "10:00:00",
  finishedAt: 1350,
  method: "POST",
  endpoint: "/predictions",
  requestHeaders: { Accept: "application/json" },
  requestBody: { input: { prompt: "hello" } },
  responseStatus: 200,
  responseHeaders: { "Content-Type": "application/json" },
  responseBody: { output: "done" },
  events: [
    { id: "1", elapsedMs: 0, kind: "request" as const, label: "POST /predictions" },
    { id: "2", elapsedMs: 100, kind: "sse" as const, label: "output", data: { chunk: "one" } },
    { id: "3", elapsedMs: 200, kind: "sse" as const, label: "output", data: { chunk: "two" } },
  ],
};

describe("RequestInspector", () => {
  it("compacts streaming events and reveals their payload", () => {
    render(<RequestInspector view="timeline" trace={trace} running={false} />);

    expect(screen.getByText("output × 2")).toBeVisible();
    toggleDetails(screen.getByText("Payload"));
    expect(screen.getByLabelText("Timeline event payload")).toHaveTextContent("one");
    expect(screen.getByLabelText("Timeline event payload")).toHaveTextContent("two");
    expect(screen.getByLabelText("Timeline event payload")).toHaveAttribute(
      "data-highlighted",
      "true",
    );
  });

  it("shows request timing and expandable documents", () => {
    render(
      <RequestInspector
        view="request"
        trace={trace}
        envelope={{ metrics: { predict_time: 0.25 } }}
        running={false}
      />,
    );

    expect(screen.getByText("250 ms")).toBeVisible();
    expect(screen.queryByText("Total duration")).toBeNull();
    expect(screen.getByLabelText("Request body")).toHaveTextContent('"prompt": "hello"');
    expect(screen.getByLabelText("Response body")).toHaveTextContent('"output": "done"');
    expect(screen.getByLabelText("Request body")).toHaveAttribute("data-highlighted", "true");
    toggleDetails(screen.getByText("Request headers"));
    expect(screen.getByLabelText("Request headers")).toHaveTextContent("application/json");
  });

  it("shows useful empty states", () => {
    const { rerender } = render(<RequestInspector view="timeline" running={false} />);
    expect(screen.getByText(/event timeline/)).toBeVisible();
    rerender(<RequestInspector view="logs" trace={{ ...trace, events: [] }} running={false} />);
    expect(screen.getByText("No prediction logs were received.")).toBeVisible();
  });
});

function toggleDetails(summary: HTMLElement) {
  const details = summary.closest("details");
  if (!details) throw new Error("Expected summary inside details");
  details.open = true;
  fireEvent(details, new Event("toggle", { bubbles: true }));
}
