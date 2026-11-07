package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/fee"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/wallet"
)

func uniqueWalletUserID() int64 {
	return 20_000_000 + (time.Now().UnixNano() % 70_000_000) + rand.Int63n(10_000)
}

func cleanupWallet(t testing.TB, s *Store, userID int64) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.Pool().Exec(ctx, "DELETE FROM shop.wallets WHERE user_id = $1", userID)
	})
}

// TestWalletRepository_AutoCreation verifies that querying a non-existent wallet
// automatically inserts a zero-balance default wallet row (version=1) and returns it.
func TestWalletRepository_AutoCreation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	userID := uniqueWalletUserID()
	cleanupWallet(t, s, userID)

	w, err := s.Wallets().GetByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("unexpected error on GetByUserID: %v", err)
	}
	if w == nil {
		t.Fatal("expected non-nil wallet returned")
	}

	if w.UserID != userID {
		t.Errorf("expected UserID %d, got %d", userID, w.UserID)
	}
	if w.ID != userID {
		t.Errorf("expected ID %d, got %d", userID, w.ID)
	}
	if w.BalanceIRT != 0 {
		t.Errorf("expected BalanceIRT 0, got %d", w.BalanceIRT)
	}
	if w.BalanceUSDMicro != 0 {
		t.Errorf("expected BalanceUSDMicro 0, got %d", w.BalanceUSDMicro)
	}
	if w.FloorOverrideIRT != nil {
		t.Errorf("expected nil FloorOverrideIRT, got %v", *w.FloorOverrideIRT)
	}
	if w.Version != 1 {
		t.Errorf("expected initial Version 1, got %d", w.Version)
	}

	// Verify persistence in shop.wallets table
	var count int
	err = s.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM shop.wallets WHERE user_id = $1", userID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query shop.wallets: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row in shop.wallets, got %d", count)
	}

	// Second fetch returns the existing wallet
	w2, err := s.Wallets().GetByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("unexpected error on second GetByUserID: %v", err)
	}
	if w2.UserID != userID || w2.Version != 1 {
		t.Errorf("unexpected wallet on second fetch: %+v", w2)
	}
}

// TestWalletRepository_UpdateBalancesAndVersion verifies balance updates,
// custom floor override updates, and atomic version counter increments.
func TestWalletRepository_UpdateBalancesAndVersion(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	userID := uniqueWalletUserID()
	cleanupWallet(t, s, userID)

	w, err := s.Wallets().GetByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("failed to get initial wallet: %v", err)
	}
	if w.Version != 1 {
		t.Fatalf("expected initial version 1, got %d", w.Version)
	}

	// Mutate balances and add custom floor
	w.BalanceIRT = 750_000
	w.BalanceUSDMicro = 15_000_000
	floor := pricing.IRT(-200_000)
	w.FloorOverrideIRT = &floor

	if err := s.Wallets().UpdateBalances(ctx, w); err != nil {
		t.Fatalf("UpdateBalances failed: %v", err)
	}

	if w.Version != 2 {
		t.Errorf("expected w.Version incremented to 2, got %d", w.Version)
	}

	// Verify database state matches
	fetched, err := s.Wallets().GetByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("failed to reload wallet: %v", err)
	}
	if fetched.BalanceIRT != 750_000 {
		t.Errorf("expected BalanceIRT 750000, got %d", fetched.BalanceIRT)
	}
	if fetched.BalanceUSDMicro != 15_000_000 {
		t.Errorf("expected BalanceUSDMicro 15000000, got %d", fetched.BalanceUSDMicro)
	}
	if fetched.FloorOverrideIRT == nil || *fetched.FloorOverrideIRT != -200_000 {
		t.Errorf("expected FloorOverrideIRT -200000, got %v", fetched.FloorOverrideIRT)
	}
	if fetched.Version != 2 {
		t.Errorf("expected fetched Version 2, got %d", fetched.Version)
	}

	// Second update increments version again
	fetched.BalanceIRT += 100_000
	if err := s.Wallets().UpdateBalances(ctx, fetched); err != nil {
		t.Fatalf("second UpdateBalances failed: %v", err)
	}
	if fetched.Version != 3 {
		t.Errorf("expected version 3 after second update, got %d", fetched.Version)
	}
}

// TestWalletRepository_RecordLedgerEntryAndIdempotency tests inserting entries
// into shop.wallet_ledger and verifies uq_ledger_idempotency constraint prevents duplicates.
func TestWalletRepository_RecordLedgerEntryAndIdempotency(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	userID := uniqueWalletUserID()
	cleanupWallet(t, s, userID)

	w, err := s.Wallets().GetByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("GetByUserID failed: %v", err)
	}

	orderRefID := fmt.Sprintf("ord_%d", time.Now().UnixNano())
	entryID := fmt.Sprintf("ledg_%d", time.Now().UnixNano())

	entry := &wallet.LedgerEntry{
		ID:           entryID,
		WalletID:     w.ID,
		Currency:     wallet.CurrencyIRT,
		Amount:       -50_000,
		Kind:         wallet.KindPurchase,
		BalanceAfter: 0,
		RefType:      "order",
		RefID:        orderRefID,
		CreatedAt:    time.Now().UTC(),
	}

	if err := s.Wallets().RecordLedgerEntry(ctx, entry); err != nil {
		t.Fatalf("failed to record ledger entry: %v", err)
	}

	// Verify row was inserted into shop.wallet_ledger
	var (
		dbWalletID int64
		dbCurrency string
		dbAmount   int64
		dbKind     string
		dbBalAfter int64
		dbRefType  string
		dbRefID    string
	)
	err = s.Pool().QueryRow(ctx, `
		SELECT wallet_id, currency, amount, kind, balance_after, ref_type, ref_id
		FROM shop.wallet_ledger
		WHERE id = $1
	`, entryID).Scan(&dbWalletID, &dbCurrency, &dbAmount, &dbKind, &dbBalAfter, &dbRefType, &dbRefID)
	if err != nil {
		t.Fatalf("failed to query inserted ledger row: %v", err)
	}

	if dbWalletID != w.ID || dbCurrency != "IRT" || dbAmount != -50_000 ||
		dbKind != string(wallet.KindPurchase) || dbRefType != "order" || dbRefID != orderRefID {
		t.Errorf("unexpected ledger row values: wallet=%d curr=%s amt=%d kind=%s refType=%s refID=%s",
			dbWalletID, dbCurrency, dbAmount, dbKind, dbRefType, dbRefID)
	}

	// Duplicate attempt with same (ref_type, ref_id, kind, currency) must be rejected
	// by uq_ledger_idempotency constraint
	duplicateEntry := &wallet.LedgerEntry{
		ID:           fmt.Sprintf("ledg_dup_%d", time.Now().UnixNano()),
		WalletID:     w.ID,
		Currency:     wallet.CurrencyIRT,
		Amount:       -50_000,
		Kind:         wallet.KindPurchase,
		BalanceAfter: -50_000,
		RefType:      "order",
		RefID:        orderRefID,
		CreatedAt:    time.Now().UTC(),
	}

	err = s.Wallets().RecordLedgerEntry(ctx, duplicateEntry)
	if err == nil {
		t.Fatal("expected duplicate ledger entry to fail due to uq_ledger_idempotency, but it succeeded")
	}
	if !strings.Contains(err.Error(), "uq_ledger_idempotency") && !strings.Contains(err.Error(), "duplicate key") && !strings.Contains(err.Error(), "unique constraint") {
		t.Errorf("expected unique constraint violation error, got: %v", err)
	}

	// Unrelated entries without ref_id (e.g. admin adjustments) should not collide
	adminEntry1 := &wallet.LedgerEntry{
		ID:           fmt.Sprintf("ledg_adm1_%d", time.Now().UnixNano()),
		WalletID:     w.ID,
		Currency:     wallet.CurrencyIRT,
		Amount:       10_000,
		Kind:         wallet.KindAdminAdjustment,
		BalanceAfter: 10_000,
		RefType:      "",
		RefID:        "",
	}
	adminEntry2 := &wallet.LedgerEntry{
		ID:           fmt.Sprintf("ledg_adm2_%d", time.Now().UnixNano()),
		WalletID:     w.ID,
		Currency:     wallet.CurrencyIRT,
		Amount:       20_000,
		Kind:         wallet.KindAdminAdjustment,
		BalanceAfter: 30_000,
		RefType:      "",
		RefID:        "",
	}
	if err := s.Wallets().RecordLedgerEntry(ctx, adminEntry1); err != nil {
		t.Fatalf("first admin adjustment entry failed: %v", err)
	}
	if err := s.Wallets().RecordLedgerEntry(ctx, adminEntry2); err != nil {
		t.Fatalf("second admin adjustment entry should succeed without ref_id: %v", err)
	}
}

// TestWalletRepository_LedgerImmutability verifies that the database triggers
// wallet_ledger_no_update and wallet_ledger_no_delete prevent any UPDATE or DELETE operations.
func TestWalletRepository_LedgerImmutability(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	userID := uniqueWalletUserID()
	cleanupWallet(t, s, userID)

	w, err := s.Wallets().GetByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("GetByUserID failed: %v", err)
	}

	entryID := fmt.Sprintf("ledg_immut_%d", time.Now().UnixNano())
	entry := &wallet.LedgerEntry{
		ID:           entryID,
		WalletID:     w.ID,
		Currency:     wallet.CurrencyIRT,
		Amount:       100_000,
		Kind:         wallet.KindTopupReceipt,
		BalanceAfter: 100_000,
		RefType:      "receipt",
		RefID:        fmt.Sprintf("rcpt_%d", time.Now().UnixNano()),
		CreatedAt:    time.Now().UTC(),
	}

	if err := s.Wallets().RecordLedgerEntry(ctx, entry); err != nil {
		t.Fatalf("failed to record ledger entry: %v", err)
	}

	// Attempt to UPDATE row - must be blocked by trigger
	_, err = s.Pool().Exec(ctx, "UPDATE shop.wallet_ledger SET amount = 999999 WHERE id = $1", entryID)
	if err == nil {
		t.Fatal("expected UPDATE on shop.wallet_ledger to fail, but it succeeded")
	}
	if !strings.Contains(err.Error(), "immutable") && !strings.Contains(err.Error(), "prohibited") {
		t.Errorf("unexpected error on UPDATE: %v", err)
	}

	// Attempt to DELETE row - must be blocked by trigger
	_, err = s.Pool().Exec(ctx, "DELETE FROM shop.wallet_ledger WHERE id = $1", entryID)
	if err == nil {
		t.Fatal("expected DELETE on shop.wallet_ledger to fail, but it succeeded")
	}
	if !strings.Contains(err.Error(), "immutable") && !strings.Contains(err.Error(), "prohibited") {
		t.Errorf("unexpected error on DELETE: %v", err)
	}
}

// TestWalletRepository_DebitAtomic tests pessimistic row locking, floor checking,
// balance debiting, and ledger recording in an isolated transaction.
func TestWalletRepository_DebitAtomic(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	repo, ok := s.Wallets().(*walletRepo)
	if !ok {
		t.Fatal("Wallets() does not expose *walletRepo")
	}

	userID := uniqueWalletUserID()
	cleanupWallet(t, s, userID)

	// Setup initial wallet with 50,000 IRT and 1,000,000 USD micro-units ($1 USD)
	w, err := s.Wallets().GetByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("GetByUserID failed: %v", err)
	}
	w.BalanceIRT = 50_000
	w.BalanceUSDMicro = 1_000_000
	if err := s.Wallets().UpdateBalances(ctx, w); err != nil {
		t.Fatalf("UpdateBalances failed: %v", err)
	}

	sellRate := pricing.IRT(100_000) // 100,000 IRT per USD

	// 1. Successful debit: debit 80,000 IRT (takes 50,000 IRT + 300,000 USD micro-units)
	refID := fmt.Sprintf("ord_%d", time.Now().UnixNano())
	updated, err := repo.DebitAtomic(
		ctx,
		userID,
		80_000,
		fee.SegmentCustomer, // floor = -100
		sellRate,
		wallet.KindPurchase,
		"order",
		refID,
	)
	if err != nil {
		t.Fatalf("DebitAtomic failed: %v", err)
	}

	if updated.BalanceIRT != 0 {
		t.Errorf("expected BalanceIRT 0 after debit, got %d", updated.BalanceIRT)
	}
	// 30,000 IRT remaining / 100,000 = 0.3 USD = 300,000 USD micro-units debited
	if updated.BalanceUSDMicro != 700_000 {
		t.Errorf("expected BalanceUSDMicro 700000, got %d", updated.BalanceUSDMicro)
	}

	// 2. Debit exceeding effective floor is rejected
	_, err = repo.DebitAtomic(
		ctx,
		userID,
		500_000,        // Available: 700,000 microUSD * 100,000 / 1,000,000 = 70,000 IRT. 500,000 breaches floor.
		fee.SegmentNew, // floor = 0
		sellRate,
		wallet.KindPurchase,
		"order",
		fmt.Sprintf("ord_exceed_%d", time.Now().UnixNano()),
	)
	if !errors.Is(err, wallet.ErrBelowFloor) {
		t.Fatalf("expected ErrBelowFloor, got %v", err)
	}

	// 3. Concurrency test: ensure serialized pessimistic locking prevents overdraft
	w.BalanceIRT = 100_000
	w.BalanceUSDMicro = 0
	if err := s.Wallets().UpdateBalances(ctx, w); err != nil {
		t.Fatalf("reset balance failed: %v", err)
	}

	const goroutines = 5
	debitAmount := pricing.IRT(30_000) // Total available = 100,000 IRT. Only 3 can succeed (3 * 30k = 90k, 4th breaches floor 0).
	var wg sync.WaitGroup
	var successCount int64
	var failCount int64
	var mu sync.Mutex

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := repo.DebitAtomic(
				ctx,
				userID,
				debitAmount,
				fee.SegmentNew,
				sellRate,
				wallet.KindPurchase,
				"order",
				fmt.Sprintf("concurrent_%d_%d", userID, idx),
			)
			mu.Lock()
			if err == nil {
				successCount++
			} else if errors.Is(err, wallet.ErrBelowFloor) {
				failCount++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	if successCount != 3 {
		t.Errorf("expected exactly 3 successful debits, got %d (failed: %d)", successCount, failCount)
	}

	finalW, err := s.Wallets().GetByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("final wallet lookup failed: %v", err)
	}
	if finalW.BalanceIRT != 10_000 {
		t.Errorf("expected final BalanceIRT 10000, got %d", finalW.BalanceIRT)
	}

	// 4. Validate sellRate <= 0 error
	if _, err := s.Wallets().DebitAtomic(ctx, userID, 10_000, fee.SegmentNew, 0, wallet.KindPurchase, "order", "ord_zero_rate"); err == nil {
		t.Error("expected error for sellRate <= 0, got nil")
	}
}
