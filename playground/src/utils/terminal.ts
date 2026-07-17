/** Displays terminal carriage-return updates as separate lines without changing captured text. */
export function renderTerminalText(value: string): string {
  return value.replaceAll("\r\n", "\n").replaceAll("\r", "\n");
}
