import assert from "node:assert/strict";
import { test } from "vitest";

import { ConnectionState } from "../src/lib/state/connection-state.js";
import type {
  ConnectionApi,
  HealthResponse,
  OpenAPIDocument,
  PlaygroundConfig,
} from "../src/types.js";

test("loads config and connects to its default target", async () => {
  await withWindow(async () => {
    const api = modelApi({ config: { target: "http://configured", webhookBase: "http://hook" } });
    const state = new ConnectionState(api);
    await state.start();
    await settle();

    assert.equal(state.target, "http://configured");
    assert.equal(state.targetDraft, "http://configured");
    assert.equal(state.webhookBase, "http://hook");
    assert.equal(state.health.status, "READY");
    assert.equal(state.capabilities?.endpoint, "/predictions");
    state.destroy();
  });
});

test("falls back to localhost when config loading fails", async () => {
  await withWindow(async () => {
    const api = modelApi({ configError: new Error("offline") });
    const state = new ConnectionState(api);
    await state.start();
    await settle();

    assert.equal(state.target, "http://localhost:8393");
    assert.equal(state.schemaError, "");
    state.destroy();
  });
});

test("does not overwrite a manually connected target with late config", async () => {
  await withWindow(async () => {
    const config = deferred<PlaygroundConfig>();
    const api = modelApi({ configPromise: config.promise });
    const state = new ConnectionState(api);
    const starting = state.start();
    state.setDraft("http://manual");
    state.connect();
    config.resolve({ target: "http://configured" });
    await starting;
    await settle();

    assert.equal(state.target, "http://manual");
    assert.equal(state.targetDraft, "http://manual");
    state.destroy();
  });
});

test("ignores stale schema and health responses after reconnecting", async () => {
  await withWindow(async () => {
    const firstSchema = deferred<OpenAPIDocument>();
    const firstHealth = deferred<HealthResponse>();
    const api = modelApi({
      schema: (target) =>
        target === "http://first" ? firstSchema.promise : Promise.resolve(openapi("SecondInput")),
      health: (target) =>
        target === "http://first"
          ? firstHealth.promise
          : Promise.resolve({ status: "READY", user_healthcheck_error: "second" }),
    });
    const state = new ConnectionState(api);
    state.connect("http://first");
    state.connect("http://second");
    await settle();
    firstSchema.resolve(openapi("FirstInput"));
    firstHealth.resolve({ status: "DEFUNCT" });
    await settle();

    assert.equal(state.target, "http://second");
    assert.ok(state.schema?.components?.schemas?.SecondInput);
    assert.equal(state.health.user_healthcheck_error, "second");
    state.destroy();
  });
});

test("reports schema retries and unreachable health with original messages", async () => {
  await withWindow(async () => {
    const api = modelApi({
      schemaError: new Error("connection refused"),
      healthError: new Error("connection refused"),
    });
    const state = new ConnectionState(api);
    state.connect("http://offline");
    await settle();

    assert.equal(state.schemaError, "Waiting for schema... (connection refused)");
    assert.deepEqual(state.health, {
      status: "unreachable",
      user_healthcheck_error: "target unreachable",
    });
    state.destroy();
  });
});

test("destroy while config is pending does not reconnect", async () => {
  await withWindow(async () => {
    const config = deferred<PlaygroundConfig>();
    let schemaRequests = 0;
    let healthRequests = 0;
    const api = modelApi({
      configPromise: config.promise,
      schema: async () => {
        schemaRequests += 1;
        return openapi("Input");
      },
      health: async () => {
        healthRequests += 1;
        return { status: "READY" };
      },
    });
    const state = new ConnectionState(api);
    const starting = state.start();

    state.destroy();
    config.resolve({ target: "http://configured" });
    await starting;
    await settle();

    assert.equal(schemaRequests, 0);
    assert.equal(healthRequests, 0);
    assert.equal(state.hasConnected, false);
  });
});

type ModelApiOptions = {
  config?: PlaygroundConfig;
  configError?: Error;
  configPromise?: Promise<PlaygroundConfig>;
  schema?: (target: string) => Promise<OpenAPIDocument>;
  schemaError?: Error;
  health?: (target: string) => Promise<HealthResponse>;
  healthError?: Error;
};

function modelApi(options: ModelApiOptions = {}): ConnectionApi {
  return {
    config: async () => {
      if (options.configPromise) return options.configPromise;
      if (options.configError) throw options.configError;
      return options.config ?? {};
    },
    schema: async (target: string) => {
      if (options.schema) return options.schema(target);
      if (options.schemaError) throw options.schemaError;
      return openapi("Input");
    },
    health: async (target: string) => {
      if (options.health) return options.health(target);
      if (options.healthError) throw options.healthError;
      return { status: "READY" };
    },
  };
}

function openapi(name: string): OpenAPIDocument {
  return {
    components: { schemas: { Input: { type: "object", properties: {} }, [name]: {} } },
    paths: { "/predictions": { post: {} } },
  };
}

function deferred<T>(): { promise: Promise<T>; resolve: (value: T) => void } {
  let resolve: (value: T) => void = () => {};
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

async function settle(): Promise<void> {
  await new Promise((resolve) => setTimeout(resolve, 0));
}

async function withWindow(run: () => Promise<void>): Promise<void> {
  const descriptor = Object.getOwnPropertyDescriptor(globalThis, "window");
  Object.defineProperty(globalThis, "window", { configurable: true, value: globalThis });
  try {
    await run();
  } finally {
    if (descriptor) Object.defineProperty(globalThis, "window", descriptor);
    else Reflect.deleteProperty(globalThis, "window");
  }
}
