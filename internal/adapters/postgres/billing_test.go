package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
)

func uniqueResourceID(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), rand.Int63n(10_000))
}

func cleanupResource(t testing.TB, s *Store, resourceID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.Pool().Exec(ctx, "DELETE FROM shop.provisioned_resources WHERE id = $1", resourceID)
	})
}

func TestBillingRepository_ResourceLifecycle(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	resID := uniqueResourceID("res-life")
	cleanupResource(t, s, resID)

	// 1. Non-existent resource returns ErrNotFound
	_, err := s.Billing().GetResource(ctx, resID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent resource, got %v", err)
	}

	userID := uniqueUserID()
	now := time.Now().UTC().Truncate(time.Microsecond)
	billingStart := now.Add(-2 * time.Hour)

	r := &billing.ProvisionedResource{
		ID:                  resID,
		UserID:              userID,
		ServerName:          "prod-web-01",
		OrderItemID:         "item-cx23-01",
		ExternalID:          "h-123456",
		Status:              billing.ResourceStatusActive,
		PowerState:          billing.PowerStateRunning,
		HourlyPriceUSD:      7000,
		MonthlyCapUSD:       4490000,
		IPv4Address:         "188.114.96.1",
		IPv6Address:         "2a01:4f8:c010::1",
		Location:            "fsn1",
		ImageName:           "ubuntu-24.04",
		RootPassword:        "initial-secret",
		SSHKey:              "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI...",
		BillingStartAt:      billingStart,
		PostedThroughMinute: 120,
		RemainderMicroIRT:   15,
		LastVerifiedAt:      now,
		CreatedAt:           now,
	}

	// 2. Save resource
	if err := s.Billing().SaveResource(ctx, r); err != nil {
		t.Fatalf("SaveResource failed: %v", err)
	}

	// 3. Get resource and verify all fields
	fetched, err := s.Billing().GetResource(ctx, resID)
	if err != nil {
		t.Fatalf("GetResource failed: %v", err)
	}
	if fetched.ID != resID {
		t.Errorf("ID mismatch: got %s, want %s", fetched.ID, resID)
	}
	if fetched.UserID != userID {
		t.Errorf("UserID mismatch: got %d, want %d", fetched.UserID, userID)
	}
	if fetched.ServerName != "prod-web-01" {
		t.Errorf("ServerName mismatch: got %s, want prod-web-01", fetched.ServerName)
	}
	if fetched.OrderItemID != "item-cx23-01" {
		t.Errorf("OrderItemID mismatch: got %s, want item-cx23-01", fetched.OrderItemID)
	}
	if fetched.ExternalID != "h-123456" {
		t.Errorf("ExternalID mismatch: got %s, want h-123456", fetched.ExternalID)
	}
	if fetched.Status != billing.ResourceStatusActive {
		t.Errorf("Status mismatch: got %s, want %s", fetched.Status, billing.ResourceStatusActive)
	}
	if fetched.PowerState != billing.PowerStateRunning {
		t.Errorf("PowerState mismatch: got %s, want %s", fetched.PowerState, billing.PowerStateRunning)
	}
	if fetched.HourlyPriceUSD != 7000 {
		t.Errorf("HourlyPriceUSD mismatch: got %d, want 7000", fetched.HourlyPriceUSD)
	}
	if fetched.MonthlyCapUSD != 4490000 {
		t.Errorf("MonthlyCapUSD mismatch: got %d, want 4490000", fetched.MonthlyCapUSD)
	}
	if fetched.IPv4Address != "188.114.96.1" {
		t.Errorf("IPv4Address mismatch: got %s, want 188.114.96.1", fetched.IPv4Address)
	}
	if fetched.IPv6Address != "2a01:4f8:c010::1" {
		t.Errorf("IPv6Address mismatch: got %s, want 2a01:4f8:c010::1", fetched.IPv6Address)
	}
	if fetched.Location != "fsn1" {
		t.Errorf("Location mismatch: got %s, want fsn1", fetched.Location)
	}
	if fetched.ImageName != "ubuntu-24.04" {
		t.Errorf("ImageName mismatch: got %s, want ubuntu-24.04", fetched.ImageName)
	}
	if fetched.RootPassword != "initial-secret" {
		t.Errorf("RootPassword mismatch: got %s, want initial-secret", fetched.RootPassword)
	}
	if fetched.SSHKey != "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI..." {
		t.Errorf("SSHKey mismatch: got %s", fetched.SSHKey)
	}
	if fetched.PostedThroughMinute != 120 {
		t.Errorf("PostedThroughMinute mismatch: got %d, want 120", fetched.PostedThroughMinute)
	}
	if fetched.RemainderMicroIRT != 15 {
		t.Errorf("RemainderMicroIRT mismatch: got %d, want 15", fetched.RemainderMicroIRT)
	}
	if fetched.BillingEndAt != nil {
		t.Errorf("expected nil BillingEndAt, got %v", fetched.BillingEndAt)
	}

	// 4. Upsert (update mutable fields)
	fetched.PowerState = billing.PowerStateOff
	fetched.IPv4Address = "188.114.96.2"
	fetched.PostedThroughMinute = 180
	fetched.RemainderMicroIRT = 25
	if err := s.Billing().SaveResource(ctx, fetched); err != nil {
		t.Fatalf("SaveResource upsert failed: %v", err)
	}

	updated, err := s.Billing().GetResource(ctx, resID)
	if err != nil {
		t.Fatalf("GetResource after upsert failed: %v", err)
	}
	if updated.PowerState != billing.PowerStateOff {
		t.Errorf("updated PowerState mismatch: got %s, want off", updated.PowerState)
	}
	if updated.IPv4Address != "188.114.96.2" {
		t.Errorf("updated IPv4Address mismatch: got %s, want 188.114.96.2", updated.IPv4Address)
	}
	if updated.PostedThroughMinute != 180 {
		t.Errorf("updated PostedThroughMinute mismatch: got %d, want 180", updated.PostedThroughMinute)
	}

	// 5. UpdateResourceBillingEnd
	billingEnd := time.Now().UTC().Truncate(time.Microsecond)
	if err := s.Billing().UpdateResourceBillingEnd(ctx, resID, billingEnd, billing.ResourceStatusDeleted); err != nil {
		t.Fatalf("UpdateResourceBillingEnd failed: %v", err)
	}

	afterEnd, err := s.Billing().GetResource(ctx, resID)
	if err != nil {
		t.Fatalf("GetResource after end failed: %v", err)
	}
	if afterEnd.Status != billing.ResourceStatusDeleted {
		t.Errorf("expected status deleted, got %s", afterEnd.Status)
	}
	if afterEnd.BillingEndAt == nil || !afterEnd.BillingEndAt.Equal(billingEnd) {
		t.Errorf("expected BillingEndAt %v, got %v", billingEnd, afterEnd.BillingEndAt)
	}

	// UpdateResourceBillingEnd on non-existent resource
	if err := s.Billing().UpdateResourceBillingEnd(ctx, "non-existent-id", billingEnd, billing.ResourceStatusDeleted); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent resource update, got %v", err)
	}
}

func TestBillingRepository_Listing(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	userA := uniqueUserID()
	userB := uniqueUserID()

	resA1 := uniqueResourceID("res-a1")
	resA2 := uniqueResourceID("res-a2")
	resB1 := uniqueResourceID("res-b1")
	cleanupResource(t, s, resA1)
	cleanupResource(t, s, resA2)
	cleanupResource(t, s, resB1)

	now := time.Now().UTC()

	// Active server for user A
	err := s.Billing().SaveResource(ctx, &billing.ProvisionedResource{
		ID:             resA1,
		UserID:         userA,
		ServerName:     "user-a-srv1",
		OrderItemID:    "ord-1",
		Status:         billing.ResourceStatusActive,
		PowerState:     billing.PowerStateRunning,
		HourlyPriceUSD: 7000,
		MonthlyCapUSD:  4490000,
		BillingStartAt: now,
	})
	if err != nil {
		t.Fatalf("SaveResource resA1 failed: %v", err)
	}

	// Deleted server for user A
	billingEnd := now.Add(time.Hour)
	err = s.Billing().SaveResource(ctx, &billing.ProvisionedResource{
		ID:             resA2,
		UserID:         userA,
		ServerName:     "user-a-srv2-deleted",
		OrderItemID:    "ord-2",
		Status:         billing.ResourceStatusDeleted,
		PowerState:     billing.PowerStateOff,
		HourlyPriceUSD: 7000,
		MonthlyCapUSD:  4490000,
		BillingStartAt: now,
		BillingEndAt:   &billingEnd,
	})
	if err != nil {
		t.Fatalf("SaveResource resA2 failed: %v", err)
	}

	// Active server for user B
	err = s.Billing().SaveResource(ctx, &billing.ProvisionedResource{
		ID:             resB1,
		UserID:         userB,
		ServerName:     "user-b-srv1",
		OrderItemID:    "ord-3",
		Status:         billing.ResourceStatusActive,
		PowerState:     billing.PowerStateRunning,
		HourlyPriceUSD: 13000,
		MonthlyCapUSD:  8490000,
		BillingStartAt: now,
	})
	if err != nil {
		t.Fatalf("SaveResource resB1 failed: %v", err)
	}

	// 1. ListUserResources for user A: should only return resA1
	userAResources, err := s.Billing().ListUserResources(ctx, userA)
	if err != nil {
		t.Fatalf("ListUserResources for user A failed: %v", err)
	}
	var foundA1, foundA2 bool
	for _, r := range userAResources {
		if r.ID == resA1 {
			foundA1 = true
		}
		if r.ID == resA2 {
			foundA2 = true
		}
	}
	if !foundA1 {
		t.Errorf("expected resA1 in user A active resources")
	}
	if foundA2 {
		t.Errorf("resA2 is deleted and should NOT be in user A active resources")
	}

	// 2. ListUserResources for user with no resources: returns empty slice, not nil
	emptyResources, err := s.Billing().ListUserResources(ctx, uniqueUserID())
	if err != nil {
		t.Fatalf("ListUserResources empty failed: %v", err)
	}
	if emptyResources == nil {
		t.Errorf("expected non-nil slice from empty ListUserResources")
	}
	if len(emptyResources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(emptyResources))
	}

	// 3. ListActiveResources: should include resA1 and resB1, but NOT resA2
	activeResources, err := s.Billing().ListActiveResources(ctx)
	if err != nil {
		t.Fatalf("ListActiveResources failed: %v", err)
	}
	var foundActiveA1, foundActiveB1, foundActiveA2 bool
	for _, r := range activeResources {
		if r.ID == resA1 {
			foundActiveA1 = true
		}
		if r.ID == resB1 {
			foundActiveB1 = true
		}
		if r.ID == resA2 {
			foundActiveA2 = true
		}
	}
	if !foundActiveA1 {
		t.Errorf("expected resA1 in ListActiveResources")
	}
	if !foundActiveB1 {
		t.Errorf("expected resB1 in ListActiveResources")
	}
	if foundActiveA2 {
		t.Errorf("expected resA2 (deleted) to NOT be in ListActiveResources")
	}
}

func TestBillingRepository_MinutePostings(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	resID := uniqueResourceID("res-posting")
	cleanupResource(t, s, resID)

	// Save parent resource
	err := s.Billing().SaveResource(ctx, &billing.ProvisionedResource{
		ID:             resID,
		UserID:         uniqueUserID(),
		ServerName:     "posting-host",
		OrderItemID:    "ord-p",
		Status:         billing.ResourceStatusActive,
		PowerState:     billing.PowerStateRunning,
		HourlyPriceUSD: 7000,
		MonthlyCapUSD:  4490000,
	})
	if err != nil {
		t.Fatalf("SaveResource parent failed: %v", err)
	}

	// 1. Insert minute posting 0
	p0 := &billing.MinutePosting{
		ID:                fmt.Sprintf("mp-%s-0", resID),
		ResourceID:        resID,
		MinuteIndex:       0,
		State:             "posted",
		USDMicro:          116,
		AmountIRT:         31,
		RemainderMicroIRT: 400,
		RateObsID:         "obs-1",
		LedgerID:          "led-1",
	}
	if err := s.Billing().SaveMinutePosting(ctx, p0); err != nil {
		t.Fatalf("SaveMinutePosting p0 failed: %v", err)
	}

	// 2. Insert minute posting 1
	p1 := &billing.MinutePosting{
		ID:                fmt.Sprintf("mp-%s-1", resID),
		ResourceID:        resID,
		MinuteIndex:       1,
		State:             "posted",
		USDMicro:          116,
		AmountIRT:         31,
		RemainderMicroIRT: 800,
		RateObsID:         "obs-1",
		LedgerID:          "led-2",
	}
	if err := s.Billing().SaveMinutePosting(ctx, p1); err != nil {
		t.Fatalf("SaveMinutePosting p1 failed: %v", err)
	}

	// 3. Unique constraint enforcement on (resource_id, minute_index)
	duplicateP0 := &billing.MinutePosting{
		ID:          fmt.Sprintf("mp-%s-dup", resID),
		ResourceID:  resID,
		MinuteIndex: 0, // Duplicate index for same resource
		State:       "posted",
		USDMicro:    116,
		AmountIRT:   31,
	}
	err = s.Billing().SaveMinutePosting(ctx, duplicateP0)
	if err == nil {
		t.Fatalf("expected unique constraint error on duplicate (resource_id, minute_index), got nil")
	}

	// 4. Validation errors
	if err := s.Billing().SaveMinutePosting(ctx, nil); err == nil {
		t.Errorf("expected error for nil minute posting")
	}
	pInvalidIndex := &billing.MinutePosting{
		ResourceID:  resID,
		MinuteIndex: -1,
	}
	if err := s.Billing().SaveMinutePosting(ctx, pInvalidIndex); !errors.Is(err, billing.ErrInvalidMinuteIndex) {
		t.Errorf("expected ErrInvalidMinuteIndex, got %v", err)
	}
	pEmptyRes := &billing.MinutePosting{
		ResourceID:  "",
		MinuteIndex: 5,
	}
	if err := s.Billing().SaveMinutePosting(ctx, pEmptyRes); err == nil {
		t.Errorf("expected error for empty resource ID")
	}
}

func TestBillingRepository_Validation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Empty ID on GetResource
	if _, err := s.Billing().GetResource(ctx, ""); err == nil {
		t.Errorf("expected error for empty resource ID on GetResource")
	}

	// Empty ID on UpdateResourceBillingEnd
	if err := s.Billing().UpdateResourceBillingEnd(ctx, "", time.Now(), billing.ResourceStatusDeleted); err == nil {
		t.Errorf("expected error for empty resource ID on UpdateResourceBillingEnd")
	}

	// Validation on SaveResource
	if err := s.Billing().SaveResource(ctx, nil); err == nil {
		t.Errorf("expected error for nil resource on SaveResource")
	}
	if err := s.Billing().SaveResource(ctx, &billing.ProvisionedResource{ID: ""}); err == nil {
		t.Errorf("expected error for empty ID on SaveResource")
	}
	if err := s.Billing().SaveResource(ctx, &billing.ProvisionedResource{ID: "valid", UserID: 0}); err == nil {
		t.Errorf("expected error for non-positive UserID on SaveResource")
	}
}
