package osimage

import (
	"errors"
	"strings"
	"time"
)

// Distribution represents the OS family.
type Distribution string

const (
	DistroUbuntu Distribution = "ubuntu"
	DistroDebian Distribution = "debian"
)

// Architecture represents CPU architecture.
type Architecture string

const (
	ArchX86 Architecture = "x86"
	ArchARM Architecture = "arm" // Deferred in MVP catalog, supported in type system
)

// Status tracks image availability from Hetzner API.
type Status string

const (
	StatusAvailable  Status = "available"
	StatusDeprecated Status = "deprecated"
	StatusMissing    Status = "missing"
)

var (
	ErrUnsupportedDistribution = errors.New("unsupported OS distribution")
	ErrUnsupportedArchitecture = errors.New("unsupported CPU architecture")
	ErrImageUnavailable        = errors.New("selected OS image is currently unavailable")
)

// Entry represents an OS choice synced from Hetzner Cloud.
type Entry struct {
	ID               string
	Distribution     Distribution
	Version          string // e.g. "24.04" or "12"
	Architecture     Architecture
	ImageName        string // e.g. "ubuntu-24.04"
	HetznerStatus    Status
	UnavailableAfter *time.Time
	DisplayNameFA    string
	DisplayNameEN    string
	SortOrder        int
	SyncedAt         time.Time
}

// IsOffered returns true if image is currently creatable (available and not expired).
func (e *Entry) IsOffered(now time.Time) bool {
	if e.HetznerStatus != StatusAvailable {
		return false
	}
	if e.UnavailableAfter != nil && now.After(*e.UnavailableAfter) {
		return false
	}
	return true
}

// ParseDistribution normalizes user selection string.
func ParseDistribution(s string) (Distribution, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ubuntu":
		return DistroUbuntu, nil
	case "debian":
		return DistroDebian, nil
	default:
		return "", ErrUnsupportedDistribution
	}
}
