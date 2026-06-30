// Minimal DOM helpers — a thin wrapper over document.createElement so building
// nodes reads top-to-bottom without a framework or a build step.
//
//   el("div", { class: "field" }, el("label", { text: "name" }), input)
//
// Props: `class` -> className, `text` -> textContent, `html` is intentionally
// unsupported (we never inject untrusted HTML). `onclick`/`oninput`/... attach
// listeners. Boolean true sets a bare attribute; null/false/undefined skip it.
export function el(tag, props = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    setProp(node, key, value);
  }
  append(node, children);
  return node;
}

// setProp applies a single prop. Known DOM properties are set directly; an
// `on*` function becomes an event listener; everything else is an attribute
// (a `true` value renders as a bare boolean attribute).
function setProp(node, key, value) {
  if (value == null || value === false) return;
  switch (key) {
    case "class":
      node.className = value;
      break;
    case "text":
      node.textContent = value;
      break;
    case "value":
      node.value = value;
      break;
    case "checked":
      node.checked = Boolean(value);
      break;
    default:
      if (key.startsWith("on") && typeof value === "function") {
        node.addEventListener(key.slice(2).toLowerCase(), value);
      } else {
        node.setAttribute(key, value === true ? "" : value);
      }
  }
}

// append flattens arrays and turns primitives into text nodes.
export function append(node, children) {
  for (const child of children.flat()) {
    if (child == null || child === false) continue;
    node.append(child.nodeType ? child : document.createTextNode(String(child)));
  }
}

// clear removes all children of a node.
export function clear(node) {
  while (node.firstChild) node.removeChild(node.firstChild);
}
