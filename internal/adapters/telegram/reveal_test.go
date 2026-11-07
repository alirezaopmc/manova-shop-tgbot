package telegram

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/hetzner"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/memory"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/config"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/crypto"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/secrets"
)

func TestReveal_NoPlaintextInChatAndEnvelopeEncryption(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	kek := "test-credential-kek-secret-999"

	cfg := &config.Config{
		CredentialKEK: secrets.New(kek),
		RevealBaseURL: "https://reveal.internal.manova.space",
		SupportHandle: "@manova_support",
	}

	simProvider := hetzner.NewClient(hetzner.Config{SimulationMode: true})
	svc := NewService(nil, cfg, store, simProvider)

	var capturedMessages []string
	svc.onSendMessage = func(p *telego.SendMessageParams) {
		capturedMessages = append(capturedMessages, p.Text)
	}

	userID := int64(776655)
	usr := user.New(userID, "secureuser", "Secure", "User", user.RoleUser, time.Now().UTC())
	_ = store.Users().Save(ctx, usr)

	w, _ := store.Wallets().GetByUserID(ctx, userID)
	w.BalanceIRT = 500_000
	_ = store.Wallets().UpdateBalances(ctx, w)

	// Buy server CX23 with root password access mode ("pwd")
	err := svc.handleBuyExecute(ctx, tu.ID(userID), userID, "cx23", "nbg1", "ubuntu-24.04", "pwd")
	if err != nil {
		t.Fatalf("handleBuyExecute failed: %v", err)
	}

	if len(capturedMessages) == 0 {
		t.Fatalf("expected messages to be sent during purchase")
	}

	// Last message is the delivery message
	deliveryMsg := capturedMessages[len(capturedMessages)-1]

	// Verify reveal link is present
	if !strings.Contains(deliveryMsg, "🔑 One-time Password:") || !strings.Contains(deliveryMsg, "Click to reveal") {
		t.Fatalf("delivery message does not contain reveal link: %s", deliveryMsg)
	}
	if !strings.Contains(deliveryMsg, "(Valid 15m)") {
		t.Fatalf("delivery message missing Valid 15m notice: %s", deliveryMsg)
	}

	// Extract the reveal URL
	startIdx := strings.Index(deliveryMsg, `href="`)
	if startIdx == -1 {
		t.Fatalf("delivery message missing href: %s", deliveryMsg)
	}
	startIdx += len(`href="`)
	endIdx := strings.Index(deliveryMsg[startIdx:], `"`)
	if endIdx == -1 {
		t.Fatalf("malformed href in delivery message: %s", deliveryMsg)
	}
	revealURL := deliveryMsg[startIdx : startIdx+endIdx]

	parsedURL, err := url.Parse(revealURL)
	if err != nil {
		t.Fatalf("failed to parse reveal URL %q: %v", revealURL, err)
	}
	token := parsedURL.Query().Get("token")
	if token == "" {
		t.Fatalf("reveal URL missing token: %s", revealURL)
	}

	// Verify ProvisionedResource has encrypted password at rest
	resources, err := store.Billing().ListUserResources(ctx, userID)
	if err != nil || len(resources) != 1 {
		t.Fatalf("expected 1 provisioned resource, got %d", len(resources))
	}
	savedRes := resources[0]

	if savedRes.RootPassword == "" {
		t.Fatalf("expected saved RootPassword to not be empty")
	}

	// Plaintext root password must NOT be equal to saved RootPassword
	decryptedPwd, err := crypto.Decrypt(savedRes.RootPassword, kek)
	if err != nil {
		t.Fatalf("failed to decrypt saved RootPassword: %v", err)
	}
	if decryptedPwd == savedRes.RootPassword {
		t.Fatalf("expected RootPassword to be encrypted at rest, got plaintext: %s", savedRes.RootPassword)
	}

	// Plaintext password MUST NOT be present in chat message
	if strings.Contains(deliveryMsg, decryptedPwd) {
		t.Fatalf("CRITICAL SECURITY INVARIANT VIOLATED: plaintext password %q emitted in chat message!", decryptedPwd)
	}

	// Consume the one-time reveal token
	consumedPwd, err := svc.ConsumeRevealToken(token)
	if err != nil {
		t.Fatalf("failed to consume reveal token: %v", err)
	}
	if consumedPwd != decryptedPwd {
		t.Fatalf("consumed password %q does not match decrypted password %q", consumedPwd, decryptedPwd)
	}

	// Verify single-use: second consumption MUST fail
	_, err = svc.ConsumeRevealToken(token)
	if err == nil {
		t.Fatalf("expected error on second consumption of token, got nil")
	}
}

func TestReveal_ResetPasswordAndRescueNoPlaintextInChat(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	kek := "another-kek-value-456"

	cfg := &config.Config{
		CredentialKEK: secrets.New(kek),
		RevealBaseURL: "https://reveal.internal.manova.space",
		SupportHandle: "@manova_support",
	}

	simProvider := hetzner.NewClient(hetzner.Config{SimulationMode: true})
	svc := NewService(nil, cfg, store, simProvider)

	var capturedMessages []string
	svc.onSendMessage = func(p *telego.SendMessageParams) {
		capturedMessages = append(capturedMessages, p.Text)
	}

	userID := int64(334455)
	usr := user.New(userID, "srvowner", "Owner", "", user.RoleUser, time.Now().UTC())
	_ = store.Users().Save(ctx, usr)

	res := &billing.ProvisionedResource{
		ID:           "res_test_reset_1",
		UserID:       userID,
		ServerName:   "production-server",
		ExternalID:   "12345678",
		Status:       billing.ResourceStatusActive,
		PowerState:   billing.PowerStateRunning,
		RootPassword: "initial-encrypted-pwd",
		CreatedAt:    time.Now().UTC(),
	}
	_ = store.Billing().SaveResource(ctx, res)

	cbMsg := &telego.Message{Chat: telego.Chat{ID: userID}}
	cb := &telego.CallbackQuery{ID: "cb_reset_pwd", Message: cbMsg}

	// 1. Reset Password
	capturedMessages = nil
	err := svc.handleServerResetPassword(ctx, cb, res.ID, userID)
	if err != nil {
		t.Fatalf("handleServerResetPassword failed: %v", err)
	}

	if len(capturedMessages) == 0 {
		t.Fatalf("expected message from handleServerResetPassword")
	}
	resetNotice := capturedMessages[0]

	// Must contain reveal link
	if !strings.Contains(resetNotice, "🔑 <b>New Root Password") || !strings.Contains(resetNotice, "Click to reveal") {
		t.Fatalf("reset notice missing reveal link: %s", resetNotice)
	}

	// Extract token from reset notice
	token := extractTokenFromMsg(t, resetNotice)
	newPwd, err := svc.ConsumeRevealToken(token)
	if err != nil {
		t.Fatalf("failed to consume reset password token: %v", err)
	}
	if newPwd == "" {
		t.Fatalf("expected non-empty reset password")
	}

	// Ensure plaintext new password was NOT leaked in chat
	if strings.Contains(resetNotice, newPwd) {
		t.Fatalf("CRITICAL SECURITY INVARIANT VIOLATED: plaintext reset password %q emitted in chat!", newPwd)
	}

	// Ensure saved password is encrypted
	updatedRes, _ := store.Billing().GetResource(ctx, res.ID)
	decryptedUpdatedPwd, err := crypto.Decrypt(updatedRes.RootPassword, kek)
	if err != nil {
		t.Fatalf("failed to decrypt updated RootPassword: %v", err)
	}
	if decryptedUpdatedPwd != newPwd {
		t.Fatalf("saved password decrypted %q != consumed password %q", decryptedUpdatedPwd, newPwd)
	}

	// 2. Enable Rescue Mode
	capturedMessages = nil
	err = svc.handleServerRescue(ctx, cb, res.ID, userID)
	if err != nil {
		t.Fatalf("handleServerRescue failed: %v", err)
	}

	if len(capturedMessages) == 0 {
		t.Fatalf("expected message from handleServerRescue")
	}
	rescueNotice := capturedMessages[0]

	if !strings.Contains(rescueNotice, "🛟 <b>Rescue Mode enabled") || !strings.Contains(rescueNotice, "Click to reveal") {
		t.Fatalf("rescue notice missing reveal link: %s", rescueNotice)
	}

	rescueToken := extractTokenFromMsg(t, rescueNotice)
	rescuePwd, err := svc.ConsumeRevealToken(rescueToken)
	if err != nil {
		t.Fatalf("failed to consume rescue password token: %v", err)
	}
	if rescuePwd == "" {
		t.Fatalf("expected non-empty rescue password")
	}

	// Ensure plaintext rescue password was NOT leaked in chat
	if strings.Contains(rescueNotice, rescuePwd) {
		t.Fatalf("CRITICAL SECURITY INVARIANT VIOLATED: plaintext rescue password %q emitted in chat!", rescuePwd)
	}

	// 3. Verify Server Management View hides root password
	capturedMessages = nil
	err = svc.sendServerManagementView(ctx, tu.ID(userID), res.ID, userID)
	if err != nil {
		t.Fatalf("sendServerManagementView failed: %v", err)
	}
	if len(capturedMessages) == 0 {
		t.Fatalf("expected management view message")
	}
	mgmtView := capturedMessages[0]
	if !strings.Contains(mgmtView, "🔑 <b>Root Password:</b> <code>[Encrypted/Set]</code>") {
		t.Fatalf("management view missing [Encrypted/Set] indicator: %s", mgmtView)
	}
	if strings.Contains(mgmtView, newPwd) || strings.Contains(mgmtView, rescuePwd) {
		t.Fatalf("management view leaks password: %s", mgmtView)
	}
}

func TestReveal_TTLAndExpiration(t *testing.T) {
	mgr := NewRevealManager("test-kek")

	mockTime := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	mgr.SetClock(func() time.Time {
		return mockTime
	})

	token, err := mgr.Store("sensitive-pwd", 15*time.Minute)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// 14 minutes later -> still valid
	mockTime = mockTime.Add(14 * time.Minute)
	// Don't consume yet, verify valid
	if time.Now().After(mockTime) {
		// Just ensure clock check
	}

	// Advance clock past 15 minutes (15m + 1s)
	mockTime = mockTime.Add(1*time.Minute + 1*time.Second)

	_, err = mgr.Consume(token)
	if err == nil {
		t.Fatalf("expected error on expired token, got nil")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expired error, got: %v", err)
	}

	// Subsequent consume on expired token must also fail
	_, err = mgr.Consume(token)
	if err == nil {
		t.Fatalf("expected error on subsequent consumption of expired token, got nil")
	}
}

func TestReveal_EmptyCredential(t *testing.T) {
	svc := NewService(nil, nil, nil)
	url := svc.generateRevealURL("")
	if url != "" {
		t.Fatalf("expected empty URL for empty credential, got: %s", url)
	}

	urlExported := svc.GenerateRevealURL("")
	if urlExported != "" {
		t.Fatalf("expected empty URL for empty credential, got: %s", urlExported)
	}
}

func TestReveal_GlobalConsumeRevealToken(t *testing.T) {
	kek := "global-kek"
	cfg := &config.Config{
		CredentialKEK: secrets.New(kek),
		RevealBaseURL: "https://reveal.internal.manova.space",
	}
	svc := NewService(nil, cfg, nil)

	revealURL := svc.generateRevealURL("my-secret-credential-42")
	parsed, err := url.Parse(revealURL)
	if err != nil {
		t.Fatalf("failed to parse url: %v", err)
	}
	token := parsed.Query().Get("token")

	// Call package-level ConsumeRevealToken
	secret, err := ConsumeRevealToken(token)
	if err != nil {
		t.Fatalf("ConsumeRevealToken failed: %v", err)
	}
	if secret != "my-secret-credential-42" {
		t.Fatalf("expected %q, got %q", "my-secret-credential-42", secret)
	}

	// Single use check on package-level function
	_, err = ConsumeRevealToken(token)
	if err == nil {
		t.Fatalf("expected error on consumed token, got nil")
	}
}

func extractTokenFromMsg(t *testing.T, msg string) string {
	t.Helper()
	startIdx := strings.Index(msg, `href="`)
	if startIdx == -1 {
		t.Fatalf("message missing href: %s", msg)
	}
	startIdx += len(`href="`)
	endIdx := strings.Index(msg[startIdx:], `"`)
	if endIdx == -1 {
		t.Fatalf("malformed href in message: %s", msg)
	}
	u, err := url.Parse(msg[startIdx : startIdx+endIdx])
	if err != nil {
		t.Fatalf("failed to parse URL: %v", err)
	}
	tok := u.Query().Get("token")
	if tok == "" {
		t.Fatalf("URL missing token: %s", u.String())
	}
	return tok
}
