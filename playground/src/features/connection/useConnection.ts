import { useEffect, useMemo, useState } from "react";

import type { CogApi } from "../../api/cog";
import { inputAndOutputSchemas } from "../../domain/schema";
import type { HealthResponse, OpenAPIDocument } from "../../domain/types";

const DEFAULT_TARGET = "http://localhost:8393";
const TARGET_STORAGE_KEY = "cog-playground-target";

export function useConnection(api: CogApi) {
  const initialTarget = useMemo(() => {
    const query = new URLSearchParams(location.search).get("target");
    return query || localStorage.getItem(TARGET_STORAGE_KEY) || DEFAULT_TARGET;
  }, []);
  const [targetDraft, setTargetDraft] = useState(initialTarget);
  const [target, setTarget] = useState(initialTarget);
  const [health, setHealth] = useState<HealthResponse>({ status: "unknown" });
  const [schema, setSchema] = useState<OpenAPIDocument>();
  const [schemaError, setSchemaError] = useState("");
  const [webhookBase, setWebhookBase] = useState("");
  const capabilities = useMemo(
    () => (schema ? inputAndOutputSchemas(schema) : undefined),
    [schema],
  );

  useEffect(() => {
    api
      .config()
      .then((config) => setWebhookBase(config.webhookBase ?? ""))
      .catch(() => {});
  }, [api]);

  useEffect(() => {
    api.setTarget(target);
    localStorage.setItem(TARGET_STORAGE_KEY, target);
    history.replaceState(null, "", `?target=${encodeURIComponent(target)}`);
    let canceled = false;
    let retry: number | undefined;
    const loadSchema = async () => {
      try {
        const document = await api.schema();
        if (canceled) return;
        setSchema(document);
        setSchemaError("");
      } catch (error) {
        if (canceled) return;
        setSchema(undefined);
        setSchemaError(`Waiting for schema... (${errorMessage(error)})`);
        retry = window.setTimeout(loadSchema, 3000);
      }
    };
    void loadSchema();
    return () => {
      canceled = true;
      if (retry) clearTimeout(retry);
    };
  }, [api, target]);

  useEffect(() => {
    let canceled = false;
    const poll = async () => {
      try {
        const next = await api.health();
        if (!canceled) setHealth(next);
      } catch {
        if (!canceled) {
          setHealth({ status: "unreachable", user_healthcheck_error: "target unreachable" });
        }
      }
    };
    void poll();
    const timer = window.setInterval(poll, 5000);
    return () => {
      canceled = true;
      clearInterval(timer);
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
