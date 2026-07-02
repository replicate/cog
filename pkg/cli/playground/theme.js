// Light/dark theming via a data-mode attribute on <html> (the attribute Kumo
// design tokens key off of). The initial value is set by a tiny inline script
// in index.html (before paint, to avoid a flash); this module handles toggling
// and persistence.

const STORAGE_KEY = "cog-playground-theme";

export function currentTheme() {
  return document.documentElement.dataset.mode === "light" ? "light" : "dark";
}

export function toggleTheme() {
  const next = currentTheme() === "dark" ? "light" : "dark";
  document.documentElement.dataset.mode = next;
  localStorage.setItem(STORAGE_KEY, next);
  return next;
}
