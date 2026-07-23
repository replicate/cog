// @ts-check
// cdnjs version 6.65.7 is CodeMirror 5.65.7 under an accidentally published
// npm version. Its UMD files install the CodeMirror browser global.
// Source: https://github.com/codemirror/codemirror5 (MIT)

import "https://cdnjs.cloudflare.com/ajax/libs/codemirror/6.65.7/codemirror.min.js";
import "https://cdnjs.cloudflare.com/ajax/libs/codemirror/6.65.7/mode/javascript/javascript.min.js";
import "https://cdnjs.cloudflare.com/ajax/libs/codemirror/6.65.7/addon/edit/matchbrackets.min.js";
import "https://cdnjs.cloudflare.com/ajax/libs/codemirror/6.65.7/addon/selection/active-line.min.js";
import "https://cdnjs.cloudflare.com/ajax/libs/codemirror/6.65.7/addon/fold/foldcode.min.js";
import "https://cdnjs.cloudflare.com/ajax/libs/codemirror/6.65.7/addon/fold/foldgutter.min.js";
import "https://cdnjs.cloudflare.com/ajax/libs/codemirror/6.65.7/addon/fold/brace-fold.min.js";

export const CodeMirror =
  /** @type {import("./codemirror").CodeMirrorStatic} */ (
    /** @type {Record<string, unknown>} */ (globalThis).CodeMirror
  );
