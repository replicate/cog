import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/editor/LazyJsonEditor", () => ({
  LazyJsonEditor: ({
    label,
    readOnly,
    value,
  }: {
    label: string;
    readOnly?: boolean;
    value: string;
  }) => (
    <pre aria-label={label} aria-readonly={readOnly}>
      {value}
    </pre>
  ),
}));

import { OutputPanel } from "./OutputPanel";

describe("OutputPanel", () => {
  it("renders text, metrics, and prediction errors", () => {
    render(
      <OutputPanel
        envelope={{
          status: "failed",
          output: "ignored",
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
    expect(screen.getByText("0.2")).toBeVisible();
    expect(screen.getByRole("region", { name: "Prediction output" })).toHaveAttribute(
      "tabindex",
      "0",
    );
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

  it.each([
    ["data:image/png;base64,AA==", "img", "Prediction output"],
    ["https://example.com/image.png", "link", "https://example.com/image.png"],
    ["data:application/octet-stream;base64,AA==", "link", "Download file"],
    ["https://example.com/file.txt", "link", "https://example.com/file.txt"],
  ])("renders media and URL output", (value, role, label) => {
    render(<OutputPanel output={value} rawEvents={[]} running={false} streaming={false} />);

    expect(screen.getByRole(role, { name: label })).toBeVisible();
  });

  it("shows a highlighted response document and request inspector views", () => {
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

    fireEvent.click(screen.getByRole("tab", { name: "Response" }));
    expect(screen.getByLabelText("Prediction response")).toHaveTextContent('"answer": 42');
    expect(screen.getByLabelText("Prediction response")).toHaveAttribute("aria-readonly", "true");
    fireEvent.click(screen.getByRole("tab", { name: "Request" }));
    expect(screen.getByText("Total duration")).toBeVisible();
    expect(screen.getByText("120 ms")).toBeVisible();
  });

  it("concatenates only outputs marked for concatenation", () => {
    const { rerender } = render(
      <OutputPanel
        output={["hello ", "world"]}
        outputSchema={{ type: "array", items: { type: "string" } }}
        rawEvents={[]}
        running={false}
        streaming={false}
      />,
    );
    expect(screen.getByLabelText("Structured prediction output")).toHaveTextContent("hello ");

    rerender(
      <OutputPanel
        output={["hello ", "world"]}
        outputSchema={{
          type: "array",
          items: { type: "string" },
          "x-cog-array-type": "iterator",
        }}
        rawEvents={[]}
        running={false}
        streaming
      />,
    );
    expect(screen.getByLabelText("Structured prediction output")).toHaveTextContent("hello ");

    rerender(
      <OutputPanel
        output={["hello ", "world"]}
        outputSchema={{
          type: "array",
          items: { type: "string" },
          "x-cog-array-type": "iterator",
          "x-cog-array-display": "concatenate",
        }}
        rawEvents={[]}
        running={false}
        streaming
      />,
    );
    expect(screen.getByText("hello world")).toBeVisible();
  });

  it("renders progressive output as a live preview", () => {
    const schema = {
      type: "array",
      items: { type: "string" },
      "x-cog-array-display": "concatenate",
    };
    const { container, rerender } = render(
      <OutputPanel output={["hello "]} outputSchema={schema} rawEvents={[]} running streaming />,
    );

    expect(screen.getByRole("region", { name: "Prediction output" })).toBeVisible();
    expect(screen.getByText("Prediction running")).toHaveAttribute("aria-live", "polite");
    expect(screen.getByText("hello")).toBeVisible();
    expect(container.querySelector(".streaming-cursor")).toBeVisible();

    rerender(
      <OutputPanel
        envelope={{ status: "succeeded" }}
        output={["hello ", "world"]}
        outputSchema={schema}
        rawEvents={[]}
        running={false}
        streaming
      />,
    );
    expect(screen.getByText("Prediction succeeded")).toHaveAttribute("aria-live", "polite");
    expect(screen.getByText("hello world")).toBeVisible();
    expect(container.querySelector(".streaming-cursor")).not.toBeInTheDocument();
  });

  it("resumes following output when a new run starts", () => {
    const { rerender } = render(
      <OutputPanel output="first" rawEvents={[]} running={false} streaming />,
    );
    const output = screen.getByRole("region", { name: "Prediction output" });
    Object.defineProperties(output, {
      clientHeight: { configurable: true, value: 100 },
      scrollHeight: { configurable: true, value: 1000 },
      scrollTop: { configurable: true, value: 100, writable: true },
    });
    fireEvent.scroll(output);

    rerender(<OutputPanel output="second" rawEvents={[]} running={false} streaming />);
    expect(output.scrollTop).toBe(100);

    rerender(<OutputPanel output="third" rawEvents={[]} running streaming />);
    expect(output.scrollTop).toBe(1000);
  });

  it("returns to Output when a new run starts", () => {
    const props = { output: "done", rawEvents: ['{"output":"done"}'], streaming: false };
    const { rerender } = render(<OutputPanel {...props} running={false} />);
    fireEvent.click(screen.getByRole("tab", { name: "Response" }));
    expect(screen.getByRole("tab", { name: "Response" })).toHaveAttribute("aria-selected", "true");

    rerender(<OutputPanel {...props} output={undefined} running />);
    expect(screen.getByRole("tab", { name: "Output" })).toHaveAttribute("aria-selected", "true");
  });

  it("turns SSE frames into readable response JSON", () => {
    render(
      <OutputPanel
        output="hello"
        rawEvents={['event: output\ndata: {"chunk":"hello"}']}
        running={false}
        streaming
      />,
    );
    fireEvent.click(screen.getByRole("tab", { name: "Response" }));

    expect(screen.getByLabelText("Prediction response")).toHaveTextContent('"event": "output"');
    expect(screen.getByLabelText("Prediction response")).toHaveTextContent('"chunk": "hello"');
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

  it("renders audio and video outputs", () => {
    const { container, rerender } = render(
      <OutputPanel
        output="data:audio/wav;base64,AA=="
        rawEvents={[]}
        running={false}
        streaming={false}
      />,
    );
    expect(container.querySelector("audio")).toHaveAttribute("controls");

    rerender(
      <OutputPanel
        output="data:video/mp4;base64,AA=="
        rawEvents={[]}
        running={false}
        streaming={false}
      />,
    );
    expect(container.querySelector("video")).toHaveAttribute("controls");
  });
});
