(() => {
  let mode = localStorage.getItem("cog-playground-theme");
  if (mode !== "light" && mode !== "dark") {
    mode = matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
  }
  document.documentElement.dataset.mode = mode;
})();
