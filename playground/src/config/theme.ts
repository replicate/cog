export type ThemeMode = "light" | "dark";

/** Returns light only when explicitly selected; the playground otherwise defaults to dark. */
export function currentTheme(): ThemeMode {
  return document.documentElement.dataset.mode === "light" ? "light" : "dark";
}

/** Updates Kumo's root color mode and persists it in local storage. */
export function setTheme(mode: ThemeMode) {
  document.documentElement.dataset.mode = mode;
  localStorage.setItem("cog-playground-theme", mode);
}
