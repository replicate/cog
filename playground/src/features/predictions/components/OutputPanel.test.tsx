import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/components/editor/LazyJsonEditor", async () => {
  const { FakeJsonEditor } = await import("@/test/FakeJsonEditor");
  return { LazyJsonEditor: FakeJsonEditor };
});

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

  it("shows the sync response and request details", () => {
    render(
      <OutputPanel
        envelope={{ status: "succeeded", metrics: { predict_time: 0.25 } }}
        output={{ answer: 42 }}
        rawEvents={[JSON.stringify({ answer: 42 }, null, 2)]}
        running={false}
        streaming={false}
        trace={{
          startedAtLabel: "10:00:00",
          method: "POST",
          endpoint: "/predictions",
          requestHeaders: { Accept: "application/json" },
          requestBody: { input: { prompt: "hello" } },
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
    expect(screen.getByText("250 ms")).toBeVisible();
    expect(screen.getByLabelText("Request body")).toHaveTextContent('"prompt": "hello"');
    expect(screen.getByLabelText("Response body")).toHaveTextContent('"answer": 42');
    toggleDetails(screen.getByText("Request headers"));
    expect(screen.getByLabelText("Request headers")).toHaveTextContent("application/json");
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
    expect(screen.getByText("output × 2")).toBeVisible();

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
        envelope={{ status: "succeeded", logs: "model log line\rprogress 1\rprogress 2\n" }}
        output="done"
        rawEvents={[]}
        running={false}
        streaming={false}
      />,
    );

    fireEvent.click(screen.getByRole("tab", { name: "Logs" }));
    expect(screen.getByLabelText("Prediction logs")).toHaveTextContent("model log line");
    expect(screen.getByLabelText("Prediction logs")).toHaveTextContent("progress 1");
    expect(screen.getByLabelText("Prediction logs")).toHaveTextContent("progress 2");

    fireEvent.click(screen.getByRole("tab", { name: "Timeline" }));
    expect(screen.getByText(/event timeline/)).toBeVisible();
  });
});

function toggleDetails(summary: HTMLElement) {
  const details = summary.closest("details");
  if (!details) throw new Error("Expected summary inside details");
  details.open = true;
  fireEvent(details, new Event("toggle", { bubbles: true }));
}
