// Package config loads the YAML configuration file and exposes a typed view.
//
// The configuration is read once at startup and validated eagerly so the
// process either starts in a known-good state or fails fast.
package config

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/assaneko/workstation-probe/internal/cors"
)

// Config is the root of the YAML document.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Sampler  SamplerConfig  `yaml:"sampler"`
	Modules  ModulesConfig  `yaml:"modules"`
	Security SecurityConfig `yaml:"security"`
	Logging  LoggingConfig  `yaml:"logging"`
}

// ServerConfig controls the HTTP listener.
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// SamplerConfig defines the sampling cadence and per-module ring buffer size.
// Both fields are populated for every enabled module.
type SamplerConfig struct {
	Interval        time.Duration `yaml:"interval"`
	HistoryCapacity int           `yaml:"history_capacity"`
}

// ModulesConfig groups every optional collector under one struct.
type ModulesConfig struct {
	CPU     CPUConfig     `yaml:"cpu"`
	Memory  MemoryConfig  `yaml:"memory"`
	GPU     GPUConfig     `yaml:"gpu"`
	Storage StorageConfig `yaml:"storage"`
}

// CPUConfig is currently just an enable flag.
type CPUConfig struct {
	Enabled bool `yaml:"enabled"`
}

// MemoryConfig is currently just an enable flag.
type MemoryConfig struct {
	Enabled bool `yaml:"enabled"`
}

// GPUConfig is currently just an enable flag.
type GPUConfig struct {
	Enabled bool `yaml:"enabled"`
}

// StorageConfig enables the storage module and lists the mount points to track.
type StorageConfig struct {
	Enabled     bool               `yaml:"enabled"`
	MountPoints []MountPointConfig `yaml:"mount_points"`
}

// MountPointConfig is a user-named filesystem mount point to monitor.
type MountPointConfig struct {
	Path  string `yaml:"path"`
	Alias string `yaml:"alias"`
}

// SecurityConfig groups CORS and rate-limiting knobs.
type SecurityConfig struct {
	CORS      CORSConfig      `yaml:"cors"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
}

// CORSConfig controls the optional CORS middleware.
type CORSConfig struct {
	Enabled        bool     `yaml:"enabled"`
	AllowedOrigins []string `yaml:"allowed_origins"`
	AllowMethods   []string `yaml:"allow_methods"`
	AllowHeaders   []string `yaml:"allow_headers"`
	MaxAgeSeconds  int      `yaml:"max_age_seconds"`
}

// RateLimitConfig controls the optional per-IP rate limiter.
type RateLimitConfig struct {
	Enabled           bool     `yaml:"enabled"`
	RequestsPerSecond float64  `yaml:"requests_per_second"`
	Burst             int      `yaml:"burst"`
	TrustProxyHeaders bool     `yaml:"trust_proxy_headers"`
	ExemptPaths       []string `yaml:"exempt_paths"`
}

// LoggingConfig controls the global slog setup.
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Load reads, parses, fills defaults for, and validates the config file at path.
// On any failure it returns a descriptive error so main() can log it and exit.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true) // reject unknown keys to catch typos early
	if err := dec.Decode(&c); err != nil {
		if err == io.EOF {
			// Empty file → fall back to zero-valued Config; applyDefaults() fills the rest.
		} else {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Host == "" {
		c.Server.Host = "127.0.0.1"
	}
	if c.Sampler.Interval == 0 {
		c.Sampler.Interval = time.Second
	}
	if c.Sampler.HistoryCapacity <= 0 {
		c.Sampler.HistoryCapacity = 60
	}
	if c.Security.CORS.MaxAgeSeconds == 0 {
		c.Security.CORS.MaxAgeSeconds = 600
	}
	if len(c.Security.CORS.AllowMethods) == 0 {
		c.Security.CORS.AllowMethods = []string{"GET", "OPTIONS"}
	}
	if len(c.Security.CORS.AllowHeaders) == 0 {
		c.Security.CORS.AllowHeaders = []string{"Content-Type"}
	}
	if c.Security.RateLimit.Burst == 0 {
		c.Security.RateLimit.Burst = 20
	}
	if c.Security.RateLimit.RequestsPerSecond == 0 {
		c.Security.RateLimit.RequestsPerSecond = 10
	}
	if len(c.Security.RateLimit.ExemptPaths) == 0 {
		c.Security.RateLimit.ExemptPaths = []string{"/health"}
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "json"
	}
}

// Validate enforces hard constraints. Returns the first violation.
func (c *Config) Validate() error {
	if c.Server.Host == "" {
		return fmt.Errorf("server.host must not be empty")
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be in [1, 65535], got %d", c.Server.Port)
	}
	if c.Sampler.Interval < 100*time.Millisecond {
		return fmt.Errorf("sampler.interval must be >= 100ms (gopsutil constraint), got %s", c.Sampler.Interval)
	}
	if c.Sampler.HistoryCapacity < 1 {
		return fmt.Errorf("sampler.history_capacity must be >= 1, got %d", c.Sampler.HistoryCapacity)
	}

	// storage-specific syntactic checks (mount-point existence is checked at
	// module construction time once the platform layer is available).
	if c.Modules.Storage.Enabled {
		seen := make(map[string]struct{}, len(c.Modules.Storage.MountPoints))
		for i, mp := range c.Modules.Storage.MountPoints {
			if mp.Path == "" {
				return fmt.Errorf("modules.storage.mount_points[%d].path is empty", i)
			}
			if mp.Alias == "" {
				return fmt.Errorf("modules.storage.mount_points[%d].alias is empty", i)
			}
			if _, dup := seen[mp.Path]; dup {
				return fmt.Errorf("modules.storage.mount_points has duplicate path %q", mp.Path)
			}
			seen[mp.Path] = struct{}{}
		}
	}

	if c.Security.CORS.Enabled {
		for i, o := range c.Security.CORS.AllowedOrigins {
			if _, err := cors.Parse(o); err != nil {
				return fmt.Errorf("security.cors.allowed_origins[%d] (%q): %w", i, o, err)
			}
		}
	}

	if c.Security.RateLimit.Enabled {
		if c.Security.RateLimit.RequestsPerSecond <= 0 {
			return fmt.Errorf("security.rate_limit.requests_per_second must be > 0 when enabled")
		}
		if c.Security.RateLimit.Burst <= 0 {
			return fmt.Errorf("security.rate_limit.burst must be > 0 when enabled")
		}
	}

	return nil
}
