# Stability check

`scripts/stability-check.sh` runs a short or long soak against a local
`workstation-probe` instance and records request-health artifacts under
`build/stability/<timestamp>/`.

Use it before tagging a release, after sampler or shutdown changes, and when
checking behavior on real hardware.

## Run

```bash
# Default: 5-minute direct run on port 19090.
./scripts/stability-check.sh

# Longer run.
STABILITY_DURATION=72h ./scripts/stability-check.sh

# Use an existing binary path and port.
MONITOR_BIN=./workstation-probe MONITOR_PORT=19191 ./scripts/stability-check.sh

# Rebuild with the race detector before running.
RACE_BUILD=1 ./scripts/stability-check.sh
```

## Modes

- `MODE=direct` starts the binary directly with a generated config.
- `MODE=systemd` restarts and stops the systemd service expected by the script.

## Artifacts

The script writes:

- `config.yaml` — generated direct-mode config.
- `workstation-probe.log` — server stdout/stderr for direct mode.
- `workstation-probe.pid` — direct-mode process pid.
- `sample.log` — periodic `/metrics` request status and latency.
- `goroutine.log` — periodic `/health` snapshots.
- `summary.json` — run duration, mode, port, sample count, and error count.

The check fails if any periodic `/metrics` request records an error.
