package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/fee"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/invite"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/receipt"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/wallet"
)

func TestMemoryStoreUsersAndInvites(t *testing.T) {
	ctx := context.Background()
	s := NewStore()
	now := time.Now().UTC()

	// 1. Create and fetch invite
	inv := invite.New(100, 1, nil, now)
	if err := s.Invites().Create(ctx, inv); err != nil {
		t.Fatalf("create invite err: %v", err)
	}

	fetchedInv, err := s.Invites().GetByCode(ctx, inv.Code)
	if err != nil || fetchedInv.Code != inv.Code {
		t.Fatalf("get invite err: %v", err)
	}

	// 2. Claim invite
	claimed, err := s.Invites().Claim(ctx, inv.Code, 200, now)
	if err != nil || claimed.UsesCount != 1 || claimed.IsActive {
		t.Fatalf("claim err: %v", err)
	}

	// 3. Save and retrieve user
	u := user.New(200, "bob", "Bob", "", user.RoleUser, now)
	if err := s.Users().Save(ctx, u); err != nil {
		t.Fatalf("save user err: %v", err)
	}

	fetchedU, err := s.Users().GetByID(ctx, 200)
	if err != nil || fetchedU.Username != "bob" {
		t.Fatalf("get user err: %v", err)
	}

	// 4. Wallet auto-initialization
	w, err := s.Wallets().GetByUserID(ctx, 200)
	if err != nil || w.BalanceIRT != 0 {
		t.Fatalf("wallet err: %v", err)
	}

	// 5. Pre-seeded catalog items
	items, err := s.Catalog().ListActiveItems(ctx)
	if err != nil || len(items) == 0 {
		t.Fatalf("catalog items err: %v, len %d", err, len(items))
	}

	// 6. Test ListUsers
	users, err := s.Users().ListUsers(ctx, 10)
	if err != nil || len(users) != 1 {
		t.Fatalf("expected 1 user in ListUsers, got %d, err: %v", len(users), err)
	}

	// 7. Test Wallet balance update
	w.BalanceIRT = 250_000
	if err := s.Wallets().UpdateBalances(ctx, w); err != nil {
		t.Fatalf("update balances err: %v", err)
	}
	w2, err := s.Wallets().GetByUserID(ctx, 200)
	if err != nil || w2.BalanceIRT != 250_000 {
		t.Fatalf("expected updated balance 250000, got %d", w2.BalanceIRT)
	}

	// 8. Ensure no static backdoor invites exist
	if _, err := s.Invites().GetByCode(ctx, "invite"); err == nil {
		t.Fatalf("static invite code 'invite' must not exist in store")
	}
	if _, err := s.Invites().GetByCode(ctx, "shop"); err == nil {
		t.Fatalf("static invite code 'shop' must not exist in store")
	}

	// 9. Second claim on single-use invite must fail
	if _, err := s.Invites().Claim(ctx, inv.Code, 300, now); err == nil {
		t.Fatalf("expected second claim on single-use invite to fail")
	}

	// 10. Phone index unlinking on reset
	hash := "test-hash-123"
	u.PhoneHash = &hash
	if err := s.Users().Save(ctx, u); err != nil {
		t.Fatalf("save with phone hash err: %v", err)
	}
	if found, err := s.Users().FindByPhoneHash(ctx, hash); err != nil || found.TelegramUserID != 200 {
		t.Fatalf("expected find by phone hash to succeed")
	}
	u.PhoneHash = nil
	if err := s.Users().Save(ctx, u); err != nil {
		t.Fatalf("save after clearing phone hash err: %v", err)
	}
	if _, err := s.Users().FindByPhoneHash(ctx, hash); err == nil {
		t.Fatalf("expected find by phone hash to fail after clearing")
	}
}

func TestMemoryStoreDebitAtomicAndApproveReceipt(t *testing.T) {
	ctx := context.Background()
	s := NewStore()
	now := time.Now().UTC()

	// Test ApproveReceipt
	rcp := &receipt.Receipt{
		ID:               "rcp-mem-1",
		UserID:           500,
		Purpose:          receipt.PurposeWalletTopup,
		Status:           receipt.StatusSubmitted,
		CreatedAt:        now,
		TrackingCodeNorm: "TRK-MEM-1",
		ImageSHA256:      "sha-mem-1",
	}
	if err := s.Receipts().Save(ctx, rcp); err != nil {
		t.Fatalf("save receipt err: %v", err)
	}

	approvedRcp, w, err := s.Receipts().ApproveReceipt(ctx, rcp.ID, 999, 500_000, now)
	if err != nil {
		t.Fatalf("ApproveReceipt err: %v", err)
	}
	if approvedRcp.Status != receipt.StatusApproved || w.BalanceIRT != 500_000 {
		t.Fatalf("expected approved and balance 500000, got status %s, bal %d", approvedRcp.Status, w.BalanceIRT)
	}

	// Double approval returns ErrReceiptAlreadyDecided
	if _, _, err := s.Receipts().ApproveReceipt(ctx, rcp.ID, 999, 500_000, now); !errors.Is(err, receipt.ErrReceiptAlreadyDecided) {
		t.Fatalf("expected ErrReceiptAlreadyDecided, got %v", err)
	}

	// Test DebitAtomic
	debitedW, err := s.Wallets().DebitAtomic(ctx, 500, 200_000, fee.SegmentNew, 100_000, wallet.KindPurchase, "order", "ord-1")
	if err != nil {
		t.Fatalf("DebitAtomic err: %v", err)
	}
	if debitedW.BalanceIRT != 300_000 {
		t.Fatalf("expected balance 300000, got %d", debitedW.BalanceIRT)
	}

	// Debit exceeding balance/floor returns ErrBelowFloor
	if _, err := s.Wallets().DebitAtomic(ctx, 500, 1_000_000, fee.SegmentNew, 100_000, wallet.KindPurchase, "order", "ord-2"); !errors.Is(err, wallet.ErrBelowFloor) {
		t.Fatalf("expected ErrBelowFloor, got %v", err)
	}

	// Sell rate <= 0 returns error
	if _, err := s.Wallets().DebitAtomic(ctx, 500, 100_000, fee.SegmentNew, 0, wallet.KindPurchase, "order", "ord-3"); err == nil {
		t.Fatalf("expected error for zero sell rate, got nil")
	}

	entries := s.LedgerEntries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 ledger entries (1 topup, 1 debit), got %d", len(entries))
	}
	if entries[0].Kind != wallet.KindTopupReceipt || entries[0].Amount != 500_000 {
		t.Errorf("entry 0 mismatch: %+v", entries[0])
	}
	if entries[1].Kind != wallet.KindPurchase || entries[1].Amount != -200_000 {
		t.Errorf("entry 1 mismatch: %+v", entries[1])
	}
}
