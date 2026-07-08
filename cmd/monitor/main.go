// Command monitor is a REST server that exposes CPU/GPU/memory/storage load
// for the local machine as JSON.
//
// The server is composed of four independent sub-modules (cpu, memory, gpu,
// storage). Each is constructed according to its YAML configuration, started
// in its own goroutine, and exposes its own routes via the shared mux. The
// merged /metrics, /profile and /health endpoints aggregate the per-module
// state.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/assaneko/workstation-probe/internal/config"
	"github.com/assaneko/workstation-probe/internal/lock"
	"github.com/assaneko/workstation-probe/internal/cpu"
	"github.com/assaneko/workstation-probe/internal/gpu"
	"github.com/assaneko/workstation-probe/internal/logging"
	"github.com/assaneko/workstation-probe/internal/memory"
	"github.com/assaneko/workstation-probe/internal/metrics"
	"github.com/assaneko/workstation-probe/internal/server"
	"github.com/assaneko/workstation-probe/internal/server/handlers"
	"github.com/assaneko/workstation-probe/internal/storage"
)

func main() {
	os.Exit(run())
}

func run() int {
	configPath := flag.String("config", "", "path to YAML config file (required)")
	pidFile := flag.String("pid-file", "/tmp/monitor.pid", "path to PID lock file")
	flag.Parse()

	l, err := lock.Acquire(*pidFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer l.Release()

	if *configPath == "" {
		slog.Error("-config is required")
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("load config failed", "err", err, "path", *configPath)
		return 1
	}

	logger := logging.Setup(cfg.Logging.Level, cfg.Logging.Format)
	logger.Info("config loaded",
		"path", *configPath,
		"host", cfg.Server.Host,
		"port", cfg.Server.Port,
		"interval", cfg.Sampler.Interval.String(),
		"history_capacity", cfg.Sampler.HistoryCapacity,
	)

	// Warn loudly when trust_proxy_headers is enabled: per-IP rate limiting
	// trusts the first X-Forwarded-For entry, which means a malicious client
	// that can reach this service directly (i.e. without going through a
	// stripping proxy) can forge the header and bypass limits. Operators
	// must ensure the service is only reachable through a proxy that
	// strips incoming XFF.
	if cfg.Security.RateLimit.Enabled && cfg.Security.RateLimit.TrustProxyHeaders {
		logger.Warn("rate_limit.trust_proxy_headers=true: ensure the service is behind a proxy that strips incoming X-Forwarded-For; otherwise rate limits can be bypassed by client-controlled headers")
	}

	rootCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	// SIGHUP is ignored so the process is not killed by logrotate or the
	// stability test HUP drill.
	signal.Notify(make(chan os.Signal, 1), syscall.SIGHUP)
	defer cancel()

	// Build enabled modules. Disabled modules are omitted entirely so the
	// merged view, /profile, and /health all reflect them as "absent".
	mods, err := buildModules(rootCtx, cfg, logger)
	if err != nil {
		logger.Error("module setup failed", "err", err)
		return 1
	}

	mux := http.NewServeMux()
	for _, m := range mods {
		m.RegisterRoutes(mux)
	}
	hostname, _ := os.Hostname()
	mux.Handle("GET /metrics", handlers.NewCombined(mods))
	mux.Handle("GET /health", handlers.NewHealth(mods, 2*cfg.Sampler.Interval))
	mux.Handle("GET /profile", handlers.NewProfile(hostname, handlers.ProfileSamplerConfig{
		IntervalMs:      int(cfg.Sampler.Interval.Milliseconds()),
		HistoryCapacity: cfg.Sampler.HistoryCapacity,
	}, mods))

	handler := server.Build(rootCtx, mux, cfg, logger)

	httpServer := &http.Server{
		Addr:              server.Addr(cfg.Server.Host, cfg.Server.Port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http listening", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	select {
	case <-rootCtx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErr:
		logger.Error("http server failed", "err", err)
		return 1
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown failed", "err", err)
	}
	for _, m := range mods {
		if err := m.Shutdown(shutdownCtx); err != nil {
			logger.Warn("module shutdown failed", "module", m.Name(), "err", err)
		}
	}
	logger.Info("bye")
	return 0
}

// buildModules constructs each enabled sub-module from cfg, primes an initial
// sample, and starts its background sampler. The returned slice is in a
// stable order (cpu, memory, gpu, storage) so the merged view always has
// the same field order regardless of which are enabled.
func buildModules(ctx context.Context, cfg *config.Config, logger *slog.Logger) ([]metrics.Module, error) {
	mods := []metrics.Module{}
	var err error

	if cfg.Modules.CPU.Enabled {
		mods, err = appendEnabled(mods, "cpu", func() (metrics.Module, error) {
			return cpu.New(cfg.Sampler.Interval, cfg.Sampler.HistoryCapacity, logger)
		})
		if err != nil {
			return nil, err
		}
	}
	if cfg.Modules.Memory.Enabled {
		mods, err = appendEnabled(mods, "memory", func() (metrics.Module, error) {
			return memory.New(cfg.Sampler.Interval, cfg.Sampler.HistoryCapacity, logger), nil
		})
		if err != nil {
			return nil, err
		}
	}
	if cfg.Modules.GPU.Enabled {
		mods, err = appendEnabled(mods, "gpu", func() (metrics.Module, error) {
			return gpu.New(cfg.Sampler.Interval, cfg.Sampler.HistoryCapacity, logger), nil
		})
		if err != nil {
			return nil, err
		}
	}
	if cfg.Modules.Storage.Enabled {
		mountConfigs := make([]storage.MountConfig, len(cfg.Modules.Storage.MountPoints))
		for i, mp := range cfg.Modules.Storage.MountPoints {
			mountConfigs[i] = storage.MountConfig{Path: mp.Path, Alias: mp.Alias}
		}
		mods, err = appendEnabled(mods, "storage", func() (metrics.Module, error) {
			return storage.New(mountConfigs, cfg.Sampler.Interval, cfg.Sampler.HistoryCapacity, logger)
		})
		if err != nil {
			return nil, err
		}
	}

	// Start each module's background sampler in the same order they were
	// appended. Module.Start is fast (it only launches a goroutine) and
	// always returns nil for the four collectors we ship.
	for _, m := range mods {
		if err := m.Start(ctx); err != nil {
			return nil, fmt.Errorf("%s start: %w", m.Name(), err)
		}
	}
	return mods, nil
}

// appendEnabled builds a module via build and appends it on success. On
// failure it returns an error because silent skip would leave /metrics
// reporting a half-broken host.
func appendEnabled(mods []metrics.Module, name string, build func() (metrics.Module, error)) ([]metrics.Module, error) {
	m, err := build()
	if err != nil {
		return nil, fmt.Errorf("%s init: %w", name, err)
	}
	return append(mods, m), nil
}
