import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { json } from "@codemirror/lang-json";
import {
  bracketMatching,
  foldGutter,
  foldKeymap,
  HighlightStyle,
  syntaxHighlighting,
} from "@codemirror/language";
import { Compartment, EditorState, Transaction, type Extension } from "@codemirror/state";
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

const editorTheme = EditorView.theme({
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
const highlightStyle = HighlightStyle.define([
  { tag: tags.propertyName, color: "var(--text-color-kumo-link)" },
  { tag: [tags.string, tags.special(tags.string)], color: "var(--text-color-kumo-success)" },
  { tag: tags.number, color: "var(--text-color-kumo-warning)" },
  { tag: [tags.bool, tags.null], color: "var(--text-color-kumo-brand)" },
  { tag: tags.escape, color: "var(--text-color-kumo-info)" },
  { tag: [tags.brace, tags.squareBracket, tags.separator], color: "var(--text-color-kumo-subtle)" },
]);

export type EditorOptions = {
  value: string;
  label: string;
  onChange?: (value: string) => void;
  readOnly?: boolean;
  disabled?: boolean;
  invalid?: boolean;
  describedBy?: string;
  followTail?: boolean;
  active?: boolean;
};

export class JsonEditor {
  host: HTMLElement;
  options: EditorOptions;
  behavior: Compartment;
  updating: boolean;
  following: boolean;
  scrolling: boolean;
  scrollRequest: number;
  view: EditorView;
  updateFollow: () => void;
  stopFollowing: () => void;
  onWheel: (event: WheelEvent) => void;
  onPointer: (event: PointerEvent) => void;
  onKey: (event: KeyboardEvent) => void;

  constructor(host: HTMLElement, options: EditorOptions) {
    this.host = host;
    this.options = options;
    this.behavior = new Compartment();
    this.updating = false;
    this.following = true;
    this.scrolling = false;
    this.scrollRequest = 0;
    this.view = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: options.value,
        extensions: [
          lineNumbers(),
          foldGutter(),
          highlightSpecialChars(),
          history(),
          drawSelection(),
          bracketMatching(),
          highlightActiveLine(),
          highlightActiveLineGutter(),
          keymap.of([...defaultKeymap, ...historyKeymap, ...foldKeymap]),
          json(),
          syntaxHighlighting(highlightStyle),
          editorTheme,
          EditorView.lineWrapping,
          this.behavior.of(editorBehavior(options)),
          EditorView.updateListener.of((update) => {
            if (update.docChanged && !this.updating) {
              const value = update.state.doc.toString();
              this.options.value = value;
              this.options.onChange?.(value);
            }
          }),
        ],
      }),
    });
    this.updateFollow = () => {
      if (this.scrolling) return;
      const { clientHeight, scrollHeight, scrollTop } = this.view.scrollDOM;
      this.following = scrollHeight - scrollTop - clientHeight < 48;
    };
    this.stopFollowing = () => {
      this.following = false;
      this.scrolling = false;
      this.scrollRequest += 1;
    };
    this.onWheel = (event: WheelEvent): void => {
      if (event.deltaY < 0) this.stopFollowing();
    };
    this.onPointer = (event: PointerEvent): void => {
      if (event.target === this.view.scrollDOM) this.stopFollowing();
    };
    this.onKey = (event: KeyboardEvent): void => {
      if (["ArrowUp", "Home", "PageUp"].includes(event.key)) this.stopFollowing();
    };
    this.view.scrollDOM.addEventListener("scroll", this.updateFollow, { passive: true });
    this.view.scrollDOM.addEventListener("wheel", this.onWheel, { passive: true });
    this.view.scrollDOM.addEventListener("touchstart", this.stopFollowing, { passive: true });
    this.view.scrollDOM.addEventListener("pointerdown", this.onPointer, { passive: true });
    this.view.scrollDOM.addEventListener("keydown", this.onKey);
  }

  update(options: Partial<EditorOptions>): void {
    const wasFollowing = this.options.followTail;
    const updatesValue = Object.hasOwn(options, "value");
    this.options = { ...this.options, ...options };

    if (this.options.followTail && !wasFollowing) this.following = true;
    this.view.dispatch({ effects: this.behavior.reconfigure(editorBehavior(this.options)) });

    const current = this.view.state.doc.toString();
    if (updatesValue && current !== this.options.value) {
      const { anchor, head } = this.view.state.selection.main;
      this.updating = true;
      this.view.dispatch({
        changes: changedRange(current, this.options.value),
        annotations: Transaction.addToHistory.of(false),
        selection: this.options.readOnly
          ? undefined
          : {
              anchor: Math.min(anchor, this.options.value.length),
              head: Math.min(head, this.options.value.length),
            },
      });
      this.updating = false;
    }
    if (this.options.followTail && this.options.active !== false && this.following)
      this.scrollToTail();
  }

  value(): string {
    return this.view.state.doc.toString();
  }

  async copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(this.value());
    } catch {
      this.view.dispatch({ selection: { anchor: 0, head: this.view.state.doc.length } });
      this.view.focus();
    }
  }

  scrollToTail(): void {
    const request = ++this.scrollRequest;
    this.scrolling = true;
    this.view.requestMeasure({
      read: (view: EditorView): number => view.scrollDOM.scrollHeight,
      write: (height: number, view: EditorView): void => {
        if (
          this.scrollRequest === request &&
          this.options.active !== false &&
          this.options.followTail &&
          this.following
        )
          view.scrollDOM.scrollTop = height;
        if (this.scrollRequest === request)
          requestAnimationFrame(() => {
            if (this.scrollRequest === request) this.scrolling = false;
          });
      },
    });
  }

  destroy(): void {
    this.view.scrollDOM.removeEventListener("scroll", this.updateFollow);
    this.view.scrollDOM.removeEventListener("wheel", this.onWheel);
    this.view.scrollDOM.removeEventListener("touchstart", this.stopFollowing);
    this.view.scrollDOM.removeEventListener("pointerdown", this.onPointer);
    this.view.scrollDOM.removeEventListener("keydown", this.onKey);
    this.view.destroy();
  }
}

function editorBehavior(options: EditorOptions): Extension {
  const readOnly = Boolean(options.readOnly || options.disabled);
  const attributes: { [name: string]: string } = {
    "aria-label": options.label,
    "aria-readonly": String(readOnly),
    spellcheck: "false",
  };
  if (options.describedBy) attributes["aria-describedby"] = options.describedBy;
  if (options.invalid) attributes["aria-invalid"] = "true";
  if (options.disabled) {
    attributes["aria-disabled"] = "true";
    attributes.tabindex = "-1";
  } else if (options.readOnly) attributes.tabindex = "0";
  return [
    EditorState.readOnly.of(readOnly),
    EditorView.editable.of(!readOnly),
    EditorView.contentAttributes.of(attributes),
  ];
}

export function changedRange(
  current: string,
  value: string,
): { from: number; to: number; insert: string } {
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
