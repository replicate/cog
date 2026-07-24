// @ts-check
// CodeMirror 6 is loaded as browser-native ESM from UNPKG's Cloudflare-backed
// module CDN. Each URL is pinned, and the CDN resolves shared dependencies to
// the same pinned module URLs so CodeMirror's state/view singletons stay intact.
// Source: https://github.com/codemirror (MIT)

export { Compartment, EditorState, Transaction } from "https://esm.unpkg.com/@codemirror/state@6.7.1";
export {
  drawSelection,
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
} from "https://esm.unpkg.com/@codemirror/view@6.43.6";
export {
  defaultKeymap,
  history,
  historyKeymap,
} from "https://esm.unpkg.com/@codemirror/commands@6.10.4";
export {
  bracketMatching,
  foldGutter,
  foldKeymap,
  HighlightStyle,
  syntaxHighlighting,
} from "https://esm.unpkg.com/@codemirror/language@6.12.4";
export { json } from "https://esm.unpkg.com/@codemirror/lang-json@6.0.2";
export { tags } from "https://esm.unpkg.com/@lezer/highlight@1.2.3";
