#!/usr/bin/env bash
# Prints the release version and writes it into internal/version/version.go (not committed).
# Tag v1.2.3 -> 1.2.3; manual runs -> <version in the code>-dev.<short commit>.
set -euo pipefail

file=internal/version/version.go
current="$(sed -n 's/^var Version = "\(.*\)"$/\1/p' "$file")"

if [[ "${GITHUB_REF:-}" == refs/tags/v* ]]; then
  version="${GITHUB_REF_NAME#v}"
else
  version="${current}-dev.${GITHUB_SHA:0:7}"
fi

sed "s/^var Version = \".*\"$/var Version = \"${version}\"/" "$file" > "$file.tmp"
mv "$file.tmp" "$file"
echo "$version"
