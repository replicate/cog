import assert from "node:assert/strict";
import { test } from "vitest";

import { InputState } from "../src/lib/state/input-state.js";
import type {
  OpenAPIDocument,
  PlaygroundCapabilities,
  ValidationIssue,
  ValidationRequest,
  ValidationResponse,
} from "../src/types.js";

const document: OpenAPIDocument = {
  components: {
    schemas: {
      Input: {
        type: "object",
        required: ["choice", "enabled"],
        properties: {
          text: { type: "string", default: "hello", "x-order": 3 },
          choice: { type: "integer", enum: [1, 2], "x-order": 1 },
          enabled: { type: "boolean", "x-order": 2 },
          nullable: { type: ["string", "null"], default: null },
        },
      },
    },
  },
  paths: { "/predictions": { post: {} } },
};
const inputSchema = document.components?.schemas?.Input;
if (!inputSchema) throw new Error("Input schema fixture is missing");

const capabilities: PlaygroundCapabilities = {
  endpoint: "/predictions",
  input: inputSchema,
  streaming: false,
  async: false,
};

test("connect omits optional fields with declared defaults", () => {
  const state = new InputState();
  state.connect("http://model", document, capabilities);
  assert.deepEqual(state.input, { choice: 1, enabled: false });
  assert.deepEqual(JSON.parse(state.jsonInput), state.input);
  state.destroy();
});

test("form changes keep canonical JSON synchronized", () => {
  const state = connectedInput();
  state.changeForm({ ...state.input, text: "changed", enabled: true });
  assert.equal(state.input.text, "changed");
  assert.equal(JSON.parse(state.jsonInput).enabled, true);
  state.destroy();
});

test("malformed JSON preserves the last valid object", () => {
  const state = connectedInput();
  const previous = state.input;
  state.changeJSON('{"text":');
  assert.match(state.jsonError, /JSON/);
  assert.equal(state.input, previous);
  state.destroy();
});

test("leaving malformed JSON mode restores canonical JSON", () => {
  const state = connectedInput();
  state.changeMode("json");
  state.changeJSON("{");
  state.changeMode("form");
  assert.equal(state.jsonError, "");
  assert.deepEqual(JSON.parse(state.jsonInput), state.input);
  state.destroy();
});

test("format normalizes valid JSON and retains malformed drafts", () => {
  const state = connectedInput();
  state.changeMode("json");
  state.changeJSON('{"choice":2,"enabled":true}');
  state.formatJSON();
  assert.equal(state.jsonInput, '{\n  "choice": 2,\n  "enabled": true\n}');
  state.changeJSON("{");
  state.formatJSON();
  assert.equal(state.jsonInput, "{");
  assert.notEqual(state.jsonError, "");
  state.destroy();
});

test("reset restores defaults and clears local field state", () => {
  const state = connectedInput();
  state.changeForm({ choice: 2, enabled: true, text: "changed" });
  state.setFieldBusy("file", true);
  state.setFieldValid("file", false);
  state.reset();
  assert.deepEqual(state.input, { choice: 1, enabled: false });
  assert.equal(state.formBusy, false);
  assert.equal(state.formValid, true);
  state.destroy();
});

test("successful worker validation returns the current input", async () => {
  await withWorker(
    () => [],
    async () => {
      const state = connectedInput();
      const input = await state.validateForRun();
      assert.deepEqual(input, state.input);
      assert.equal(state.validating, false);
      assert.deepEqual(state.issues, []);
      state.destroy();
    },
  );
});

test("worker validation issues remain field-oriented and block running", async () => {
  const issues: ValidationIssue[] = [
    { field: "text", keyword: "pattern", message: "Bad text", path: "input.text" },
  ];
  await withWorker(
    () => issues,
    async () => {
      const state = connectedInput();
      assert.equal(await state.validateForRun(), undefined);
      assert.deepEqual(state.issues, issues);
      state.destroy();
    },
  );
});

test("reset invalidates a pending validation response", async () => {
  const validation = deferred<ValidationIssue[]>();
  await withWorker(
    () => validation.promise,
    async () => {
      const state = connectedInput();
      const pending = state.validateForRun();
      assert.equal(state.validating, true);
      state.reset();
      validation.resolve([
        { field: "text", keyword: "pattern", message: "stale", path: "input.text" },
      ]);
      assert.equal(await pending, undefined);
      assert.equal(state.validating, false);
      assert.deepEqual(state.issues, []);
      state.destroy();
    },
  );
});

test("validation timeout terminates the stuck worker", async () => {
  let terminated = 0;
  await withWorker(
    () => new Promise(() => {}),
    async () => {
      const state = connectedInput();
      assert.equal(await state.validateForRun(), undefined);
      assert.match(state.issues[0]?.message ?? "", /validation timed out/);
      assert.equal(terminated, 1);
      state.destroy();
    },
    {
      onTerminate: () => {
        terminated += 1;
      },
      setTimeout: (callback: () => void) => {
        queueMicrotask(callback);
        return 1;
      },
    },
  );
});

test("disconnect clears busy and invalid fields", () => {
  const state = connectedInput();
  state.setFieldBusy("file", true);
  state.setFieldValid("file", false);
  state.connect("http://model", undefined, undefined);
  assert.equal(state.formBusy, false);
  assert.equal(state.formValid, true);
  state.destroy();
});

test("unmounting a file control clears its pending read without clearing its error", () => {
  const state = connectedInput();
  state.setFieldBusy("file", true);
  state.setFieldValid("file", false);

  assert.equal(state.clearFieldBusy("file"), true);
  assert.equal(state.formBusy, false);
  assert.equal(state.formValid, false);
  assert.equal(state.clearFieldBusy("file"), false);
  state.destroy();
});

function connectedInput(): InputState {
  const state = new InputState();
  state.connect("http://model", document, capabilities);
  return state;
}

type WorkerOptions = {
  onTerminate?: () => void;
  setTimeout?: (callback: () => void, delay?: number) => number;
};

async function withWorker(
  validate: () => ValidationIssue[] | Promise<ValidationIssue[]>,
  run: () => Promise<void>,
  options: WorkerOptions = {},
): Promise<void> {
  const windowDescriptor = Object.getOwnPropertyDescriptor(globalThis, "window");
  const workerDescriptor = Object.getOwnPropertyDescriptor(globalThis, "Worker");
  class FakeWorker {
    onmessage: ((event: MessageEvent<ValidationResponse>) => void) | null = null;
    onerror: ((event: Event) => void) | null = null;
    postMessage(message: ValidationRequest): void {
      void Promise.resolve(validate()).then((issues) => {
        const handler = this.onmessage;
        handler?.(new MessageEvent("message", { data: { id: message.id, issues } }));
      });
    }
    terminate(): void {
      options.onTerminate?.();
    }
  }
  Object.defineProperty(globalThis, "window", {
    configurable: true,
    value: options.setTimeout ? { setTimeout: options.setTimeout } : globalThis,
  });
  Object.defineProperty(globalThis, "Worker", { configurable: true, value: FakeWorker });
  try {
    await run();
  } finally {
    restore("window", windowDescriptor);
    restore("Worker", workerDescriptor);
  }
}

function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve: (value: T) => void = () => {};
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function restore(name: "window" | "Worker", descriptor: PropertyDescriptor | undefined): void {
  if (descriptor) Object.defineProperty(globalThis, name, descriptor);
  else Reflect.deleteProperty(globalThis, name);
}
