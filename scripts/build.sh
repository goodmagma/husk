#!/usr/bin/env bash
# Builds husk (CLI) and/or husk-gui (GUI) into dist/ for the current or the given system.
#
#   scripts/build.sh [cli|gui|all]            default: all
#
# Environment:
#   GOOS, GOARCH   target system (default: this machine); the GUI needs CGO and a native build
#   VERSION        program version (default: the one in internal/version/version.go)
#   OUT            output folder (default: dist)
#   WINRES=1       always regenerate the Windows resources (used by the release pipeline)
#
# On Windows run it from Git Bash. Windows builds embed the icon and version information
# from cmd/*/rsrc_windows_*.syso; they are regenerated when VERSION differs from the code.
set -euo pipefail
cd "$(dirname "$0")/.."

what="${1:-all}"
goos="${GOOS:-$(go env GOOS)}"
goarch="${GOARCH:-$(go env GOARCH)}"
out="${OUT:-dist}"
code_version="$(sed -n 's/^var Version = "\(.*\)"$/\1/p' internal/version/version.go)"
version="${VERSION:-$code_version}"
ext=""
[ "$goos" = windows ] && ext=".exe"
ldflags="-s -w -X github.com/goodmagma/husk/internal/version.Version=${version}"
winres="github.com/tc-hib/go-winres@v0.3.3"

# Windows resources (icon, manifest, version); only the numeric part fits the version fields.
winres() {
  local dir="$1"
  if [ "$goos" != windows ] || { [ "$version" = "$code_version" ] && [ "${WINRES:-}" != 1 ]; }; then
    return
  fi
  local numeric="${version%%-*}"
  go run "$winres" make --in "$dir/winres/winres.json" --out "$dir/rsrc" --arch amd64,arm64 \
    --product-version "$numeric" --file-version "$numeric"
}

build_cli() {
  winres cmd/husk
  echo "husk $version ($goos/$goarch) -> $out/husk$ext"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "$ldflags" -o "$out/husk$ext" ./cmd/husk
}

build_gui() {
  winres cmd/husk-gui
  local flags="$ldflags"
  [ "$goos" = windows ] && flags="$flags -H=windowsgui"
  echo "husk-gui $version ($goos/$goarch) -> $out/husk-gui$ext"
  CGO_ENABLED=1 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "$flags" -o "$out/husk-gui$ext" ./cmd/husk-gui
}

mkdir -p "$out"
case "$what" in
  cli) build_cli ;;
  gui) build_gui ;;
  all) build_cli; build_gui ;;
  *) echo "usage: $0 [cli|gui|all]" >&2; exit 2 ;;
esac
