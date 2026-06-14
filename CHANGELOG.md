# Changelog

GitHub Releases are the source of truth for per-version release notes. The
release workflow publishes generated notes for each `v*` tag and attaches the
signed updater assets, `SHA256SUMS`, and runtime manifest.

## Unreleased

- Added signed Wails self-updates from GitHub Releases.
- Added Windows media runtime update support for ffmpeg, libmpv, codec DLLs,
  runtime licenses, and dependency manifests.
- Added sanitized media runtime source records and source archive attachments
  for release provenance.
