import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { json } from "@codemirror/lang-json";
import { bracketMatching, HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { Compartment, EditorState, Transaction } from "@codemirror/state";
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
import { useEffect, useRef } from "react";

export type JsonEditorProps = {
  value: string;
  onChange?: (value: string) => void;
  readOnly?: boolean;
  followTail?: boolean;
  className?: string;
  label: string;
  autoHeight?: boolean;
};

export function JsonEditor({
  value,
  onChange,
  readOnly = false,
  followTail = false,
  className = "",
  label,
  autoHeight = false,
}: JsonEditorProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const editorRef = useRef<EditorView | undefined>(undefined);
  const initialValue = useRef(value);
  const initialLabel = useRef(label);
  const initialReadOnly = useRef(readOnly);
  const onChangeRef = useRef(onChange);
  const updatingValue = useRef(false);
  const followRef = useRef(true);
  const behavior = useRef(new Compartment());
  onChangeRef.current = onChange;

  useEffect(() => {
    if (!hostRef.current) return;
    const editor = new EditorView({
      parent: hostRef.current,
      state: EditorState.create({
        doc: initialValue.current,
        extensions: [
          lineNumbers(),
          highlightSpecialChars(),
          history(),
          drawSelection(),
          bracketMatching(),
          highlightActiveLine(),
          highlightActiveLineGutter(),
          keymap.of([...defaultKeymap, ...historyKeymap]),
          json(),
          syntaxHighlighting(kumoHighlightStyle),
          kumoEditorTheme,
          EditorView.lineWrapping,
          behavior.current.of(editorBehavior(initialReadOnly.current, initialLabel.current)),
          EditorView.updateListener.of((update) => {
            if (update.docChanged && !updatingValue.current) {
              onChangeRef.current?.(update.state.doc.toString());
            }
          }),
        ],
      }),
    });
    editorRef.current = editor;
    const updateFollow = () => {
      const { clientHeight, scrollHeight, scrollTop } = editor.scrollDOM;
      followRef.current = scrollHeight - scrollTop - clientHeight < 48;
    };
    editor.scrollDOM.addEventListener("scroll", updateFollow, { passive: true });
    return () => {
      editor.scrollDOM.removeEventListener("scroll", updateFollow);
      editor.destroy();
      editorRef.current = undefined;
    };
  }, []);

  useEffect(() => {
    editorRef.current?.dispatch({
      effects: behavior.current.reconfigure(editorBehavior(readOnly, label)),
    });
  }, [label, readOnly]);

  useEffect(() => {
    const editor = editorRef.current;
    if (!editor) return;
    const current = editor.state.doc.toString();
    const scroll = followTail && followRef.current;
    if (current !== value) {
      updatingValue.current = true;
      editor.dispatch({
        changes: { from: 0, to: current.length, insert: value },
        annotations: Transaction.addToHistory.of(false),
        selection: readOnly
          ? undefined
          : { anchor: Math.min(editor.state.selection.main.head, value.length) },
        effects: scroll ? EditorView.scrollIntoView(value.length, { y: "end" }) : undefined,
      });
      updatingValue.current = false;
    } else if (scroll) {
      editor.dispatch({ effects: EditorView.scrollIntoView(value.length, { y: "end" }) });
    }
  }, [followTail, readOnly, value]);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(editorRef.current?.state.doc.toString() ?? value);
    } catch {
      const editor = editorRef.current;
      if (!editor) return;
      editor.dispatch({ selection: { anchor: 0, head: editor.state.doc.length } });
      editor.focus();
    }
  };

  return (
    <div
      className={`json-editor ${className}`}
      style={autoHeight ? { height: `${editorHeight(value)}px` } : undefined}
    >
      <div ref={hostRef} />
      <button type="button" className="editor-copy" onClick={() => void copy()}>
        Copy
      </button>
    </div>
  );
}

function editorHeight(value: string): number {
  return Math.min(320, Math.max(80, value.split("\n").length * 18 + 18));
}

function editorBehavior(readOnly: boolean, label: string) {
  return [
    EditorState.readOnly.of(readOnly),
    EditorView.editable.of(!readOnly),
    EditorView.contentAttributes.of({
      "aria-label": label,
      "aria-readonly": String(readOnly),
      spellcheck: "false",
    }),
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
