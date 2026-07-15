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
  active?: boolean;
  className?: string;
  label: string;
  autoHeight?: boolean;
};

/**
 * Renders a controlled CodeMirror JSON editor with accessible read-only and disabled modes,
 * copying, optional content-based sizing, and live-output tail following.
 */
export function JsonEditor({
  value,
  onChange,
  readOnly = false,
  disabled = false,
  invalid = false,
  describedBy,
  followTail = false,
  active = true,
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
  const followEnabled = useRef(followTail);
  const scrollingToTail = useRef(false);
  const scrollRequest = useRef(0);
  const activeRef = useRef(active);
  const followTailRef = useRef(followTail);
  const behavior = useRef(new Compartment());
  onChangeRef.current = onChange;
  activeRef.current = active;
  followTailRef.current = followTail;

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
      if (scrollingToTail.current) return;
      const { clientHeight, scrollHeight, scrollTop } = editor.scrollDOM;
      followRef.current = scrollHeight - scrollTop - clientHeight < 48;
    };
    const stopFollowing = () => {
      followRef.current = false;
      scrollingToTail.current = false;
      scrollRequest.current += 1;
    };
    const stopFollowingFromWheel = (event: WheelEvent) => {
      if (event.deltaY < 0) stopFollowing();
    };
    const stopFollowingFromPointer = (event: PointerEvent) => {
      if (event.target === editor.scrollDOM) stopFollowing();
    };
    const stopFollowingFromKey = (event: KeyboardEvent) => {
      if (["ArrowUp", "Home", "PageUp"].includes(event.key)) stopFollowing();
    };
    editor.scrollDOM.addEventListener("scroll", updateFollow, { passive: true });
    editor.scrollDOM.addEventListener("wheel", stopFollowingFromWheel, { passive: true });
    editor.scrollDOM.addEventListener("touchstart", stopFollowing, { passive: true });
    editor.scrollDOM.addEventListener("pointerdown", stopFollowingFromPointer, { passive: true });
    editor.scrollDOM.addEventListener("keydown", stopFollowingFromKey);
    return () => {
      editor.scrollDOM.removeEventListener("scroll", updateFollow);
      editor.scrollDOM.removeEventListener("wheel", stopFollowingFromWheel);
      editor.scrollDOM.removeEventListener("touchstart", stopFollowing);
      editor.scrollDOM.removeEventListener("pointerdown", stopFollowingFromPointer);
      editor.scrollDOM.removeEventListener("keydown", stopFollowingFromKey);
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
    if (followTail && !followEnabled.current) followRef.current = true;
    followEnabled.current = followTail;
    const scroll = followTail && active && followRef.current;
    if (current !== value) {
      updatingValue.current = true;
      editor.dispatch({
        changes: changedRange(current, value),
        annotations: Transaction.addToHistory.of(false),
        selection: readOnly
          ? undefined
          : { anchor: Math.min(editor.state.selection.main.head, value.length) },
      });
      updatingValue.current = false;
    }
    if (!scroll) return;

    const request = ++scrollRequest.current;
    scrollingToTail.current = true;
    editor.requestMeasure({
      read(view) {
        return view.scrollDOM.scrollHeight;
      },
      write(scrollHeight, view) {
        if (
          scrollRequest.current !== request ||
          !activeRef.current ||
          !followTailRef.current ||
          !followRef.current
        ) {
          if (scrollRequest.current === request) scrollingToTail.current = false;
          return;
        }
        view.scrollDOM.scrollTop = scrollHeight;
        window.requestAnimationFrame(() => {
          if (scrollRequest.current === request) scrollingToTail.current = false;
        });
      },
    });
  }, [active, followTail, readOnly, value]);

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

function changedRange(current: string, value: string) {
  let from = 0;
  while (from < current.length && from < value.length && current[from] === value[from]) from += 1;

  let currentTo = current.length;
  let valueTo = value.length;
  while (currentTo > from && valueTo > from && current[currentTo - 1] === value[valueTo - 1]) {
    currentTo -= 1;
    valueTo -= 1;
  }
  return { from, to: currentTo, insert: value.slice(from, valueTo) };
}
