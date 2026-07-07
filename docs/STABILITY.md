# Stability Acceptance — 0.1.0

The `release/0.1.0` branch exists to converge on a stable, releasable 0.1.0 build.
Nothing new lands here — only bug fixes, tightening of acceptance, and docs that
match what we actually ship.

This document defines "done" for the stability effort.

## Exit criteria

The branch is considered stable when **every** item below holds for at least one
72-hour soak run on real hardware, plus the targeted regression suites pass on CI.

### Functional must-pass

- [ ] `make test` is green (race detector, both build tags).
- [ ] `make vet` is green.
- [ ] `make lint` is green.
- [ ] `make hugo-snapshot` is green (`webview/hugo/exampleSite`).
- [ ] `make build` and `make build-nvml` both succeed.
- [ ] Smoke (`scripts/smoke.sh`) against a direct-run instance returns 200 for
      every public route under both `/metrics` and `/health`.
- [ ] `scripts/stability-check.sh` exits 0 (see "Soak" below).

### Soak (runtime)

`scripts/stability-check.sh 72h` (configurable via `STABILITY_DURATION`) must:

- [ ] Hold the process RSS delta under **+10 %** of its value at minute 1.
- [ ] Keep `p99 sample latency` for `/metrics` under **50 ms** for the whole run.
- [ ] Produce **zero** non-recovery goroutine count drift
      (`runtime.NumGoroutine()` stable ±2 over 10-minute windows).
- [ ] Survive two intentional `kill -HUP` reload cycles without losing
      in-flight requests.
- [ ] Resume cleanly after `kill -STOP` / `kill -CONT` without leaking fds.
- [ ] Record no `DATA RACE` lines from `-race` builds.

### Operational

- [ ] `systemd` unit (`contrib/systemd/monitor.service`) starts and stops cleanly
      under `make live-test`.
- [ ] Logs are structured JSON (slog) and rotate without losing sample data.
- [ ] `config.yaml` schema validates; bad config refuses to start with a
      useful error.

### Documentation

- [ ] `README.md` documents `--version`, log format, and the config schema.
- [ ] `CHANGELOG.md` has the **0.1.0** entry listing the closed issues.

## How to drive the soak

```bash
# Quick local check (5 minutes). Use during development.
make stability-check STABILITY_DURATION=5m

# Full 72-hour run on dedicated hardware or a VM.
make stability-check STABILITY_DURATION=72h
```

The script writes to `build/stability/<timestamp>/`:

- `metrics.log` — sampler output as JSON lines, one record per minute.
- `goroutine.log` — `runtime.NumGoroutine()` over time.
- `summary.json` — pass / fail per acceptance item.

## Merging strategy

- All fixes land as PRs into `release/0.1.0` with the
  `stability` label and `[no-ff]` merge strategy.
- After merging into `main`, the `v0.1.0` tag is cut from `main`'s tip.
- The `release/0.1.0` branch is then archived (`git branch -a` keeps the ref)
  but no new PRs are accepted against it.
