import ace from "./ace";

type AceWithDefine = typeof ace & {
  define: (name: string, dependencies: string[], factory: (...args: unknown[]) => void) => void;
};

const themeCSS = (className: string) => `
.${className} { background-color: var(--color-kumo-base); color: var(--text-color-kumo-default); }
.${className} .ace_gutter { background: var(--color-kumo-elevated); color: var(--text-color-kumo-subtle); }
.${className} .ace_print-margin { width: 1px; background: var(--color-kumo-hairline); }
.${className} .ace_cursor { color: var(--text-color-kumo-default); }
.${className} .ace_marker-layer .ace_selection { background: var(--color-kumo-info-tint); }
.${className} .ace_marker-layer .ace_active-line,
.${className} .ace_gutter-active-line { background: color-mix(in srgb, var(--color-kumo-fill) 45%, transparent); }
.${className} .ace_marker-layer .ace_selected-word { border: 1px solid var(--color-kumo-line); }
.${className} .ace_fold { background-color: var(--color-kumo-brand); border-color: var(--text-color-kumo-default); }
.${className} .ace_variable { color: var(--text-color-kumo-link); }
.${className} .ace_string { color: var(--text-color-kumo-success); }
.${className} .ace_constant.ace_numeric { color: var(--text-color-kumo-warning); }
.${className} .ace_constant.ace_language { color: var(--text-color-kumo-brand); }
.${className} .ace_constant.ace_language.ace_escape { color: var(--text-color-kumo-info); }
.${className} .ace_paren, .${className} .ace_punctuation { color: var(--text-color-kumo-subtle); }
`;

function defineTheme(name: string, className: string, isDark: boolean) {
  (ace as AceWithDefine).define(
    `ace/theme/${name}`,
    ["require", "exports", "module", "ace/lib/dom"],
    (requireValue: unknown, exportsValue: unknown) => {
      const requireAce = requireValue as (module: string) => {
        importCssString: (css: string, id: string) => void;
      };
      const exports = exportsValue as { isDark: boolean; cssClass: string; cssText: string };
      exports.isDark = isDark;
      exports.cssClass = className;
      exports.cssText = themeCSS(className);
      requireAce("ace/lib/dom").importCssString(exports.cssText, className);
    },
  );
}

defineTheme("kumo-light", "ace-kumo-light", false);
defineTheme("kumo-dark", "ace-kumo-dark", true);

export function currentAceTheme() {
  return document.documentElement.dataset.mode === "light"
    ? "ace/theme/kumo-light"
    : "ace/theme/kumo-dark";
}
