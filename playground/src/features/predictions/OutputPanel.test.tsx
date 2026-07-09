import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/editor/LazyJsonEditor", () => ({
  LazyJsonEditor: ({ label, value }: { label: string; value: string }) => (
    <pre aria-label={label}>{value}</pre>
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
    expect(screen.getByText("predict_time")).toBeVisible();
    expect(screen.getByText("0.2")).toBeVisible();
  });

  it.each([
    ["https://example.com/image.png", "img", "Prediction output"],
    ["data:application/octet-stream;base64,AA==", "link", "Download file"],
    ["https://example.com/file.txt", "link", "https://example.com/file.txt"],
  ])("renders media and URL output", (value, role, label) => {
    render(<OutputPanel output={value} rawEvents={[]} running={false} streaming={false} />);

    expect(screen.getByRole(role, { name: label })).toBeVisible();
  });

  it("shows raw response and request inspector views", () => {
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
    expect(screen.getByLabelText("Raw prediction events")).toHaveTextContent('{"answer":42}');
    fireEvent.click(screen.getByRole("tab", { name: "Request" }));
    expect(screen.getByText("Total duration")).toBeVisible();
    expect(screen.getByText("120 ms")).toBeVisible();
  });
});
