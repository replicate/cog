#!/usr/bin/env python3
"""Generate weights_manifest.json from the local weights/ directory.

Run this whenever the weights change:
    python generate_manifest.py

The output file gets baked into the Docker image and used by predict.py
to verify that all expected weight files are present at runtime.

This is a temporary hack — it will be replaced by the real weight manifest
(/.cog/weights.json) once that's embedded in the model image.
"""

import hashlib
import json
import sys
from pathlib import Path

WEIGHTS_DIR = Path(__file__).parent / "weights"
OUTPUT = Path(__file__).parent / "weights_manifest.json"


def file_sha256(path: Path) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        while chunk := f.read(8 * 1024 * 1024):
            h.update(chunk)
    return f"sha256:{h.hexdigest()}"


def main() -> None:
    if not WEIGHTS_DIR.is_dir():
        print(f"error: {WEIGHTS_DIR} does not exist", file=sys.stderr)
        sys.exit(1)

    files = []
    for p in sorted(WEIGHTS_DIR.rglob("*")):
        if not p.is_file():
            continue
        files.append(
            {
                "path": str(p.relative_to(WEIGHTS_DIR)),
                "size": p.stat().st_size,
                "digest": file_sha256(p),
            }
        )

    manifest = {
        "target": "/src/weights",
        "files": files,
    }

    OUTPUT.write_text(json.dumps(manifest, indent=2) + "\n")
    print(f"wrote {OUTPUT} ({len(files)} files)", file=sys.stderr)


if __name__ == "__main__":
    main()
