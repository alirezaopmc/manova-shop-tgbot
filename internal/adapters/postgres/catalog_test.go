package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/catalog"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/osimage"
)

func TestCatalogRepository_SeededItems(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// 1. Verify CX23 baseline facts
	cx23, err := s.Catalog().GetItem(ctx, "cx23")
	if err != nil {
		t.Fatalf("GetItem(cx23) failed: %v", err)
	}
	if cx23.Name != "CX23" {
		t.Errorf("expected Name CX23, got %s", cx23.Name)
	}
	if cx23.Family != catalog.FamilyCX {
		t.Errorf("expected Family CX, got %s", cx23.Family)
	}
	if cx23.VCPU != 2 {
		t.Errorf("expected 2 VCPU, got %d", cx23.VCPU)
	}
	if cx23.RAMGB != 4 {
		t.Errorf("expected 4 RAMGB, got %d", cx23.RAMGB)
	}
	if cx23.DiskGB != 40 {
		t.Errorf("expected 40 DiskGB, got %d", cx23.DiskGB)
	}
	if cx23.HetznerType != "cx23" {
		t.Errorf("expected HetznerType cx23, got %s", cx23.HetznerType)
	}
	if cx23.HourlyUSDMicro != 7000 {
		t.Errorf("expected HourlyUSDMicro 7000, got %d", cx23.HourlyUSDMicro)
	}
	if cx23.MonthlyCapUSD != 4490000 {
		t.Errorf("expected MonthlyCapUSD 4490000, got %d", cx23.MonthlyCapUSD)
	}
	if !cx23.Active {
		t.Errorf("expected Active true for cx23")
	}

	// 2. Verify CX33
	cx33, err := s.Catalog().GetItem(ctx, "cx33")
	if err != nil {
		t.Fatalf("GetItem(cx33) failed: %v", err)
	}
	if cx33.VCPU != 4 || cx33.RAMGB != 8 || cx33.DiskGB != 80 || cx33.HourlyUSDMicro != 13000 {
		t.Errorf("cx33 unexpected specs: %+v", cx33)
	}

	// 3. Verify CX43
	cx43, err := s.Catalog().GetItem(ctx, "cx43")
	if err != nil {
		t.Fatalf("GetItem(cx43) failed: %v", err)
	}
	if cx43.VCPU != 8 || cx43.RAMGB != 16 || cx43.DiskGB != 160 || cx43.HourlyUSDMicro != 25000 {
		t.Errorf("cx43 unexpected specs: %+v", cx43)
	}

	// 4. Non-existent item returns ErrNotFound
	_, err = s.Catalog().GetItem(ctx, "nonexistent-item")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for nonexistent item, got %v", err)
	}

	// 5. Empty ID returns error
	if _, err := s.Catalog().GetItem(ctx, ""); err == nil {
		t.Errorf("expected error for empty item ID")
	}

	// 6. ListActiveItems returns sorted active items
	items, err := s.Catalog().ListActiveItems(ctx)
	if err != nil {
		t.Fatalf("ListActiveItems failed: %v", err)
	}
	if len(items) < 3 {
		t.Fatalf("expected at least 3 active catalog items, got %d", len(items))
	}
	// Verify ascending price order
	for i := 1; i < len(items); i++ {
		if items[i].HourlyUSDMicro < items[i-1].HourlyUSDMicro {
			t.Errorf("items not sorted by price: item[%d]=%d < item[%d]=%d",
				i, items[i].HourlyUSDMicro, i-1, items[i-1].HourlyUSDMicro)
		}
	}
}

func TestOSCatalogRepository_GetOffered(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// 1. All offered images
	all, err := s.OSCatalog().GetOffered(ctx, "", "")
	if err != nil {
		t.Fatalf("GetOffered(all) failed: %v", err)
	}
	if len(all) < 3 {
		t.Fatalf("expected at least 3 seeded OS entries, got %d", len(all))
	}

	// 2. Filter by Ubuntu
	ubuntuEntries, err := s.OSCatalog().GetOffered(ctx, osimage.DistroUbuntu, "")
	if err != nil {
		t.Fatalf("GetOffered(Ubuntu) failed: %v", err)
	}
	if len(ubuntuEntries) < 2 {
		t.Fatalf("expected at least 2 Ubuntu entries, got %d", len(ubuntuEntries))
	}
	for _, e := range ubuntuEntries {
		if e.Distribution != osimage.DistroUbuntu {
			t.Errorf("expected Ubuntu distribution, got %s", e.Distribution)
		}
	}

	// 3. Filter by Debian
	debianEntries, err := s.OSCatalog().GetOffered(ctx, osimage.DistroDebian, "")
	if err != nil {
		t.Fatalf("GetOffered(Debian) failed: %v", err)
	}
	if len(debianEntries) < 1 {
		t.Fatalf("expected at least 1 Debian entry, got %d", len(debianEntries))
	}
	for _, e := range debianEntries {
		if e.Distribution != osimage.DistroDebian {
			t.Errorf("expected Debian distribution, got %s", e.Distribution)
		}
	}

	// 4. Filter by Architecture
	x86Entries, err := s.OSCatalog().GetOffered(ctx, "", osimage.ArchX86)
	if err != nil {
		t.Fatalf("GetOffered(x86) failed: %v", err)
	}
	for _, e := range x86Entries {
		if e.Architecture != osimage.ArchX86 {
			t.Errorf("expected x86 arch, got %s", e.Architecture)
		}
	}

	// ARM has no default seeded images
	armEntries, err := s.OSCatalog().GetOffered(ctx, "", osimage.ArchARM)
	if err != nil {
		t.Fatalf("GetOffered(ARM) failed: %v", err)
	}
	if len(armEntries) != 0 {
		t.Errorf("expected 0 ARM entries in seed, got %d", len(armEntries))
	}
}

func TestOSCatalogRepository_ReplaceSnapshot(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Capture existing snapshot to restore after test
	existing, err := s.OSCatalog().GetOffered(ctx, "", "")
	if err != nil {
		t.Fatalf("capture existing snapshot failed: %v", err)
	}
	t.Cleanup(func() {
		_ = s.OSCatalog().ReplaceSnapshot(context.Background(), existing, time.Now().UTC())
	})

	past := time.Now().UTC().Add(-24 * time.Hour)
	future := time.Now().UTC().Add(48 * time.Hour)

	newEntries := []osimage.Entry{
		{
			ID:            "custom-u24",
			Distribution:  osimage.DistroUbuntu,
			Version:       "24.04",
			Architecture:  osimage.ArchX86,
			ImageName:     "ubuntu-24.04-custom",
			HetznerStatus: osimage.StatusAvailable,
			DisplayNameFA: "اوبونتو ۲۴",
			DisplayNameEN: "Ubuntu 24.04 Custom",
			SortOrder:     1,
		},
		{
			ID:               "custom-u22-expiring",
			Distribution:     osimage.DistroUbuntu,
			Version:          "22.04",
			Architecture:     osimage.ArchX86,
			ImageName:        "ubuntu-22.04-custom",
			HetznerStatus:    osimage.StatusAvailable,
			UnavailableAfter: &future,
			DisplayNameFA:    "اوبونتو ۲۲",
			DisplayNameEN:    "Ubuntu 22.04 Expiring Future",
			SortOrder:        2,
		},
		{
			ID:               "custom-deb-expired",
			Distribution:     osimage.DistroDebian,
			Version:          "11",
			Architecture:     osimage.ArchX86,
			ImageName:        "debian-11",
			HetznerStatus:    osimage.StatusAvailable,
			UnavailableAfter: &past, // Expired: IsOffered should filter it out
			DisplayNameFA:    "دبیان منقضی",
			DisplayNameEN:    "Debian 11 Expired",
			SortOrder:        3,
		},
		{
			ID:            "custom-deprecated",
			Distribution:  osimage.DistroUbuntu,
			Version:       "20.04",
			Architecture:  osimage.ArchX86,
			ImageName:     "ubuntu-20.04",
			HetznerStatus: osimage.StatusDeprecated, // Not available
			DisplayNameFA: "منسوخ",
			DisplayNameEN: "Deprecated Ubuntu",
			SortOrder:     4,
		},
	}

	syncedAt := time.Now().UTC()
	if err := s.OSCatalog().ReplaceSnapshot(ctx, newEntries, syncedAt); err != nil {
		t.Fatalf("ReplaceSnapshot failed: %v", err)
	}

	// Verify GetOffered returns only offered entries (custom-u24 and custom-u22-expiring)
	offered, err := s.OSCatalog().GetOffered(ctx, "", "")
	if err != nil {
		t.Fatalf("GetOffered after replace failed: %v", err)
	}
	if len(offered) != 2 {
		t.Fatalf("expected exactly 2 offered entries, got %d", len(offered))
	}
	if offered[0].ID != "custom-u24" {
		t.Errorf("expected first offered item custom-u24, got %s", offered[0].ID)
	}
	if offered[1].ID != "custom-u22-expiring" {
		t.Errorf("expected second offered item custom-u22-expiring, got %s", offered[1].ID)
	}
}
