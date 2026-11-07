package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/memory"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/fee"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
)

func uniqueUserID() int64 {
	return 10_000_000 + (time.Now().UnixNano() % 80_000_000) + rand.Int63n(10_000)
}

func cleanupUser(t testing.TB, s *Store, userID int64) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.Pool().Exec(ctx, "DELETE FROM shop.users WHERE telegram_user_id = $1", userID)
	})
}

func TestUserRepository_SaveAndGetByID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	userID := uniqueUserID()
	cleanupUser(t, s, userID)

	// Non-existent lookup returns ErrNotFound (matching memory.ErrNotFound)
	_, err := s.Users().GetByID(ctx, userID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent user, got %v", err)
	}
	if !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected memory.ErrNotFound compatibility, got %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	invitedBy := int64(999001)
	inviteCode := "inv_init"
	phone := "+989121112233"
	phoneHash := fmt.Sprintf("hash_%d", userID)

	u := &user.User{
		TelegramUserID:     userID,
		Username:           "test_tguser",
		FirstName:          "Ahmad",
		LastName:           "Tehrani",
		Role:               user.RoleAdmin,
		Locale:             "fa",
		Segment:            fee.SegmentLoyal,
		InvitedBy:          &invitedBy,
		InviteCode:         &inviteCode,
		FirstPurchaseState: fee.FirstPurchaseConsumed,
		PhoneVerifiedAt:    &now,
		PhoneE164:          &phone,
		PhoneHash:          &phoneHash,
		Blocked:            false,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := s.Users().Save(ctx, u); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := s.Users().GetByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if got.TelegramUserID != userID {
		t.Errorf("expected ID %d, got %d", userID, got.TelegramUserID)
	}
	if got.Username != "test_tguser" {
		t.Errorf("expected username test_tguser, got %s", got.Username)
	}
	if got.FirstName != "Ahmad" || got.LastName != "Tehrani" {
		t.Errorf("unexpected name %s %s", got.FirstName, got.LastName)
	}
	if got.Role != user.RoleAdmin {
		t.Errorf("expected RoleAdmin, got %s", got.Role)
	}
	if got.Locale != "fa" {
		t.Errorf("expected fa locale, got %s", got.Locale)
	}
	if got.Segment != fee.SegmentLoyal {
		t.Errorf("expected SegmentLoyal, got %s", got.Segment)
	}
	if got.InvitedBy == nil || *got.InvitedBy != invitedBy {
		t.Errorf("expected InvitedBy %d, got %v", invitedBy, got.InvitedBy)
	}
	if got.InviteCode == nil || *got.InviteCode != inviteCode {
		t.Errorf("expected InviteCode %s, got %v", inviteCode, got.InviteCode)
	}
	if got.FirstPurchaseState != fee.FirstPurchaseConsumed {
		t.Errorf("expected FirstPurchaseConsumed, got %s", got.FirstPurchaseState)
	}
	if got.PhoneE164 == nil || *got.PhoneE164 != phone {
		t.Errorf("expected PhoneE164 %s, got %v", phone, got.PhoneE164)
	}
	if got.PhoneHash == nil || *got.PhoneHash != phoneHash {
		t.Errorf("expected PhoneHash %s, got %v", phoneHash, got.PhoneHash)
	}
	if got.PhoneVerifiedAt == nil || !got.PhoneVerifiedAt.Equal(now) {
		t.Errorf("expected PhoneVerifiedAt %v, got %v", now, got.PhoneVerifiedAt)
	}
	if got.Blocked != false {
		t.Errorf("expected Blocked false, got %v", got.Blocked)
	}
}

func TestUserRepository_SaveUpsert(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	userID := uniqueUserID()
	cleanupUser(t, s, userID)

	now := time.Now().UTC().Truncate(time.Microsecond)
	u := user.New(userID, "initial_nick", "Initial", "User", user.RoleUser, now)
	if err := s.Users().Save(ctx, u); err != nil {
		t.Fatalf("initial Save failed: %v", err)
	}

	// Mutate user fields
	later := now.Add(2 * time.Hour)
	u.FirstName = "Updated"
	u.LastName = "Family"
	u.Blocked = true
	u.Role = user.RoleAdmin
	u.Segment = fee.SegmentLoyal
	u.UpdatedAt = later

	if err := s.Users().Save(ctx, u); err != nil {
		t.Fatalf("upsert Save failed: %v", err)
	}

	got, err := s.Users().GetByID(ctx, userID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if got.FirstName != "Updated" || got.LastName != "Family" {
		t.Errorf("expected updated name, got %s %s", got.FirstName, got.LastName)
	}
	if !got.Blocked {
		t.Errorf("expected Blocked to be true")
	}
	if got.Role != user.RoleAdmin {
		t.Errorf("expected RoleAdmin, got %s", got.Role)
	}
	if got.Segment != fee.SegmentLoyal {
		t.Errorf("expected SegmentLoyal, got %s", got.Segment)
	}
	// Verify original created_at was preserved
	if !got.CreatedAt.Equal(now) {
		t.Errorf("expected CreatedAt preserved at %v, got %v", now, got.CreatedAt)
	}
}

func TestUserRepository_FindByPhoneHash(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	userID := uniqueUserID()
	cleanupUser(t, s, userID)

	phone := "+989129998877"
	phoneHash := fmt.Sprintf("phone_hash_%d", userID)
	now := time.Now().UTC().Truncate(time.Microsecond)

	u := user.New(userID, "hashuser", "Hash", "Tester", user.RoleUser, now)
	if err := u.SetVerifiedPhone(phone, phoneHash, now); err != nil {
		t.Fatalf("SetVerifiedPhone failed: %v", err)
	}
	if err := s.Users().Save(ctx, u); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	found, err := s.Users().FindByPhoneHash(ctx, phoneHash)
	if err != nil {
		t.Fatalf("FindByPhoneHash failed: %v", err)
	}
	if found.TelegramUserID != userID {
		t.Fatalf("expected user ID %d, got %d", userID, found.TelegramUserID)
	}

	// Missing phone hash lookup
	_, err = s.Users().FindByPhoneHash(ctx, "non_existent_hash")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent hash, got %v", err)
	}
}

func TestUserRepository_PhoneUnlinkingAndReset(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	user1ID := uniqueUserID()
	user2ID := uniqueUserID()
	cleanupUser(t, s, user1ID)
	cleanupUser(t, s, user2ID)

	phone := "+989123334455"
	sharedHash := fmt.Sprintf("shared_hash_%d", user1ID)
	now := time.Now().UTC().Truncate(time.Microsecond)

	u1 := user.New(user1ID, "user1", "One", "First", user.RoleUser, now)
	_ = u1.SetVerifiedPhone(phone, sharedHash, now)
	if err := s.Users().Save(ctx, u1); err != nil {
		t.Fatalf("Save user1 failed: %v", err)
	}

	// Unlink / reset phone from user1
	resetTime := now.Add(time.Hour)
	u1.ResetPhone(resetTime)
	if err := s.Users().Save(ctx, u1); err != nil {
		t.Fatalf("Save unlinked user1 failed: %v", err)
	}

	reloaded, err := s.Users().GetByID(ctx, user1ID)
	if err != nil {
		t.Fatalf("GetByID user1 failed: %v", err)
	}
	if reloaded.PhoneE164 != nil || reloaded.PhoneHash != nil || reloaded.PhoneVerifiedAt != nil {
		t.Fatalf("expected cleared phone fields, got phone=%v hash=%v verified=%v",
			reloaded.PhoneE164, reloaded.PhoneHash, reloaded.PhoneVerifiedAt)
	}

	// Old hash is no longer found
	_, err = s.Users().FindByPhoneHash(ctx, sharedHash)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unlinked phone hash, got %v", err)
	}

	// User2 can now adopt the unlinked phone hash without unique index violation
	u2 := user.New(user2ID, "user2", "Two", "Second", user.RoleUser, resetTime)
	_ = u2.SetVerifiedPhone(phone, sharedHash, resetTime)
	if err := s.Users().Save(ctx, u2); err != nil {
		t.Fatalf("Save user2 with reclaimed phone hash failed: %v", err)
	}

	found2, err := s.Users().FindByPhoneHash(ctx, sharedHash)
	if err != nil {
		t.Fatalf("FindByPhoneHash user2 failed: %v", err)
	}
	if found2.TelegramUserID != user2ID {
		t.Fatalf("expected user2 ID %d, got %d", user2ID, found2.TelegramUserID)
	}
}

func TestUserRepository_CountAndListUsers(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	initialCount, err := s.Users().Count(ctx)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}

	uID1 := uniqueUserID()
	uID2 := uID1 + 1
	uID3 := uID1 + 2

	cleanupUser(t, s, uID1)
	cleanupUser(t, s, uID2)
	cleanupUser(t, s, uID3)

	now := time.Now().UTC().Truncate(time.Microsecond)
	_ = s.Users().Save(ctx, user.New(uID1, "u1", "A", "A", user.RoleUser, now))
	_ = s.Users().Save(ctx, user.New(uID2, "u2", "B", "B", user.RoleUser, now))
	_ = s.Users().Save(ctx, user.New(uID3, "u3", "C", "C", user.RoleUser, now))

	newCount, err := s.Users().Count(ctx)
	if err != nil {
		t.Fatalf("Count after insert failed: %v", err)
	}
	if newCount != initialCount+3 {
		t.Fatalf("expected count %d, got %d", initialCount+3, newCount)
	}

	// List with limit
	limited, err := s.Users().ListUsers(ctx, 2)
	if err != nil {
		t.Fatalf("ListUsers(2) failed: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("expected 2 users from ListUsers(2), got %d", len(limited))
	}

	// List without limit
	all, err := s.Users().ListUsers(ctx, 0)
	if err != nil {
		t.Fatalf("ListUsers(0) failed: %v", err)
	}
	if int64(len(all)) != newCount {
		t.Fatalf("expected %d users, got %d", newCount, len(all))
	}

	// Verify ordering by telegram_user_id ascending
	for i := 1; i < len(all); i++ {
		if all[i].TelegramUserID < all[i-1].TelegramUserID {
			t.Fatalf("users not sorted by ID ascending at index %d", i)
		}
	}
}
