import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/components/editor/LazyJsonEditor", () => ({
  LazyJsonEditor: ({
    label,
    readOnly,
    value,
    autoHeight,
    followTail,
    active,
  }: {
    label: string;
    readOnly?: boolean;
    value: string;
    autoHeight?: boolean;
    followTail?: boolean;
    active?: boolean;
  }) => (
    <pre
      aria-label={label}
      aria-readonly={readOnly}
      data-auto-height={autoHeight}
      data-follow-tail={followTail}
      data-active={active}
    >
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
    expect(screen.getByText("Started")).toBeVisible();
    expect(screen.queryByText("Total duration")).toBeNull();
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

  it("keeps the selected tab when a new run starts", () => {
    const props = { output: "done", rawEvents: ['{"output":"done"}'], streaming: false };
    const { rerender } = render(<OutputPanel {...props} running={false} />);
    fireEvent.click(screen.getByRole("tab", { name: "Raw" }));
    expect(screen.getByRole("tab", { name: "Raw" })).toHaveAttribute("aria-selected", "true");

    rerender(<OutputPanel {...props} output={undefined} running />);
    expect(screen.getByRole("tab", { name: "Raw" })).toHaveAttribute("aria-selected", "true");
  });

  it("preserves visited panels while switching tabs during a stream", () => {
    const trace = {
      startedAt: 0,
      startedAtLabel: "10:00:00",
      method: "POST" as const,
      endpoint: "/predictions",
      requestHeaders: {},
      requestBody: { input: {} },
      events: [{ id: "1", elapsedMs: 10, kind: "sse" as const, label: "output", data: "one" }],
    };
    const { rerender } = render(
      <OutputPanel
        output={["one", "two"]}
        rawEvents={["one", "two"]}
        running
        streaming
        trace={trace}
      />,
    );

    fireEvent.click(screen.getByRole("tab", { name: "Raw" }));
    const rawEditor = screen.getByLabelText("Raw prediction response");
    expect(rawEditor).toHaveAttribute("data-follow-tail", "true");
    expect(rawEditor).toHaveAttribute("data-active", "true");

    fireEvent.click(screen.getByRole("tab", { name: "Timeline" }));
    expect(rawEditor).toHaveAttribute("data-follow-tail", "true");
    expect(rawEditor).toHaveAttribute("data-active", "false");
    toggleDetails(screen.getByText("Payload"));
    const details = screen.getByText("Payload").closest("details");

    rerender(
      <OutputPanel
        output={["one", "two", "three"]}
        rawEvents={["one", "two", "three"]}
        running
        streaming
        trace={{
          ...trace,
          events: [{ ...trace.events[0], elapsedMs: 20, data: "one\n\ntwo", count: 2 }],
        }}
      />,
    );
    expect(rawEditor).not.toHaveTextContent("three");

    fireEvent.click(screen.getByRole("tab", { name: "Request" }));
    fireEvent.click(screen.getByRole("tab", { name: "Timeline" }));
    expect(details).toHaveAttribute("open");

    fireEvent.click(screen.getByRole("tab", { name: "Raw" }));
    expect(screen.getByLabelText("Raw prediction response")).toBe(rawEditor);
    expect(rawEditor).toHaveTextContent("three");
    expect(rawEditor).toHaveAttribute("data-follow-tail", "true");
    expect(rawEditor).toHaveAttribute("data-active", "true");
  });

  it("resets tail following for a new run while a panel is hidden", () => {
    const props = { output: ["one"], rawEvents: ["one"], streaming: true };
    const { rerender } = render(<OutputPanel {...props} running />);
    fireEvent.click(screen.getByRole("tab", { name: "Raw" }));
    const rawEditor = screen.getByLabelText("Raw prediction response");
    fireEvent.click(screen.getByRole("tab", { name: "Timeline" }));

    rerender(<OutputPanel {...props} running={false} />);
    expect(rawEditor).toHaveAttribute("data-follow-tail", "false");
    rerender(<OutputPanel {...props} rawEvents={["new run"]} running />);

    expect(rawEditor).toHaveAttribute("data-follow-tail", "true");
    expect(rawEditor).toHaveAttribute("data-active", "false");
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

function toggleDetails(summary: HTMLElement) {
  const details = summary.closest("details");
  if (!details) throw new Error("Expected summary inside details");
  details.open = true;
  fireEvent(details, new Event("toggle", { bubbles: true }));
}
