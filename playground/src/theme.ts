export type ThemeMode = "light" | "dark";

export function currentTheme(): ThemeMode {
  return document.documentElement.dataset.mode === "light" ? "light" : "dark";
}

export function setTheme(mode: ThemeMode) {
  document.documentElement.dataset.mode = mode;
  localStorage.setItem("cog-playground-theme", mode);
  window.dispatchEvent(new Event("cog-theme-change"));
}
