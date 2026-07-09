# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Nothing yet.

## [0.1.0] - TBD

First public release of `workstation-probe`. The functionality is sufficient for
day-to-day use, but the API and configuration schema are **not yet frozen** —
breaking changes between `0.x` releases are expected.

### Added

- CPU metrics: per-core usage, frequency, load average, uptime.
- Memory metrics: total / used / cached / swap.
- GPU metrics: stub collector for systems without NVML; NVML-backed collector
  gated by the `nvml` build tag (power, temperature, utilization, memory).
- Storage metrics: per-mount point usage and I/O counters where available.
- Unified `/metrics`, `/profile`, and `/health` JSON endpoints.
- CORS origin parser with allow / deny / preflight middleware.
- Request-id, access-log, recovery, and token-bucket rate-limit middleware.
- Hugo webview module with snapshot test fixture.
- `contrib/systemd` unit for installing workstation-probe as a service.

### Changed

- N/A — this is the first release.

### Deprecated

- N/A.

### Removed

- N/A.

### Fixed

- N/A.

### Security

- N/A.

### Known limitations

- The `nvml` build tag requires `libnvidia-ml.so` at runtime.
- Configuration schema will change in `0.2.x`; do not pin to `0.1.0` for long.

[Unreleased]: https://github.com/assaneko/workstation-probe/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/assaneko/workstation-probe/releases/tag/v0.1.0
