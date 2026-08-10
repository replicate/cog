import type { ConnectionState } from "../lib/state/connection-state.js";
import type { InputState } from "../lib/state/input-state.js";
import type {
  InputMode,
  JsonValue,
  OpenAPIDocument,
  OpenAPISchema,
  ValidationIssue,
} from "../types.js";
import { fileToDataURI, FileReadGuard } from "../lib/input/files.js";
import { dataMediaKind, emptyInputValue, formatBytes, isDataURI } from "../lib/input/input.js";
import {
  constraintText,
  effectiveSchema,
  enumValues,
  initialInputValue,
  orderedProperties,
} from "../lib/input/openapi.js";
import { JsonEditor } from "./json-editor.js";
import { isJsonValue } from "../lib/json.js";
import {
  element,
  replaceChildrenPreservingFocus,
  segmentedTabs,
  tabId,
  tabPanelId,
} from "./dom.js";

export class InputView {
  root: HTMLElement;
  state: InputState;
  connection: ConnectionState;
  editors: JsonEditor[];
  jsonEditor: JsonEditor | undefined;
  lastMode: InputMode | "";
  lastRevision: number;
  lastDocument: OpenAPIDocument | undefined;
  lastIssues: string;
  cleanups: Array<() => void>;
  structuredDrafts: Map<string, { raw: string; error: string }>;
  fileDrafts: Map<string, { error: string; fileName: string }>;
  fieldEditors: Map<string, { editor: JsonEditor; described: string | undefined; errorId: string }>;
  unsubscribe: () => void;

  constructor(root: HTMLElement, state: InputState, connection: ConnectionState) {
    this.root = root;
    this.state = state;
    this.connection = connection;
    this.editors = [];
    this.lastMode = "";
    this.lastRevision = -1;
    this.lastDocument = undefined;
    this.lastIssues = "";
    this.cleanups = [];
    this.structuredDrafts = new Map();
    this.fileDrafts = new Map();
    this.fieldEditors = new Map();
    this.unsubscribe = state.subscribe(() => this.update());
    this.render();
  }

  inputShapeChanged(): boolean {
    return (
      this.lastMode !== this.state.mode ||
      this.lastRevision !== this.state.revision ||
      this.lastDocument !== this.state.document
    );
  }

  update(): void {
    const issues = JSON.stringify(this.state.issues);
    if (this.inputShapeChanged()) {
      this.render();
      return;
    }

    if (this.lastIssues !== issues) this.updateIssues();
    this.jsonEditor?.update({
      value: this.state.jsonInput,
      invalid: Boolean(this.state.jsonError) || Boolean(this.state.issues.length),
      describedBy: this.describedBy(),
    });
    const error = this.root.querySelector("#json-input-error");
    if (error instanceof HTMLElement) {
      error.textContent = this.state.jsonError;
      error.hidden = !this.state.jsonError;
    }
  }

  updateConnection(): void {
    const notice = this.root.querySelector("#input-schema-notice");
    if (!(notice instanceof HTMLOutputElement)) return;
    notice.textContent = this.connection.schemaError;
    notice.hidden = !this.connection.schemaError;
  }

  render(): void {
    if (this.inputShapeChanged()) {
      this.structuredDrafts.clear();
      this.fileDrafts.clear();
    }

    for (const cleanup of this.cleanups) cleanup();
    this.cleanups = [];
    for (const editor of this.editors) editor.destroy();
    this.editors = [];
    this.fieldEditors.clear();
    this.jsonEditor = undefined;
    this.lastMode = this.state.mode;
    this.lastRevision = this.state.revision;
    this.lastDocument = this.state.document;
    this.lastIssues = JSON.stringify(this.state.issues);
    const section = element("section", { id: "input-panel" });
    const head = element(
      "div",
      { className: "panel-head" },
      element("h2", { text: "Input" }),
      segmentedTabs(
        "input-mode",
        "Input editor mode",
        [
          { value: "form", label: "Form" },
          { value: "json", label: "JSON" },
        ],
        this.state.mode,
        (mode: InputMode) => this.state.changeMode(mode),
      ),
    );
    section.append(head);
    const notice = element("output", {
      id: "input-schema-notice",
      className: "notice",
      text: this.connection.schemaError,
    });
    notice.hidden = !this.connection.schemaError;
    section.append(notice);
    const panel = element("div", {
      id: tabPanelId("input-mode", this.state.mode),
      role: "tabpanel",
      "aria-labelledby": tabId("input-mode", this.state.mode),
      tabindex: "0",
    });
    if (this.state.mode === "form" && this.state.document && this.state.capabilities)
      panel.append(this.renderForm(this.state.document, this.state.capabilities.input));
    if (this.state.mode === "json") panel.append(this.renderJSON());
    panel.append(this.renderValidation());
    section.append(panel);
    replaceChildrenPreservingFocus(this.root, section);
  }

  renderJSON(): HTMLDivElement {
    const wrapper = element("div", { id: "json-container" });
    const editorWrap = element("div", { className: "json-editor json-input" });
    const host = element("div");
    editorWrap.append(
      host,
      element("button", {
        type: "button",
        className: "editor-copy",
        "aria-label": "Copy Prediction input JSON",
        text: "Copy",
        onclick: () => this.jsonEditor?.copy(),
      }),
    );
    this.jsonEditor = new JsonEditor(host, {
      value: this.state.jsonInput,
      label: "Prediction input JSON",
      invalid: Boolean(this.state.jsonError) || Boolean(this.state.issues.length),
      describedBy: this.describedBy(),
      onChange: (value: string) => this.state.changeJSON(value),
    });
    this.editors.push(this.jsonEditor);
    wrapper.append(editorWrap);
    const error = element("small", {
      id: "json-input-error",
      className: "field-error",
      role: "alert",
      text: this.state.jsonError,
    });
    error.hidden = !this.state.jsonError;
    wrapper.append(error);
    wrapper.append(
      element("button", { type: "button", text: "Format", onclick: () => this.state.formatJSON() }),
    );
    return wrapper;
  }

  describedBy(): string | undefined {
    return (
      [
        this.state.jsonError ? "json-input-error" : "",
        this.state.issues.length ? "input-validation-errors" : "",
      ]
        .filter(Boolean)
        .join(" ") || undefined
    );
  }

  renderValidation(): HTMLDivElement {
    const root = element("div", {
      id: "input-validation-errors",
      "aria-live": "polite",
      "aria-atomic": "true",
    });
    if (!this.state.issues.length) return root;
    const list = element("ul", { className: "validation-summary" });
    for (const issue of this.state.issues)
      list.append(element("li", {}, element("code", { text: issue.path }), `: ${issue.message}`));
    root.append(
      element(
        "div",
        { className: "error-container" },
        element("strong", { text: "Input does not match the OpenAPI schema." }),
        list,
      ),
    );
    return root;
  }

  updateIssues(): void {
    this.lastIssues = JSON.stringify(this.state.issues);
    const currentSummary = this.root.querySelector("#input-validation-errors");
    currentSummary?.replaceWith(this.renderValidation());
    this.jsonEditor?.update({
      invalid: Boolean(this.state.jsonError) || Boolean(this.state.issues.length),
      describedBy: this.describedBy(),
    });
    for (const field of this.root.querySelectorAll(".field[data-field-name]")) {
      if (!(field instanceof HTMLElement)) continue;
      const name = field.dataset.fieldName;
      if (!name) continue;
      const errors = this.state.issues.filter((issue) => issue.field === name);
      const validationId = `field-${name}-validation`;
      const existing = field.querySelector(`#${CSS.escape(validationId)}`);
      if (errors.length) {
        const list = element("ul", { id: validationId, className: "field-errors" });
        for (const error of errors) list.append(element("li", { text: error.message }));
        if (existing) existing.replaceWith(list);
        else field.append(list);
      } else existing?.remove();
      const localInvalid = Boolean(
        this.structuredDrafts.get(name)?.error || this.fileDrafts.get(name)?.error,
      );
      for (const control of field.querySelectorAll(
        "input:not([type=checkbox]), textarea, select",
      )) {
        control.setAttribute("aria-invalid", String(localInvalid || errors.length > 0));
        const ids = (control.getAttribute("aria-describedby") ?? "")
          .split(/\s+/)
          .filter((id) => id && id !== validationId);
        if (errors.length) ids.push(validationId);
        if (ids.length) control.setAttribute("aria-describedby", ids.join(" "));
        else control.removeAttribute("aria-describedby");
      }
      const editor = this.fieldEditors.get(name);
      if (editor)
        editor.editor.update({
          invalid: localInvalid || errors.length > 0,
          describedBy:
            [
              editor.described,
              errors.length ? validationId : "",
              this.structuredDrafts.get(name)?.error ? editor.errorId : "",
            ]
              .filter(Boolean)
              .join(" ") || undefined,
        });
    }
  }

  renderForm(document: OpenAPIDocument, schema: OpenAPISchema): HTMLElement {
    const properties: [string, OpenAPISchema][] = orderedProperties(schema).map(([name, value]) => [
      name,
      effectiveSchema(document, value),
    ]);
    if (!properties.length)
      return element("p", { className: "muted", text: "This model takes no inputs." });
    const root = element("div");
    const required = new Set(schema.required ?? []);
    for (const [name, property] of properties)
      root.append(this.renderField(document, schema, name, property, required.has(name)));
    return root;
  }

  renderField(
    document: OpenAPIDocument,
    parent: OpenAPISchema,
    name: string,
    schema: OpenAPISchema,
    required: boolean,
  ): HTMLDivElement {
    const included = required || Object.hasOwn(this.state.input, name);
    const errors = this.state.issues.filter((issue) => issue.field === name);
    const description = [schema.description, constraintText(schema)].filter(Boolean).join(" · ");
    const field = element("div", { className: "field", "data-field-name": name });
    const heading = element("div", { className: "field-heading" });
    if (!required) {
      const checkbox = element("input", {
        id: `include-field-${name}`,
        type: "checkbox",
        "aria-label": `Include optional field ${name}`,
        checked: included,
      });
      checkbox.addEventListener("change", () => {
        const next = { ...this.state.input };
        if (checkbox.checked) {
          const effective = effectiveSchema(document, parent.properties?.[name] ?? {});
          next[name] = initialInputValue(document, effective);
        } else {
          delete next[name];
          this.state.clearField(name);
          this.structuredDrafts.delete(name);
          this.fileDrafts.delete(name);
        }
        this.state.changeForm(next);
        this.render();
      });
      heading.append(checkbox);
    }
    heading.append(
      element(
        "label",
        { htmlFor: `field-${name}` },
        name,
        required ? element("span", { className: "req", text: " *" }) : undefined,
        schema.deprecated
          ? element("span", { className: "deprecated-tag", text: " (deprecated)" })
          : undefined,
      ),
    );
    field.append(heading);
    if (description)
      field.append(
        element("small", { id: `field-${name}-description`, className: "desc", text: description }),
      );
    const controlRoot = element("div", {
      "aria-disabled": String(!included),
      className: included ? "" : "field-disabled",
    });
    if (!included) controlRoot.inert = true;
    controlRoot.append(
      this.renderControl(
        document,
        name,
        schema,
        this.state.input[name],
        required,
        !included,
        errors,
      ),
    );
    field.append(controlRoot);
    if (errors.length) {
      const list = element("ul", { id: `field-${name}-validation`, className: "field-errors" });
      for (const error of errors) list.append(element("li", { text: error.message }));
      field.append(list);
    }
    return field;
  }

  renderControl(
    document: OpenAPIDocument,
    name: string,
    schema: OpenAPISchema,
    value: JsonValue | undefined,
    required: boolean,
    disabled: boolean,
    errors: ValidationIssue[],
  ): HTMLElement {
    const described =
      [
        schema.description || constraintText(schema) ? `field-${name}-description` : "",
        errors.length ? `field-${name}-validation` : "",
      ]
        .filter(Boolean)
        .join(" ") || undefined;
    const set = (next: JsonValue | undefined): void => {
      if (next !== undefined) this.state.changeForm({ ...this.state.input, [name]: next });
    };
    const common = {
      id: `field-${name}`,
      disabled,
      required,
      "aria-describedby": described,
      "aria-invalid": errors.length ? "true" : undefined,
    };
    const choices = enumValues(document, schema);
    if (choices) {
      const select = element("select", common);
      if (!required) select.append(element("option", { value: "-1", text: "Select a value" }));
      choices.forEach((choice, index) =>
        select.append(element("option", { value: String(index), text: String(choice) })),
      );
      select.value = String(choices.findIndex((choice) => Object.is(choice, value)));
      select.addEventListener("change", () => set(choices[Number(select.value)]));
      return select;
    }
    if (schema.type === "string" && schema.format === "uri")
      return this.renderFile(name, schema, value, disabled, errors, set, described);
    if (schema.type === "string") {
      const input =
        schema.format === "password"
          ? element("input", {
              ...common,
              type: "password",
              "aria-label": name,
              value: String(value ?? ""),
            })
          : element("textarea", { ...common, "aria-label": name, text: String(value ?? "") });
      input.addEventListener("input", () => set(input.value));
      return input;
    }
    if (schema.type === "integer" || schema.type === "number") {
      const input = element("input", {
        ...common,
        type: "number",
        "aria-label": name,
        min: schema.minimum,
        max: schema.maximum,
        step: schema.type === "integer" ? "1" : "any",
        value: value === undefined ? "" : String(value),
      });
      input.addEventListener("input", () => set(input.value === "" ? "" : Number(input.value)));
      return input;
    }
    if (schema.type === "boolean") {
      const select = element(
        "select",
        common,
        element("option", { value: "true", text: "true" }),
        element("option", { value: "false", text: "false" }),
      );
      select.value = String(value ?? false);
      select.addEventListener("change", () => set(select.value === "true"));
      return select;
    }
    const root = element("div");
    const wrapper = element("div", { className: "json-editor structured-input" });
    const host = element("div");
    const errorId = `field-${name}-error`;
    let draft = this.structuredDrafts.get(name);
    if (!draft) {
      draft = { raw: JSON.stringify(value ?? emptyInputValue(schema), null, 2), error: "" };
      this.structuredDrafts.set(name, draft);
    }
    const localError = element("small", {
      id: errorId,
      className: "field-error",
      role: "alert",
      text: draft.error,
    });
    localError.hidden = !draft.error;
    const editor = new JsonEditor(host, {
      value: draft.raw,
      label: `${name} JSON`,
      disabled,
      invalid: Boolean(draft.error) || errors.length > 0,
      describedBy: [described, draft.error ? errorId : ""].filter(Boolean).join(" ") || undefined,
      onChange: (text: string) => {
        draft.raw = text;
        try {
          const parsed: unknown = JSON.parse(text);
          if (!isJsonValue(parsed)) throw new Error("Value must be valid JSON");
          set(parsed);
          this.state.setFieldValid(name, true);
          draft.error = "";
          localError.textContent = "";
          localError.hidden = true;
          editor.update({ invalid: errors.length > 0, describedBy: described });
        } catch (error) {
          this.state.setFieldValid(name, false);
          draft.error = `Invalid JSON: ${error instanceof Error ? error.message : "Invalid JSON"}`;
          localError.textContent = draft.error;
          localError.hidden = false;
          editor.update({
            invalid: true,
            describedBy: [described, errorId].filter(Boolean).join(" "),
          });
        }
      },
    });
    this.editors.push(editor);
    this.fieldEditors.set(name, { editor, described, errorId });
    wrapper.append(
      host,
      element("button", {
        type: "button",
        className: "editor-copy",
        "aria-label": `Copy ${name} JSON`,
        text: "Copy",
        disabled,
        onclick: () => editor.copy(),
      }),
    );
    root.append(wrapper, localError);
    return root;
  }

  renderFile(
    name: string,
    schema: OpenAPISchema,
    value: JsonValue | undefined,
    disabled: boolean,
    errors: ValidationIssue[],
    set: (value: JsonValue) => void,
    described: string | undefined,
  ): HTMLDivElement {
    const root = element("div", { className: "file-widget" });
    const reads = new FileReadGuard();
    this.cleanups.push(() => {
      reads.cancel();
      if (this.state.clearFieldBusy(name)) queueMicrotask(() => this.state.emit());
    });
    let draft = this.fileDrafts.get(name);
    if (!draft) {
      draft = { error: "", fileName: "" };
      this.fileDrafts.set(name, draft);
    }
    const acceptValue = schema["x-cog-accept"];
    const accept = Array.isArray(acceptValue)
      ? acceptValue.join(",")
      : typeof acceptValue === "string"
        ? acceptValue
        : undefined;
    const file = element("input", {
      id: `field-${name}`,
      type: "file",
      accept,
      disabled,
      "aria-describedby": described,
      "aria-invalid": errors.length ? "true" : undefined,
    });
    const fileName = element("span", { className: "file-name", text: draft.fileName });
    const errorId = `field-${name}-error`;
    const errorMessage = element("small", { id: errorId, className: "field-error", role: "alert" });
    errorMessage.textContent = draft.error;
    errorMessage.hidden = !draft.error;
    const preview = element("div", { className: "file-preview" });
    const setError = (message: string): void => {
      draft.error = message;
      errorMessage.textContent = message;
      errorMessage.hidden = !message;
      const invalid = Boolean(message) || errors.length > 0;
      file.setAttribute("aria-invalid", String(invalid));
      url.setAttribute("aria-invalid", String(invalid));
      const ids = [described, message ? errorId : ""].filter(Boolean).join(" ");
      if (ids) {
        file.setAttribute("aria-describedby", ids);
        url.setAttribute("aria-describedby", ids);
      } else {
        file.removeAttribute("aria-describedby");
        url.removeAttribute("aria-describedby");
      }
    };
    file.addEventListener("change", async () => {
      const selected = file.files?.[0];
      if (!selected) return;
      const token = reads.begin();
      this.state.setFieldBusy(name, true);
      setError("");
      this.state.emit();
      try {
        const data = await fileToDataURI(selected);
        if (!reads.isCurrent(token)) return;
        set(data);
        this.state.setFieldValid(name, true);
        draft.fileName = `${selected.name} (${formatBytes(selected.size)})`;
        fileName.textContent = draft.fileName;
        preview.replaceChildren(this.mediaPreview(data));
      } catch (error) {
        if (reads.isCurrent(token)) {
          this.state.setFieldValid(name, false);
          draft.fileName = "";
          fileName.textContent = "";
          setError(error instanceof Error ? error.message : "Could not read file");
        }
      } finally {
        if (reads.isCurrent(token)) {
          this.state.setFieldBusy(name, false);
        }
      }
    });
    const url = element("input", {
      "aria-label": `${name} URL`,
      "aria-describedby": described,
      "aria-invalid": errors.length ? "true" : "false",
      disabled,
      placeholder: "https://...",
      value: typeof value === "string" && !isDataURI(value) ? value : "",
    });
    url.addEventListener("input", () => {
      reads.cancel();
      this.state.setFieldBusy(name, false);
      this.state.setFieldValid(name, true);
      draft.fileName = "";
      fileName.textContent = "";
      setError("");
      set(url.value);
      preview.replaceChildren(this.mediaPreview(url.value));
    });
    setError(draft.error);
    root.append(
      file,
      fileName,
      element("span", { className: "muted", text: "or URL" }),
      url,
      errorMessage,
      preview,
    );
    if (typeof value === "string") preview.append(this.mediaPreview(value));
    return root;
  }

  mediaPreview(
    value: string,
  ): DocumentFragment | HTMLImageElement | HTMLAudioElement | HTMLVideoElement {
    if (!isDataURI(value)) return document.createDocumentFragment();
    const kind = dataMediaKind(value);
    if (kind === "image")
      return element("img", {
        className: "input-media",
        src: value,
        alt: "Selected input preview",
      });
    if (kind === "audio") return element("audio", { controls: true, src: value });
    if (kind === "video") return element("video", { controls: true, src: value });
    return document.createDocumentFragment();
  }

  destroy(): void {
    this.unsubscribe();
    for (const cleanup of this.cleanups) cleanup();
    for (const editor of this.editors) editor.destroy();
  }
}
