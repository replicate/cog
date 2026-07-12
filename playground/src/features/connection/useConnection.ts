import { useEffect, useMemo, useState } from "react";

import type { CogApi } from "../../api/cog";
import { inputAndOutputSchemas } from "../../domain/schema";
import type { HealthResponse, OpenAPIDocument } from "../../domain/types";

const DEFAULT_TARGET = "http://localhost:8393";
const REQUEST_TIMEOUT = 10_000;

export function useConnection(api: CogApi) {
  const [targetDraft, setTargetDraft] = useState("");
  const [target, setTarget] = useState("");
  const [health, setHealth] = useState<HealthResponse>({ status: "unknown" });
  const [schema, setSchema] = useState<OpenAPIDocument>();
  const [schemaError, setSchemaError] = useState("");
  const [webhookBase, setWebhookBase] = useState("");
  const capabilities = useMemo(
    () => (schema ? inputAndOutputSchemas(schema) : undefined),
    [schema],
  );

  useEffect(() => {
    const controller = new AbortController();
    api
      .config(AbortSignal.any([controller.signal, AbortSignal.timeout(REQUEST_TIMEOUT)]))
      .then((config) => {
        const initialTarget = config.target?.trim() || DEFAULT_TARGET;
        setTargetDraft(initialTarget);
        setTarget(initialTarget);
        setWebhookBase(config.webhookBase ?? "");
      })
      .catch((error: unknown) => {
        if (isAbortError(error)) return;
        setTargetDraft(DEFAULT_TARGET);
        setTarget(DEFAULT_TARGET);
      });
    return () => controller.abort();
  }, [api]);

  useEffect(() => {
    if (!target) return;
    api.setTarget(target);
    setHealth({ status: "unknown" });
    setSchema(undefined);
    setSchemaError("Loading schema...");
    let canceled = false;
    let retry: number | undefined;
    const controller = new AbortController();
    const loadSchema = async () => {
      try {
        const document = await api.schema(
          AbortSignal.any([controller.signal, AbortSignal.timeout(REQUEST_TIMEOUT)]),
        );
        if (canceled) return;
        setSchema(document);
        setSchemaError("");
      } catch (error) {
        if (canceled || isAbortError(error)) return;
        setSchema(undefined);
        setSchemaError(`Waiting for schema... (${errorMessage(error)})`);
        retry = window.setTimeout(loadSchema, 3000);
      }
    };
    void loadSchema();
    return () => {
      canceled = true;
      controller.abort();
      if (retry) clearTimeout(retry);
    };
  }, [api, target]);

  useEffect(() => {
    if (!target) return;
    let canceled = false;
    let timer: number | undefined;
    const controller = new AbortController();
    const poll = async () => {
      try {
        const next = await api.health(
          AbortSignal.any([controller.signal, AbortSignal.timeout(REQUEST_TIMEOUT)]),
        );
        if (!canceled) setHealth(next);
      } catch (error) {
        if (!canceled && !isAbortError(error)) {
          setHealth({ status: "unreachable", user_healthcheck_error: "target unreachable" });
        }
      } finally {
        if (!canceled) timer = window.setTimeout(poll, 5000);
      }
    };
    void poll();
    return () => {
      canceled = true;
      controller.abort();
      if (timer) clearTimeout(timer);
    };
  }, [api, target]);

  return {
    target,
    targetDraft,
    setTargetDraft,
    connect: () => {
      const next = targetDraft.trim();
      if (next) setTarget(next);
    },
    health,
    schema,
    schemaError,
    webhookBase,
    capabilities,
  };
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}
