import { useEffect, useRef } from "react";

type Props = {
  value: string;
  label: string;
  className?: string;
  followTail?: boolean;
};

export function CodeViewer({ value, label, className = "", followTail = false }: Props) {
  const content = useRef<HTMLPreElement>(null);

  useEffect(() => {
    if (followTail && content.current) content.current.scrollTop = content.current.scrollHeight;
  }, [followTail, value]);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
    } catch {
      return;
    }
  };

  return (
    <div className={`code-viewer ${className}`}>
      <pre ref={content} aria-label={label}>
        {value}
      </pre>
      <button type="button" className="editor-copy" onClick={() => void copy()}>
        Copy
      </button>
    </div>
  );
}
