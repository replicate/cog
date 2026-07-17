import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { PredictionOutput } from "@/features/predictions/components/PredictionOutput";

describe("PredictionOutput", () => {
  it.each([
    ["data:image/png;base64,AA==", "img", "Prediction output"],
    ["https://example.com/image.png", "link", "https://example.com/image.png"],
    ["data:application/octet-stream;base64,AA==", "link", "Download file"],
  ])("renders media and URL output", (value, role, label) => {
    render(<PredictionOutput value={value} running={false} />);
    expect(screen.getByRole(role, { name: label })).toBeVisible();
  });

  it("concatenates string chunks as plain text without a read-only editor", () => {
    const { container } = render(<PredictionOutput value={["hello ", "world"]} running={false} />);

    expect(screen.getByText("hello world")).toBeVisible();
    expect(container.querySelector(".cm-editor")).not.toBeInTheDocument();
  });

  it("renders structured output as plain preformatted text", () => {
    const { container } = render(<PredictionOutput value={{ answer: 42 }} running={false} />);

    expect(screen.getByText(/"answer": 42/)).toBeVisible();
    expect(container.querySelector(".cm-editor")).not.toBeInTheDocument();
  });

  it("keeps ordinary data-prefixed text visible", () => {
    render(<PredictionOutput value="data: analyze this model" running={false} />);
    expect(screen.getByText("data: analyze this model")).toBeVisible();
  });

  it("renders progressive output as a live preview", () => {
    const { container, rerender } = render(
      <PredictionOutput value={["hello "]} running metrics={{ predict_time: 0.2 }} />,
    );

    expect(screen.getByRole("region", { name: "Prediction output" })).toHaveAttribute(
      "aria-busy",
      "true",
    );
    expect(screen.getByText("hello")).toBeVisible();
    expect(container.querySelector(".streaming-cursor")).toBeVisible();

    rerender(<PredictionOutput value={["hello ", "world"]} running={false} />);
    expect(screen.getByText("hello world")).toBeVisible();
    expect(container.querySelector(".streaming-cursor")).not.toBeInTheDocument();
  });

  it("resumes following output when a new run starts", () => {
    const { rerender } = render(<PredictionOutput value="first" running={false} />);
    const output = screen.getByRole("region", { name: "Prediction output" });
    Object.defineProperties(output, {
      clientHeight: { configurable: true, value: 100 },
      scrollHeight: { configurable: true, value: 1000 },
      scrollTop: { configurable: true, value: 100, writable: true },
    });
    fireEvent.scroll(output);

    rerender(<PredictionOutput value="second" running={false} />);
    expect(output.scrollTop).toBe(100);
    rerender(<PredictionOutput value="third" running />);
    expect(output.scrollTop).toBe(1000);
  });

  it("renders audio and video outputs", () => {
    const { container, rerender } = render(
      <PredictionOutput value="data:audio/wav;base64,AA==" running={false} />,
    );
    expect(container.querySelector("audio")).toHaveAttribute("controls");

    rerender(<PredictionOutput value="data:video/mp4;base64,AA==" running={false} />);
    expect(container.querySelector("video")).toHaveAttribute("controls");
  });
});
