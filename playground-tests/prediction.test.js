// @ts-check

import assert from "node:assert/strict";
import test from "node:test";

import { InputState } from "../pkg/cli/playground/lib/state/input-state.js";
import { PredictionState } from "../pkg/cli/playground/lib/state/prediction-state.js";

test("retains the selected run mode independently of the output shape", async () => {
  const api = {
    submit: async () => ({ status: "succeeded", output: ["first", "second"] }),
  };
  const state = new PredictionState(/** @type {any} */ (api));

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
  /** @type {FakeEventSource | undefined} */ let events;
  class FakeEventSource {
    constructor() {
      events = this;
      queueMicrotask(() => this.onopen?.(new Event("open")));
    }
    /** @type {((event:Event)=>void) | null} */ onopen = null;
    /** @type {((event:Event)=>void) | null} */ onerror = null;
    /** @type {((event:MessageEvent<string>)=>void) | null} */ onmessage = null;
    close() {}
  }
  Object.defineProperty(globalThis, "window", { configurable: true, value: globalThis });
  Object.defineProperty(globalThis, "EventSource", { configurable: true, value: FakeEventSource });
  try {
    const api = {
      submit: async () => {
        events?.onmessage?.(
          /** @type {MessageEvent<string>} */ (
            new MessageEvent("message", {
              data: JSON.stringify({ status: "processing", output: "from webhook" }),
            })
          ),
        );
        return { status: "starting", output: null };
      },
    };
    const state = new PredictionState(/** @type {any} */ (api));

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

/** @param {"window" | "EventSource"} name @param {PropertyDescriptor | undefined} descriptor */
function restoreProperty(name, descriptor) {
  if (descriptor) Object.defineProperty(globalThis, name, descriptor);
  else Reflect.deleteProperty(globalThis, name);
}
