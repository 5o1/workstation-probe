package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/assaneko/workstation-probe/internal/cors"
)

func TestLoad_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	yaml := `
server:
  host: 127.0.0.1
  port: 19090
sampler:
  interval: 500ms
  history_capacity: 120
modules:
  cpu: {enabled: true}
  memory: {enabled: true}
  gpu: {enabled: false}
  storage:
    enabled: true
    mount_points:
      - {path: /, alias: root}
      - {path: /data, alias: data}
security:
  cors:
    enabled: true
    allowed_origins:
      - https://dashboard.example.com
      - https://*.internal.example.com
    max_age_seconds: 300
  rate_limit:
    enabled: true
    requests_per_second: 5
    burst: 10
logging:
  level: debug
  format: text
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Server.Port != 19090 {
		t.Errorf("port = %d, want 19090", c.Server.Port)
	}
	if c.Server.Host != "127.0.0.1" {
		t.Errorf("host = %q, want 127.0.0.1", c.Server.Host)
	}
	if c.Sampler.Interval != 500*time.Millisecond {
		t.Errorf("interval = %s, want 500ms", c.Sampler.Interval)
	}
	if c.Sampler.HistoryCapacity != 120 {
		t.Errorf("history = %d, want 120", c.Sampler.HistoryCapacity)
	}
	if !c.Modules.CPU.Enabled || !c.Modules.Memory.Enabled || c.Modules.GPU.Enabled {
		t.Errorf("module flags wrong: cpu=%v memory=%v gpu=%v", c.Modules.CPU.Enabled, c.Modules.Memory.Enabled, c.Modules.GPU.Enabled)
	}
	if !c.Modules.Storage.Enabled || len(c.Modules.Storage.MountPoints) != 2 {
		t.Errorf("storage mount points wrong")
	}
}

func TestLoad_Defaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	yaml := `
server: {port: 9090}
modules: {cpu: {enabled: true}}
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Sampler.Interval != time.Second {
		t.Errorf("default interval = %s, want 1s", c.Sampler.Interval)
	}
	if c.Server.Host != "127.0.0.1" {
		t.Errorf("default host = %q, want 127.0.0.1", c.Server.Host)
	}
	if c.Sampler.HistoryCapacity != 60 {
		t.Errorf("default history = %d, want 60", c.Sampler.HistoryCapacity)
	}
	if c.Logging.Level != "info" || c.Logging.Format != "json" {
		t.Errorf("default logging = %s/%s, want info/json", c.Logging.Level, c.Logging.Format)
	}
}

func TestValidate_PortRange(t *testing.T) {
	cases := []struct {
		port int
		ok   bool
	}{
		{0, false}, {-1, false}, {1, true}, {65535, true}, {65536, false}, {100000, false},
	}
	for _, tc := range cases {
		c := &Config{Server: ServerConfig{Host: "127.0.0.1", Port: tc.port}, Sampler: SamplerConfig{Interval: time.Second, HistoryCapacity: 60}}
		err := c.Validate()
		if tc.ok && err != nil {
			t.Errorf("port %d: expected ok, got %v", tc.port, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("port %d: expected error, got nil", tc.port)
		}
	}
}

func TestValidate_Interval(t *testing.T) {
	c := &Config{
		Server:  ServerConfig{Host: "127.0.0.1", Port: 19090},
		Sampler: SamplerConfig{Interval: 50 * time.Millisecond, HistoryCapacity: 60},
	}
	if err := c.Validate(); err == nil {
		t.Errorf("expected error for 50ms interval, got nil")
	}
	c.Sampler.Interval = 100 * time.Millisecond
	if err := c.Validate(); err != nil {
		t.Errorf("100ms should be ok, got %v", err)
	}
}

func TestValidate_CORSOrigins(t *testing.T) {
	cases := []struct {
		origin string
		ok     bool
	}{
		{"https://example.com", true},
		{"http://localhost:3000", true},
		{"https://*.example.com", true},
		{"*", false},
		{"https://*", false},
		{"https://*.com", false},
		{"https://a.*.example.com", false},
		{"ftp://example.com", false},
		{"https://user:pass@example.com", false},
		{"", false},
		{"example.com", false},
	}
	for _, tc := range cases {
		_, err := cors.Parse(tc.origin)
		if tc.ok && err != nil {
			t.Errorf("origin %q: expected ok, got %v", tc.origin, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("origin %q: expected error, got nil", tc.origin)
		}
	}
}

func TestValidate_DuplicateMountPoints(t *testing.T) {
	c := &Config{
		Server:  ServerConfig{Host: "127.0.0.1", Port: 19090},
		Sampler: SamplerConfig{Interval: time.Second, HistoryCapacity: 60},
		Modules: ModulesConfig{Storage: StorageConfig{
			Enabled: true,
			MountPoints: []MountPointConfig{
				{Path: "/", Alias: "a"},
				{Path: "/", Alias: "b"},
			},
		}},
	}
	if err := c.Validate(); err == nil {
		t.Errorf("expected error for duplicate mount path")
	}
}

func TestLoad_UnknownKeyRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	yaml := `
server: {port: 19090}
unknown_key: 1
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Errorf("expected error for unknown yaml key")
	}
}
