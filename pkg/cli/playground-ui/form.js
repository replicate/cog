import { el, clear } from "./dom.js";
import { fieldKind, orderedInputs, coerceEnum } from "./schema.js";
import { fileToDataURI, formatBytes } from "./api.js";
import { mediaNode } from "./media.js";
import { createJSONEditor, destroyEditor } from "./editor.js";

// Ace editors created for object/dict fields in the current form, destroyed and
// rebuilt whenever the form is rebuilt.
let activeEditors = [];

// buildForm renders the Input fields into `container` and returns a handle with
// collect(), which reads the current values on demand. There is no reactive
// state: inputs are built once and queried when the user runs a prediction.
export function buildForm(container, root, inputSchema, value = {}) {
  for (const editor of activeEditors) destroyEditor(editor);
  activeEditors = [];
  clear(container);
  const inputs = orderedInputs(inputSchema);
  if (inputs.length === 0) {
    container.append(el("p", { class: "muted", text: "This model takes no inputs." }));
    return { collect: () => ({}) };
  }

  const fields = [];
  for (const { name, prop, required } of inputs) {
    const field = buildField(root, name, prop, required, value[name]);
    container.append(field.element);
    fields.push({ name, read: field.read, included: field.included });
  }
  // Editors must be resized once their hosts are attached to the document.
  for (const editor of activeEditors) editor.resize();

  return {
    // collect includes required fields always and optional fields only when
    // their include checkbox is ticked (ticking happens automatically when the
    // field is edited).
    collect() {
      const out = {};
      for (const { name, included, read } of fields) {
        if (included()) out[name] = read();
      }
      return out;
    },
  };
}

// buildField renders one labelled field and returns its value reader plus an
// `included` predicate. Optional fields get an include checkbox so they can be
// omitted from the request; it auto-ticks when the field is edited.
function buildField(root, name, prop, required, initial) {
  const kind = fieldKind(root, prop);
  const widget = buildWidget(root, kind, initial);

  const label = el("label");
  let includeBox = null;
  if (!required) {
    includeBox = el("input", {
      type: "checkbox",
      class: "include-box",
      checked: initial !== undefined,
      title: "Include this optional field in the request",
    });
    label.append(includeBox);
    const touch = () => {
      includeBox.checked = true;
    };
    widget.element.addEventListener("input", touch);
    widget.element.addEventListener("change", touch);
    if (widget.onChange) widget.onChange(touch);
  }
  label.append(name);
  if (required) label.append(el("span", { class: "req", text: " *" }));
  if (kind.prop.deprecated) {
    label.append(el("span", { class: "deprecated-tag", text: " (deprecated)" }));
  }

  const field = el("div", { class: "field" }, label);
  if (kind.prop.description) {
    field.append(el("small", { class: "desc", text: kind.prop.description }));
  }
  const hint = constraintText(kind.prop);
  if (hint) field.append(el("small", { class: "constraint", text: hint }));
  field.append(widget.element);

  return {
    element: field,
    read: widget.read,
    included: () => required || includeBox.checked,
  };
}

// constraintText summarizes the numeric/string constraints emitted in the
// schema (minimum/maximum, minLength/maxLength, pattern) for display.
function constraintText(prop) {
  const parts = [];
  if (prop.minimum !== undefined && prop.maximum !== undefined) {
    parts.push(`${prop.minimum}–${prop.maximum}`);
  } else if (prop.minimum !== undefined) {
    parts.push(`min ${prop.minimum}`);
  } else if (prop.maximum !== undefined) {
    parts.push(`max ${prop.maximum}`);
  }
  if (prop.minLength !== undefined && prop.maxLength !== undefined) {
    parts.push(`${prop.minLength}–${prop.maxLength} chars`);
  } else if (prop.minLength !== undefined) {
    parts.push(`min ${prop.minLength} chars`);
  } else if (prop.maxLength !== undefined) {
    parts.push(`max ${prop.maxLength} chars`);
  }
  if (prop.pattern) parts.push(`pattern: ${prop.pattern}`);
  return parts.join(" · ");
}

// buildWidget maps a field kind to a DOM widget + value reader. Reused for both
// top-level fields and array items.
function buildWidget(root, kind, initial) {
  switch (kind.kind) {
    case "enum":
      return enumWidget(kind.choices, kind.prop, initial);
    case "file":
      return fileWidget(initial);
    case "secret":
      return textWidget("password", initial ?? kind.prop.default);
    case "string":
      return textareaWidget(initial ?? kind.prop.default);
    case "integer":
      return numberWidget(kind.prop, true, initial);
    case "number":
      return numberWidget(kind.prop, false, initial);
    case "boolean":
      return booleanWidget(initial ?? kind.prop.default);
    case "array":
      return arrayWidget(root, kind.items, initial);
    default:
      return objectWidget(kind.prop, initial);
  }
}

function textWidget(type, initial) {
  const input = el("input", { type, value: initial ?? "" });
  return { element: input, read: () => input.value };
}

function textareaWidget(initial) {
  const input = el("textarea", { rows: "2", value: initial ?? "" });
  return { element: input, read: () => input.value };
}

function numberWidget(prop, isInt, initial) {
  const input = el("input", {
    type: "number",
    value: initial ?? prop.default ?? "",
    min: prop.minimum,
    max: prop.maximum,
    step: isInt ? "1" : "any",
  });
  return {
    element: input,
    read: () => {
      if (input.value === "") return "";
      return isInt ? parseInt(input.value, 10) : parseFloat(input.value);
    },
  };
}

// Booleans render as a true/false select rather than a checkbox so they don't
// collide visually with the optional-field include checkbox.
function booleanWidget(initial) {
  const select = el("select");
  const current = initial === true;
  for (const option of [true, false]) {
    const opt = el("option", { value: String(option), text: String(option) });
    if (option === current) opt.selected = true;
    select.append(opt);
  }
  return { element: select, read: () => select.value === "true" };
}

function enumWidget(choices, prop, initial) {
  const current = initial ?? prop.default;
  const select = el("select");
  if (current === undefined || current === null) {
    select.append(el("option", { value: "", text: "— select —" }));
  }
  for (const choice of choices) {
    const option = el("option", { value: String(choice), text: String(choice) });
    if (choice === current) option.selected = true;
    select.append(option);
  }
  return { element: select, read: () => coerceEnum(choices, select.value) };
}

// fileWidget: upload a file (-> data: URI) OR paste a URL. Mutually exclusive;
// reads as a single string value that round-trips into the JSON editor. Shows
// an inline preview for image/audio/video so you can confirm the input.
function fileWidget(initial) {
  let currentValue = typeof initial === "string" ? initial : "";

  const fileInput = el("input", { type: "file" });
  const fileName = el("span", { class: "file-name" });
  const urlInput = el("input", {
    type: "text",
    class: "url-input",
    placeholder: "https://...",
    value: currentValue,
  });
  const preview = el("div", { class: "input-preview" });

  function updatePreview() {
    clear(preview);
    const node = mediaNode(currentValue);
    if (node) preview.append(node);
  }

  fileInput.addEventListener("change", async () => {
    const file = fileInput.files[0];
    if (!file) return;
    currentValue = await fileToDataURI(file);
    urlInput.value = "";
    fileName.textContent = `${file.name} (${formatBytes(file.size)})`;
    updatePreview();
  });

  urlInput.addEventListener("input", () => {
    currentValue = urlInput.value;
    fileInput.value = "";
    fileName.textContent = "";
    updatePreview();
  });

  const controls = el(
    "div",
    { class: "file-widget" },
    fileInput,
    fileName,
    el("span", { class: "muted", text: "or URL" }),
    urlInput,
  );
  const element = el("div", {}, controls, preview);
  updatePreview();
  return { element, read: () => currentValue };
}

// Object/dict/Any inputs are edited as JSON in a code editor (folding + syntax
// highlighting) instead of a bare textarea.
function objectWidget(prop, initial) {
  const text =
    initial !== undefined
      ? typeof initial === "string"
        ? initial
        : JSON.stringify(initial, null, 2)
      : prop.default !== undefined && prop.default !== null
        ? JSON.stringify(prop.default, null, 2)
        : "";

  const host = el("div", { class: "ace-json ace-field" });
  const error = el("small", { class: "field-error" });
  const element = el("div", {}, host, error);
  const editor = createJSONEditor(host, { value: text, autosize: false });
  activeEditors.push(editor);

  return {
    element,
    onChange: (cb) => editor.on("change", cb),
    read: () => {
      const raw = editor.getValue().trim();
      if (raw === "") {
        error.textContent = "";
        return "";
      }
      try {
        const parsed = JSON.parse(raw);
        error.textContent = "";
        return parsed;
      } catch (err) {
        error.textContent = "Invalid JSON: " + err.message;
        return "";
      }
    },
  };
}

// arrayWidget renders a growable list of item widgets.
function arrayWidget(root, items, initial) {
  const rows = el("div");
  const itemKind = fieldKind(root, items);
  const readers = [];

  function addRow(value) {
    const widget = buildWidget(root, itemKind, value);
    const reader = widget.read;
    readers.push(reader);

    const remove = el("button", {
      type: "button",
      class: "ghost-btn danger",
      text: "−",
      onclick: () => {
        row.remove();
        const idx = readers.indexOf(reader);
        if (idx >= 0) readers.splice(idx, 1);
      },
    });
    const row = el("div", { class: "array-row" }, widget.element, remove);
    rows.append(row);
  }

  const addBtn = el("button", {
    type: "button",
    class: "ghost-btn",
    text: "+ Add",
    onclick: () => addRow(undefined),
  });

  const initialItems = Array.isArray(initial) ? initial : [];
  for (const v of initialItems) addRow(v);
  if (readers.length === 0) addRow(undefined);

  const element = el("div", { class: "array-input" }, rows, addBtn);
  return {
    element,
    read: () =>
      readers
        .map((r) => r())
        .filter((v) => v !== "" && v !== null && v !== undefined),
  };
}
