// @ts-check

export class Store extends EventTarget {
  emit() {
    this.dispatchEvent(new Event("change"));
  }
  /** @param {() => void} listener */
  subscribe(listener) {
    this.addEventListener("change", listener);
    return () => this.removeEventListener("change", listener);
  }
}
