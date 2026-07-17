/** Renders a value as a string, pretty-printing non-string values as JSON. */
export function formatValue(value: unknown): string {
  return typeof value === "string" ? value : JSON.stringify(value, null, 2);
}

/** Formats a millisecond duration as milliseconds under a second, otherwise seconds. */
export function formatDuration(milliseconds: number): string {
  return milliseconds < 1000
    ? `${Math.round(milliseconds)} ms`
    : `${(milliseconds / 1000).toFixed(2)} s`;
}
