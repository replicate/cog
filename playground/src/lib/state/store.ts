export class Store extends EventTarget {
  emit(): void {
    this.dispatchEvent(new Event("change"));
  }

  subscribe(listener: () => void): () => void {
    this.addEventListener("change", listener);
    return () => this.removeEventListener("change", listener);
  }
}
