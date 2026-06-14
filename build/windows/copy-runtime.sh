#!/usr/bin/env bash
set -euo pipefail

BIN_DIR="bin"
REQUIRE_MEDIA_RUNTIME=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --bin-dir)
      BIN_DIR="$2"
      shift 2
      ;;
    --require-media-runtime)
      REQUIRE_MEDIA_RUNTIME=true
      shift
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
BIN="$ROOT_DIR/$BIN_DIR"
RUNTIME="$ROOT_DIR/build/windows/runtime"
LICENSE_OUT="$BIN/licenses"

mkdir -p "$BIN"
mkdir -p "$LICENSE_OUT"

copy_tree_if_present() {
  local src="$1"
  local dst="$2"
  if [ -d "$src" ]; then
    rm -rf "$dst"
    mkdir -p "$(dirname "$dst")"
    cp -r "$src" "$dst"
  fi
}

copy_file_if_present() {
  local src="$1"
  local dst="$2"
  if [ -f "$src" ]; then
    mkdir -p "$(dirname "$dst")"
    cp -f "$src" "$dst"
  fi
}

copy_tree_if_present "$RUNTIME/ffmpeg" "$BIN/runtime/ffmpeg"
copy_tree_if_present "$RUNTIME/licenses" "$LICENSE_OUT/media-runtime"
copy_file_if_present "$RUNTIME/version.txt" "$BIN/runtime/version.txt"

if [ -d "$RUNTIME" ]; then
  for dll in "$RUNTIME"/*.dll; do
    [ -e "$dll" ] || continue
    cp -f "$dll" "$BIN/$(basename "$dll")"
  done
fi

for mpv_dir in "$RUNTIME/mpv" "$RUNTIME/mpv/bin"; do
  if [ -d "$mpv_dir" ]; then
    for dll in "$mpv_dir"/*.dll; do
      [ -e "$dll" ] || continue
      cp -f "$dll" "$BIN/$(basename "$dll")"
    done
  fi
done

copy_file_if_present "$ROOT_DIR/LICENSE" "$LICENSE_OUT/LICENSE"
copy_file_if_present "$ROOT_DIR/THIRD_PARTY_NOTICES.md" "$LICENSE_OUT/THIRD_PARTY_NOTICES.md"
copy_file_if_present "$ROOT_DIR/docs/dependency-sources.md" "$LICENSE_OUT/dependency-sources.md"
copy_file_if_present "$ROOT_DIR/build/deps/manifest.json" "$LICENSE_OUT/dependency-manifest.json"

if [ "$REQUIRE_MEDIA_RUNTIME" = true ]; then
  missing=""
  for f in \
    "$BIN/runtime/ffmpeg/bin/ffmpeg.exe" \
    "$BIN/runtime/ffmpeg/bin/ffprobe.exe" \
    "$BIN/runtime/version.txt" \
    "$BIN/libmpv.dll" \
    "$LICENSE_OUT/LICENSE" \
    "$LICENSE_OUT/THIRD_PARTY_NOTICES.md" \
    "$LICENSE_OUT/dependency-sources.md" \
    "$LICENSE_OUT/dependency-manifest.json"; do
    if [ ! -e "$f" ]; then
      missing="$missing $f"
    fi
  done

  for dll in "$RUNTIME"/*.dll; do
    [ -e "$dll" ] || continue
    if [ ! -e "$BIN/$(basename "$dll")" ]; then
      missing="$missing $BIN/$(basename "$dll")"
    fi
  done

  for dll in "$RUNTIME/ffmpeg/bin"/*.dll; do
    [ -e "$dll" ] || continue
    if [ ! -e "$BIN/runtime/ffmpeg/bin/$(basename "$dll")" ]; then
      missing="$missing $BIN/runtime/ffmpeg/bin/$(basename "$dll")"
    fi
  done

  if [ -n "$missing" ]; then
    echo "Release media runtime is incomplete. Missing:$missing" >&2
    exit 1
  fi
fi
