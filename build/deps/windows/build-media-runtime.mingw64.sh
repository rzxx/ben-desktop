#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../../.." && pwd)"
RUNTIME_DIR="$ROOT_DIR/build/windows/runtime"
WORK_DIR="$ROOT_DIR/build/deps/.work/windows"

FFMPEG_VERSION="${FFMPEG_VERSION:-8.1.1}"
MPV_VERSION="${MPV_VERSION:-0.41.0}"
RUNTIME_VERSION="${RUNTIME_VERSION:-2}"
FFMPEG_SOURCE_ARCHIVE="ffmpeg-$FFMPEG_VERSION.tar.xz"
FFMPEG_SOURCE_URL="https://ffmpeg.org/releases/$FFMPEG_SOURCE_ARCHIVE"
MPV_SOURCE_ARCHIVE="mpv-v$MPV_VERSION.tar.gz"
MPV_SOURCE_URL="https://github.com/mpv-player/mpv/archive/refs/tags/v$MPV_VERSION.tar.gz"

rm -rf "$RUNTIME_DIR"
mkdir -p "$RUNTIME_DIR/ffmpeg/bin" "$RUNTIME_DIR/licenses" "$WORK_DIR"
printf '%s\n' "$RUNTIME_VERSION" > "$RUNTIME_DIR/version.txt"

if [ "$MSYSTEM" != "MINGW64" ]; then
  echo "This script must run inside MSYS2 MINGW64. MSYSTEM is '$MSYSTEM'." >&2
  exit 1
fi

_compiler_target="$(gcc -dumpmachine 2>/dev/null || true)"
if [ "$_compiler_target" != "x86_64-w64-mingw32" ]; then
  echo "Compiler target must be x86_64-w64-mingw32, got: '$_compiler_target'" >&2
  exit 1
fi

if ! command -v pacman >/dev/null 2>&1; then
  echo "This script must run inside MSYS2 MINGW64." >&2
  exit 1
fi

pacman -S --needed --noconfirm \
  base-devel git curl pkgconf nasm yasm meson ninja \
  mingw-w64-x86_64-toolchain \
  mingw-w64-x86_64-cmake \
  mingw-w64-x86_64-meson \
  mingw-w64-x86_64-python \
  mingw-w64-x86_64-pkgconf \
  mingw-w64-x86_64-ntldd \
  mingw-w64-x86_64-libwebp \
  mingw-w64-x86_64-aom \
  mingw-w64-x86_64-zlib \
  mingw-w64-x86_64-libplacebo \
  mingw-w64-x86_64-libass

copy_mingw_dll_deps() {
  local binary="$1"
  local dest="$2"
  local current dep dep_path dep_name any_tool
  local copied=false
  local -a queue

  any_tool=false
  if command -v ntldd >/dev/null 2>&1 || command -v ldd >/dev/null 2>&1; then
    any_tool=true
  fi
  if [ "$any_tool" != true ]; then
    echo "Neither ntldd nor ldd is available; cannot copy dependency DLL closure for $binary" >&2
    return 1
  fi

  queue=("$binary")
  while [ "${#queue[@]}" -gt 0 ]; do
    current="${queue[0]}"
    queue=("${queue[@]:1}")
    while IFS= read -r dep; do
      [ -n "$dep" ] || continue
      dep_path="$(cygpath -u "$dep" 2>/dev/null || printf '%s' "$dep")"
      [ -f "$dep_path" ] || continue
      dep_name="$(basename "$dep_path")"
      if [ ! -f "$dest/$dep_name" ]; then
        cp -f "$dep_path" "$dest/$dep_name"
        queue+=("$dest/$dep_name")
        copied=true
      fi
    done < <(mingw_dll_deps_for "$current")
  done

  if [ "${copied:-false}" != true ]; then
    echo "No MinGW dependency DLLs were discovered for $binary" >&2
  fi
}

mingw_dll_deps_for() {
  local binary="$1"
  local output=""
  local path_prefix
  path_prefix="$RUNTIME_DIR:$RUNTIME_DIR/ffmpeg/bin:/mingw64/bin:$PATH"

  if command -v ntldd >/dev/null 2>&1; then
    output="$output
$(PATH="$path_prefix" ntldd -R "$binary" 2>&1 || true)"
  fi
  if command -v ldd >/dev/null 2>&1; then
    output="$output
$(PATH="$path_prefix" ldd "$binary" 2>&1 || true)"
  fi

  printf '%s\n' "$output" |
    grep -Eio '(/[[:alnum:]_.+ /-]*mingw64/bin/[^[:space:]]+\.dll|[A-Za-z]:[\\/][^[:space:]]*[\\/]mingw64[\\/]bin[\\/][^[:space:]]+\.dll)' |
    sort -u
}

validate_runtime_deps() {
  local binary="$1"
  local path_prefix="$2"
  local missing ldd_cmd
  if ! command -v ldd >/dev/null 2>&1; then
    return 0
  fi
  ldd_cmd="$(command -v ldd)"
  missing="$(PATH="$path_prefix:/usr/bin" "$ldd_cmd" "$binary" 2>&1 |
    grep -E '=> not found' |
    grep -Eiv '^[[:space:]]*(api-ms-win|ext-ms-win)' || true)"
  if [ -n "$missing" ]; then
    echo "Missing runtime DLL dependencies for $binary:" >&2
    echo "$missing" >&2
    return 1
  fi
}

sha256_file() {
  sha256sum "$1" | awk '{ print $1 }'
}

sanitize_file() {
  local file="$1"
  [ -f "$file" ] || return 0

  local root_mixed root_win work_mixed work_win runtime_mixed runtime_win home_mixed home_win
  root_mixed="$(cygpath -m "$ROOT_DIR" 2>/dev/null || printf '%s' "$ROOT_DIR")"
  root_win="$(cygpath -w "$ROOT_DIR" 2>/dev/null || printf '%s' "$ROOT_DIR")"
  work_mixed="$(cygpath -m "$WORK_DIR" 2>/dev/null || printf '%s' "$WORK_DIR")"
  work_win="$(cygpath -w "$WORK_DIR" 2>/dev/null || printf '%s' "$WORK_DIR")"
  runtime_mixed="$(cygpath -m "$RUNTIME_DIR" 2>/dev/null || printf '%s' "$RUNTIME_DIR")"
  runtime_win="$(cygpath -w "$RUNTIME_DIR" 2>/dev/null || printf '%s' "$RUNTIME_DIR")"
  home_mixed="$(cygpath -m "${HOME:-}" 2>/dev/null || printf '%s' "${HOME:-}")"
  home_win="$(cygpath -w "${HOME:-}" 2>/dev/null || printf '%s' "${HOME:-}")"

  python - "$file" \
    "<media-build-work>=$WORK_DIR" "<media-build-work>=$work_mixed" "<media-build-work>=$work_win" \
    "<runtime-staging>=$RUNTIME_DIR" "<runtime-staging>=$runtime_mixed" "<runtime-staging>=$runtime_win" \
    "<repo>=$ROOT_DIR" "<repo>=$root_mixed" "<repo>=$root_win" \
    "<home>=${HOME:-}" "<home>=$home_mixed" "<home>=$home_win" <<'PY'
import sys
import re
from pathlib import Path

path = Path(sys.argv[1])
text = path.read_text(encoding="utf-8", errors="replace")
replacements = []
for spec in sys.argv[2:]:
    label, _, value = spec.partition("=")
    if value:
        replacements.append((value, label))
for value, label in sorted(set(replacements), key=lambda item: len(item[0]), reverse=True):
    text = text.replace(value, label)
text = re.sub(r"(?i)(?:/[a-z]/|[a-z]:[/\\])[^ \t\r\n'\";]*?[\\/]build[\\/]deps[\\/]\.work[\\/]windows", "<media-build-work>", text)
text = re.sub(r"(?i)(?:/[a-z]/|[a-z]:[/\\])[^ \t\r\n'\";]*?[\\/]build[\\/]windows[\\/]runtime", "<runtime-staging>", text)
text = re.sub(r"(?i)(?:/[a-z]/|[a-z]:[/\\])[^ \t\r\n'\";]*?ben-desktop", "<repo>", text)
text = re.sub(r"(?i)[a-z]:[/\\]Users[/\\][^/\\ \t\r\n'\";,)]*", "<home>", text)
text = re.sub(r"(?i)/(?:home|Users)/[^/\\ \t\r\n'\";,)]*", "<home>", text)
path.write_text(text, encoding="utf-8", newline="\n")
PY
}

write_source_record() {
  local ffmpeg_source_sha="$1"
  local mpv_source_sha="$2"
  local mpv_commit="$3"
  local generated_at
  generated_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

  python - "$RUNTIME_DIR" "$FFMPEG_VERSION" "$FFMPEG_SOURCE_URL" "$ffmpeg_source_sha" "$FFMPEG_SOURCE_ARCHIVE" "$MPV_VERSION" "$MPV_SOURCE_URL" "$mpv_source_sha" "$MPV_SOURCE_ARCHIVE" "$mpv_commit" "$generated_at" <<'PY'
import hashlib
import json
import sys
from pathlib import Path

runtime = Path(sys.argv[1])
ffmpeg_version = sys.argv[2]
ffmpeg_url = sys.argv[3]
ffmpeg_sha = sys.argv[4]
ffmpeg_archive = sys.argv[5]
mpv_version = sys.argv[6]
mpv_url = sys.argv[7]
mpv_sha = sys.argv[8]
mpv_archive = sys.argv[9]
mpv_commit = sys.argv[10]
generated_at = sys.argv[11]

def file_info(path: Path, package_path: str) -> dict:
    body = path.read_bytes()
    return {
        "path": package_path.replace("\\", "/"),
        "sha256": hashlib.sha256(body).hexdigest(),
        "size": len(body),
    }

runtime_files = []
for path in sorted((runtime / "ffmpeg").rglob("*")):
    if path.is_file():
        runtime_files.append(file_info(path, "runtime/ffmpeg/" + path.relative_to(runtime / "ffmpeg").as_posix()))
for path in sorted((runtime / "licenses").rglob("*")):
    if path.is_file() and path.name != "media-runtime-source-record.json":
        runtime_files.append(file_info(path, "licenses/media-runtime/" + path.relative_to(runtime / "licenses").as_posix()))
for path in sorted(runtime.glob("*.dll")):
    runtime_files.append(file_info(path, path.name))
version_file = runtime / "version.txt"
if version_file.is_file():
    runtime_files.append(file_info(version_file, "runtime/version.txt"))

produced = [
    item for item in runtime_files
    if item["path"] in {"runtime/ffmpeg/bin/ffmpeg.exe", "runtime/ffmpeg/bin/ffprobe.exe", "libmpv.dll"}
]

record = {
    "schemaVersion": 1,
    "generatedAt": generated_at,
    "generatedBy": "build/deps/windows/build-media-runtime.mingw64.sh",
    "pathSanitization": {
        "workspace": "<repo>",
        "buildWork": "<media-build-work>",
        "runtimeStaging": "<runtime-staging>",
        "home": "<home>"
    },
    "sources": [
        {
            "name": "ffmpeg",
            "version": ffmpeg_version,
            "sourceArchive": {
                "url": ffmpeg_url,
                "releaseAssetName": ffmpeg_archive,
                "sha256": ffmpeg_sha
            },
            "localChanges": {
                "status": "none applied by release build script",
                "diffPath": "licenses/media-runtime/ffmpeg-local-changes.diff"
            },
            "buildRecordPaths": [
                "licenses/media-runtime/ffmpeg-buildconf.txt"
            ]
        },
        {
            "name": "mpv",
            "version": mpv_version,
            "gitTag": "v" + mpv_version,
            "gitCommit": mpv_commit,
            "sourceArchive": {
                "url": mpv_url,
                "releaseAssetName": mpv_archive,
                "sha256": mpv_sha
            },
            "localChanges": {
                "status": "recorded by git diff",
                "diffPath": "licenses/media-runtime/mpv-local-changes.diff"
            },
            "buildRecordPaths": [
                "licenses/media-runtime/mpv-meson-configure.txt"
            ]
        }
    ],
    "producedBinaries": produced,
    "runtimeFiles": runtime_files,
}

target = runtime / "licenses" / "media-runtime-source-record.json"
target.write_text(json.dumps(record, indent=2, sort_keys=True) + "\n", encoding="utf-8")
PY
  sanitize_file "$RUNTIME_DIR/licenses/media-runtime-source-record.json"
}

cd "$WORK_DIR"

if [ ! -f "$FFMPEG_SOURCE_ARCHIVE" ]; then
  curl --retry 10 --retry-delay 5 --retry-all-errors -fL "$FFMPEG_SOURCE_URL" -o "$FFMPEG_SOURCE_ARCHIVE"
fi
FFMPEG_SOURCE_SHA256="$(sha256_file "$FFMPEG_SOURCE_ARCHIVE")"
if [ ! -d "ffmpeg-$FFMPEG_VERSION" ]; then
  tar -xf "$FFMPEG_SOURCE_ARCHIVE"
fi

pushd "ffmpeg-$FFMPEG_VERSION" >/dev/null
./configure \
  --prefix="$WORK_DIR/ffmpeg-prefix" \
  --disable-everything \
  --disable-autodetect \
  --disable-doc \
  --disable-debug \
  --disable-network \
  --enable-small \
  --enable-shared \
  --disable-static \
  --disable-ffplay \
  --enable-ffmpeg \
  --enable-ffprobe \
  --enable-protocol=file,pipe \
  --enable-demuxer=aac,flac,mov,mp3,ogg,wav,image2,webp_pipe \
  --enable-muxer=ipod,mp4,mov,image2,webp,avif,null \
  --enable-parser=aac,flac,mpegaudio,opus,vorbis,mjpeg,png,webp,av1 \
  --enable-decoder=aac,aac_fixed,aac_latm,flac,mp3,mp3float,alac,opus,vorbis,pcm_s16le,pcm_s24le,pcm_s32le,pcm_f32le,pcm_f64le,pcm_u8,pcm_s16be,pcm_s24be,pcm_s32be,mjpeg,png,webp,av1 \
  --enable-encoder=aac,mjpeg,libwebp,libaom_av1 \
  --enable-filter=scale,crop,aresample \
  --enable-swscale \
  --enable-swresample \
  --enable-zlib \
  --enable-libwebp \
  --enable-libaom
make -j"$(nproc)"
make install
PATH="$WORK_DIR/ffmpeg-prefix/bin:$PATH" "$WORK_DIR/ffmpeg-prefix/bin/ffmpeg.exe" -buildconf > "$RUNTIME_DIR/licenses/ffmpeg-buildconf.txt"
sanitize_file "$RUNTIME_DIR/licenses/ffmpeg-buildconf.txt"
popd >/dev/null
: > "$RUNTIME_DIR/licenses/ffmpeg-local-changes.diff"

cp -f "$WORK_DIR/ffmpeg-prefix/bin/ffmpeg.exe" "$RUNTIME_DIR/ffmpeg/bin/ffmpeg.exe"
cp -f "$WORK_DIR/ffmpeg-prefix/bin/ffprobe.exe" "$RUNTIME_DIR/ffmpeg/bin/ffprobe.exe"
cp -f "$WORK_DIR/ffmpeg-prefix/bin/"*.dll "$RUNTIME_DIR/ffmpeg/bin/" 2>/dev/null || true
cp -f "$WORK_DIR/ffmpeg-prefix/bin/"*.dll "$RUNTIME_DIR/" 2>/dev/null || true
copy_mingw_dll_deps "$RUNTIME_DIR/ffmpeg/bin/ffmpeg.exe" "$RUNTIME_DIR/ffmpeg/bin"
copy_mingw_dll_deps "$RUNTIME_DIR/ffmpeg/bin/ffprobe.exe" "$RUNTIME_DIR/ffmpeg/bin"
validate_runtime_deps "$RUNTIME_DIR/ffmpeg/bin/ffmpeg.exe" "$RUNTIME_DIR/ffmpeg/bin"
validate_runtime_deps "$RUNTIME_DIR/ffmpeg/bin/ffprobe.exe" "$RUNTIME_DIR/ffmpeg/bin"

if [ ! -f "$MPV_SOURCE_ARCHIVE" ]; then
  curl --retry 10 --retry-delay 5 --retry-all-errors -fL "$MPV_SOURCE_URL" -o "$MPV_SOURCE_ARCHIVE"
fi
MPV_SOURCE_SHA256="$(sha256_file "$MPV_SOURCE_ARCHIVE")"

if [ ! -d "mpv" ]; then
  git clone --depth 1 --branch "v$MPV_VERSION" https://github.com/mpv-player/mpv.git
else
  git -C mpv fetch --depth 1 origin "v$MPV_VERSION" || true
  git -C mpv checkout -f "v$MPV_VERSION"
  git -C mpv reset --hard "HEAD"
fi
MPV_COMMIT="$(git -C mpv rev-parse HEAD)"

pushd mpv >/dev/null
export PKG_CONFIG_PATH="$WORK_DIR/ffmpeg-prefix/lib/pkgconfig:${PKG_CONFIG_PATH:-}"
export PATH="$WORK_DIR/ffmpeg-prefix/bin:$PATH"
MPV_MESON_OPTIONS=(
  -Dlibmpv=true
  -Ddefault_library=shared
  -Dgpl=false
  -Drubberband=disabled
  -Djavascript=disabled
  -Dlua=disabled
  -Dlibarchive=disabled
  -Dlibbluray=disabled
  -Dvapoursynth=disabled
  -Duchardet=disabled
  -Dlcms2=disabled
  -Djpeg=disabled
  -Dzimg=disabled
  -Dvaapi=disabled
  -Dd3d11=disabled
  -Dgl=disabled
  -Dplain-gl=disabled
  -Dshaderc=disabled
  -Dspirv-cross=disabled
)
if [ -d build ]; then
  meson setup build --wipe "${MPV_MESON_OPTIONS[@]}"
else
  meson setup build "${MPV_MESON_OPTIONS[@]}"
fi
meson configure build > "$RUNTIME_DIR/licenses/mpv-meson-configure.txt"
sanitize_file "$RUNTIME_DIR/licenses/mpv-meson-configure.txt"
ninja -C build libmpv-2.dll || ninja -C build
git diff -- . > "$RUNTIME_DIR/licenses/mpv-local-changes.diff"
sanitize_file "$RUNTIME_DIR/licenses/mpv-local-changes.diff"

LIBMPV_DLL="$(find build -name 'libmpv-*.dll' -o -name 'libmpv.dll' | head -n 1)"
if [ -z "$LIBMPV_DLL" ]; then
  echo "libmpv DLL was not produced" >&2
  exit 1
fi
cp -f "$LIBMPV_DLL" "$RUNTIME_DIR/libmpv.dll"
copy_mingw_dll_deps "$RUNTIME_DIR/libmpv.dll" "$RUNTIME_DIR"
validate_runtime_deps "$RUNTIME_DIR/libmpv.dll" "$RUNTIME_DIR:$RUNTIME_DIR/ffmpeg/bin"

# Stage libmpv public headers so CGO-based tooling (e.g. wails3 generate bindings)
# can find <mpv/client.h> without a system mpv development package.
MPV_HEADER_DIR=""
if [ -d "include/mpv" ]; then
  MPV_HEADER_DIR="include/mpv"
elif [ -d "libmpv" ]; then
  MPV_HEADER_DIR="libmpv"
fi
if [ -z "$MPV_HEADER_DIR" ]; then
  echo "libmpv public headers not found (tried include/mpv and libmpv)" >&2
  exit 1
fi
mkdir -p "$WORK_DIR/mpv-include/mpv"
cp -f "$MPV_HEADER_DIR"/*.h "$WORK_DIR/mpv-include/mpv/"

popd >/dev/null

cat > "$RUNTIME_DIR/licenses/media-runtime-build.txt" <<EOF
FFmpeg: $FFMPEG_VERSION
FFmpeg source archive: $FFMPEG_SOURCE_URL
FFmpeg source archive SHA-256: $FFMPEG_SOURCE_SHA256
mpv: $MPV_VERSION
mpv source archive: $MPV_SOURCE_URL
mpv source archive SHA-256: $MPV_SOURCE_SHA256
mpv commit: $MPV_COMMIT
Built with: MSYS2 MINGW64
Generated by: build/deps/windows/build-media-runtime.mingw64.sh
Source record: media-runtime-source-record.json
EOF

{
  echo ""
  echo "Copied DLLs:"
  for dll in "$RUNTIME_DIR"/*.dll; do
    [ -e "$dll" ] || continue
    basename "$dll"
  done | sort
  echo ""
  echo "FFmpeg bin DLLs:"
  for dll in "$RUNTIME_DIR/ffmpeg/bin"/*.dll; do
    [ -e "$dll" ] || continue
    basename "$dll"
  done | sort
} >> "$RUNTIME_DIR/licenses/media-runtime-build.txt"
sanitize_file "$RUNTIME_DIR/licenses/media-runtime-build.txt"

write_source_record "$FFMPEG_SOURCE_SHA256" "$MPV_SOURCE_SHA256" "$MPV_COMMIT"

echo "Media runtime staged in $RUNTIME_DIR"
