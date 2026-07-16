const MAX_LOCAL_FILE_SIZE = 16 * 1024 * 1024;

/** Converts a browser file up to 16 MiB to a data URI and rejects if it cannot be read. */
export function fileToDataURI(file: File): Promise<string> {
  if (file.size > MAX_LOCAL_FILE_SIZE) {
    return Promise.reject(new Error("Local files must be 16 MiB or smaller"));
  }
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error ?? new Error("Could not read file"));
    reader.readAsDataURL(file);
  });
}
