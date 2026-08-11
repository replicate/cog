export const MAX_LOCAL_FILE_SIZE = 16 * 1024 * 1024;

export class FileReadGuard {
  #revision = 0;

  begin(): number {
    return ++this.#revision;
  }

  cancel(): void {
    this.#revision += 1;
  }

  isCurrent(revision: number): boolean {
    return revision === this.#revision;
  }
}

export function fileToDataURI(file: File): Promise<string> {
  if (file.size > MAX_LOCAL_FILE_SIZE)
    return Promise.reject(new Error("Local files must be 16 MiB or smaller"));

  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error("Could not read file"));
    reader.onload = () =>
      typeof reader.result === "string"
        ? resolve(reader.result)
        : reject(new Error("Could not read file"));
    reader.readAsDataURL(file);
  });
}
