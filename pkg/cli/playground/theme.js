// Light/dark theming via a data-theme attribute on <html>. The initial value
// is set by a tiny inline script in index.html (before paint, to avoid a
// flash); this module handles toggling and persistence.

const STORAGE_KEY = "cog-playground-theme";

export function currentTheme() {
  return document.documentElement.dataset.theme === "light" ? "light" : "dark";
}

export function toggleTheme() {
  const next = currentTheme() === "dark" ? "light" : "dark";
  document.documentElement.dataset.theme = next;
  localStorage.setItem(STORAGE_KEY, next);
  return next;
}
