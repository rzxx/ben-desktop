# Distribution and Media Dependency Plan

This document is the implementation strategy for distributing this Wails v3 desktop app without requiring users to install FFmpeg, FFprobe, or libmpv themselves, while keeping the project license posture explicit and reproducible.

This is engineering guidance, not legal advice. Before publishing signed release artifacts, do one final legal review of the selected project license and the bundled dependency notices.

## Goals

- Ship an installer or downloadable package that works on a clean user machine.
- Bundle the media runtime needed by the app: FFmpeg, FFprobe, libmpv, and libmpv's non-system runtime libraries.
- Avoid redistributing broad third-party binary builds such as "full" FFmpeg or generic mpv packages.
- Keep the distributed media stack as small as practical by building only the features this app uses.
- Keep the app and every bundled binary legally redistributable.
- Put all required license, notice, source, build configuration, and attribution information in the repo and release artifacts.

## Current State

The app is a Wails v3 app using `github.com/wailsapp/wails/v3`.

The media pipeline currently depends on external command-line binaries:

- `internal/desktopcore/service_transcode.go` shells out to `ffmpeg` to create AAC/M4A files.
- `internal/desktopcore/service_artwork.go` shells out to `ffmpeg` to extract embedded artwork and render JPG, WebP, and AVIF artwork variants.
- `internal/desktopcore/inspect_health.go` uses `ffprobe` for metadata and `ffmpeg` for decode health checks.
- `internal/desktopcore/config.go` defaults an empty FFmpeg path to `ffmpeg`, so release builds currently depend on `PATH`.

Playback uses libmpv:

- `internal/playback/backend_mpv.go` imports `github.com/gen2brain/go-mpv`.
- `internal/playback/backend_stub.go` is only used with the `nompv` build tag.
- Release builds must not use `nompv`; users need real playback.
- Current Windows packaging only copies `build/windows/runtime/libmpv.dll` to `bin/libmpv.dll` if that file exists. It does not bundle FFmpeg, FFprobe, libmpv dependency DLLs, or notices.

The repo currently has no root `LICENSE`, `NOTICE`, or third-party notices file. Build metadata is still placeholder in several files:

- `build/config.yml`
- `build/windows/info.json`
- `build/darwin/Info.plist`
- `build/linux/nfpm/nfpm.yaml`

The installed local `ffmpeg` detected during investigation is a broad Gyan full build with GPL and many extra libraries enabled. That is acceptable as a local development tool, but it should not be used as a release redistributable for this project.

## Core Decisions

### 1. Use GPL-3.0-or-later for the app unless the mpv wrapper is replaced first

The current Go mpv wrapper, `github.com/gen2brain/go-mpv v0.2.3`, is GPL-3.0 licensed. Because the app imports it directly, the safest immediate release license for this project is:

```text
GPL-3.0-or-later
```

This is compatible with the user's stated direction that the project is open source and will remain open source.

If the project owner wants a permissive project license such as MIT or Apache-2.0, do not ship the current code first. Replace `github.com/gen2brain/go-mpv` with an in-repo libmpv binding or another compatible wrapper, then re-audit the full dependency tree.

### 2. Build FFmpeg and libmpv ourselves for release

Do not redistribute generic prebuilt FFmpeg or mpv packages for the main release line. Build pinned, documented, reproducible release binaries instead.

This gives us:

- Smaller binaries.
- A known feature set.
- A known license set.
- Exact source and build configuration for release notices.
- No accidental GPL or nonfree FFmpeg components.

The first pinned versions should be:

- FFmpeg `8.1.1` unless implementation testing exposes a regression.
- mpv `0.41.0` unless implementation testing exposes a regression.

Both version pins must live in a checked-in manifest with source URLs, checksums, configure options, build host details, and produced binary hashes.

### 3. Build FFmpeg in LGPL mode

The FFmpeg binary must be built without:

- `--enable-gpl`
- `--enable-nonfree`
- GPL-only libraries such as `libx264`, `libx265`, `libxvid`, `frei0r`, `rubberband`, or `libcdio`
- Nonfree libraries such as `libfdk-aac`

Use LGPL-compatible components only. The needed external libraries are currently:

- `libwebp` for WebP artwork output.
- `libaom` for AVIF artwork output.
- `zlib` if needed by PNG/image handling in the selected FFmpeg build.

The app itself can be GPL-3.0-or-later, but keeping FFmpeg LGPL reduces compliance risk and keeps the option open to reuse the runtime in other contexts later.

### 4. Build libmpv in LGPL mode where possible

mpv is GPL by default, but can be built as LGPL for libmpv use when GPL-only components are disabled. The release libmpv build should use:

```sh
meson setup build -Dlibmpv=true -Ddefault_library=shared -Dgpl=false
ninja -C build libmpv
```

During implementation, run `meson configure build` and lock the exact enabled/disabled option set in the dependency manifest. The important rule is that libmpv must not link to a GPL FFmpeg build or other GPL-only libraries if we are claiming LGPL-mode libmpv.

### 5. Keep FFmpeg as bundled sidecar processes for the first distribution milestone

Do not switch to libav bindings for the first milestone.

The current FFmpeg usage is process-oriented, stable, and easy to isolate. Moving to direct bindings would add CGO, ABI, cross-platform loader, LGPL relinking, and crash-isolation complexity while not removing the need to ship and credit FFmpeg-derived libraries.

The right near-term fix is a reliable packaged binary resolver:

- Prefer bundled `ffmpeg` and `ffprobe`.
- Fall back to explicit config or `PATH` only for development and diagnostics.
- Log the resolved binary paths.
- Fail with an actionable internal error if a release package is missing its bundled binaries.

### 6. Ship Windows first with NSIS

Wails v3 already supports Windows NSIS packaging through `wails3 package GOOS=windows`. NSIS is the correct first user-facing Windows distribution format.

Do not make MSIX the first milestone. MSIX requires more publisher identity, signing, and packaging decisions. It can be added after the NSIS release path is correct.

## FFmpeg Build Plan

### Required app behavior

FFmpeg must support these app workflows:

- Transcode supported audio files to AAC in an M4A/MP4-family container.
- Extract embedded cover art from audio files.
- Render artwork variants:
  - JPG thumbnail.
  - WebP medium image.
  - AVIF large image.
- Probe duration and stream metadata with FFprobe.
- Decode audio files to `null` output for health checks while emitting progress to stdout.

The currently scanned audio extensions are:

- `.aac`
- `.flac`
- `.m4a`
- `.mp3`
- `.ogg`
- `.opus`
- `.wav`

### Candidate FFmpeg configure profile

Start from this candidate profile, then adjust only when smoke tests prove a missing component:

```sh
./configure \
  --prefix="$PREFIX" \
  --disable-everything \
  --disable-autodetect \
  --disable-doc \
  --disable-debug \
  --disable-network \
  --enable-small \
  --disable-ffplay \
  --enable-ffmpeg \
  --enable-ffprobe \
  --enable-protocol=file,pipe \
  --enable-demuxer=aac,flac,mov,mp3,ogg,wav,image2,webp_pipe \
  --enable-muxer=ipod,mp4,mov,image2,webp,avif,null \
  --enable-parser=aac,flac,mpegaudio,opus,vorbis,mjpeg,png,webp,av1 \
  --enable-decoder=aac,aac_fixed,aac_latm,flac,mp3,mp3float,alac,opus,vorbis,pcm_s16le,pcm_s24le,pcm_s32le,pcm_f32le,pcm_f64le,pcm_u8,pcm_s16be,pcm_s24be,pcm_s32be,mjpeg,png,webp,av1 \
  --enable-encoder=aac,mjpeg,libwebp,libaom_av1 \
  --enable-filter=scale,crop \
  --enable-swscale \
  --enable-swresample \
  --enable-zlib \
  --enable-libwebp \
  --enable-libaom
```

Notes:

- Include the `ipod` muxer because FFmpeg commonly maps `.m4a` output through the iPod/M4A muxer path.
- Include the `null` muxer because the health checker decodes to `-f null -`.
- Include `pipe` because the health checker reads progress through `pipe:1`.
- Do not include network protocols.
- Do not include `ffplay`.
- Do not enable GPL or nonfree options.
- If AVIF output makes the FFmpeg binary too large, the fallback is to switch the app's large artwork format from AVIF to WebP or JPEG. That is a product/quality tradeoff, not a licensing blocker.

### FFmpeg smoke tests

Before accepting the build profile, add automated smoke tests or release scripts that verify:

- `ffmpeg -buildconf` does not contain `--enable-gpl` or `--enable-nonfree`.
- `ffmpeg -version` reports the pinned FFmpeg version.
- `ffmpeg -encoders` includes `aac`, `mjpeg`, `libwebp`, and `libaom_av1`.
- `ffmpeg -decoders` includes every supported audio format and artwork input format.
- `ffmpeg -formats`, `ffmpeg -muxers`, `ffmpeg -demuxers`, `ffmpeg -filters`, and `ffmpeg -protocols` include the allowlisted components.
- Each supported audio fixture can be transcoded to M4A AAC.
- Embedded JPG and PNG cover art can be extracted from fixtures.
- JPG, PNG, and WebP source images can render to JPG, WebP, and AVIF variants.
- `ffprobe` can return duration for each supported audio fixture.
- The health-check command can decode each supported fixture to `-f null -` with progress output.

The release process must archive the exact `ffmpeg -buildconf` output.

## libmpv Build Plan

### Required app behavior

libmpv is used only for audio playback.

The app already sets these runtime options:

- `terminal=no`
- `video=no`
- `audio-display=no`
- `gapless-audio=yes`
- `prefetch-playlist=yes`

The release build still needs libmpv's normal audio demuxing and decoding stack, but it does not need video display support.

### Windows build path

Use MSYS2 MINGW64 for the first Windows build path because it is also the environment required by this repo's race-test instructions.

Implementation should create a checked-in build script under a path such as:

```text
build/deps/windows/build-media-runtime.ps1
build/deps/windows/build-media-runtime.mingw64.sh
```

The PowerShell script should drive setup and artifact copying. The MINGW64 shell script should build the actual C/C++ dependencies.

The mpv build should:

- Pin mpv by tag.
- Build shared libmpv.
- Use `-Dgpl=false`.
- Link to the project's LGPL FFmpeg build, or to a separately built LGPL FFmpeg library set with the same manifest discipline.
- Disable video/display features where mpv's Meson options allow that without breaking audio playback.
- Copy `libmpv.dll` and every non-system dependency DLL into the release runtime directory.

With the current `go-mpv` package, Windows must place `libmpv.dll` where `LoadLibrary("libmpv.dll")` can find it before Go package initialization finishes. The most reliable immediate layout is next to the app executable:

```text
ben-desktop.exe
libmpv.dll
<libmpv dependency DLLs>
runtime/ffmpeg/bin/ffmpeg.exe
runtime/ffmpeg/bin/ffprobe.exe
licenses/
```

A better follow-up is to replace `github.com/gen2brain/go-mpv` with a small in-repo wrapper that loads libmpv from an explicit path. That would:

- Remove the GPL-3.0 wrapper dependency.
- Allow `runtime/mpv/bin/libmpv.dll` instead of polluting the executable directory.
- Make missing dependency errors easier to report.
- Reopen a permissive-license option for the app if desired.

That wrapper replacement is useful, but it is not required for the first GPL-3.0-or-later release.

### libmpv smoke tests

Before accepting the package:

- Start the app on a clean machine with `PATH` scrubbed of FFmpeg and mpv.
- Confirm playback initializes without the `nompv` tag.
- Play fixtures for each supported audio extension.
- Seek, pause, resume, change volume, and move between tracks.
- Confirm no video window or album-art video display is opened by mpv.
- Confirm dependency closure with a DLL inspection tool such as `ntldd`, `objdump -p`, `dumpbin`, or a dedicated packaging script.
- Confirm the app does not load `libmpv.dll` from the system or MSYS2 path.

## Runtime Integration Plan

Add a small media runtime resolver in Go, for example:

```text
internal/mediaexec/
```

It should expose:

- Resolved FFmpeg path.
- Resolved FFprobe path.
- Whether the path came from the packaged runtime, explicit user config, environment override, or `PATH`.
- A validation method used by diagnostics and startup health checks.

Resolution order:

1. Environment override for development, such as `BEN_DESKTOP_FFMPEG` and `BEN_DESKTOP_FFPROBE`.
2. Explicit user or CLI configuration, if kept.
3. Packaged app runtime next to the executable.
4. `PATH` fallback in development builds.

For release builds, the packaged runtime should be the default and expected path. A release package that silently depends on the user's `PATH` should be treated as broken.

Code changes:

- Stop defaulting `Config.FFmpegPath` to plain `ffmpeg`.
- Route `service_transcode.go`, `service_artwork.go`, and `inspect_health.go` through the resolver.
- Make `ffprobe` resolution explicit instead of deriving it only by replacing the FFmpeg executable name.
- Log resolved binary locations at startup or first use.
- Keep manual FFmpeg configuration only as an advanced/developer override.

## Packaging Plan

### Windows NSIS milestone

Use Wails v3's existing Windows NSIS package path first:

```sh
wails3 package GOOS=windows
```

Update the Windows task and NSIS script so the installer includes:

- `ben-desktop.exe`
- `libmpv.dll`
- libmpv dependency DLLs
- `runtime/ffmpeg/bin/ffmpeg.exe`
- `runtime/ffmpeg/bin/ffprobe.exe`
- license and notice files
- dependency build manifest

Update metadata before release:

- `build/config.yml`
- `build/windows/info.json`
- `build/windows/nsis/project.nsi`

The installer should not require users to install FFmpeg, mpv, MSYS2, or any codec package. WebView2 handling should remain Wails' normal Windows behavior.

### Portable Windows ZIP

After NSIS works, produce a portable ZIP from the same staged package directory.

The ZIP layout should match the installed directory layout so both package types exercise the same runtime resolver.

### macOS follow-up

For macOS, bundle dependencies inside the `.app`:

```text
Ben.app/Contents/MacOS/<app executable>
Ben.app/Contents/Frameworks/<dylibs>
Ben.app/Contents/Resources/runtime/ffmpeg/bin/ffmpeg
Ben.app/Contents/Resources/runtime/ffmpeg/bin/ffprobe
Ben.app/Contents/Resources/licenses/
```

The macOS packaging task must fix install names and rpaths before signing/notarization.

### Linux follow-up

For the "download and use" Linux path, prefer an AppImage that bundles the media runtime:

```text
AppDir/usr/bin/<app executable>
AppDir/usr/lib/ben-desktop/runtime/ffmpeg/bin/ffmpeg
AppDir/usr/lib/ben-desktop/runtime/ffmpeg/bin/ffprobe
AppDir/usr/lib/ben-desktop/runtime/mpv/
AppDir/usr/share/doc/ben-desktop/
```

For `.deb` and `.rpm`, choose deliberately between:

- Bundling the same runtime for consistency.
- Depending on distro packages for FFmpeg/libmpv.

The user's stated goal is no dependency setup for users, so the first Linux binary download should bundle.

## License and Notice Work

### Repository files to add

Add these files before the first release:

```text
LICENSE
THIRD_PARTY_NOTICES.md
docs/dependency-sources.md
build/deps/manifest.json
```

Recommended contents:

- `LICENSE`: GPL-3.0-or-later for this app, unless the mpv wrapper is replaced before release.
- `THIRD_PARTY_NOTICES.md`: human-readable notices for FFmpeg, mpv/libmpv, go-mpv, Wails, WebView2 runtime notes, TagLib/go-taglib, libwebp, libaom, zlib, and other bundled runtime components.
- `docs/dependency-sources.md`: source URLs, source archive checksums, build commands, configure output locations, patches, and release artifact locations.
- `build/deps/manifest.json`: machine-readable dependency inventory used by package scripts.

### Release artifact obligations

For each binary release:

- Publish the app source for the exact release tag.
- Publish or link the exact FFmpeg source used.
- Publish or link the exact mpv source used.
- Publish source and license text for bundled external libraries such as libwebp, libaom, and zlib.
- Include the exact FFmpeg configure line and `ffmpeg -buildconf` output.
- Include the exact mpv Meson options.
- Include any local patches.
- Include checksums for source archives and built binaries.
- Mention FFmpeg and mpv/libmpv in the release notes or download page.
- Include notices in the installed app directory and portable ZIP.

### In-app legal surface

Add an About or Legal dialog that includes:

- App license.
- FFmpeg notice and source link.
- mpv/libmpv notice and source link.
- Third-party notices link or viewer.

Do not add an EULA that restricts reverse engineering or redistribution in a way that conflicts with GPL/LGPL obligations.

## Build and Release Verification

A release candidate is not done until these pass:

```sh
goimports -w .
golangci-lint run ./...
go test ./...
govulncheck ./...
```

From `frontend/`:

```sh
bun format
bun lint
bun typecheck
bun run test:run
```

For race tests on Windows, use the MSYS2 MINGW64 terminal with libmpv installed:

```sh
CGO_ENABLED=1 CC=gcc go test -race ./...
```

Additional release checks:

- Build the media runtime from clean source pins.
- Verify FFmpeg has no GPL or nonfree flags.
- Verify libmpv was built with `-Dgpl=false` if claiming LGPL-mode libmpv.
- Verify package contents include every required DLL/executable and notice file.
- Install on a clean Windows VM.
- Remove FFmpeg, FFprobe, mpv, and MSYS2 from `PATH`.
- Start the app.
- Scan a fixture library.
- Generate artwork.
- Transcode audio.
- Run health inspection.
- Play audio through libmpv.
- Uninstall cleanly.

## Implementation Order

1. Choose and add the app license.
2. Add third-party notice and dependency manifest skeletons.
3. Build the minimal LGPL FFmpeg/FFprobe runtime and lock the build profile.
4. Add FFmpeg smoke fixtures and release validation scripts.
5. Build the LGPL-mode libmpv runtime and dependency DLL closure.
6. Add libmpv smoke tests.
7. Implement the Go media runtime resolver.
8. Update Wails Windows packaging and NSIS script to include runtime files and notices.
9. Fix app metadata in Wails build config files.
10. Build and test a Windows NSIS installer on a clean machine.
11. Add portable ZIP packaging from the same staged runtime directory.
12. Add macOS and Linux packaging after Windows is proven.

## Primary Sources

- FFmpeg legal checklist: <https://www.ffmpeg.org/legal.html>
- FFmpeg license notes: <https://github.com/FFmpeg/FFmpeg/blob/master/LICENSE.md>
- FFmpeg downloads and releases: <https://ffmpeg.org/download.html>
- mpv copyright and LGPL build notes: <https://raw.githubusercontent.com/mpv-player/mpv/master/Copyright>
- mpv README: <https://raw.githubusercontent.com/mpv-player/mpv/master/README.md>
- mpv Windows build docs: <https://raw.githubusercontent.com/mpv-player/mpv/master/DOCS/compile-windows.md>
- mpv installation notes: <https://mpv.io/installation/>
- go-mpv license: <https://raw.githubusercontent.com/gen2brain/go-mpv/v0.2.3/LICENSE>
- go-mpv README: <https://raw.githubusercontent.com/gen2brain/go-mpv/v0.2.3/README.md>
- Wails v3 build docs: <https://v3.wails.io/guides/build/building/>
- Wails v3 Windows packaging docs: <https://v3.wails.io/guides/build/windows/>
- Wails v3 macOS packaging docs: <https://v3.wails.io/guides/build/macos/>
- Wails v3 Linux packaging docs: <https://v3.wails.io/guides/build/linux/>
- Wails v3 cross-platform build docs: <https://v3.wails.io/guides/build/cross-platform/>
- libwebp license: <https://raw.githubusercontent.com/webmproject/libwebp/main/COPYING>
- libaom license: <https://aomedia.googlesource.com/aom/+/refs/heads/main/LICENSE>
- zlib license: <https://zlib.net/zlib_license.html>
