package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/memory"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/receipt"
)

func uniqueReceiptID() string {
	return fmt.Sprintf("rcp-%d-%04d", time.Now().UnixNano(), rand.Intn(10000))
}

func cleanupReceipt(t testing.TB, s *Store, id string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.Pool().Exec(ctx, "DELETE FROM shop.receipts WHERE id = $1", id)
	})
}

func TestReceiptRepository_SaveAndGetByID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	rcpID := uniqueReceiptID()
	cleanupReceipt(t, s, rcpID)

	// Non-existent lookup returns ErrNotFound (compatible with memory.ErrNotFound)
	_, err := s.Receipts().GetByID(ctx, rcpID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent receipt, got %v", err)
	}
	if !errors.Is(err, memory.ErrNotFound) {
		t.Fatalf("expected memory.ErrNotFound compatibility, got %v", err)
	}

	// Empty ID returns ErrNotFound
	if _, err := s.Receipts().GetByID(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty ID, got %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	orderID := "ord-12345"
	quoteID := "quo-67890"
	note := "Test receipt note"
	rcp := &receipt.Receipt{
		ID:               rcpID,
		UserID:           888001,
		Purpose:          receipt.PurposeOrder,
		OrderID:          &orderID,
		QuoteID:          &quoteID,
		AmountIRR:        pricing.IRR(270_000_000),
		AmountIRT:        pricing.IRT(27_000_000),
		TrackingCodeNorm: fmt.Sprintf("TRK%d", time.Now().UnixNano()),
		ImageSHA256:      fmt.Sprintf("sha256-%d", time.Now().UnixNano()),
		ImageRef:         "s3://receipts/test.jpg",
		Status:           receipt.StatusSubmitted,
		Note:             &note,
		CreatedAt:        now,
	}

	if err := s.Receipts().Save(ctx, rcp); err != nil {
		t.Fatalf("failed to save receipt: %v", err)
	}

	got, err := s.Receipts().GetByID(ctx, rcpID)
	if err != nil {
		t.Fatalf("failed to retrieve saved receipt: %v", err)
	}

	if got.ID != rcp.ID {
		t.Errorf("ID mismatch: got %s, want %s", got.ID, rcp.ID)
	}
	if got.UserID != rcp.UserID {
		t.Errorf("UserID mismatch: got %d, want %d", got.UserID, rcp.UserID)
	}
	if got.Purpose != rcp.Purpose {
		t.Errorf("Purpose mismatch: got %s, want %s", got.Purpose, rcp.Purpose)
	}
	if got.OrderID == nil || *got.OrderID != orderID {
		t.Errorf("OrderID mismatch: got %v, want %s", got.OrderID, orderID)
	}
	if got.QuoteID == nil || *got.QuoteID != quoteID {
		t.Errorf("QuoteID mismatch: got %v, want %s", got.QuoteID, quoteID)
	}
	if got.AmountIRR != rcp.AmountIRR {
		t.Errorf("AmountIRR mismatch: got %d, want %d", got.AmountIRR, rcp.AmountIRR)
	}
	if got.AmountIRT != rcp.AmountIRT {
		t.Errorf("AmountIRT mismatch: got %d, want %d", got.AmountIRT, rcp.AmountIRT)
	}
	if got.TrackingCodeNorm != rcp.TrackingCodeNorm {
		t.Errorf("TrackingCodeNorm mismatch: got %s, want %s", got.TrackingCodeNorm, rcp.TrackingCodeNorm)
	}
	if got.ImageSHA256 != rcp.ImageSHA256 {
		t.Errorf("ImageSHA256 mismatch: got %s, want %s", got.ImageSHA256, rcp.ImageSHA256)
	}
	if got.ImageRef != rcp.ImageRef {
		t.Errorf("ImageRef mismatch: got %s, want %s", got.ImageRef, rcp.ImageRef)
	}
	if got.Status != rcp.Status {
		t.Errorf("Status mismatch: got %s, want %s", got.Status, rcp.Status)
	}
	if got.Note == nil || *got.Note != note {
		t.Errorf("Note mismatch: got %v, want %s", got.Note, note)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt mismatch: got %v, want %v", got.CreatedAt, now)
	}
}

func TestReceiptRepository_FindByImageSHA256(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Empty hash
	if _, err := s.Receipts().FindByImageSHA256(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty hash, got %v", err)
	}

	// Non-existent hash
	if _, err := s.Receipts().FindByImageSHA256(ctx, "nonexistent-hash"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent hash, got %v", err)
	}

	rcpID := uniqueReceiptID()
	cleanupReceipt(t, s, rcpID)

	imageHash := fmt.Sprintf("sha256-img-%d", time.Now().UnixNano())
	rcp := &receipt.Receipt{
		ID:               rcpID,
		UserID:           888002,
		Purpose:          receipt.PurposeWalletTopup,
		AmountIRR:        50_000_000,
		AmountIRT:        5_000_000,
		TrackingCodeNorm: fmt.Sprintf("TRK%d", time.Now().UnixNano()),
		ImageSHA256:      imageHash,
		ImageRef:         "s3://receipts/hash-lookup.jpg",
		Status:           receipt.StatusSubmitted,
		CreatedAt:        time.Now().UTC(),
	}

	if err := s.Receipts().Save(ctx, rcp); err != nil {
		t.Fatalf("failed to save receipt: %v", err)
	}

	found, err := s.Receipts().FindByImageSHA256(ctx, imageHash)
	if err != nil {
		t.Fatalf("failed to find receipt by image hash: %v", err)
	}
	if found.ID != rcpID {
		t.Errorf("expected ID %s, got %s", rcpID, found.ID)
	}
	if found.ImageSHA256 != imageHash {
		t.Errorf("expected hash %s, got %s", imageHash, found.ImageSHA256)
	}
}

func TestReceiptRepository_FindByTrackingCode(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Empty tracking code
	if _, err := s.Receipts().FindByTrackingCode(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty tracking code, got %v", err)
	}

	// Non-existent tracking code
	if _, err := s.Receipts().FindByTrackingCode(ctx, "NONEXISTENT999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent tracking code, got %v", err)
	}

	rcpID := uniqueReceiptID()
	cleanupReceipt(t, s, rcpID)

	// Save receipt with Latin tracking code
	trackCode := fmt.Sprintf("TRK%d", time.Now().UnixNano()%100000000)
	rcp := &receipt.Receipt{
		ID:               rcpID,
		UserID:           888003,
		Purpose:          receipt.PurposeUSDTopup,
		AmountIRR:        100_000_000,
		AmountIRT:        10_000_000,
		TrackingCodeNorm: trackCode,
		ImageSHA256:      fmt.Sprintf("sha256-%d", time.Now().UnixNano()),
		Status:           receipt.StatusSubmitted,
		CreatedAt:        time.Now().UTC(),
	}

	if err := s.Receipts().Save(ctx, rcp); err != nil {
		t.Fatalf("failed to save receipt: %v", err)
	}

	// Exact lookup
	found, err := s.Receipts().FindByTrackingCode(ctx, trackCode)
	if err != nil {
		t.Fatalf("failed to find receipt by exact tracking code: %v", err)
	}
	if found.ID != rcpID {
		t.Errorf("expected ID %s, got %s", rcpID, found.ID)
	}

	// Lookup with padding and dashes
	foundPadded, err := s.Receipts().FindByTrackingCode(ctx, fmt.Sprintf("  %s  ", trackCode))
	if err != nil {
		t.Fatalf("failed to find receipt with padded tracking code: %v", err)
	}
	if foundPadded.ID != rcpID {
		t.Errorf("expected ID %s, got %s", rcpID, foundPadded.ID)
	}

	// Test Persian digits normalization lookup
	rcpIDPersian := uniqueReceiptID()
	cleanupReceipt(t, s, rcpIDPersian)

	// Persian digits: '۱۲۳۴۵۶' -> '123456'
	persianCodeRaw := fmt.Sprintf("۱۲۳۴%d", time.Now().UnixNano()%10000)
	normalizedCode := receipt.NormalizeTrackingCode(persianCodeRaw)

	rcpPersian := &receipt.Receipt{
		ID:               rcpIDPersian,
		UserID:           888004,
		Purpose:          receipt.PurposeWalletTopup,
		AmountIRR:        20_000_000,
		AmountIRT:        2_000_000,
		TrackingCodeNorm: persianCodeRaw, // Save will normalize it to Latin digits
		ImageSHA256:      fmt.Sprintf("sha256-persian-%d", time.Now().UnixNano()),
		Status:           receipt.StatusSubmitted,
		CreatedAt:        time.Now().UTC(),
	}
	if err := s.Receipts().Save(ctx, rcpPersian); err != nil {
		t.Fatalf("failed to save receipt with persian tracking code: %v", err)
	}

	// Find using Persian input
	foundByPersian, err := s.Receipts().FindByTrackingCode(ctx, persianCodeRaw)
	if err != nil {
		t.Fatalf("failed to find receipt by persian tracking digits: %v", err)
	}
	if foundByPersian.ID != rcpIDPersian {
		t.Errorf("expected ID %s, got %s", rcpIDPersian, foundByPersian.ID)
	}

	// Find using normalized Latin digits
	foundByLatin, err := s.Receipts().FindByTrackingCode(ctx, normalizedCode)
	if err != nil {
		t.Fatalf("failed to find receipt by latin normalized digits: %v", err)
	}
	if foundByLatin.ID != rcpIDPersian {
		t.Errorf("expected ID %s, got %s", rcpIDPersian, foundByLatin.ID)
	}
}

func TestReceiptRepository_StatusMutation_ApproveAndReject(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// 1. Approval flow
	rcpID1 := uniqueReceiptID()
	cleanupReceipt(t, s, rcpID1)

	rcp1 := &receipt.Receipt{
		ID:               rcpID1,
		UserID:           888005,
		Purpose:          receipt.PurposeOrder,
		AmountIRR:        100_000_000,
		AmountIRT:        10_000_000,
		TrackingCodeNorm: fmt.Sprintf("TRK-APP-%d", time.Now().UnixNano()),
		ImageSHA256:      fmt.Sprintf("sha256-app-%d", time.Now().UnixNano()),
		Status:           receipt.StatusSubmitted,
		CreatedAt:        time.Now().UTC(),
	}
	if err := s.Receipts().Save(ctx, rcp1); err != nil {
		t.Fatalf("failed to save receipt 1: %v", err)
	}

	adminID := int64(700001)
	decisionTime := time.Now().UTC().Truncate(time.Microsecond)
	if err := rcp1.Approve(adminID, decisionTime); err != nil {
		t.Fatalf("rcp.Approve failed: %v", err)
	}

	if err := s.Receipts().Save(ctx, rcp1); err != nil {
		t.Fatalf("failed to save approved receipt: %v", err)
	}

	got1, err := s.Receipts().GetByID(ctx, rcpID1)
	if err != nil {
		t.Fatalf("failed to get approved receipt: %v", err)
	}
	if got1.Status != receipt.StatusApproved {
		t.Errorf("expected StatusApproved, got %s", got1.Status)
	}
	if got1.DecidedByAdminID == nil || *got1.DecidedByAdminID != adminID {
		t.Errorf("expected DecidedByAdminID %d, got %v", adminID, got1.DecidedByAdminID)
	}
	if got1.DecidedAt == nil || !got1.DecidedAt.Equal(decisionTime) {
		t.Errorf("expected DecidedAt %v, got %v", decisionTime, got1.DecidedAt)
	}

	// 2. Rejection flow
	rcpID2 := uniqueReceiptID()
	cleanupReceipt(t, s, rcpID2)

	rcp2 := &receipt.Receipt{
		ID:               rcpID2,
		UserID:           888006,
		Purpose:          receipt.PurposeWalletTopup,
		AmountIRR:        50_000_000,
		AmountIRT:        5_000_000,
		TrackingCodeNorm: fmt.Sprintf("TRK-REJ-%d", time.Now().UnixNano()),
		ImageSHA256:      fmt.Sprintf("sha256-rej-%d", time.Now().UnixNano()),
		Status:           receipt.StatusPendingReview,
		CreatedAt:        time.Now().UTC(),
	}
	if err := s.Receipts().Save(ctx, rcp2); err != nil {
		t.Fatalf("failed to save receipt 2: %v", err)
	}

	rejReason := receipt.RejectDuplicate
	rejNote := "Receipt has already been redeemed"
	if err := rcp2.Reject(adminID, rejReason, rejNote, decisionTime); err != nil {
		t.Fatalf("rcp.Reject failed: %v", err)
	}

	if err := s.Receipts().Save(ctx, rcp2); err != nil {
		t.Fatalf("failed to save rejected receipt: %v", err)
	}

	got2, err := s.Receipts().GetByID(ctx, rcpID2)
	if err != nil {
		t.Fatalf("failed to get rejected receipt: %v", err)
	}
	if got2.Status != receipt.StatusRejected {
		t.Errorf("expected StatusRejected, got %s", got2.Status)
	}
	if got2.DecidedByAdminID == nil || *got2.DecidedByAdminID != adminID {
		t.Errorf("expected DecidedByAdminID %d, got %v", adminID, got2.DecidedByAdminID)
	}
	if got2.RejectReason == nil || *got2.RejectReason != rejReason {
		t.Errorf("expected RejectReason %s, got %v", rejReason, got2.RejectReason)
	}
	if got2.Note == nil || *got2.Note != rejNote {
		t.Errorf("expected Note %q, got %v", rejNote, got2.Note)
	}
}

func TestReceiptRepository_AntiReplayUniqueness(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	rcpID1 := uniqueReceiptID()
	cleanupReceipt(t, s, rcpID1)

	imageHash := fmt.Sprintf("sha256-replay-%d", time.Now().UnixNano())
	trackCode := fmt.Sprintf("TRKREPLAY%d", time.Now().UnixNano()%10000000)

	rcp1 := &receipt.Receipt{
		ID:               rcpID1,
		UserID:           888007,
		Purpose:          receipt.PurposeWalletTopup,
		AmountIRR:        30_000_000,
		AmountIRT:        3_000_000,
		TrackingCodeNorm: trackCode,
		ImageSHA256:      imageHash,
		Status:           receipt.StatusSubmitted,
		CreatedAt:        time.Now().UTC(),
	}
	if err := s.Receipts().Save(ctx, rcp1); err != nil {
		t.Fatalf("failed to save original receipt: %v", err)
	}

	// 1. Replay with duplicate image hash on a new receipt ID -> must fail
	rcpID2 := uniqueReceiptID()
	cleanupReceipt(t, s, rcpID2)

	rcpDuplicateHash := &receipt.Receipt{
		ID:               rcpID2,
		UserID:           888008,
		Purpose:          receipt.PurposeWalletTopup,
		AmountIRR:        30_000_000,
		AmountIRT:        3_000_000,
		TrackingCodeNorm: fmt.Sprintf("TRK-DIFF-%d", time.Now().UnixNano()),
		ImageSHA256:      imageHash, // Duplicate image hash!
		Status:           receipt.StatusSubmitted,
		CreatedAt:        time.Now().UTC(),
	}
	err := s.Receipts().Save(ctx, rcpDuplicateHash)
	if err == nil {
		t.Fatal("expected unique violation error for duplicate image_sha256, got nil")
	}

	// 2. Replay with duplicate tracking code on a new receipt ID -> must fail
	rcpID3 := uniqueReceiptID()
	cleanupReceipt(t, s, rcpID3)

	rcpDuplicateTrack := &receipt.Receipt{
		ID:               rcpID3,
		UserID:           888009,
		Purpose:          receipt.PurposeWalletTopup,
		AmountIRR:        30_000_000,
		AmountIRT:        3_000_000,
		TrackingCodeNorm: trackCode, // Duplicate tracking code!
		ImageSHA256:      fmt.Sprintf("sha256-diff-%d", time.Now().UnixNano()),
		Status:           receipt.StatusSubmitted,
		CreatedAt:        time.Now().UTC(),
	}
	err = s.Receipts().Save(ctx, rcpDuplicateTrack)
	if err == nil {
		t.Fatal("expected unique violation error for duplicate tracking_code_norm, got nil")
	}

	// 3. Upserting the same receipt ID -> succeeds
	rcp1.Status = receipt.StatusApproved
	if err := s.Receipts().Save(ctx, rcp1); err != nil {
		t.Fatalf("expected upsert on same ID to succeed, got %v", err)
	}
}

func TestReceiptRepository_ValidationErrors(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Nil receipt
	if err := s.Receipts().Save(ctx, nil); err == nil {
		t.Fatal("expected error for nil receipt, got nil")
	}

	// Empty ID
	rcp := &receipt.Receipt{
		ID:               "",
		UserID:           888010,
		Purpose:          receipt.PurposeWalletTopup,
		TrackingCodeNorm: "TRK123",
		ImageSHA256:      "sha256-test",
	}
	if err := s.Receipts().Save(ctx, rcp); err == nil {
		t.Fatal("expected error for empty receipt ID, got nil")
	}

	// Empty image hash
	rcp.ID = uniqueReceiptID()
	rcp.ImageSHA256 = ""
	if err := s.Receipts().Save(ctx, rcp); err == nil {
		t.Fatal("expected error for empty ImageSHA256, got nil")
	}

	// Empty tracking code
	rcp.ImageSHA256 = "sha256-valid"
	rcp.TrackingCodeNorm = ""
	if err := s.Receipts().Save(ctx, rcp); err == nil {
		t.Fatal("expected error for empty TrackingCodeNorm, got nil")
	}

	// Nil store pool
	nilStore := NewStore(nil)
	if _, err := nilStore.Receipts().GetByID(ctx, "rcp-1"); err == nil {
		t.Fatal("expected error for nil pool GetByID, got nil")
	}
	if _, err := nilStore.Receipts().FindByImageSHA256(ctx, "hash"); err == nil {
		t.Fatal("expected error for nil pool FindByImageSHA256, got nil")
	}
	if _, err := nilStore.Receipts().FindByTrackingCode(ctx, "code"); err == nil {
		t.Fatal("expected error for nil pool FindByTrackingCode, got nil")
	}
	if err := nilStore.Receipts().Save(ctx, rcp); err == nil {
		t.Fatal("expected error for nil pool Save, got nil")
	}
}

func TestReceiptRepository_ApproveReceipt(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	rcpID := uniqueReceiptID()
	cleanupReceipt(t, s, rcpID)
	userID := int64(888777)
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.Pool().Exec(cleanCtx, "DELETE FROM shop.wallets WHERE user_id = $1", userID)
		_, _ = s.Pool().Exec(cleanCtx, "DELETE FROM shop.wallet_ledger WHERE wallet_id = $1", userID)
	})

	now := time.Now().UTC().Truncate(time.Microsecond)
	rcp := &receipt.Receipt{
		ID:               rcpID,
		UserID:           userID,
		Purpose:          receipt.PurposeWalletTopup,
		AmountIRR:        pricing.IRR(100_000_000),
		AmountIRT:        pricing.IRT(10_000_000),
		TrackingCodeNorm: fmt.Sprintf("TRK-APP-%d", time.Now().UnixNano()),
		ImageSHA256:      fmt.Sprintf("sha256-app-%d", time.Now().UnixNano()),
		ImageRef:         "s3://receipts/app.jpg",
		Status:           receipt.StatusSubmitted,
		CreatedAt:        now,
	}
	if err := s.Receipts().Save(ctx, rcp); err != nil {
		t.Fatalf("failed to save test receipt: %v", err)
	}

	adminID := int64(999001)
	creditIRT := pricing.IRT(10_000_000)
	approvedRcp, w, err := s.Receipts().ApproveReceipt(ctx, rcpID, adminID, creditIRT, now)
	if err != nil {
		t.Fatalf("ApproveReceipt failed: %v", err)
	}

	if approvedRcp.Status != receipt.StatusApproved {
		t.Errorf("expected approved status, got %s", approvedRcp.Status)
	}
	if approvedRcp.DecidedByAdminID == nil || *approvedRcp.DecidedByAdminID != adminID {
		t.Errorf("expected decided_by_admin_id %d, got %v", adminID, approvedRcp.DecidedByAdminID)
	}
	if approvedRcp.AmountIRT != creditIRT {
		t.Errorf("expected amount_irt %d, got %d", creditIRT, approvedRcp.AmountIRT)
	}
	if w.BalanceIRT != creditIRT {
		t.Errorf("expected wallet balance %d, got %d", creditIRT, w.BalanceIRT)
	}

	// Verify ledger entry
	var (
		ledgerKind   string
		ledgerAmount int64
		refType      string
		refID        string
	)
	err = s.Pool().QueryRow(ctx, `
		SELECT kind, amount, ref_type, ref_id
		FROM shop.wallet_ledger
		WHERE ref_type = 'receipt' AND ref_id = $1
	`, rcpID).Scan(&ledgerKind, &ledgerAmount, &refType, &refID)
	if err != nil {
		t.Fatalf("failed to find ledger entry for approved receipt: %v", err)
	}
	if ledgerKind != "topup_receipt" {
		t.Errorf("expected ledger kind topup_receipt, got %s", ledgerKind)
	}
	if ledgerAmount != int64(creditIRT) {
		t.Errorf("expected ledger amount %d, got %d", creditIRT, ledgerAmount)
	}

	// Double approval must fail with ErrReceiptAlreadyDecided
	_, _, err = s.Receipts().ApproveReceipt(ctx, rcpID, adminID, creditIRT, now)
	if !errors.Is(err, receipt.ErrReceiptAlreadyDecided) {
		t.Fatalf("expected ErrReceiptAlreadyDecided on second approval, got %v", err)
	}
}
