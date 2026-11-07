package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/invite"
)

func uniqueInviteCode(prefix string) string {
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano()%100_000_000, rand.Int63n(10_000))
}

func cleanupInvite(t testing.TB, s *Store, code string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.Pool().Exec(ctx, "DELETE FROM shop.invites WHERE code = $1", code)
	})
}

func TestInviteRepository_CreateAndGetByCode(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	code := uniqueInviteCode("t_create")
	cleanupInvite(t, s, code)

	// Non-existent lookup returns ErrInviteNotFound
	_, err := s.Invites().GetByCode(ctx, code)
	if !errors.Is(err, invite.ErrInviteNotFound) {
		t.Fatalf("expected ErrInviteNotFound, got %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	exp := now.Add(24 * time.Hour)
	inv := &invite.Invite{
		Code:      code,
		CreatedBy: 888123,
		MaxUses:   5,
		UsesCount: 0,
		ExpiresAt: &exp,
		IsActive:  true,
		CreatedAt: now,
	}

	if err := s.Invites().Create(ctx, inv); err != nil {
		t.Fatalf("Create invite failed: %v", err)
	}

	got, err := s.Invites().GetByCode(ctx, code)
	if err != nil {
		t.Fatalf("GetByCode failed: %v", err)
	}

	if got.Code != code {
		t.Errorf("expected code %s, got %s", code, got.Code)
	}
	if got.CreatedBy != 888123 {
		t.Errorf("expected CreatedBy 888123, got %d", got.CreatedBy)
	}
	if got.MaxUses != 5 {
		t.Errorf("expected MaxUses 5, got %d", got.MaxUses)
	}
	if got.UsesCount != 0 {
		t.Errorf("expected UsesCount 0, got %d", got.UsesCount)
	}
	if !got.IsActive {
		t.Errorf("expected IsActive true, got %v", got.IsActive)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) {
		t.Errorf("expected ExpiresAt %v, got %v", exp, got.ExpiresAt)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("expected CreatedAt %v, got %v", now, got.CreatedAt)
	}
}

func TestInviteRepository_Claim_Success(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	code := uniqueInviteCode("t_claim")
	cleanupInvite(t, s, code)

	adminID := int64(111222)
	now := time.Now().UTC().Truncate(time.Microsecond)
	inv := &invite.Invite{
		Code:      code,
		CreatedBy: adminID,
		MaxUses:   2,
		UsesCount: 0,
		IsActive:  true,
		CreatedAt: now,
	}
	if err := s.Invites().Create(ctx, inv); err != nil {
		t.Fatalf("Create invite failed: %v", err)
	}

	// Claim 1
	user1ID := int64(333444)
	c1, err := s.Invites().Claim(ctx, code, user1ID, now)
	if err != nil {
		t.Fatalf("Claim 1 failed: %v", err)
	}
	if c1.UsesCount != 1 {
		t.Errorf("expected UsesCount 1, got %d", c1.UsesCount)
	}
	if !c1.IsActive {
		t.Errorf("expected IsActive true after 1 use of 2, got false")
	}

	// Claim 2: exhaust capacity
	user2ID := int64(555666)
	c2, err := s.Invites().Claim(ctx, code, user2ID, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Claim 2 failed: %v", err)
	}
	if c2.UsesCount != 2 {
		t.Errorf("expected UsesCount 2, got %d", c2.UsesCount)
	}
	if c2.IsActive {
		t.Errorf("expected IsActive false after reaching max_uses 2, got true")
	}

	// Verify persistence in DB
	reloaded, err := s.Invites().GetByCode(ctx, code)
	if err != nil {
		t.Fatalf("GetByCode after claims failed: %v", err)
	}
	if reloaded.UsesCount != 2 || reloaded.IsActive != false {
		t.Fatalf("unexpected reloaded invite: %+v", reloaded)
	}
}

func TestInviteRepository_Claim_DoubleClaimPrevention(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	code := uniqueInviteCode("t_dup")
	cleanupInvite(t, s, code)

	adminID := int64(1001)
	userID := int64(2002)
	now := time.Now().UTC().Truncate(time.Microsecond)

	inv := &invite.Invite{
		Code:      code,
		CreatedBy: adminID,
		MaxUses:   5,
		UsesCount: 0,
		IsActive:  true,
		CreatedAt: now,
	}
	if err := s.Invites().Create(ctx, inv); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// First claim succeeds
	if _, err := s.Invites().Claim(ctx, code, userID, now); err != nil {
		t.Fatalf("first Claim failed: %v", err)
	}

	// Second claim by the same user is rejected with ErrInviteAlreadyUsed
	_, err := s.Invites().Claim(ctx, code, userID, now.Add(time.Minute))
	if !errors.Is(err, invite.ErrInviteAlreadyUsed) {
		t.Fatalf("expected ErrInviteAlreadyUsed for double-claim, got %v", err)
	}
}

func TestInviteRepository_Claim_MaxUsesExhaustion(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	code := uniqueInviteCode("t_exhaust")
	cleanupInvite(t, s, code)

	adminID := int64(1001)
	user1ID := int64(2001)
	user2ID := int64(2002)
	now := time.Now().UTC().Truncate(time.Microsecond)

	inv := &invite.Invite{
		Code:      code,
		CreatedBy: adminID,
		MaxUses:   1,
		UsesCount: 0,
		IsActive:  true,
		CreatedAt: now,
	}
	if err := s.Invites().Create(ctx, inv); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// First claim uses all capacity
	if _, err := s.Invites().Claim(ctx, code, user1ID, now); err != nil {
		t.Fatalf("claim 1 failed: %v", err)
	}

	// Second user claim is rejected with ErrInviteUsed
	_, err := s.Invites().Claim(ctx, code, user2ID, now.Add(time.Minute))
	if !errors.Is(err, invite.ErrInviteUsed) {
		t.Fatalf("expected ErrInviteUsed when exhausted, got %v", err)
	}
}

func TestInviteRepository_Claim_Expired(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	code := uniqueInviteCode("t_exp")
	cleanupInvite(t, s, code)

	adminID := int64(1001)
	userID := int64(2001)
	now := time.Now().UTC().Truncate(time.Microsecond)
	past := now.Add(-1 * time.Hour)

	inv := &invite.Invite{
		Code:      code,
		CreatedBy: adminID,
		MaxUses:   5,
		UsesCount: 0,
		ExpiresAt: &past,
		IsActive:  true,
		CreatedAt: now.Add(-2 * time.Hour),
	}
	if err := s.Invites().Create(ctx, inv); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	_, err := s.Invites().Claim(ctx, code, userID, now)
	if !errors.Is(err, invite.ErrInviteExpired) {
		t.Fatalf("expected ErrInviteExpired, got %v", err)
	}
}

func TestInviteRepository_Claim_SelfClaim(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	code := uniqueInviteCode("t_self")
	cleanupInvite(t, s, code)

	adminID := int64(777888)
	now := time.Now().UTC().Truncate(time.Microsecond)

	inv := &invite.Invite{
		Code:      code,
		CreatedBy: adminID,
		MaxUses:   5,
		UsesCount: 0,
		IsActive:  true,
		CreatedAt: now,
	}
	if err := s.Invites().Create(ctx, inv); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Admin attempts to claim their own invite
	_, err := s.Invites().Claim(ctx, code, adminID, now)
	if !errors.Is(err, invite.ErrInviteSelf) {
		t.Fatalf("expected ErrInviteSelf, got %v", err)
	}
}

func TestInviteRepository_ConcurrentClaims(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	code := uniqueInviteCode("t_conc")
	cleanupInvite(t, s, code)

	adminID := int64(999999)
	maxUses := 3
	numAttempts := 10
	now := time.Now().UTC().Truncate(time.Microsecond)

	inv := &invite.Invite{
		Code:      code,
		CreatedBy: adminID,
		MaxUses:   maxUses,
		UsesCount: 0,
		IsActive:  true,
		CreatedAt: now,
	}
	if err := s.Invites().Create(ctx, inv); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	var (
		successCount atomic.Int32
		usedCount    atomic.Int32
		otherCount   atomic.Int32
		wg           sync.WaitGroup
	)

	startBarrier := make(chan struct{})

	for i := 0; i < numAttempts; i++ {
		wg.Add(1)
		candidateUserID := int64(40_000_000 + i)
		go func(uid int64) {
			defer wg.Done()
			<-startBarrier

			_, err := s.Invites().Claim(ctx, code, uid, now)
			if err == nil {
				successCount.Add(1)
			} else if errors.Is(err, invite.ErrInviteUsed) {
				usedCount.Add(1)
			} else {
				otherCount.Add(1)
				t.Logf("unexpected error for candidate %d: %v", uid, err)
			}
		}(candidateUserID)
	}

	close(startBarrier)
	wg.Wait()

	if int(successCount.Load()) != maxUses {
		t.Fatalf("expected exactly %d successful claims, got %d", maxUses, successCount.Load())
	}
	if int(usedCount.Load()) != numAttempts-maxUses {
		t.Fatalf("expected %d ErrInviteUsed rejections, got %d", numAttempts-maxUses, usedCount.Load())
	}
	if otherCount.Load() != 0 {
		t.Fatalf("encountered %d unexpected errors during concurrent claim", otherCount.Load())
	}

	// Verify database state
	finalInv, err := s.Invites().GetByCode(ctx, code)
	if err != nil {
		t.Fatalf("GetByCode failed: %v", err)
	}
	if finalInv.UsesCount != maxUses {
		t.Fatalf("expected uses_count %d, got %d", maxUses, finalInv.UsesCount)
	}
	if finalInv.IsActive {
		t.Fatalf("expected is_active false, got true")
	}

	// Verify shop.invite_claims rows
	var recordedClaims int
	err = s.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM shop.invite_claims WHERE invite_code = $1", code).Scan(&recordedClaims)
	if err != nil {
		t.Fatalf("count invite_claims failed: %v", err)
	}
	if recordedClaims != maxUses {
		t.Fatalf("expected %d rows in shop.invite_claims, got %d", maxUses, recordedClaims)
	}
}

func TestInviteRepository_ListByCreator(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	admin1 := int64(7001)
	admin2 := int64(7002)

	code1 := uniqueInviteCode("t_lst1")
	code2 := uniqueInviteCode("t_lst2")
	code3 := uniqueInviteCode("t_lst3")

	cleanupInvite(t, s, code1)
	cleanupInvite(t, s, code2)
	cleanupInvite(t, s, code3)

	now := time.Now().UTC().Truncate(time.Microsecond)

	_ = s.Invites().Create(ctx, &invite.Invite{
		Code:      code1,
		CreatedBy: admin1,
		MaxUses:   1,
		IsActive:  true,
		CreatedAt: now.Add(-10 * time.Minute),
	})
	_ = s.Invites().Create(ctx, &invite.Invite{
		Code:      code2,
		CreatedBy: admin1,
		MaxUses:   2,
		IsActive:  true,
		CreatedAt: now,
	})
	_ = s.Invites().Create(ctx, &invite.Invite{
		Code:      code3,
		CreatedBy: admin2,
		MaxUses:   3,
		IsActive:  true,
		CreatedAt: now,
	})

	list1, err := s.Invites().ListByCreator(ctx, admin1)
	if err != nil {
		t.Fatalf("ListByCreator admin1 failed: %v", err)
	}
	if len(list1) < 2 {
		t.Fatalf("expected at least 2 invites for admin1, got %d", len(list1))
	}

	// Verify only admin1 invites are returned and code2 (newer) comes before code1 (older)
	foundCode1 := false
	foundCode2 := false
	var idx1, idx2 int
	for i, inv := range list1 {
		if inv.CreatedBy != admin1 {
			t.Fatalf("unexpected creator %d in admin1 list", inv.CreatedBy)
		}
		if inv.Code == code1 {
			foundCode1 = true
			idx1 = i
		}
		if inv.Code == code2 {
			foundCode2 = true
			idx2 = i
		}
	}
	if !foundCode1 || !foundCode2 {
		t.Fatalf("expected code1 and code2 in list1, got %+v", list1)
	}
	if idx2 > idx1 {
		t.Fatalf("expected newer invite code2 (idx %d) before older invite code1 (idx %d)", idx2, idx1)
	}
}
