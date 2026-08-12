type ElementAttributeValue = boolean | number | string | EventListener | null | undefined;
type ElementAttributes = { [name: string]: ElementAttributeValue };
type ElementChild = Node | string | false | null | undefined;
type TabItem<T extends string> = { value: T; label: string };
type StatusVariant = "error" | "neutral" | "success" | "warning";
type TabNavigation = (current: number, count: number) => number;

const STATUS_VARIANTS: { [status: string]: StatusVariant } = {
  ready: "success",
  succeeded: "success",
  busy: "warning",
  starting: "warning",
  processing: "warning",
  defunct: "error",
  error: "error",
  failed: "error",
  failure: "error",
  setup_failed: "error",
  unhealthy: "error",
  unreachable: "error",
};

const TAB_NAVIGATION: { [key: string]: TabNavigation } = {
  ArrowRight: (current, count) => (current + 1) % count,
  ArrowLeft: (current, count) => (current - 1 + count) % count,
  Home: () => 0,
  End: (_current, count) => count - 1,
};

export function element<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attributes: ElementAttributes = {},
  ...children: ElementChild[]
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  for (const [name, value] of Object.entries(attributes)) {
    if (value === undefined || value === null || (value === false && !name.startsWith("aria-")))
      continue;
    setElementAttribute(node, name, value);
  }
  for (const child of children)
    if (child !== undefined && child !== null && child !== false) node.append(child);
  return node;
}

function setElementAttribute(
  node: HTMLElement,
  name: string,
  value: Exclude<ElementAttributeValue, null | undefined>,
): void {
  if (name.startsWith("on") && typeof value === "function") {
    node.addEventListener(name.slice(2).toLowerCase(), value);
    return;
  }
  const text = String(value);
  const property = name === "text" ? "textContent" : name;
  if (property in node) {
    const current = Reflect.get(node, property);
    Reflect.set(node, property, typeof current === "boolean" ? Boolean(value) : text);
    return;
  }
  node.setAttribute(name, text);
}

export function tabId(id: string, value: string): string {
  return `${id}-tab-${value}`;
}

export function tabPanelId(id: string, value: string): string {
  return `${id}-panel-${value}`;
}

export function segmentedTabs<T extends string>(
  id: string,
  label: string,
  items: readonly TabItem<T>[],
  value: T,
  onChange: (value: T) => void,
): HTMLDivElement {
  const tablist = element("div", { role: "tablist", "aria-label": label });
  const tabs: HTMLButtonElement[] = [];

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
      const navigate = TAB_NAVIGATION[event.key];
      if (!navigate) return;

      event.preventDefault();
      const next = navigate(current, tabs.length);
      tabs[next]?.focus();
    });

    tabs.push(button);
    tablist.append(button);
  }
  return element("div", { className: "playground-segmented" }, tablist);
}

export function statusBadge(status?: string): HTMLOutputElement {
  const normalized = (status || "unknown").toLowerCase();
  const variant = STATUS_VARIANTS[normalized] ?? "neutral";
  return element("output", { className: `status-badge status-${variant}`, text: normalized });
}

export function followTail(node: HTMLElement): boolean {
  return node.scrollHeight - node.scrollTop - node.clientHeight < 48;
}

export function replaceChildrenPreservingFocus(root: HTMLElement, ...children: Node[]): void {
  const active = root.contains(document.activeElement) ? document.activeElement : null;
  const id = active instanceof HTMLElement ? active.id : "";
  const selection: [number | null, number | null] | undefined =
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
