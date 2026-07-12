import { Compartment, EditorState, Transaction } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { useEffect, useRef } from "react";

import { editorBehavior, jsonEditorExtensions } from "@/components/editor/config";

export type JsonEditorProps = {
  value: string;
  onChange?: (value: string) => void;
  readOnly?: boolean;
  disabled?: boolean;
  invalid?: boolean;
  describedBy?: string;
  followTail?: boolean;
  className?: string;
  label: string;
  autoHeight?: boolean;
};

export function JsonEditor({
  value,
  onChange,
  readOnly = false,
  disabled = false,
  invalid = false,
  describedBy,
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
  const initialDisabled = useRef(disabled);
  const initialInvalid = useRef(invalid);
  const initialDescribedBy = useRef(describedBy);
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
          ...jsonEditorExtensions(),
          behavior.current.of(
            editorBehavior(
              initialReadOnly.current,
              initialDisabled.current,
              initialInvalid.current,
              initialLabel.current,
              initialDescribedBy.current,
            ),
          ),
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
      effects: behavior.current.reconfigure(
        editorBehavior(readOnly, disabled, invalid, label, describedBy),
      ),
    });
  }, [describedBy, disabled, invalid, label, readOnly]);

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
      <button
        type="button"
        className="editor-copy"
        aria-label={`Copy ${label}`}
        disabled={disabled}
        onClick={() => void copy()}
      >
        Copy
      </button>
    </div>
  );
}

function editorHeight(value: string): number {
  return Math.min(320, Math.max(80, value.split("\n").length * 18 + 18));
}
