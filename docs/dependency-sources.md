# Dependency Sources and Release Build Records

This file records the source and build requirements for bundled media dependencies. Every binary release must update this file or attach an equivalent generated build record to the release.

## Policy

- Do not ship broad third-party FFmpeg or mpv binary packages as the official release runtime.
- Build release media binaries from pinned source versions.
- Keep FFmpeg in LGPL mode by omitting `--enable-gpl` and `--enable-nonfree`.
- Keep libmpv in LGPL mode where possible by using `-Dgpl=false` and avoiding GPL-only linked libraries.
- Include exact sources, local patches, build commands, configure output, and checksums with each binary release.

## FFmpeg

- Target version: 8.1.1
- Source homepage: <https://ffmpeg.org/>
- Source archives: <https://ffmpeg.org/releases/>
- License target: LGPL-2.1-or-later
- Bundled programs: `ffmpeg`, `ffprobe`
- Excluded program: `ffplay`

Required release record:

```text
version:
source_archive_url:
source_archive_sha256:
local_patches:
configure_command:
ffmpeg_buildconf_output:
ffmpeg_binary_sha256:
ffprobe_binary_sha256:
```

The configure profile must not contain `--enable-gpl` or `--enable-nonfree`.

## mpv / libmpv

- Target version: 0.41.0
- Source homepage: <https://mpv.io/>
- Source repository: <https://github.com/mpv-player/mpv>
- License target: LGPL-2.1-or-later for libmpv builds created with `-Dgpl=false`

Required release record:

```text
version:
source_archive_url:
source_archive_sha256:
local_patches:
meson_setup_command:
meson_configure_output:
libmpv_binary_sha256:
copied_dependency_dlls:
```

The libmpv release runtime must include every non-system dependency DLL or shared library required to start playback on a clean machine.

## External Media Libraries

The first planned FFmpeg profile uses:

- libwebp for WebP artwork output.
- libaom for AVIF artwork output.
- zlib where required by FFmpeg image handling.

If the actual build uses additional libraries, update `THIRD_PARTY_NOTICES.md`, this file, and `build/deps/manifest.json` before publishing.

The Windows libmpv dependency closure currently includes additional MSYS2 MINGW64 DLLs such as libass, fontconfig, FreeType, FriBidi, HarfBuzz, GLib, libplacebo, libdovi, shaderc, SPIRV-Cross, libpng, libiconv, gettext/libintl, brotli, bzip2, PCRE2, graphite2, libunibreak, Vulkan loader, GCC runtime, and winpthreads. The exact DLL list is generated from `ntldd -R` by `build/deps/windows/build-media-runtime.mingw64.sh` and must be reviewed for every release.

## Release Checklist

Before publishing a binary release:

1. Attach the app source archive for the exact tag.
2. Attach or link the exact FFmpeg and mpv source archives.
3. Attach local patches, if any.
4. Attach FFmpeg `-buildconf` output.
5. Attach mpv Meson configuration output.
6. Attach checksums for source archives and produced binaries.
7. Confirm the installer and portable ZIP include `LICENSE`, `THIRD_PARTY_NOTICES.md`, this file, and `build/deps/manifest.json`.
