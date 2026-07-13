import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { json } from "@codemirror/lang-json";
import {
  bracketMatching,
  foldGutter,
  foldKeymap,
  HighlightStyle,
  syntaxHighlighting,
} from "@codemirror/language";
import { EditorState, type Extension } from "@codemirror/state";
import {
  drawSelection,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
} from "@codemirror/view";
import { tags } from "@lezer/highlight";

/** Builds the shared JSON language, editing, highlighting, and theme extensions. */
export function jsonEditorExtensions(): Extension[] {
  return [
    lineNumbers(),
    foldGutter(),
    highlightSpecialChars(),
    history(),
    drawSelection(),
    bracketMatching(),
    highlightActiveLine(),
    highlightActiveLineGutter(),
    keymap.of([...defaultKeymap, ...historyKeymap, ...foldKeymap]),
    json(),
    syntaxHighlighting(kumoHighlightStyle),
    kumoEditorTheme,
    EditorView.lineWrapping,
  ];
}

/** Maps editor mode and accessibility state into reconfigurable CodeMirror extensions. */
export function editorBehavior(
  readOnly: boolean,
  disabled: boolean,
  invalid: boolean,
  label: string,
  describedBy?: string,
): Extension[] {
  const attributes: Record<string, string> = {
    "aria-label": label,
    "aria-readonly": String(readOnly || disabled),
    spellcheck: "false",
  };
  if (describedBy) attributes["aria-describedby"] = describedBy;
  if (invalid) attributes["aria-invalid"] = "true";
  if (disabled) {
    attributes["aria-disabled"] = "true";
    attributes.tabindex = "-1";
  } else if (readOnly) {
    attributes.tabindex = "0";
  }
  return [
    EditorState.readOnly.of(readOnly || disabled),
    EditorView.editable.of(!readOnly && !disabled),
    EditorView.contentAttributes.of(attributes),
  ];
}

const kumoEditorTheme = EditorView.theme({
  "&": {
    height: "100%",
    backgroundColor: "var(--color-kumo-base)",
    color: "var(--text-color-kumo-default)",
  },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": {
    fontFamily: '"SF Mono", Monaco, Consolas, "Liberation Mono", monospace',
    fontSize: "12px",
    lineHeight: "1.5",
  },
  ".cm-content": { caretColor: "var(--text-color-kumo-default)", padding: "8px 0" },
  ".cm-line": { padding: "0 10px" },
  ".cm-gutters": {
    backgroundColor: "var(--color-kumo-elevated)",
    borderRight: "1px solid var(--color-kumo-hairline)",
    color: "var(--text-color-kumo-subtle)",
  },
  ".cm-activeLine, .cm-activeLineGutter": {
    backgroundColor: "color-mix(in srgb, var(--color-kumo-fill) 45%, transparent)",
  },
  ".cm-selectionBackground, &.cm-focused .cm-selectionBackground": {
    backgroundColor: "var(--color-kumo-info-tint) !important",
  },
  ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--text-color-kumo-default)" },
});

const kumoHighlightStyle = HighlightStyle.define([
  { tag: tags.propertyName, color: "var(--text-color-kumo-link)" },
  { tag: [tags.string, tags.special(tags.string)], color: "var(--text-color-kumo-success)" },
  { tag: tags.number, color: "var(--text-color-kumo-warning)" },
  { tag: [tags.bool, tags.null], color: "var(--text-color-kumo-brand)" },
  { tag: tags.escape, color: "var(--text-color-kumo-info)" },
  {
    tag: [tags.brace, tags.squareBracket, tags.separator],
    color: "var(--text-color-kumo-subtle)",
  },
]);
