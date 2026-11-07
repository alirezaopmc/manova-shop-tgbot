package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/memory"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/totp"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/config"
)

func TestAdminWalletAdjustBlockedWithoutTOTP(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	adminID := int64(99999)
	targetUserID := int64(12345)

	cfg := &config.Config{
		AdminTelegramID: adminID,
		SupportHandle:   "@support",
	}

	svc := NewService(nil, cfg, store)

	var lastMsg *telego.SendMessageParams
	svc.onSendMessage = func(p *telego.SendMessageParams) {
		lastMsg = p
	}

	// Create admin and target user
	adminUser := user.New(adminID, "admin", "Admin", "", user.RoleAdmin, time.Now().UTC())
	if err := store.Users().Save(ctx, adminUser); err != nil {
		t.Fatalf("failed to save admin: %v", err)
	}
	targetUser := user.New(targetUserID, "target", "Target", "", user.RoleUser, time.Now().UTC())
	if err := store.Users().Save(ctx, targetUser); err != nil {
		t.Fatalf("failed to save target user: %v", err)
	}

	// Initialize wallet with 100,000 IRT
	w, err := store.Wallets().GetByUserID(ctx, targetUserID)
	if err != nil {
		t.Fatalf("failed to get target wallet: %v", err)
	}
	w.BalanceIRT = 100000
	if err := store.Wallets().UpdateBalances(ctx, w); err != nil {
		t.Fatalf("failed to set balance: %v", err)
	}

	// Setup TOTP verifier
	totpVerifier := totp.NewVerifier()
	secret := "JBSWY3DPEHPK3PXP"
	if err := totpVerifier.RegisterSecret(adminID, secret); err != nil {
		t.Fatalf("failed to register totp secret: %v", err)
	}
	svc.SetTotpVerifier(totpVerifier)

	// Admin session is not active initially
	if svc.isTotpVerified(adminID) {
		t.Fatalf("expected admin session to not be verified initially")
	}

	// 1. Attempt wallet adjust while unverified
	cb := &telego.CallbackQuery{
		ID:      "cb_wallet_adjust",
		From:    telego.User{ID: adminID},
		Message: &telego.Message{Chat: telego.Chat{ID: adminID}},
	}
	if err := svc.handleAdminWalletAdjustExec(ctx, cb, targetUserID, "add", 50000); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert challenge message sent and balance NOT changed
	if lastMsg == nil || !strings.Contains(lastMsg.Text, "🔐 TOTP authentication required") {
		t.Fatalf("expected TOTP challenge message, got: %v", lastMsg)
	}
	wAfter, _ := store.Wallets().GetByUserID(ctx, targetUserID)
	if wAfter.BalanceIRT != 100000 {
		t.Fatalf("expected wallet balance to remain 100000, got: %d", wAfter.BalanceIRT)
	}
}

func TestAdminTotpActivationAndSubsequentWalletAdjust(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	adminID := int64(99999)
	targetUserID := int64(12345)

	cfg := &config.Config{
		AdminTelegramID: adminID,
		SupportHandle:   "@support",
	}

	svc := NewService(nil, cfg, store)

	var lastMsg *telego.SendMessageParams
	svc.onSendMessage = func(p *telego.SendMessageParams) {
		lastMsg = p
	}

	adminUser := user.New(adminID, "admin", "Admin", "", user.RoleAdmin, time.Now().UTC())
	_ = store.Users().Save(ctx, adminUser)
	targetUser := user.New(targetUserID, "target", "Target", "", user.RoleUser, time.Now().UTC())
	_ = store.Users().Save(ctx, targetUser)

	w, _ := store.Wallets().GetByUserID(ctx, targetUserID)
	w.BalanceIRT = 100000
	_ = store.Wallets().UpdateBalances(ctx, w)

	totpVerifier := totp.NewVerifier()
	secret := "JBSWY3DPEHPK3PXP"
	_ = totpVerifier.RegisterSecret(adminID, secret)
	svc.SetTotpVerifier(totpVerifier)

	// 1. Send invalid TOTP code
	badMsg := &telego.Message{
		From: &telego.User{ID: adminID},
		Chat: telego.Chat{ID: adminID},
		Text: "/totp 000000",
	}
	if err := svc.handleMessage(ctx, badMsg); err != nil {
		t.Fatalf("handleMessage returned error: %v", err)
	}
	if lastMsg == nil || lastMsg.Text != "❌ Invalid TOTP code." {
		t.Fatalf("expected invalid totp notice, got: %v", lastMsg)
	}
	if svc.isTotpVerified(adminID) {
		t.Fatalf("expected admin to remain unverified")
	}

	// 2. Send valid TOTP code
	code, err := totp.GenerateCode(secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("failed to generate totp code: %v", err)
	}
	goodMsg := &telego.Message{
		From: &telego.User{ID: adminID},
		Chat: telego.Chat{ID: adminID},
		Text: "/totp " + code,
	}
	if err := svc.handleMessage(ctx, goodMsg); err != nil {
		t.Fatalf("handleMessage returned error: %v", err)
	}
	if lastMsg == nil || !strings.Contains(lastMsg.Text, "✅ TOTP verified") {
		t.Fatalf("expected TOTP verified success message, got: %v", lastMsg)
	}
	if !svc.isTotpVerified(adminID) {
		t.Fatalf("expected admin session to be verified")
	}

	// 3. Subsequent wallet adjustment succeeds
	cb := &telego.CallbackQuery{
		ID:      "cb_wallet_adjust",
		From:    telego.User{ID: adminID},
		Message: &telego.Message{Chat: telego.Chat{ID: adminID}},
	}
	if err := svc.handleAdminWalletAdjustExec(ctx, cb, targetUserID, "add", 50000); err != nil {
		t.Fatalf("handleAdminWalletAdjustExec failed: %v", err)
	}

	wAfter, _ := store.Wallets().GetByUserID(ctx, targetUserID)
	if wAfter.BalanceIRT != pricing.IRT(150000) {
		t.Fatalf("expected balance to be 150000, got: %d", wAfter.BalanceIRT)
	}
}

func TestAdminUserSetRoleBlockedWithoutTOTP(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	adminID := int64(99999)
	targetUserID := int64(12345)

	cfg := &config.Config{
		AdminTelegramID: adminID,
		SupportHandle:   "@support",
	}

	svc := NewService(nil, cfg, store)

	var lastMsg *telego.SendMessageParams
	svc.onSendMessage = func(p *telego.SendMessageParams) {
		lastMsg = p
	}

	adminUser := user.New(adminID, "admin", "Admin", "", user.RoleAdmin, time.Now().UTC())
	_ = store.Users().Save(ctx, adminUser)
	targetUser := user.New(targetUserID, "target", "Target", "", user.RoleUser, time.Now().UTC())
	_ = store.Users().Save(ctx, targetUser)

	totpVerifier := totp.NewVerifier()
	secret := "JBSWY3DPEHPK3PXP"
	_ = totpVerifier.RegisterSecret(adminID, secret)
	svc.SetTotpVerifier(totpVerifier)

	cb := &telego.CallbackQuery{
		ID:      "cb_set_role",
		From:    telego.User{ID: adminID},
		Message: &telego.Message{Chat: telego.Chat{ID: adminID}},
	}

	// 1. Attempt role elevation while unverified
	if err := svc.handleAdminUserSetRole(ctx, cb, tu.ID(adminID), targetUserID, "admin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lastMsg == nil || !strings.Contains(lastMsg.Text, "🔐 TOTP authentication required") {
		t.Fatalf("expected challenge message, got: %v", lastMsg)
	}

	uAfter, _ := store.Users().GetByID(ctx, targetUserID)
	if uAfter.Role != user.RoleUser {
		t.Fatalf("expected role to remain user, got: %v", uAfter.Role)
	}

	// 2. Activate TOTP session
	code, _ := totp.GenerateCode(secret, time.Now().UTC())
	msg := &telego.Message{
		From: &telego.User{ID: adminID},
		Chat: telego.Chat{ID: adminID},
		Text: "/totp " + code,
	}
	_ = svc.handleMessage(ctx, msg)

	// 3. Attempt role elevation again
	if err := svc.handleAdminUserSetRole(ctx, cb, tu.ID(adminID), targetUserID, "admin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	uElevated, _ := store.Users().GetByID(ctx, targetUserID)
	if uElevated.Role != user.RoleAdmin {
		t.Fatalf("expected role to be elevated to admin, got: %v", uElevated.Role)
	}
}

func TestAdminUserSetBlockBlockedWithoutTOTP(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	adminID := int64(99999)
	targetUserID := int64(12345)

	cfg := &config.Config{
		AdminTelegramID: adminID,
		SupportHandle:   "@support",
	}

	svc := NewService(nil, cfg, store)

	var lastMsg *telego.SendMessageParams
	svc.onSendMessage = func(p *telego.SendMessageParams) {
		lastMsg = p
	}

	adminUser := user.New(adminID, "admin", "Admin", "", user.RoleAdmin, time.Now().UTC())
	_ = store.Users().Save(ctx, adminUser)
	targetUser := user.New(targetUserID, "target", "Target", "", user.RoleUser, time.Now().UTC())
	_ = store.Users().Save(ctx, targetUser)

	cb := &telego.CallbackQuery{
		ID:      "cb_block_user",
		From:    telego.User{ID: adminID},
		Message: &telego.Message{Chat: telego.Chat{ID: adminID}},
	}

	// 1. Attempt blocking without TOTP
	if err := svc.handleAdminUserSetBlock(ctx, cb, tu.ID(adminID), targetUserID, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if lastMsg == nil || !strings.Contains(lastMsg.Text, "🔐 TOTP authentication required") {
		t.Fatalf("expected challenge message, got: %v", lastMsg)
	}

	uAfter, _ := store.Users().GetByID(ctx, targetUserID)
	if uAfter.Blocked {
		t.Fatalf("expected user to not be blocked")
	}

	// 2. Verify with TOTP
	totpVerifier := totp.NewVerifier()
	secret := "JBSWY3DPEHPK3PXP"
	_ = totpVerifier.RegisterSecret(adminID, secret)
	svc.SetTotpVerifier(totpVerifier)

	code, _ := totp.GenerateCode(secret, time.Now().UTC())
	_ = svc.handleMessage(ctx, &telego.Message{
		From: &telego.User{ID: adminID},
		Chat: telego.Chat{ID: adminID},
		Text: "/totp " + code,
	})

	// 3. Block user succeeds
	if err := svc.handleAdminUserSetBlock(ctx, cb, tu.ID(adminID), targetUserID, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	uBlocked, _ := store.Users().GetByID(ctx, targetUserID)
	if !uBlocked.Blocked {
		t.Fatalf("expected user to be blocked")
	}
}

func TestTotpSessionExpiry(t *testing.T) {
	adminID := int64(8888)
	svc := NewService(nil, nil, nil)

	// Session in the past
	svc.adminTotpSessions[adminID] = time.Now().UTC().Add(-1 * time.Minute)
	if svc.isTotpVerified(adminID) {
		t.Fatalf("expected expired session to be invalid")
	}

	// Session in the future
	svc.adminTotpSessions[adminID] = time.Now().UTC().Add(10 * time.Minute)
	if !svc.isTotpVerified(adminID) {
		t.Fatalf("expected active session to be valid")
	}
}
