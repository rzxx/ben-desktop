# Third-Party Notices

This project is distributed under GPL-3.0-or-later. Binary releases also include third-party components with their own licenses. This file summarizes the components that are intentionally bundled or directly required by release builds.

For exact source URLs, versions, checksums, and build flags for bundled media binaries, see `docs/dependency-sources.md` and `build/deps/manifest.json`.

## FFmpeg and FFprobe

- Project: FFmpeg
- Release target: 8.1.1
- License target for bundled binaries: LGPL-2.1-or-later
- Upstream: <https://ffmpeg.org/>
- Source: <https://ffmpeg.org/releases/>

The bundled FFmpeg and FFprobe binaries must be built without `--enable-gpl` and without `--enable-nonfree`. Release artifacts must include the exact FFmpeg configure line and `ffmpeg -buildconf` output.

## mpv / libmpv

- Project: mpv / libmpv
- Release target: 0.41.0
- License target for bundled libmpv build: LGPL-2.1-or-later where built with `-Dgpl=false`
- Upstream: <https://mpv.io/>
- Source: <https://github.com/mpv-player/mpv>

The app uses libmpv for audio playback. Release builds must include libmpv and the non-system runtime libraries needed by libmpv.

## go-mpv

- Project: github.com/gen2brain/go-mpv
- Version: v0.2.3
- License: GPL-3.0
- Source: <https://github.com/gen2brain/go-mpv>

This dependency is why the app itself is licensed as GPL-3.0-or-later for the current release path.

## libwebp

- Project: libwebp
- License: BSD-style license
- Upstream: <https://chromium.googlesource.com/webm/libwebp>

Used by the bundled FFmpeg build for WebP artwork output.

## libaom

- Project: Alliance for Open Media AV1 codec
- License: BSD 2-Clause
- Upstream: <https://aomedia.googlesource.com/aom/>

Used by the bundled FFmpeg build for AVIF artwork output.

## zlib

- Project: zlib
- License: zlib license
- Upstream: <https://zlib.net/>

Used by the bundled media runtime where required by the selected FFmpeg/libmpv build.

## Additional Windows media-runtime DLLs

The Windows libmpv runtime may also include LGPL/permissive support libraries copied from the MSYS2 MINGW64 dependency closure, including libass, fontconfig, FreeType, FriBidi, HarfBuzz, GLib, libplacebo, libdovi, shaderc, SPIRV-Cross, libpng, libiconv, gettext/libintl, brotli, bzip2, PCRE2, graphite2, libunibreak, Vulkan loader, GCC runtime, and winpthreads.

The exact copied DLL set must be recorded for each release in `build/deps/manifest.json` or the generated release build record.

## TagLib / go-taglib

- Project: go.senan.xyz/taglib
- License: LGPL-2.1
- Source: <https://github.com/sentriz/go-taglib>

Used for audio metadata reading.

## Wails

- Project: Wails v3
- License: MIT
- Source: <https://github.com/wailsapp/wails>

Used as the native desktop application framework.

## Microsoft Edge WebView2 Runtime

Windows builds use the Wails WebView2 bootstrapper path for the embedded browser runtime when needed. WebView2 is provided by Microsoft under Microsoft's runtime terms.

## Go, JavaScript, and Other Dependencies

The app also uses Go modules and frontend packages listed in `go.mod`, `go.sum`, `frontend/package.json`, and `frontend/bun.lock`. Release audits should generate a dependency inventory from those lockfiles before publishing.
