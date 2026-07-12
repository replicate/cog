import { Loader } from "@cloudflare/kumo/components/loader";
import { lazy, Suspense } from "react";

import type { JsonEditorProps } from "@/components/editor/JsonEditor";

const JsonEditor = lazy(async () => ({
  default: (await import("@/components/editor/JsonEditor")).JsonEditor,
}));

export function LazyJsonEditor(props: JsonEditorProps) {
  return (
    <Suspense
      fallback={<Loader aria-label="Loading JSON editor" className="editor-loader" size="sm" />}
    >
      <JsonEditor {...props} />
    </Suspense>
  );
}
