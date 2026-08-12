import assert from "node:assert/strict";
import { test } from "vitest";

import { InputState } from "../src/lib/state/input-state.js";
import { PredictionState } from "../src/lib/state/prediction-state.js";
import type { PredictionApi } from "../src/types.js";

test("retains the selected run mode independently of the output shape", async () => {
  const api = predictionApi({
    submit: async () => ({ status: "succeeded", output: ["first", "second"] }),
  });
  const state = new PredictionState(api);

  await state.run({
    target: "http://model",
    endpoint: "/predictions",
    input: {},
    mode: "sync",
    webhookBase: "",
    webhookEvents: [],
  });

  assert.equal(state.mode, "sync");
  assert.deepEqual(state.output, ["first", "second"]);

  state.reset();
  assert.equal(state.mode, undefined);
});

test("preserves webhook output when the async acknowledgement arrives later", async () => {
  const originalWindow = Object.getOwnPropertyDescriptor(globalThis, "window");
  const originalEventSource = Object.getOwnPropertyDescriptor(globalThis, "EventSource");
  let events: FakeEventSource | undefined;
  class FakeEventSource {
    constructor() {
      // oxlint-disable-next-line typescript-eslint/no-this-alias
      events = this;
      queueMicrotask(() => {
        const handler = this.onopen;
        handler?.(new Event("open"));
      });
    }
    onopen: ((event: Event) => void) | null = null;
    onerror: ((event: Event) => void) | null = null;
    onmessage: ((event: MessageEvent<string>) => void) | null = null;
    close(): void {}
  }
  Object.defineProperty(globalThis, "window", { configurable: true, value: globalThis });
  Object.defineProperty(globalThis, "EventSource", { configurable: true, value: FakeEventSource });
  try {
    const api = predictionApi({
      submit: async () => {
        const handler = events?.onmessage;
        handler?.(
          new MessageEvent("message", {
            data: JSON.stringify({ status: "processing", output: "from webhook" }),
          }),
        );
        return { status: "starting", output: null };
      },
    });
    const state = new PredictionState(api);

    await state.run({
      target: "http://model",
      endpoint: "/predictions",
      input: {},
      mode: "async",
      webhookBase: "http://webhook",
      webhookEvents: [],
    });

    assert.equal(state.output, "from webhook");
    assert.equal(state.envelope?.output, "from webhook");
    state.reset();
  } finally {
    restoreProperty("window", originalWindow);
    restoreProperty("EventSource", originalEventSource);
  }
});

test("derives form busy and validity state across independent fields", () => {
  const state = new InputState();

  state.setFieldBusy("first", true);
  state.setFieldBusy("second", true);
  state.setFieldBusy("first", false);
  assert.equal(state.formBusy, true);
  state.setFieldBusy("second", false);
  assert.equal(state.formBusy, false);

  state.setFieldValid("first", false);
  state.setFieldValid("second", false);
  state.setFieldValid("first", true);
  assert.equal(state.formValid, false);
  state.setFieldValid("second", true);
  assert.equal(state.formValid, true);
});

function predictionApi(overrides: Partial<PredictionApi>): PredictionApi {
  return {
    submit: async () => ({ status: "succeeded" }),
    stream: async function* () {},
    cancel: async () => {},
    ...overrides,
  };
}

function restoreProperty(
  name: "window" | "EventSource",
  descriptor: PropertyDescriptor | undefined,
): void {
  if (descriptor) Object.defineProperty(globalThis, name, descriptor);
  else Reflect.deleteProperty(globalThis, name);
}
