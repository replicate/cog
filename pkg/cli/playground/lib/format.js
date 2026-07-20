// @ts-check

/** @param {unknown} value */
export function formatValue(value) {
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value, null, 2) ?? String(value);
  } catch {
    return String(value);
  }
}

/** @param {number} milliseconds */
export function formatDuration(milliseconds) {
  return milliseconds < 1000
    ? `${Math.round(milliseconds)} ms`
    : `${(milliseconds / 1000).toFixed(2)} s`;
}

/** @param {string} value */
export function renderTerminalText(value) {
  return value.replaceAll("\r\n", "\n").replaceAll("\r", "\n");
}
