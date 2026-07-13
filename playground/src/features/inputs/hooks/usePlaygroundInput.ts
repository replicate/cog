import { useEffect, useRef, useState } from "react";

import type { InputMode } from "@/features/inputs/types";
import { errorMessage, parseInputObject, serializeInput } from "@/features/inputs/utils/input";
import { disposeValidationWorker, validateInput } from "@/features/inputs/validation/validateInput";
import type { ValidationIssue } from "@/features/inputs/validation/inputValidation";
import type { OpenAPIDocument } from "@/types/openapi";
import { defaultInput, type PlaygroundSchemas } from "@/utils/openapi";

type Options = {
  target: string;
  document?: OpenAPIDocument;
  capabilities?: PlaygroundSchemas;
};

type ValidatedInput = {
  input: Record<string, unknown>;
};

/**
 * Keeps form and JSON input synchronized with schema defaults and exposes an abortable,
 * stale-result-safe validation lifecycle for prediction runs.
 */
export function usePlaygroundInput({ target, document, capabilities }: Options) {
  const [input, setInput] = useState<Record<string, unknown>>({});
  const [jsonInput, setJsonInput] = useState("{}");
  const [jsonError, setJsonError] = useState("");
  const [inputMode, setInputMode] = useState<InputMode>("form");
  const [formBusy, setFormBusy] = useState(false);
  const [formValid, setFormValid] = useState(true);
  const [validating, setValidating] = useState(false);
  const [validationIssues, setValidationIssues] = useState<ValidationIssue[]>([]);
  const [formRevision, setFormRevision] = useState(0);
  const validationAttempt = useRef(0);
  const validationController = useRef<AbortController | undefined>(undefined);
  const connectionState = useRef({ target, document, capabilities });
  connectionState.current = { target, document, capabilities };
  // Identifies the connected schema so the validation worker compiles it once
  // and reuses the compiled validator across runs.
  const schemaId = useRef(0);

  useEffect(
    () => () => {
      validationAttempt.current += 1;
      validationController.current?.abort();
      disposeValidationWorker();
    },
    [],
  );

  useEffect(() => {
    validationController.current?.abort();
    validationController.current = undefined;
    validationAttempt.current += 1;
    setValidationIssues([]);
    setValidating(false);
    if (!document || !capabilities) {
      setFormBusy(false);
      setFormValid(true);
      return;
    }
    // A new connection means a new (immutable) schema to compile once.
    schemaId.current += 1;
    const defaults = defaultInput(document, capabilities.input);
    setInput(defaults);
    setJsonInput(serializeInput(defaults));
    setJsonError("");
    setFormBusy(false);
    setFormValid(true);
    setFormRevision((current) => current + 1);
  }, [capabilities, document, target]);

  const clearValidation = () => {
    validationController.current?.abort();
    validationController.current = undefined;
    validationAttempt.current += 1;
    setValidating(false);
    setValidationIssues([]);
  };

  const validateForRun = async (): Promise<ValidatedInput | undefined> => {
    if (!document || !capabilities || formBusy) return undefined;

    let nextInput = input;
    if (inputMode === "json") {
      try {
        nextInput = parseInputObject(jsonInput);
        setJsonError("");
      } catch (error) {
        setJsonError(errorMessage(error));
        return undefined;
      }
    }
    if (inputMode === "form" && !formValid) return undefined;

    const validationTarget = target;
    const validationDocument = document;
    const validationCapabilities = capabilities;
    const attempt = ++validationAttempt.current;
    const controller = new AbortController();
    validationController.current = controller;
    setValidationIssues([]);
    setValidating(true);
    const issues = await validateInput(
      validationDocument,
      validationCapabilities.input,
      nextInput,
      schemaId.current,
      controller.signal,
    );
    if (validationController.current === controller) validationController.current = undefined;
    if (
      attempt !== validationAttempt.current ||
      validationTarget !== connectionState.current.target ||
      validationDocument !== connectionState.current.document ||
      validationCapabilities !== connectionState.current.capabilities
    ) {
      return undefined;
    }
    setValidating(false);
    if (issues.length > 0) {
      setValidationIssues(issues);
      return undefined;
    }
    return { input: nextInput };
  };

  const reset = () => {
    if (!document || !capabilities) return;
    const defaults = defaultInput(document, capabilities.input);
    setInput(defaults);
    setJsonInput(serializeInput(defaults));
    setJsonError("");
    setFormBusy(false);
    setFormValid(true);
    clearValidation();
    setFormRevision((current) => current + 1);
  };

  const changeJsonInput = (next: string) => {
    clearValidation();
    setJsonInput(next);
    try {
      const parsed = parseInputObject(next);
      setInput(parsed);
      setJsonError("");
    } catch (error) {
      setJsonError(errorMessage(error));
    }
  };

  const changeFormInput = (next: Record<string, unknown>) => {
    clearValidation();
    setInput(next);
    setJsonInput(serializeInput(next));
    setJsonError("");
  };

  const changeInputMode = (next: InputMode) => {
    if (next === inputMode) return;
    clearValidation();
    if (next === "json") {
      setJsonInput(serializeInput(input));
      setJsonError("");
      setInputMode("json");
      return;
    }
    try {
      setInput(parseInputObject(jsonInput));
      setJsonError("");
    } catch {
      setJsonInput(serializeInput(input));
      setJsonError("");
    }
    setInputMode("form");
  };

  const formatJsonInput = () => {
    clearValidation();
    try {
      const parsed = parseInputObject(jsonInput);
      setInput(parsed);
      setJsonInput(serializeInput(parsed));
      setJsonError("");
    } catch (error) {
      setJsonError(errorMessage(error));
    }
  };

  return {
    input,
    jsonInput,
    jsonError,
    inputMode,
    formBusy,
    validating,
    validationIssues,
    formRevision,
    setFormBusy,
    setFormValid,
    validateForRun,
    reset,
    changeJsonInput,
    changeFormInput,
    changeInputMode,
    formatJsonInput,
  };
}

export type PlaygroundInputState = ReturnType<typeof usePlaygroundInput>;
