#!/usr/bin/env bash
set -euo pipefail

mode="${1:-}"

if [ "$mode" != "--check" ] && [ "$mode" != "--write" ]; then
  echo "Usage: $0 --check|--write" >&2
  exit 1
fi

files=()
while IFS= read -r -d '' file; do
  if [ -f "$file" ] && [ ! -L "$file" ]; then
    files+=("$file")
  fi
done < <(git ls-files -z -- '*.md')

if [ ${#files[@]} -gt 0 ]; then
  prettier "$mode" "${files[@]}"
fi
