# Repository Guidelines

Contributor guide for `workstation-probe`, a small Go REST server that
publishes CPU, memory, GPU, and storage metrics as JSON.

## Project Structure & Module Organization

```
cmd/monitor/                  # Entry point: flags, assembly, lifecycle
internal/config/              # YAML loader + validator
internal/logging/             # slog setup
internal/cors/                # CORS origin grammar (Parse/Match) + HTTP middleware
internal/cpu|memory|gpu|storage/  # One sub-module per metric family
internal/metrics/             # Module interface, RingBuffer[T], route helpers
internal/middleware/          # recovery, request-id, access-log, ratelimit
internal/server/              # mux wiring + graceful shutdown
internal/server/handlers/     # merged /metrics, /profile, /health
contrib/systemd/              # monitor.service.in template
scripts/                      # dev-server.sh, smoke.sh, live-test.sh
webview/                      # Frontend integrations, one dir per framework
webview/hugo/                 # Hugo Module (active)
webview/hugo/exampleSite/     # Dev + snapshot-test fixture site
webview/hugo/test/            # Snapshot runner + golden output
.github/workflows/ci.yml      # CI: go test, lint, hugo snapshot
.golangci.yml                 # golangci-lint configuration
```

Each metric sub-module follows the same shape: `collector.go` (data source),
`module.go` (sampler + HTTP registration), `types.go` (sample struct), and
`module_test.go`. Mirror this layout for new sub-modules.

## Build, Test, and Development Commands

All common workflows are wrapped in the `Makefile`.

- `make build` — `./monitor` with the stub GPU collector (CGO enabled).
- `make build-nvml` — same, but with the `nvml` build tag (needs
  `libnvidia-ml.so`).
- `make test` — `go test -race -timeout 30s ./...`.
- `make vet` — `go vet ./...`.
- `make lint` — `golangci-lint run ./...` (see `.golangci.yml`).
- `make run` — build and start with `config.yaml` on `MONITOR_PORT` (19090).
- `make tidy` — `go mod tidy`.
- `make install | start | stop | uninstall` — systemd lifecycle (sudo).
- `make smoke` — `scripts/smoke.sh` against a running instance.
- `make live-test` — install → start → probe → uninstall cycle. Override
  port with `PORT=19191`; force mode with `MODE=systemd` or `MODE=direct`.
- `make hugo-snapshot` — run the webview snapshot test (needs `hugo` in PATH).

## Coding Style & Naming Conventions

- Go 1.25; standard `gofmt` (tabs, Go-style braces). `goimports` is enforced
  by the linter; group project-local imports under a separate block.
- Exported `CamelCase`; unexported `lowerCamelCase`.
- Doc comment on every package and every exported type/func.
- YAML keys `snake_case`; JSON response fields `camelCase`.
- Intentional swallowed errors carry a `//nolint:errcheck // reason` comment.

## Testing Guidelines

- Standard `testing` package, table-driven tests; always `-race`.
- Tests named `TestXxx`; sub-tests via `t.Run("case_name", ...)`.
- Cover config validation, `RingBuffer` wrap-around and concurrency,
  middleware allow/deny/preflight, and per-module logic with fake
  collectors.
- End-to-end lives in `make live-test`; do not gate CI on it (needs
  systemd and sudo).
- The Hugo webview uses snapshot tests — `make hugo-snapshot` builds
  `webview/hugo/exampleSite/` and diffs against
  `webview/hugo/test/expected/basic/`.

## Commit & Pull Request Guidelines

- Imperative-mood subject ≤ 72 chars, e.g.
  `gpu: handle NVML_ERROR_NOT_SUPPORTED for power_limit`.
- Optional scope prefix: `cpu:`, `memory:`, `gpu:`, `storage:`, `server:`,
  `config:`, `cors:`, `webview:`, `docs:`, `ci:`, `lint:`.
- Body explains *why*; reference issues as `#123`.
- PRs must pass `make test`, `make vet`, `make lint`, and (when touching
  the Hugo module) `make hugo-snapshot` — these are what CI runs.

## Agent-Specific Instructions

- The GPU collector is selected by the `nvml` build tag. Default builds
  must compile without NVML — gate NVML-only code with `//go:build nvml`
  and ship a stub file with the same exported API.
- Never commit a real `config.yaml`; only `config.example.yaml` is tracked.
- A module's `Sample.error` is load-bearing — when non-empty, numeric
  fields are intentionally zero and clients must treat them as "unknown".
