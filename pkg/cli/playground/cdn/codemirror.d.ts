export const defaultKeymap: any[];
export const historyKeymap: any[];
export const foldKeymap: any[];
export function history(): any;
export function json(): any;
export function bracketMatching(): any;
export function foldGutter(): any;
export const HighlightStyle: { define(value: any[]): any };
export function syntaxHighlighting(value: any): any;
export class Compartment { of(value: any): any; reconfigure(value: any): any; }
export class EditorState {
  static create(value: any): EditorState;
  static readOnly: { of(value: boolean): any };
  doc: { length: number; toString(): string };
  selection: { main: { anchor: number; head: number } };
}
export const Transaction: { addToHistory: { of(value: boolean): any } };
export class EditorView {
  static theme(value: any): any;
  static editable: { of(value: boolean): any };
  static contentAttributes: { of(value: Record<string, string>): any };
  static updateListener: { of(value: (update: any) => void): any };
  static lineWrapping: any;
  constructor(value: any);
  state: EditorState;
  scrollDOM: HTMLElement;
  dispatch(value: any): void;
  destroy(): void;
  focus(): void;
  requestMeasure(value: any): void;
}
export function drawSelection(): any;
export function highlightActiveLine(): any;
export function highlightActiveLineGutter(): any;
export function highlightSpecialChars(): any;
export const keymap: { of(value: any[]): any };
export function lineNumbers(): any;
export const tags: Record<string, any> & { special(value: any): any };
