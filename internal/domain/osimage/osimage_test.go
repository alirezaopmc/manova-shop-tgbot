package osimage

import (
	"testing"
	"time"
)

func TestOSImageOffering(t *testing.T) {
	now := time.Now()

	// Available without deprecation date
	e1 := &Entry{
		Distribution:  DistroUbuntu,
		Version:       "24.04",
		Architecture:  ArchX86,
		ImageName:     "ubuntu-24.04",
		HetznerStatus: StatusAvailable,
	}
	if !e1.IsOffered(now) {
		t.Fatalf("expected e1 to be offered")
	}

	// Available but past unavailable_after date
	past := now.Add(-1 * time.Hour)
	e2 := &Entry{
		Distribution:     DistroDebian,
		Version:          "11",
		Architecture:     ArchX86,
		ImageName:        "debian-11",
		HetznerStatus:    StatusAvailable,
		UnavailableAfter: &past,
	}
	if e2.IsOffered(now) {
		t.Fatalf("expected e2 not to be offered when past expiration date")
	}

	// Deprecated
	e3 := &Entry{
		Distribution:  DistroUbuntu,
		HetznerStatus: StatusDeprecated,
	}
	if e3.IsOffered(now) {
		t.Fatalf("expected deprecated entry not to be offered")
	}
}

func TestParseDistribution(t *testing.T) {
	d, err := ParseDistribution(" Ubuntu ")
	if err != nil || d != DistroUbuntu {
		t.Fatalf("expected DistroUbuntu, got %s err %v", d, err)
	}

	d, err = ParseDistribution("Debian")
	if err != nil || d != DistroDebian {
		t.Fatalf("expected DistroDebian, got %s err %v", d, err)
	}

	_, err = ParseDistribution("archlinux")
	if err != ErrUnsupportedDistribution {
		t.Fatalf("expected ErrUnsupportedDistribution, got %v", err)
	}
}
