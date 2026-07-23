export type Position = { line: number; ch: number };
export type Selection = { anchor: Position; head: Position };

export type Editor = {
  getValue(): string;
  setValue(value: string): void;
  getCursor(): Position;
  setCursor(position: Position): void;
  setSelection(anchor: Position, head: Position): void;
  listSelections(): Selection[];
  setSelections(selections: Selection[]): void;
  indexFromPos(position: Position): number;
  posFromIndex(index: number): Position;
  getWrapperElement(): HTMLElement;
  getScrollerElement(): HTMLElement;
  getInputField(): HTMLElement;
  setOption(name: string, value: unknown): void;
  getScrollInfo(): { left: number; top: number };
  scrollTo(x: number | null, y: number | null): void;
  focus(): void;
  on(event: string, handler: (...args: unknown[]) => void): void;
  off(event: string, handler: (...args: unknown[]) => void): void;
};

export type CodeMirrorStatic = (
  host: HTMLElement,
  options: Record<string, unknown>,
) => Editor;

export const CodeMirror: CodeMirrorStatic;
