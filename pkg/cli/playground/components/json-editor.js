// @ts-check

import { CodeMirror } from "../cdn/codemirror.js";

/** @typedef {{value:string, label:string, onChange?:(value:string)=>void, readOnly?:boolean, disabled?:boolean, invalid?:boolean, describedBy?:string, followTail?:boolean, active?:boolean}} EditorOptions */

export class JsonEditor {
  /** @param {HTMLElement} host @param {EditorOptions} options */
  constructor(host, options) {
    this.host = host;
    this.options = options;
    this.updating = false;
    this.following = true;
    this.scrolling = false;
    this.scrollRequest = 0;
    this.editor = CodeMirror(host, {
      value: options.value,
      mode: { name: "javascript", json: true },
      lineNumbers: true,
      foldGutter: true,
      gutters: ["CodeMirror-linenumbers", "CodeMirror-foldgutter"],
      matchBrackets: true,
      styleActiveLine: true,
      lineWrapping: true,
      indentUnit: 2,
      tabSize: 2,
    });
    this.wrapper = this.editor.getWrapperElement();
    this.scroller = this.editor.getScrollerElement();
    applyEditorBehavior(this.editor, options);
    this.onChange = () => {
      if (this.updating) return;
      const value = this.editor.getValue();
      this.options.value = value;
      this.options.onChange?.(value);
    };
    this.editor.on("change", this.onChange);
    this.updateFollow = () => {
      if (this.scrolling) return;
      const { clientHeight, scrollHeight, scrollTop } = this.scroller;
      this.following = scrollHeight - scrollTop - clientHeight < 48;
    };
    this.stopFollowing = () => {
      this.following = false;
      this.scrolling = false;
      this.scrollRequest += 1;
    };
    this.onWheel = (/** @type {WheelEvent} */ event) => {
      if (event.deltaY < 0) this.stopFollowing();
    };
    this.onPointer = (/** @type {PointerEvent} */ event) => {
      if (event.target === this.scroller) this.stopFollowing();
    };
    this.onKey = (/** @type {KeyboardEvent} */ event) => {
      if (["ArrowUp", "Home", "PageUp"].includes(event.key))
        this.stopFollowing();
    };
    this.scroller.addEventListener("scroll", this.updateFollow, {
      passive: true,
    });
    this.scroller.addEventListener("wheel", this.onWheel, { passive: true });
    this.scroller.addEventListener("touchstart", this.stopFollowing, {
      passive: true,
    });
    this.scroller.addEventListener("pointerdown", this.onPointer, {
      passive: true,
    });
    this.wrapper.addEventListener("keydown", this.onKey);
  }

  /** @param {Partial<EditorOptions> & {value?:string}} options */
  update(options) {
    const wasFollowing = this.options.followTail;
    const updatesValue = Object.hasOwn(options, "value");
    this.options = { ...this.options, ...options };
    if (this.options.followTail && !wasFollowing) this.following = true;
    applyEditorBehavior(this.editor, this.options);
    const current = this.editor.getValue();
    if (updatesValue && current !== this.options.value) {
      const selections = this.editor
        .listSelections()
        .map(({ anchor, head }) => ({
          anchor: this.editor.indexFromPos(anchor),
          head: this.editor.indexFromPos(head),
        }));
      const scroll = this.editor.getScrollInfo();
      this.updating = true;
      this.editor.setValue(this.options.value);
      this.editor.setSelections(
        selections.map(({ anchor, head }) => ({
          anchor: this.editor.posFromIndex(
            Math.min(anchor, this.options.value.length),
          ),
          head: this.editor.posFromIndex(
            Math.min(head, this.options.value.length),
          ),
        })),
      );
      if (!this.options.followTail || !this.following)
        this.editor.scrollTo(scroll.left, scroll.top);
      this.updating = false;
    }
    if (
      this.options.followTail &&
      this.options.active !== false &&
      this.following
    )
      this.scrollToTail();
  }

  value() {
    return this.editor.getValue();
  }
  async copy() {
    try {
      await navigator.clipboard.writeText(this.value());
    } catch {
      this.editor.setSelection(
        this.editor.posFromIndex(0),
        this.editor.posFromIndex(this.value().length),
      );
      this.editor.focus();
    }
  }
  scrollToTail() {
    const request = ++this.scrollRequest;
    this.scrolling = true;
    requestAnimationFrame(() => {
      if (
        this.scrollRequest === request &&
        this.options.active !== false &&
        this.options.followTail &&
        this.following
      )
        this.editor.scrollTo(null, this.scroller.scrollHeight);
      if (this.scrollRequest === request) this.scrolling = false;
    });
  }
  destroy() {
    this.editor.off("change", this.onChange);
    this.scroller.removeEventListener("scroll", this.updateFollow);
    this.scroller.removeEventListener("wheel", this.onWheel);
    this.scroller.removeEventListener("touchstart", this.stopFollowing);
    this.scroller.removeEventListener("pointerdown", this.onPointer);
    this.wrapper.removeEventListener("keydown", this.onKey);
    this.host.replaceChildren();
  }
}

/** @param {import("../cdn/codemirror").Editor} editor @param {EditorOptions} options */
function applyEditorBehavior(editor, options) {
  const readOnly = Boolean(options.readOnly || options.disabled);
  editor.setOption("readOnly", options.disabled ? "nocursor" : readOnly);
  editor.setOption("screenReaderLabel", options.label);
  editor.setOption("tabindex", options.disabled ? -1 : 0);
  const wrapper = editor.getWrapperElement();
  const input = editor.getInputField();
  wrapper.setAttribute("aria-readonly", String(readOnly));
  for (const node of [wrapper, input]) {
    setOptionalAttribute(node, "aria-describedby", options.describedBy);
    setOptionalAttribute(
      node,
      "aria-invalid",
      options.invalid ? "true" : undefined,
    );
    setOptionalAttribute(
      node,
      "aria-disabled",
      options.disabled ? "true" : undefined,
    );
  }
  input.setAttribute("spellcheck", "false");
}

/** @param {HTMLElement} node @param {string} name @param {string | undefined} value */
function setOptionalAttribute(node, name, value) {
  if (value === undefined) node.removeAttribute(name);
  else node.setAttribute(name, value);
}
