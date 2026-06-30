// Thin wrapper over the vendored Ace global (window.ace) for JSON editing.
// Editors are tracked so their theme can follow the light/dark toggle.

const EDITORS = new Set();

function aceTheme() {
  return document.documentElement.dataset.theme === "light"
    ? "ace/theme/chrome"
    : "ace/theme/tomorrow_night";
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
