# Changelog

All notable changes to this project are recorded here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Entries are written at release time. Pull requests should not edit this file.

## [Unreleased]

Nothing yet.

## [0.1.0] - 2026-07-31

### Added

- Shared inventory vocabulary in `internal/model`: findings, kinds, scopes,
  reach, catalogue matches, gaps, scan errors and drift entries.
- Terminal and JSON renderers. Both always print what the run did not look at.
- Detectors for model context protocol servers, installed packages, browser
  extensions and instruction files.
- Local drift baseline at `$HOME/.config/agentsurface/baseline.json`, holding
  hashes only, disabled with `--no-baseline`.
- Repository documentation: verification steps, per-detector coverage and
  limits, and the output shapes.
- Continuous integration on Linux, macOS and Windows.
- A no-network check that fails the build if the dependency graph or the
  compiled binary acquires the ability to make network calls.
- Release pipeline producing macOS and Linux binaries with checksums, a
  cosign signature, an SBOM and a build provenance attestation.

### Notes

- No release has been published yet, so there are no version headings below
  this one.

[Unreleased]: https://github.com/Northbeams-Labs/agentsurface/commits/main
