// @ts-check

/** @template {keyof HTMLElementTagNameMap} K @param {K} tag @param {Record<string, unknown>} [attributes] @param {...(Node|string|undefined|null|false)} children @returns {HTMLElementTagNameMap[K]} */
export function element(tag, attributes = {}, ...children) {
  const node = document.createElement(tag);
  for (const [name, value] of Object.entries(attributes)) {
    if (value === undefined || value === null || (value === false && !name.startsWith("aria-")))
      continue;
    if (name === "className") node.className = String(value);
    else if (name === "text") node.textContent = String(value);
    else if (name === "checked" && node instanceof HTMLInputElement) node.checked = Boolean(value);
    else if (name === "disabled" && "disabled" in node) node.disabled = Boolean(value);
    else if (name === "required" && "required" in node) node.required = Boolean(value);
    else if (name === "open" && node instanceof HTMLDetailsElement) node.open = Boolean(value);
    else if (name === "value" && "value" in node) node.value = String(value);
    else if (name === "htmlFor" && node instanceof HTMLLabelElement) node.htmlFor = String(value);
    else if (name.startsWith("on") && typeof value === "function")
      node.addEventListener(name.slice(2).toLowerCase(), /** @type {EventListener} */ (value));
    else node.setAttribute(name, String(value));
  }
  for (const child of children)
    if (child !== undefined && child !== null && child !== false) node.append(child);
  return node;
}

/** @param {string} id @param {string} value */
export function tabId(id, value) {
  return `${id}-tab-${value}`;
}
/** @param {string} id @param {string} value */
export function tabPanelId(id, value) {
  return `${id}-panel-${value}`;
}

/** @param {string} id @param {string} label @param {{value:string,label:string}[]} items @param {string} value @param {(value:string)=>void} onChange */
export function segmentedTabs(id, label, items, value, onChange) {
  const tablist = element("div", { role: "tablist", "aria-label": label });
  /** @type {HTMLButtonElement[]} */ const tabs = [];
  for (const item of items) {
    const button = element("button", {
      id: tabId(id, item.value),
      type: "button",
      role: "tab",
      "aria-selected": item.value === value,
      "aria-controls": tabPanelId(id, item.value),
      tabindex: item.value === value ? "0" : "-1",
      text: item.label,
    });
    button.addEventListener("focus", () => onChange(item.value));
    button.addEventListener("click", () => onChange(item.value));
    button.addEventListener("keydown", (event) => {
      const current = tabs.indexOf(button);
      let next = current;
      if (event.key === "ArrowRight") next = (current + 1) % items.length;
      else if (event.key === "ArrowLeft") next = (current - 1 + items.length) % items.length;
      else if (event.key === "Home") next = 0;
      else if (event.key === "End") next = items.length - 1;
      else return;
      event.preventDefault();
      tabs[next]?.focus();
    });
    tabs.push(button);
    tablist.append(button);
  }
  return element("div", { className: "playground-segmented" }, tablist);
}

/** @param {string | undefined} status */
export function statusBadge(status) {
  const normalized = (status || "unknown").toLowerCase();
  const variant = ["ready", "succeeded"].includes(normalized)
    ? "success"
    : ["busy", "starting", "processing"].includes(normalized)
      ? "warning"
      : [
            "defunct",
            "error",
            "failed",
            "failure",
            "setup_failed",
            "unhealthy",
            "unreachable",
          ].includes(normalized)
        ? "error"
        : "neutral";
  return element("output", { className: `status-badge status-${variant}`, text: normalized });
}

/** @param {HTMLElement} node */
export function followTail(node) {
  return node.scrollHeight - node.scrollTop - node.clientHeight < 48;
}

/** @param {HTMLElement} root @param {...Node} children */
export function replaceChildrenPreservingFocus(root, ...children) {
  const active = root.contains(document.activeElement) ? document.activeElement : null;
  const id = active instanceof HTMLElement ? active.id : "";
  const selection =
    active instanceof HTMLInputElement || active instanceof HTMLTextAreaElement
      ? [active.selectionStart, active.selectionEnd]
      : undefined;
  root.replaceChildren(...children);
  if (!id) return;
  const next = root.querySelector(`#${CSS.escape(id)}`);
  if (!(next instanceof HTMLElement)) return;
  next.focus({ preventScroll: true });
  if (
    selection &&
    (next instanceof HTMLInputElement || next instanceof HTMLTextAreaElement) &&
    selection[0] !== null &&
    selection[1] !== null
  )
    next.setSelectionRange(selection[0], selection[1]);
}
