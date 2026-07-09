import ace from "ace-builds/src-noconflict/ace";

(globalThis as typeof globalThis & { ace: AceAjax.Ace }).ace = ace;
await import("ace-builds/src-noconflict/mode-json");

export default ace;
