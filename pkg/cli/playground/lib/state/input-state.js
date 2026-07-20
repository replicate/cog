// @ts-check

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
  constructor() {
    super();
    this.target = "";
    this.document = undefined;
    this.capabilities = undefined;
    /** @type {import("../../types").JsonObject} */ this.input = {};
    this.jsonInput = "{}";
    this.jsonError = "";
    /** @type {"form"|"json"} */ this.mode = "form";
    this.busyFields = new Set();
    this.invalidFields = new Set();
    this.validating = false;
    /** @type {import("../../types").ValidationIssue[]} */ this.issues = [];
    this.schemaId = 0;
    this.validationAttempt = 0;
    this.revision = 0;
  }
  get formBusy() {
    return this.busyFields.size > 0;
  }
  get formValid() {
    return this.invalidFields.size === 0;
  }
  /** @param {string} target @param {import("../../types").OpenAPIDocument | undefined} document @param {import("../../types").PlaygroundCapabilities | undefined} capabilities */
  connect(target, document, capabilities) {
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
  clearValidation() {
    this.validationAttempt += 1;
    this.validating = false;
    this.issues = [];
  }
  /** @param {import("../../types").JsonObject} value */
  changeForm(value) {
    this.clearValidation();
    this.input = value;
    this.jsonInput = serializeInput(value);
    this.jsonError = "";
    this.emit();
  }
  /** @param {string} value */
  changeJSON(value) {
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
  /** @param {"form" | "json"} mode */
  changeMode(mode) {
    if (mode === this.mode) return;
    this.clearValidation();
    this.jsonInput = serializeInput(this.input);
    this.jsonError = "";
    this.busyFields.clear();
    this.invalidFields.clear();
    this.mode = mode;
    this.emit();
  }
  formatJSON() {
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
  reset() {
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
  /** @returns {Promise<import("../../types").JsonObject | undefined>} */
  async validateForRun() {
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
  /** @param {string} name @param {boolean} busy */
  setFieldBusy(name, busy) {
    const changed = busy ? !this.busyFields.has(name) : this.busyFields.has(name);
    if (!changed) return;
    if (busy) this.busyFields.add(name);
    else this.busyFields.delete(name);
    this.emit();
  }
  /** Clears a pending local file read when its control is unmounted. @param {string} name */
  clearFieldBusy(name) {
    return this.busyFields.delete(name);
  }
  /** @param {string} name @param {boolean} valid */
  setFieldValid(name, valid) {
    const changed = valid ? this.invalidFields.has(name) : !this.invalidFields.has(name);
    if (!changed) return;
    if (valid) this.invalidFields.delete(name);
    else this.invalidFields.add(name);
    this.emit();
  }
  /** Clears local control state when a field is unmounted. @param {string} name */
  clearField(name) {
    this.busyFields.delete(name);
    this.invalidFields.delete(name);
  }
  destroy() {
    this.validationAttempt += 1;
    disposeValidationWorker();
  }
}
