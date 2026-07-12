import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/components/editor/LazyJsonEditor", () => ({
  LazyJsonEditor: ({
    label,
    readOnly,
    value,
    autoHeight,
  }: {
    label: string;
    readOnly?: boolean;
    value: string;
    autoHeight?: boolean;
  }) => (
    <pre aria-label={label} aria-readonly={readOnly} data-auto-height={autoHeight}>
      {value}
    </pre>
  ),
}));

import { OutputPanel } from "@/features/predictions/components/OutputPanel";

describe("OutputPanel", () => {
  it("renders status, errors, and output metrics", () => {
    render(
      <OutputPanel
        envelope={{
          status: "failed",
          error: "model failed",
          metrics: { predict_time: 0.2 },
        }}
        output="hello"
        rawEvents={['{"output":"hello"}']}
        running={false}
        streaming={false}
      />,
    );

    expect(screen.getByText("hello")).toBeVisible();
    expect(screen.getByText("model failed")).toHaveAttribute("role", "alert");
    expect(screen.getByRole("rowheader", { name: "predict_time" })).toBeVisible();
    expect(screen.getByRole("tabpanel")).toHaveAttribute(
      "aria-labelledby",
      "response-view-tab-output",
    );
  });

  it("announces a prediction error only once", () => {
    render(
      <OutputPanel
        envelope={{ status: "failed", error: "model failed" }}
        error="model failed"
        output={undefined}
        rawEvents={[]}
        running={false}
        streaming={false}
      />,
    );

    expect(screen.getAllByRole("alert")).toHaveLength(1);
  });

  it("pretty-prints sync JSON in Raw and opens request details", () => {
    render(
      <OutputPanel
        envelope={{ status: "succeeded" }}
        output={{ answer: 42 }}
        rawEvents={['{"answer":42}']}
        running={false}
        streaming={false}
        trace={{
          startedAt: 0,
          startedAtLabel: "10:00:00",
          finishedAt: 120,
          method: "POST",
          endpoint: "/predictions",
          requestHeaders: { Accept: "application/json" },
          requestBody: { input: {} },
          responseStatus: 200,
          responseBody: { answer: 42 },
          events: [],
        }}
      />,
    );

    fireEvent.click(screen.getByRole("tab", { name: "Raw" }));
    expect(screen.getByLabelText("Raw prediction response")).toHaveTextContent('"answer": 42');
    expect(screen.getByLabelText("Raw prediction response")).toHaveAttribute(
      "aria-readonly",
      "true",
    );
    expect(screen.getByLabelText("Raw prediction response")).toHaveAttribute(
      "data-auto-height",
      "true",
    );
    fireEvent.click(screen.getByRole("tab", { name: "Request" }));
    expect(screen.getByText("Total duration")).toBeVisible();
    expect(screen.getByText("120 ms")).toBeVisible();
  });

  it("shows live Raw frames exactly as received", () => {
    const frames = [
      'event: output\ndata: {"chunk":"hello"}',
      'event: completed\ndata: {"status":"succeeded"}',
    ];
    render(<OutputPanel output="hello" rawEvents={frames} running={false} streaming />);
    fireEvent.click(screen.getByRole("tab", { name: "Raw" }));

    expect(screen.getByLabelText("Raw prediction response").textContent).toBe(frames.join("\n\n"));
    expect(screen.getByLabelText("Raw prediction response")).not.toHaveTextContent(
      '"event": "output"',
    );
  });

  it("returns to Output when a new run starts", () => {
    const props = { output: "done", rawEvents: ['{"output":"done"}'], streaming: false };
    const { rerender } = render(<OutputPanel {...props} running={false} />);
    fireEvent.click(screen.getByRole("tab", { name: "Raw" }));
    expect(screen.getByRole("tab", { name: "Raw" })).toHaveAttribute("aria-selected", "true");

    rerender(<OutputPanel {...props} output={undefined} running />);
    expect(screen.getByRole("tab", { name: "Output" })).toHaveAttribute("aria-selected", "true");
  });

  it("shows logs when the prediction has log output", () => {
    render(
      <OutputPanel
        envelope={{ status: "succeeded", logs: "model log line" }}
        output="done"
        rawEvents={[]}
        running={false}
        streaming={false}
      />,
    );

    fireEvent.click(screen.getByRole("tab", { name: "Logs" }));
    expect(screen.getByText("model log line")).toBeVisible();
  });
});
