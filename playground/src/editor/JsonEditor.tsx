import { useEffect, useRef } from "react";

import ace from "./ace";
import { currentAceTheme } from "./kumoTheme";

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
  const editorRef = useRef<AceAjax.Editor | undefined>(undefined);
  const initialValue = useRef(value);
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  useEffect(() => {
    if (!hostRef.current) return;
    const editor = ace.edit(hostRef.current);
    editorRef.current = editor;
    editor.session.setMode("ace/mode/json");
    editor.setTheme(currentAceTheme());
    editor.setOptions({
      readOnly,
      fontSize: "12px",
      showPrintMargin: false,
      useWorker: false,
      highlightActiveLine: !readOnly,
      highlightGutterLine: !readOnly,
      showFoldWidgets: true,
      fadeFoldWidgets: false,
      tabSize: 2,
      useSoftTabs: true,
      wrap: true,
    });
    editor.setValue(initialValue.current, -1);
    hostRef.current.querySelector("textarea")?.setAttribute("aria-label", label);
    editor.on("change", () => onChangeRef.current?.(editor.getValue()));
    const updateTheme = () => editor.setTheme(currentAceTheme());
    window.addEventListener("cog-theme-change", updateTheme);
    return () => {
      window.removeEventListener("cog-theme-change", updateTheme);
      editor.destroy();
      editorRef.current = undefined;
    };
  }, [label, readOnly]);

  useEffect(() => {
    const editor = editorRef.current;
    if (!editor) return;
    if (editor.getValue() !== value) editor.setValue(value, followTail ? 1 : -1);
    editor.resize();
    if (followTail)
      editor.renderer.scrollToLine(editor.session.getLength(), false, false, () => {});
  }, [followTail, value]);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(editorRef.current?.getValue() ?? value);
    } catch {
      editorRef.current?.selectAll();
      editorRef.current?.focus();
    }
  };

  return (
    <div
      className={`ace-json ${className}`}
      style={autoHeight ? { height: `${editorHeight(value)}px` } : undefined}
    >
      <div ref={hostRef} aria-label={label} aria-readonly={readOnly} />
      <button type="button" className="editor-copy" onClick={copy}>
        Copy
      </button>
    </div>
  );
}

function editorHeight(value: string): number {
  return Math.min(320, Math.max(80, value.split("\n").length * 18 + 18));
}
