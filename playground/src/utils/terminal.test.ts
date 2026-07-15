import { describe, expect, it } from "vitest";

import { renderTerminalText } from "@/utils/terminal";

describe("renderTerminalText", () => {
  it("renders carriage-return progress updates as separate lines", () => {
    expect(renderTerminalText("starting\n\r  0%| | 0/28\r 50%|#| 14/28\r100%|#| 28/28\n")).toBe(
      "starting\n\n  0%| | 0/28\n 50%|#| 14/28\n100%|#| 28/28\n",
    );
  });

  it("normalizes CRLF without adding an extra line", () => {
    expect(renderTerminalText("first\rsecond\r\nthird")).toBe("first\nsecond\nthird");
  });
});
