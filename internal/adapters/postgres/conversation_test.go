package postgres

import (
	"context"
	"testing"
	"time"
)

type testDraftState struct {
	ItemID       string `json:"item_id"`
	Location     string `json:"location"`
	OSImage      string `json:"os_image"`
	HourlyUSD    int64  `json:"hourly_usd"`
	SSHKeyChosen bool   `json:"ssh_key_chosen"`
}

func cleanupConversation(t testing.TB, s *Store, userID int64, flow string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.Pool().Exec(ctx, "DELETE FROM shop.conversation_states WHERE user_id = $1 AND flow = $2", userID, flow)
	})
}

func TestConversationStore_SaveAndGet(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	userID := uniqueUserID()
	flow := "buy"
	cleanupConversation(t, s, userID, flow)

	// 1. Initial get on empty state returns found=false
	var emptyDraft testDraftState
	step, found, err := s.Conversations().GetConversationState(ctx, userID, flow, &emptyDraft)
	if err != nil {
		t.Fatalf("unexpected error on empty get: %v", err)
	}
	if found {
		t.Fatalf("expected found=false for non-existent conversation state")
	}
	if step != "" {
		t.Fatalf("expected empty step, got %s", step)
	}

	// 2. Save conversation state
	initialDraft := testDraftState{
		ItemID:       "cx23",
		Location:     "fsn1",
		OSImage:      "ubuntu-24.04",
		HourlyUSD:    7000,
		SSHKeyChosen: true,
	}

	err = s.Conversations().SaveConversationState(ctx, userID, flow, "confirm_order", initialDraft, time.Time{})
	if err != nil {
		t.Fatalf("SaveConversationState failed: %v", err)
	}

	// 3. Get conversation state and unmarshal into struct
	var fetchedDraft testDraftState
	step, found, err = s.Conversations().GetConversationState(ctx, userID, flow, &fetchedDraft)
	if err != nil {
		t.Fatalf("GetConversationState failed: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true for saved state")
	}
	if step != "confirm_order" {
		t.Errorf("expected step confirm_order, got %s", step)
	}
	if fetchedDraft != initialDraft {
		t.Errorf("fetched data mismatch: got %+v, want %+v", fetchedDraft, initialDraft)
	}

	// 4. Upsert (update step and data for the same user and flow)
	updatedDraft := initialDraft
	updatedDraft.Location = "hel1"
	err = s.Conversations().Save(ctx, userID, flow, "awaiting_payment", updatedDraft, time.Time{})
	if err != nil {
		t.Fatalf("Save update failed: %v", err)
	}

	var fetchedUpdated testDraftState
	step, found, err = s.Conversations().Get(ctx, userID, flow, &fetchedUpdated)
	if err != nil {
		t.Fatalf("Get after update failed: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true after update")
	}
	if step != "awaiting_payment" {
		t.Errorf("expected step awaiting_payment, got %s", step)
	}
	if fetchedUpdated.Location != "hel1" {
		t.Errorf("expected updated location hel1, got %s", fetchedUpdated.Location)
	}

	// 5. Test with nil dest (only interested in step and found flag)
	stepOnly, foundOnly, err := s.Conversations().GetConversationState(ctx, userID, flow, nil)
	if err != nil {
		t.Fatalf("Get with nil dest failed: %v", err)
	}
	if !foundOnly || stepOnly != "awaiting_payment" {
		t.Errorf("unexpected result with nil dest: found=%v step=%s", foundOnly, stepOnly)
	}
}

func TestConversationStore_Isolation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	user1 := uniqueUserID()
	user2 := uniqueUserID()
	cleanupConversation(t, s, user1, "buy")
	cleanupConversation(t, s, user1, "topup")
	cleanupConversation(t, s, user2, "buy")

	_ = s.Conversations().Save(ctx, user1, "buy", "buy_step_1", map[string]string{"foo": "bar1"}, time.Time{})
	_ = s.Conversations().Save(ctx, user1, "topup", "topup_step_1", map[string]string{"foo": "bar2"}, time.Time{})
	_ = s.Conversations().Save(ctx, user2, "buy", "buy_step_2", map[string]string{"foo": "bar3"}, time.Time{})

	var m map[string]string
	step1, found1, _ := s.Conversations().Get(ctx, user1, "buy", &m)
	if !found1 || step1 != "buy_step_1" || m["foo"] != "bar1" {
		t.Errorf("user1 buy state mismatch: found=%v, step=%s, foo=%s", found1, step1, m["foo"])
	}

	step2, found2, _ := s.Conversations().Get(ctx, user1, "topup", &m)
	if !found2 || step2 != "topup_step_1" || m["foo"] != "bar2" {
		t.Errorf("user1 topup state mismatch: found=%v, step=%s, foo=%s", found2, step2, m["foo"])
	}

	step3, found3, _ := s.Conversations().Get(ctx, user2, "buy", &m)
	if !found3 || step3 != "buy_step_2" || m["foo"] != "bar3" {
		t.Errorf("user2 buy state mismatch: found=%v, step=%s, foo=%s", found3, step3, m["foo"])
	}
}

func TestConversationStore_Expiration(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	userID := uniqueUserID()
	flow := "expiring_flow"
	cleanupConversation(t, s, userID, flow)

	// Save expired state
	past := time.Now().UTC().Add(-10 * time.Minute)
	err := s.Conversations().Save(ctx, userID, flow, "expired_step", "payload", past)
	if err != nil {
		t.Fatalf("Save expired state failed: %v", err)
	}

	// Get should report found=false due to expiration
	var dummy string
	step, found, err := s.Conversations().Get(ctx, userID, flow, &dummy)
	if err != nil {
		t.Fatalf("Get expired state returned error: %v", err)
	}
	if found {
		t.Fatalf("expected found=false for expired state")
	}
	if step != "" {
		t.Errorf("expected empty step for expired state, got %s", step)
	}

	// Save future expiring state
	future := time.Now().UTC().Add(10 * time.Minute)
	err = s.Conversations().Save(ctx, userID, flow, "future_step", "payload", future)
	if err != nil {
		t.Fatalf("Save future state failed: %v", err)
	}

	step, found, err = s.Conversations().Get(ctx, userID, flow, &dummy)
	if err != nil {
		t.Fatalf("Get future state failed: %v", err)
	}
	if !found || step != "future_step" {
		t.Errorf("expected found=true step=future_step, got found=%v step=%s", found, step)
	}
}

func TestConversationStore_Delete(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	userID := uniqueUserID()
	flow := "delete_flow"
	cleanupConversation(t, s, userID, flow)

	// Save state
	err := s.Conversations().Save(ctx, userID, flow, "to_delete", 12345, time.Time{})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Delete state
	if err := s.Conversations().Delete(ctx, userID, flow); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify not found after delete
	var val int
	_, found, err := s.Conversations().Get(ctx, userID, flow, &val)
	if err != nil {
		t.Fatalf("Get after delete failed: %v", err)
	}
	if found {
		t.Fatalf("expected found=false after delete")
	}

	// Delete on non-existent is idempotent
	if err := s.Conversations().Delete(ctx, userID, flow); err != nil {
		t.Fatalf("idempotent delete failed: %v", err)
	}
}

func TestConversationStore_Validation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Invalid userID on Save
	if err := s.Conversations().Save(ctx, 0, "flow", "step", nil, time.Time{}); err == nil {
		t.Errorf("expected error for non-positive userID on Save")
	}
	if err := s.Conversations().Save(ctx, -5, "flow", "step", nil, time.Time{}); err == nil {
		t.Errorf("expected error for negative userID on Save")
	}

	// Empty flow on Save
	if err := s.Conversations().Save(ctx, 1234, "", "step", nil, time.Time{}); err == nil {
		t.Errorf("expected error for empty flow on Save")
	}

	// Invalid userID on Get returns found=false, nil err
	_, found, err := s.Conversations().Get(ctx, 0, "flow", nil)
	if err != nil || found {
		t.Errorf("expected err=nil found=false for invalid userID on Get")
	}

	// Empty flow on Get returns found=false, nil err
	_, found, err = s.Conversations().Get(ctx, 1234, "", nil)
	if err != nil || found {
		t.Errorf("expected err=nil found=false for empty flow on Get")
	}

	// Invalid parameters on Delete return nil
	if err := s.Conversations().Delete(ctx, 0, "flow"); err != nil {
		t.Errorf("expected nil err on invalid userID for Delete")
	}
	if err := s.Conversations().Delete(ctx, 1234, ""); err != nil {
		t.Errorf("expected nil err on empty flow for Delete")
	}
}
