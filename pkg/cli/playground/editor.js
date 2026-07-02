// Thin wrapper over the vendored Ace global (window.ace) for JSON editing.
// Editors are tracked so their theme can follow the light/dark toggle.

const EDITORS = new Set();

// Ace theme colors are sourced from the Kumo design tokens (vendor/kumo.css) so
// the editor matches the rest of the UI and recolors with the data-mode toggle.
// We register two themes (light/dark) that share the same token-driven CSS; they
// differ only in `isDark`, which Ace uses for cursor/selection contrast. JSON
// token classes come from vendor/ace/mode-json.js: object keys -> `variable`,
// string values -> `string`, numbers -> `constant.numeric`, booleans ->
// `constant.language`, brackets -> `paren`, commas -> `punctuation.operator`.
function kumoThemeCss(cssClass) {
  const s = `.${cssClass}`;
  return `
${s} { background-color: var(--color-kumo-base); color: var(--text-color-kumo-default); }
${s} .ace_gutter { background: var(--color-kumo-elevated); color: var(--text-color-kumo-subtle); }
${s} .ace_print-margin { width: 1px; background: var(--color-kumo-hairline); }
${s} .ace_cursor { color: var(--text-color-kumo-default); }
${s} .ace_marker-layer .ace_selection { background: var(--color-kumo-info-tint); }
${s} .ace_marker-layer .ace_active-line { background: color-mix(in srgb, var(--color-kumo-fill) 45%, transparent); }
${s} .ace_gutter-active-line { background-color: color-mix(in srgb, var(--color-kumo-fill) 45%, transparent); }
${s} .ace_marker-layer .ace_selected-word { border: 1px solid var(--color-kumo-line); }
${s} .ace_fold { background-color: var(--color-kumo-brand); border-color: var(--text-color-kumo-default); }
${s} .ace_variable { color: var(--text-color-kumo-link); }
${s} .ace_string { color: var(--text-color-kumo-success); }
${s} .ace_constant.ace_numeric { color: var(--text-color-kumo-warning); }
${s} .ace_constant.ace_language { color: var(--text-color-kumo-brand); }
${s} .ace_constant.ace_language.ace_escape { color: var(--text-color-kumo-info); }
${s} .ace_paren, ${s} .ace_punctuation { color: var(--text-color-kumo-subtle); }
`;
}

function defineKumoTheme(id, cssClass, isDark) {
  const cssText = kumoThemeCss(cssClass);
  ace.define("ace/theme/" + id, ["require", "exports", "module", "ace/lib/dom"], function (require, exports) {
    exports.isDark = isDark;
    exports.cssClass = cssClass;
    exports.cssText = cssText;
    require("ace/lib/dom").importCssString(cssText, cssClass, false);
  });
}

defineKumoTheme("kumo-light", "ace-kumo-light", false);
defineKumoTheme("kumo-dark", "ace-kumo-dark", true);

function aceTheme() {
  return document.documentElement.dataset.mode === "light"
    ? "ace/theme/kumo-light"
    : "ace/theme/kumo-dark";
}

// createJSONEditor turns a host element into an Ace JSON editor. Read-only mode
// is used for outputs (no cursor, not editable) but still allows code folding.
export function createJSONEditor(host, opts = {}) {
  // autosize editors grow with content (good for small inline fields).
  // Fixed-height editors take their height from CSS and scroll internally,
  // which is required for drag-selection autoscroll to work on large content.
  const { value = "", readOnly = false, autosize = true, minLines = 4, maxLines = 30 } = opts;
  const editor = ace.edit(host);
  editor.session.setMode("ace/mode/json");
  editor.setTheme(aceTheme());
  const options = {
    readOnly,
    fontSize: "12px",
    showPrintMargin: false,
    useWorker: false, // we validate JSON ourselves; avoids loading a worker file
    highlightActiveLine: !readOnly,
    highlightGutterLine: !readOnly,
    showFoldWidgets: true,
    fadeFoldWidgets: false,
    tabSize: 2,
    useSoftTabs: true,
    wrap: true,
  };
  if (autosize) {
    options.minLines = minLines;
    options.maxLines = maxLines;
  }
  editor.setOptions(options);
  editor.setValue(value, -1);
  if (readOnly) {
    editor.setHighlightActiveLine(false);
    try {
      editor.renderer.$cursorLayer.element.style.display = "none";
    } catch {
      /* cursor layer not ready; ignore */
    }
  }
  addCopyButton(host, editor);
  EDITORS.add(editor);
  return editor;
}

// addCopyButton overlays a Copy button that copies the whole document. This is
// far easier than drag-selecting large content, which scrolls awkwardly.
function addCopyButton(host, editor) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "editor-copy";
  button.textContent = "Copy";
  button.addEventListener("click", async (event) => {
    event.preventDefault();
    event.stopPropagation();
    try {
      await navigator.clipboard.writeText(editor.getValue());
      button.textContent = "Copied";
      setTimeout(() => {
        button.textContent = "Copy";
      }, 1200);
    } catch {
      // Clipboard API unavailable: fall back to selecting all so Cmd/Ctrl+C works.
      editor.selectAll();
      editor.focus();
    }
  });
  host.appendChild(button);
}

export function destroyEditor(editor) {
  if (!editor) return;
  EDITORS.delete(editor);
  editor.destroy();
}

export function refreshEditorThemes() {
  const theme = aceTheme();
  for (const editor of EDITORS) editor.setTheme(theme);
}

// formatEditor pretty-prints the editor's JSON in place; returns false if the
// content isn't valid JSON.
export function formatEditor(editor) {
  try {
    editor.setValue(JSON.stringify(JSON.parse(editor.getValue()), null, 2), -1);
    return true;
  } catch {
    return false;
  }
}
