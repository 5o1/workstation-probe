# Makefile for the workstation-probe server.
#
# Targets:
#   make build           build the binary into ./monitor (CGO_ENABLED=1)
#   make build-nvml      build with the NVML collector (requires libnvidia-ml)
#   make test            run all unit tests with -race
#   make vet             go vet ./...
#   make lint            golangci-lint run ./...  (https://golangci-lint.run)
#   make run             build then run with config.yaml
#   make tidy            go mod tidy
#   make install         install binary + systemd service + sample config (sudo)
#   make start           systemctl enable --now monitor
#   make stop            systemctl stop monitor
#   make uninstall       disable + remove service and binary
#   make smoke           run scripts/smoke.sh against an already-running instance
#   make live-test       full install→start→probe→uninstall cycle (sudo, see scripts/live-test.sh)
#   make hugo-snapshot   run the webview snapshot test (needs hugo in PATH)
#
# Variables you may override on the command line:
#   MONITOR_USER         user the systemd service runs as (default: current user)
#   MONITOR_PORT         port to bind (default: 19090) — used by `make run`
#   PREFIX               install prefix (default: /usr/local)
#   PORT                 port for `make live-test` (default: 18080)
#   NVML_CGO_CFLAGS      extra C flags for NVML builds
#                        (default: suppress upstream deprecated NVML APIs)

PREFIX      ?= /usr/local
MONITOR_USER ?= $(shell id -un)
CONFIG_DIR   ?= /etc/monitor

GO        ?= go
BIN        := monitor
PKG        := ./cmd/monitor
NVML_CGO_CFLAGS ?= -Wno-deprecated-declarations

.PHONY: build build-nvml test vet lint run tidy install start stop uninstall smoke live-test hugo-snapshot clean

build:
	CGO_ENABLED=1 $(GO) build -o $(BIN) $(PKG)

build-nvml:
	CGO_ENABLED=1 CGO_CFLAGS="$(CGO_CFLAGS) $(NVML_CGO_CFLAGS)" $(GO) build -tags nvml -o $(BIN) $(PKG)

test:
	CGO_ENABLED=1 $(GO) test -race -timeout 30s ./...

vet:
	$(GO) vet ./...

lint:
	golangci-lint run ./...

hugo-snapshot:
	./webview/hugo/test/run.sh

run: build
	./$(BIN) -config config.yaml

tidy:
	$(GO) mod tidy

install: build
	install -m 0755 -D $(BIN) $(PREFIX)/bin/$(BIN)
	install -m 0755 -d $(CONFIG_DIR)
	[ -f $(CONFIG_DIR)/config.yaml ] || install -m 0644 config.example.yaml $(CONFIG_DIR)/config.yaml
	sed 's/@USER@/$(MONITOR_USER)/' contrib/systemd/monitor.service.in > /etc/systemd/system/monitor.service
	systemctl daemon-reload
	@echo "Installed: binary=$(PREFIX)/bin/$(BIN), config=$(CONFIG_DIR)/config.yaml, user=$(MONITOR_USER)"
	@echo "Next: edit $(CONFIG_DIR)/config.yaml (set server.port), then 'sudo make start'"

start:
	systemctl enable --now monitor

stop:
	systemctl stop monitor

uninstall: stop
	systemctl disable monitor || true
	rm -f /etc/systemd/system/monitor.service
	systemctl daemon-reload
	rm -f $(PREFIX)/bin/$(BIN)
	@echo "Uninstalled. $(CONFIG_DIR)/config.yaml preserved."

smoke:
	./scripts/smoke.sh

# Full end-to-end test: install → start service → probe → uninstall → cleanup.
# Runs as root via sudo for the install/uninstall steps. The probe step does
# not need elevated privileges. Override the port with PORT=19191 make live-test.
live-test:
	./scripts/live-test.sh

clean:
	rm -f $(BIN)
