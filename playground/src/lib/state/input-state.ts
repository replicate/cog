import type {
  InputMode,
  JsonObject,
  OpenAPIDocument,
  PlaygroundCapabilities,
  ValidationIssue,
} from "../../types.js";

import { defaultInput } from "../input/openapi.js";
import {
  errorMessage,
  isJsonObject,
  isJsonValue,
  parseInputObject,
  serializeInput,
} from "../json.js";
import { disposeValidationWorker, validateInput } from "../input/validation-client.js";
import { Store } from "./store.js";

export class InputState extends Store {
  target: string;
  document: OpenAPIDocument | undefined;
  capabilities: PlaygroundCapabilities | undefined;
  input: JsonObject;
  jsonInput: string;
  jsonError: string;
  mode: InputMode;
  busyFields: Set<string>;
  invalidFields: Set<string>;
  validating: boolean;
  issues: ValidationIssue[];
  schemaId: number;
  validationAttempt: number;
  revision: number;

  constructor() {
    super();
    this.target = "";
    this.document = undefined;
    this.capabilities = undefined;
    this.input = {};
    this.jsonInput = "{}";
    this.jsonError = "";
    this.mode = "form";
    this.busyFields = new Set();
    this.invalidFields = new Set();
    this.validating = false;
    this.issues = [];
    this.schemaId = 0;
    this.validationAttempt = 0;
    this.revision = 0;
  }

  get formBusy(): boolean {
    return this.busyFields.size > 0;
  }

  get formValid(): boolean {
    return this.invalidFields.size === 0;
  }

  connect(
    target: string,
    document: OpenAPIDocument | undefined,
    capabilities: PlaygroundCapabilities | undefined,
  ): void {
    if (target === this.target && document === this.document && capabilities === this.capabilities)
      return;
    this.target = target;
    this.document = document;
    this.capabilities = capabilities;
    this.clearValidation();
    if (!document || !capabilities) {
      this.busyFields.clear();
      this.invalidFields.clear();
      this.emit();
      return;
    }
    this.schemaId += 1;
    const defaults = defaultInput(document, capabilities.input);
    this.input = defaults;
    this.jsonInput = serializeInput(defaults);
    this.jsonError = "";
    this.busyFields.clear();
    this.invalidFields.clear();
    this.revision += 1;
    this.emit();
  }

  clearValidation(): void {
    this.validationAttempt += 1;
    this.validating = false;
    this.issues = [];
  }

  changeForm(value: JsonObject): void {
    this.clearValidation();
    this.input = value;
    this.jsonInput = serializeInput(value);
    this.jsonError = "";
    this.emit();
  }

  changeJSON(value: string): void {
    this.clearValidation();
    this.jsonInput = value;
    try {
      this.input = parseInputObject(value);
      this.jsonError = "";
    } catch (error) {
      this.jsonError = errorMessage(error);
    }
    this.emit();
  }

  changeMode(mode: InputMode): void {
    if (mode === this.mode) return;
    this.clearValidation();
    this.jsonInput = serializeInput(this.input);
    this.jsonError = "";
    this.busyFields.clear();
    this.invalidFields.clear();
    this.mode = mode;
    this.emit();
  }

  formatJSON(): void {
    this.clearValidation();
    try {
      this.input = parseInputObject(this.jsonInput);
      this.jsonInput = serializeInput(this.input);
      this.jsonError = "";
    } catch (error) {
      this.jsonError = errorMessage(error);
    }
    this.emit();
  }

  reset(): void {
    if (!this.document || !this.capabilities) return;
    const defaults = defaultInput(this.document, this.capabilities.input);
    this.input = defaults;
    this.jsonInput = serializeInput(defaults);
    this.jsonError = "";
    this.busyFields.clear();
    this.invalidFields.clear();
    this.revision += 1;
    this.clearValidation();
    this.emit();
  }

  async validateForRun(): Promise<JsonObject | undefined> {
    if (!this.document || !this.capabilities || this.formBusy) return undefined;
    let input = this.input;
    if (this.mode === "json") {
      try {
        input = parseInputObject(this.jsonInput);
        this.jsonError = "";
      } catch (error) {
        this.jsonError = errorMessage(error);
        this.emit();
        return undefined;
      }
    }
    if (this.mode === "form" && !this.formValid) return undefined;
    const attempt = ++this.validationAttempt;
    const target = this.target;
    const document = this.document;
    const capabilities = this.capabilities;
    this.issues = [];
    this.validating = true;
    this.emit();
    const issues = await validateInput(this.schemaId, document, capabilities.input, input);
    if (
      attempt !== this.validationAttempt ||
      target !== this.target ||
      document !== this.document ||
      capabilities !== this.capabilities
    )
      return undefined;
    this.validating = false;
    if (issues.length) {
      this.issues = issues;
      this.emit();
      return undefined;
    }
    if (!isJsonObject(input) || !isJsonValue(input)) {
      this.issues = [
        { keyword: "type", message: "Input must contain only JSON values.", path: "input" },
      ];
      this.emit();
      return undefined;
    }
    this.emit();
    return input;
  }

  setFieldBusy(name: string, busy: boolean): void {
    const wasBusy = this.busyFields.has(name);
    if (busy === wasBusy) return;
    if (busy) this.busyFields.add(name);
    else this.busyFields.delete(name);
    this.emit();
  }

  clearFieldBusy(name: string): boolean {
    return this.busyFields.delete(name);
  }

  setFieldValid(name: string, valid: boolean): void {
    const wasValid = !this.invalidFields.has(name);
    if (valid === wasValid) return;
    if (valid) this.invalidFields.delete(name);
    else this.invalidFields.add(name);
    this.emit();
  }

  clearField(name: string): void {
    this.busyFields.delete(name);
    this.invalidFields.delete(name);
  }

  destroy(): void {
    this.validationAttempt += 1;
    disposeValidationWorker();
  }
}
