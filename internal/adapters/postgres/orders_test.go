package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/memory"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/order"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

func uniqueOrderID() string {
	return fmt.Sprintf("ord-%d-%04d", time.Now().UnixNano(), rand.Intn(10000))
}

func cleanupOrder(t testing.TB, s *Store, id string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Deleting from shop.orders cascades to shop.order_events
		_, _ = s.Pool().Exec(ctx, "DELETE FROM shop.orders WHERE id = $1", id)
	})
}

func TestOrderRepository_SaveAndGetByID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	orderID := uniqueOrderID()
	cleanupOrder(t, s, orderID)

	// Non-existent lookup returns ErrNotFound (compatible with memory.ErrNotFound)
	_, err := s.Orders().GetByID(ctx, orderID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent order, got %v", err)
	}
	if !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected memory.ErrNotFound compatibility, got %v", err)
	}

	// Empty ID returns ErrNotFound
	if _, err := s.Orders().GetByID(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty ID, got %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	expiresAt := now.Add(2 * time.Hour)
	policyAcceptedAt := now.Add(-10 * time.Minute)

	quote := pricing.NewQuote(
		"quo-cx23-lock",
		7_000,   // Hetzner hourly USD base micro
		270_000, // Market IRT
		10_000,  // Spread IRT
		"obs-101",
		now,
	)

	o := &order.Order{
		ID:               orderID,
		UserID:           999001,
		Status:           order.StateAwaitingPayment,
		CheckoutMode:     order.CheckoutModeDirect,
		QuoteID:          quote.ID,
		LockedQuote:      quote,
		PolicyVersion:    "terms-2026-v1",
		PolicyAcceptedAt: policyAcceptedAt,
		ExpiresAt:        expiresAt,
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := s.Orders().Save(ctx, o, nil); err != nil {
		t.Fatalf("failed to save order: %v", err)
	}

	got, err := s.Orders().GetByID(ctx, orderID)
	if err != nil {
		t.Fatalf("failed to retrieve order: %v", err)
	}

	if got.ID != o.ID {
		t.Errorf("ID mismatch: got %s, want %s", got.ID, o.ID)
	}
	if got.UserID != o.UserID {
		t.Errorf("UserID mismatch: got %d, want %d", got.UserID, o.UserID)
	}
	if got.Status != o.Status {
		t.Errorf("Status mismatch: got %s, want %s", got.Status, o.Status)
	}
	if got.CheckoutMode != o.CheckoutMode {
		t.Errorf("CheckoutMode mismatch: got %s, want %s", got.CheckoutMode, o.CheckoutMode)
	}
	if got.QuoteID != o.QuoteID {
		t.Errorf("QuoteID mismatch: got %s, want %s", got.QuoteID, o.QuoteID)
	}
	if got.PolicyVersion != o.PolicyVersion {
		t.Errorf("PolicyVersion mismatch: got %s, want %s", got.PolicyVersion, o.PolicyVersion)
	}
	if got.Version != o.Version {
		t.Errorf("Version mismatch: got %d, want %d", got.Version, o.Version)
	}
	if !got.PolicyAcceptedAt.Equal(policyAcceptedAt) {
		t.Errorf("PolicyAcceptedAt mismatch: got %v, want %v", got.PolicyAcceptedAt, policyAcceptedAt)
	}
	if !got.ExpiresAt.Equal(expiresAt) {
		t.Errorf("ExpiresAt mismatch: got %v, want %v", got.ExpiresAt, expiresAt)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt mismatch: got %v, want %v", got.CreatedAt, now)
	}

	// Verify locked quote JSON deserialization
	if got.LockedQuote.ID != quote.ID {
		t.Errorf("LockedQuote.ID mismatch: got %s, want %s", got.LockedQuote.ID, quote.ID)
	}
	if got.LockedQuote.PriceIRT != quote.PriceIRT {
		t.Errorf("LockedQuote.PriceIRT mismatch: got %d, want %d", got.LockedQuote.PriceIRT, quote.PriceIRT)
	}
	if got.LockedQuote.SellRate != quote.SellRate {
		t.Errorf("LockedQuote.SellRate mismatch: got %d, want %d", got.LockedQuote.SellRate, quote.SellRate)
	}
	if got.LockedQuote.ObservationID != quote.ObservationID {
		t.Errorf("LockedQuote.ObservationID mismatch: got %s, want %s", got.LockedQuote.ObservationID, quote.ObservationID)
	}
}

func TestOrderRepository_SaveWithEventAndStateTransitions(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	orderID := uniqueOrderID()
	cleanupOrder(t, s, orderID)

	now := time.Now().UTC().Truncate(time.Microsecond)
	o := &order.Order{
		ID:           orderID,
		UserID:       999002,
		Status:       order.StateAwaitingPayment,
		CheckoutMode: order.CheckoutModeDirect,
		CreatedAt:    now,
		UpdatedAt:    now,
		ExpiresAt:    now.Add(time.Hour),
		Version:      1,
	}

	// Save initial order without event
	if err := s.Orders().Save(ctx, o, nil); err != nil {
		t.Fatalf("failed to save initial order: %v", err)
	}

	// 1. Transition: AwaitingPayment -> ReceiptPending
	t1 := now.Add(2 * time.Minute)
	evt1, err := o.Transition(order.StateReceiptPending, "telegram", "msg-receipt-proof", t1)
	if err != nil {
		t.Fatalf("transition 1 failed: %v", err)
	}

	if err := s.Orders().Save(ctx, o, evt1); err != nil {
		t.Fatalf("failed to save transition 1: %v", err)
	}

	// 2. Transition: ReceiptPending -> Paid
	t2 := now.Add(5 * time.Minute)
	evt2, err := o.Transition(order.StatePaid, "admin", "approval-decision-42", t2)
	if err != nil {
		t.Fatalf("transition 2 failed: %v", err)
	}

	if err := s.Orders().Save(ctx, o, evt2); err != nil {
		t.Fatalf("failed to save transition 2: %v", err)
	}

	// Verify order updated in DB
	got, err := s.Orders().GetByID(ctx, orderID)
	if err != nil {
		t.Fatalf("failed to get updated order: %v", err)
	}
	if got.Status != order.StatePaid {
		t.Errorf("expected status StatePaid, got %s", got.Status)
	}
	if got.Version != 3 {
		t.Errorf("expected version 3, got %d", got.Version)
	}

	// Verify event log audit trail
	repo, ok := s.Orders().(*orderRepo)
	if !ok {
		t.Fatal("expected s.Orders() to be *orderRepo")
	}

	events, err := repo.GetEventsByOrderID(ctx, orderID)
	if err != nil {
		t.Fatalf("failed to get events for order: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 order events, got %d", len(events))
	}

	// Event 1 checks
	if events[0].OrderID != orderID {
		t.Errorf("event 0 order_id mismatch: got %s, want %s", events[0].OrderID, orderID)
	}
	if events[0].FromStatus != order.StateAwaitingPayment || events[0].ToStatus != order.StateReceiptPending {
		t.Errorf("event 0 status transition mismatch: from %s to %s", events[0].FromStatus, events[0].ToStatus)
	}
	if events[0].Source != "telegram" || events[0].SourceRef != "msg-receipt-proof" {
		t.Errorf("event 0 source mismatch: %s / %s", events[0].Source, events[0].SourceRef)
	}

	// Event 2 checks
	if events[1].OrderID != orderID {
		t.Errorf("event 1 order_id mismatch: got %s, want %s", events[1].OrderID, orderID)
	}
	if events[1].FromStatus != order.StateReceiptPending || events[1].ToStatus != order.StatePaid {
		t.Errorf("event 1 status transition mismatch: from %s to %s", events[1].FromStatus, events[1].ToStatus)
	}
	if events[1].Source != "admin" || events[1].SourceRef != "approval-decision-42" {
		t.Errorf("event 1 source mismatch: %s / %s", events[1].Source, events[1].SourceRef)
	}
}

func TestOrderRepository_MonthCheckoutAndCheckoutCredit(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	orderID := uniqueOrderID()
	cleanupOrder(t, s, orderID)

	credit := pricing.IRT(1_250_000)
	cardAccountID := "card-account-alpha"

	o := &order.Order{
		ID:                orderID,
		UserID:            999003,
		Status:            order.StateAwaitingPayment,
		CheckoutMode:      order.CheckoutModeMonthCheckout,
		CheckoutCreditIRT: &credit,
		CardAccountID:     &cardAccountID,
		CreatedAt:         time.Now().UTC(),
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
		Version:           1,
	}

	if err := s.Orders().Save(ctx, o, nil); err != nil {
		t.Fatalf("failed to save month checkout order: %v", err)
	}

	got, err := s.Orders().GetByID(ctx, orderID)
	if err != nil {
		t.Fatalf("failed to get order: %v", err)
	}

	if got.CheckoutMode != order.CheckoutModeMonthCheckout {
		t.Errorf("expected CheckoutModeMonthCheckout, got %s", got.CheckoutMode)
	}
	if got.CheckoutCreditIRT == nil || *got.CheckoutCreditIRT != credit {
		t.Errorf("expected CheckoutCreditIRT %d, got %v", credit, got.CheckoutCreditIRT)
	}
	if got.CardAccountID == nil || *got.CardAccountID != cardAccountID {
		t.Errorf("expected CardAccountID %s, got %v", cardAccountID, got.CardAccountID)
	}
}

func TestOrderRepository_HoldReason(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	orderID := uniqueOrderID()
	cleanupOrder(t, s, orderID)

	holdReason := "Risk score exceeded threshold (0.85)"
	o := &order.Order{
		ID:           orderID,
		UserID:       999004,
		Status:       order.StateReceiptPending,
		CheckoutMode: order.CheckoutModeDirect,
		HoldReason:   &holdReason,
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		Version:      1,
	}

	if err := s.Orders().Save(ctx, o, nil); err != nil {
		t.Fatalf("failed to save held order: %v", err)
	}

	got, err := s.Orders().GetByID(ctx, orderID)
	if err != nil {
		t.Fatalf("failed to get order: %v", err)
	}
	if !got.IsOnHold() {
		t.Errorf("expected IsOnHold to be true")
	}
	if got.HoldReason == nil || *got.HoldReason != holdReason {
		t.Errorf("expected HoldReason %q, got %v", holdReason, got.HoldReason)
	}

	// Clear hold reason
	got.HoldReason = nil
	if err := s.Orders().Save(ctx, got, nil); err != nil {
		t.Fatalf("failed to update order clearing hold: %v", err)
	}

	cleared, err := s.Orders().GetByID(ctx, orderID)
	if err != nil {
		t.Fatalf("failed to get cleared order: %v", err)
	}
	if cleared.IsOnHold() {
		t.Errorf("expected IsOnHold to be false after clear")
	}
	if cleared.HoldReason != nil {
		t.Errorf("expected nil HoldReason, got %v", cleared.HoldReason)
	}
}

func TestOrderRepository_ValidationErrors(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Nil order
	if err := s.Orders().Save(ctx, nil, nil); err == nil {
		t.Fatal("expected error for nil order, got nil")
	}

	// Empty ID
	o := &order.Order{
		ID:     "",
		UserID: 999005,
	}
	if err := s.Orders().Save(ctx, o, nil); err == nil {
		t.Fatal("expected error for empty order ID, got nil")
	}

	// Nil store pool
	nilStore := NewStore(nil)
	if _, err := nilStore.Orders().GetByID(ctx, "ord-1"); err == nil {
		t.Fatal("expected error for nil pool GetByID, got nil")
	}
	if err := nilStore.Orders().Save(ctx, o, nil); err == nil {
		t.Fatal("expected error for nil pool Save, got nil")
	}

	nilRepo := &orderRepo{s: nil}
	if events, err := nilRepo.GetEventsByOrderID(ctx, "ord-1"); err == nil || events != nil {
		t.Fatalf("expected error for nil repo pool GetEventsByOrderID, got events=%v, err=%v", events, err)
	}

	repo := s.Orders().(*orderRepo)
	if events, err := repo.GetEventsByOrderID(ctx, ""); err != nil || events != nil {
		t.Fatalf("expected empty result for empty orderID in GetEventsByOrderID, got events=%v, err=%v", events, err)
	}
}
