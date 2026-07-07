package storage

import (
	"fmt"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
)

// MountEntry mirrors a single line of the system mount table. Exported so
// tests in the same package can construct canned tables without touching
// the real /proc.
type MountEntry struct {
	Device  string
	Path    string
	FSType  string
	Options string
}

// readMountTable returns the host's mount table via gopsutil.
func readMountTable() ([]MountEntry, error) {
	parts, err := disk.Partitions(true /* all */)
	if err != nil {
		return nil, fmt.Errorf("list partitions: %w", err)
	}
	out := make([]MountEntry, 0, len(parts))
	for _, p := range parts {
		out = append(out, MountEntry{
			Device:  p.Device,
			Path:    p.Mountpoint,
			FSType:  p.Fstype,
			Options: strings.Join(p.Opts, ","),
		})
	}
	return out, nil
}

// validateMountPoints filters cfg down to entries whose path matches a
// real mount point in table. An unmatched path is a fatal configuration
// error.
func validateMountPoints(cfg []MountConfig, table []MountEntry) ([]MountPoint, error) {
	byPath := make(map[string]MountEntry, len(table))
	for _, m := range table {
		byPath[m.Path] = m
	}

	out := make([]MountPoint, 0, len(cfg))
	for _, mp := range cfg {
		entry, ok := byPath[mp.Path]
		if !ok {
			return nil, fmt.Errorf("mount point %q is not mounted on this host", mp.Path)
		}
		out = append(out, MountPoint{
			Path:   mp.Path,
			Alias:  mp.Alias,
			Device: entry.Device,
			FSType: entry.FSType,
		})
	}
	return out, nil
}

// MountConfig is the input shape from config. Re-declared here so the
// storage package stays independent of internal/config (avoids import cycles
// in tests).
type MountConfig struct {
	Path  string
	Alias string
}
