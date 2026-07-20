// @ts-check

export const MAX_LOCAL_FILE_SIZE = 16 * 1024 * 1024;

export class FileReadGuard {
  #revision = 0;
  begin() {
    return ++this.#revision;
  }
  cancel() {
    this.#revision += 1;
  }
  /** @param {number} revision */
  isCurrent(revision) {
    return revision === this.#revision;
  }
}

/** @param {File} file */
export function fileToDataURI(file) {
  if (file.size > MAX_LOCAL_FILE_SIZE)
    return Promise.reject(new Error("Local files must be 16 MiB or smaller"));
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error("Could not read file"));
    reader.onload = () =>
      typeof reader.result === "string"
        ? resolve(reader.result)
        : reject(new Error("Could not read file"));
    reader.readAsDataURL(file);
  });
}
