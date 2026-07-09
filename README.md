# workstation-probe

A small REST server that exposes the local machine's CPU/GPU/memory/storage
load as JSON. Built for monitoring dashboards that need a stable contract
across heterogeneous hosts (with or without NVIDIA GPUs, with arbitrary
mount tables).

## Quick start

### Method 1: run in the foreground

```bash
# 1. Build
make build              # portable build with the stub GPU collector
# or, on NVIDIA hosts with libnvidia-ml:
# make build-nvml

# 2. Configure
cp config.example.yaml config.yaml
$EDITOR config.yaml     # set server.port, optionally tweak mount_points

# 3. Run
./workstation-probe -config config.yaml

# 4. Smoke test (in another shell)
./scripts/smoke.sh http://localhost:19090
```

### Method 2: install as a systemd service

Use the install scripts when you want `workstation-probe` managed by
systemd instead of running in the foreground.

For a root/system install:

```bash
sudo ./scripts/install.sh

# Optional variants:
# sudo ./scripts/install.sh --no-start        # install without starting it
# sudo ./scripts/install.sh --port 9090       # set server.port when creating a new config
# sudo ./scripts/install.sh --nvml            # build with NVML GPU support
# sudo ./scripts/install.sh --nvml --port 9090
```

This installs the binary under `/usr/local/bin/`, creates
`/etc/workstation-probe/config.yaml` if it is missing, and writes the
system service to `/etc/systemd/system/workstation-probe.service`.

Useful service commands:

```bash
systemctl status workstation-probe
journalctl -u workstation-probe -f

# Stop or restart:
# sudo systemctl stop workstation-probe
# sudo systemctl restart workstation-probe

# Uninstall:
# sudo make uninstall      # removes binary + system service; keeps /etc/workstation-probe/config.yaml
```

For a rootless user service:

```bash
./scripts/rootless-install.sh

# Optional variants:
# ./scripts/rootless-install.sh --no-start        # install without starting it
# ./scripts/rootless-install.sh --port 9090       # set server.port when creating a new config
# ./scripts/rootless-install.sh --nvml            # build with NVML GPU support
# ./scripts/rootless-install.sh --nvml --port 9090
```

This installs the binary under `~/.local/bin/`, creates
`~/.config/workstation-probe/config.yaml` if it is missing, and writes
the user service to `~/.config/systemd/user/workstation-probe.service`.

Useful user-service commands:

```bash
systemctl --user status workstation-probe
journalctl --user -u workstation-probe -f

# Stop or restart:
# systemctl --user stop workstation-probe
# systemctl --user restart workstation-probe

# Uninstall:
# systemctl --user disable --now workstation-probe
# rm -f ~/.config/systemd/user/workstation-probe.service ~/.local/bin/workstation-probe
# systemctl --user daemon-reload
```

## Endpoints

| Method | Path                | Purpose                                            |
|-------:|---------------------|----------------------------------------------------|
| GET    | `/profile`          | Static info: hostname, modules enabled, hardware   |
| GET    | `/health`           | Aggregate status; 503 when any enabled module stale |
| GET    | `/metrics`          | Merged JSON of every enabled module's latest sample. Opt-in `?mode=peak&window=5s` returns peak-over-window stress fields |
| GET    | `/metrics/{module}` | One module's latest sample (cpu/memory/gpu/storage) |
| GET    | `/metrics/{module}/history?duration=30s&limit=120` | Time-windowed history |

`{module}` is one of `cpu`, `memory`, `gpu`, `storage`. Disabled modules
return `404` on their dedicated endpoint and are omitted entirely from the
merged `/metrics` payload.

### Sample `/metrics` body

```json
{
  "server_time_local": "2026-07-08 01:02:03",
  "server_time_unix_seconds": 1783443723,
  "cpu":    {"timestamp":"...","overall_percent":23.4,"per_core_percent":[...],"core_count":8},
  "memory": {"timestamp":"...","total_bytes":...,"used_bytes":...,"used_percent":38.4,"swap_total_bytes":...,"swap_used_bytes":...},
  "storage":{"timestamp":"...","disks":[{"path":"/","alias":"root","total_bytes":...,"used_bytes":...,"fs_type":"ext4"}]}
}
```

### Peak mode

`GET /metrics?mode=peak&window=5s` returns the maximum value observed
over the trailing `window` for the stress fields of each enabled
module. Useful for "did the box have a spike recently?" dashboards.
The response adds two top-level fields when this mode is active:

```json
{
  "server_time_local": "2026-07-08 01:02:03",
  "server_time_unix_seconds": 1783443723,
  "mode": "peak",
  "window_seconds": 5,
  "cpu":    {"...": "overall_percent and per_core_percent[*] are maxes"},
  "memory": {"...": "used_no_cache_bytes and used_percent are maxes"},
  "gpu":    {"...": "per-device utilization_gpu_percent, utilization_memory_percent, memory_used_bytes, temperature_c, and power_draw_watts are maxes"},
  "storage": {"...": "unchanged — no stress semantics"}
}
```

`window` defaults to `60s`; invalid values return 400 with
`{"error":"invalid_duration"}`. If startup time is shorter than
`window`, the peak is computed over whatever is in the ring buffer.
Storage is intentionally not peaked (capacity monitoring, not load).

The webview panel opt-in via YAML:

```yaml
name: rig-02
api: "http://rig-02.lan:19090"
refresh: "5s"
mode: peak
window: "5s"
modules: [cpu, memory, gpu, storage]
```

The `gpu` key is absent when the module is disabled.

### GPU fields

When NVML is available, each `gpu.devices[*]` entry carries
`utilization_gpu_percent`, the GPU core utilization reported by NVML
over its most recent sampling period.

Each entry may also carry `power_limit_watts` (the current power
management limit, in W, from `GetPowerManagementLimit`). Consumer
GeForce cards commonly return `NVML_ERROR_NOT_SUPPORTED`; in that case
the field is omitted.

### Error semantics

A module's `Sample` carries an `error` string when the underlying syscall
failed. When `error` is non-empty, the numeric fields are zero values —
clients should treat them as "unknown" rather than "really zero".

## Configuration

`config.yaml` (YAML). `server.port` is required. Most other fields have
defaults; when storage is enabled, `modules.storage.mount_points` must
contain at least one real mount point.

```yaml
server:
  host: 127.0.0.1                  # default; use 0.0.0.0 only when intentionally exposing it
  port: 19090                       # required, [1, 65535]
sampler:
  interval: 1s                     # >= 100ms (gopsutil constraint)
  history_capacity: 60             # per-module ring buffer
modules:
  cpu:     {enabled: true}
  memory:  {enabled: true}
  gpu:     {enabled: true}         # auto-disabled without NVML
  storage:
    enabled: true
    mount_points:                  # each MUST be a real mount point
      - {path: /,     alias: root}
      - {path: /data, alias: data}
security:
  cors:
    enabled: false
    allowed_origins: []            # only `scheme://host` or `scheme://*.host`
    allow_methods: [GET, OPTIONS]
    allow_headers: [Content-Type]
    max_age_seconds: 600
  rate_limit:
    enabled: true
    requests_per_second: 10
    burst: 20
    trust_proxy_headers: false     # only true behind a trusted reverse proxy
    exempt_paths: [/health]
logging:
  level: info
  format: json
```

## systemd hardening

For the root/system install, `scripts/install.sh` writes
`/etc/systemd/system/workstation-probe.service` from
`contrib/systemd/workstation-probe.service.in`. That template enables a
full systemd sandbox (`NoNewPrivileges`, `ProtectSystem=strict`,
`ProtectHome`, `RestrictAddressFamilies`, `RestrictNamespaces`,
`PrivateTmp`, etc.). The only flag deliberately omitted is
`MemoryDenyWriteExecute` because NVML's driver-side code can produce
transient W^X mappings on some hosts.

The rootless installer writes a separate user service and does not include
those sandboxing directives.

If you need to relax a directive for the root/system service (e.g. to allow
a non-standard config path), edit
`/etc/systemd/system/workstation-probe.service` after installation, then
`sudo systemctl daemon-reload && sudo systemctl restart workstation-probe`.

## Deployment with TLS

This service only listens on plain HTTP. Put a reverse proxy in front for
TLS termination and (optionally) auth. Minimal nginx example:

The sample config binds to `127.0.0.1` by default. Keep that setting when
nginx runs on the same host; set `server.host: 0.0.0.0` only when you
intentionally want the probe reachable directly from other machines.

```nginx
server {
    listen 443 ssl http2;
    server_name workstation-probe.internal.example.com;

    ssl_certificate     /etc/nginx/ssl/workstation-probe.crt;
    ssl_certificate_key /etc/nginx/ssl/workstation-probe.key;

    # pass client IP for the rate limiter (trust_proxy_headers must be true)
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Real-IP       $remote_addr;

    # optional: basic auth (or replace with your auth mechanism)
    auth_basic           "workstation-probe";
    auth_basic_user_file /etc/nginx/.htpasswd;

    location / {
        proxy_pass http://127.0.0.1:19090;
        proxy_set_header Host $host;
    }
}
```

When `trust_proxy_headers: true` is set, the rate limiter trusts
`X-Forwarded-For` and uses the first entry as the client IP. **Only enable
this when the service is behind a reverse proxy that strips incoming
`X-Forwarded-For` from untrusted clients**; otherwise a malicious client can
forge the header to bypass per-IP limits.

CORS, rate limiting and basic auth are **not** authentication. They are
mitigations; deploy them as defense-in-depth, not as the sole access
control.

## Architecture

```
                ┌────────── cpu.Module ──────────┐
                │ collector + sampler + buf + H │
config ─► main─►├────────── memory.Module ────────┤──► http.ServeMux
                │ collector + sampler + buf + H │
                ├────────── gpu.Module ──────────┤── (NVML or stub)
                │ collector + sampler + buf + H │
                └────────── storage.Module ──────┘
                  (validates mount table at startup)
```

Each sub-module:

- Has its own background goroutine and per-module ring buffer.
- Publishes its latest sample via `atomic.Pointer[Sample]`; handlers read
  it lock-free.
- Registers its own `/metrics/<name>` and `/metrics/<name>/history`
  routes; the central mux exposes `/metrics` (merged), `/profile`, and
  `/health`.

Middleware chain (outer to inner):
`recovery → request-id → access-log → cors → ratelimit → mux`

CORS runs before the rate limiter so `OPTIONS` preflights do not consume
tokens.

## Build tags

`go build` (no tag) → uses the stub GPU collector that always reports
"NVML support not compiled in". This makes the binary buildable on
machines without `libnvidia-ml.so`.

`go build -tags nvml` → links against NVML via
`github.com/NVIDIA/go-nvml`. Requires `libnvidia-ml.so` at build time; the
resulting binary reports live data on NVIDIA hosts.

For webview development, `./scripts/dev-server.sh` defaults to
`GPU=auto`: it rebuilds a local development binary before starting, uses the `nvml`
build tag when `libnvidia-ml.so` is available, and falls back to the
stub collector otherwise. Use `GPU=nvml ./scripts/dev-server.sh` to
force a real-NVML build, or `GPU=stub ./scripts/dev-server.sh` to force
the portable stub.

## Testing

### Unit tests

```bash
make test                          # go test -race ./...
```

Tests cover:

- Config validation (port range, interval floor, CORS origin grammar,
  duplicate mount paths, unknown keys)
- `RingBuffer` correctness under wrap-around and concurrent access
- Middleware behavior (recovery, request-id, CORS allow/deny/preflight,
  rate-limit allow/deny/exempt/retry-after)
- Per-module unit tests with fake collectors

### End-to-end live test

`make live-test` runs the full install → start → probe → uninstall cycle
and prints a human-readable summary of CPU/GPU/memory/storage state. It
chooses one of two modes automatically:

- **systemd mode** (preferred, used when `sudo -n true` works): installs
  an isolated test binary and uniquely named systemd unit, so a regular
  `workstation-probe` install is not touched. Cleanup unregisters the unit
  and deletes the files.
- **direct mode** (fallback, when sudo isn't available): installs an isolated
  user-writable test binary and runs it directly via `nohup`. The PID is
  tracked in a file; cleanup `kill`s it and removes the install directory.

Override the port: `PORT=19191 make live-test`
Force a specific mode: `MODE=direct make live-test` or `MODE=systemd make live-test`

The probe step runs regardless of mode and produces output like:

```
Hostname: rig-01
Go:       go1.25.0
Sampler:  1000 ms interval, 30 samples history
Health:   ok

=== CPU ===
  Model:   <your CPU model>
  Cores:   <N>
  Overall: 1.5%
  Per-core:   1.5%   0.5%   1.0%   0.0%   0.0%   0.5% ...

=== Memory ===
  Total:       <size> GiB
  Used:        <used> GiB (xx.x%)
  ...

=== GPU ===
  Disabled: nvml support not compiled in ...
  (or, when NVML is enabled: per-device utilization, memory, temperature, power)

=== Storage ===
  / (root)  [<filesystem>]
    Total:  <size> GiB  Used:    <used> GiB (xx.x%)  Free:  <free> GiB

--- /health per-module freshness ---
  cpu      ok   last sample   100 ms ago
  memory   ok   last sample    99 ms ago
  ...
```

Cleanup runs via a `trap` on any exit (success, failure, SIGINT, SIGTERM),
so partial failures still leave the host clean.

## Layout

```
cmd/monitor/main.go              entrypoint: flag parsing, assembly, lifecycle
internal/config/                 YAML loader + validator
internal/logging/                slog setup
internal/metrics/                Module interface + generic RingBuffer[T]
internal/middleware/             recovery, request-id, access-log, cors, ratelimit
internal/cpu/                    CPU usage sub-module
internal/memory/                 memory + swap sub-module
internal/gpu/                    NVIDIA GPU sub-module (NVML via build tag)
internal/storage/                per-mount-point disk-usage sub-module
internal/server/                 mux + middleware wiring + graceful shutdown
internal/server/handlers/        merged /metrics, /profile, /health
contrib/systemd/               service template
scripts/smoke.sh                 end-to-end smoke test
config.example.yaml              sample config
```
