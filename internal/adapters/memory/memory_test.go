package memory

import (
	"context"
	"testing"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/invite"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
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
}
