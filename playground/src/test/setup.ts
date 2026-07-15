import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

afterEach(cleanup);

vi.stubGlobal(
  "ResizeObserver",
  class {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  },
);

HTMLElement.prototype.scrollIntoView = vi.fn();
Object.defineProperty(Range.prototype, "getClientRects", {
  configurable: true,
  value: () => [] as unknown as DOMRectList,
});
Object.defineProperty(Range.prototype, "getBoundingClientRect", {
  configurable: true,
  value: () => new DOMRect(),
});
window.requestAnimationFrame = (callback) =>
  window.setTimeout(() => callback(performance.now()), 0);
window.cancelAnimationFrame = window.clearTimeout;
