package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/memory"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/config"
)

func TestHTMLSanitization_ReceiptForwardCaption(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cfg := &config.Config{
		AdminTelegramID: 99999,
		SupportHandle:   "@support_shop",
		CardHolderName:  "Test Shop",
	}
	svc := NewService(nil, cfg, store)

	var capturedPhoto *telego.SendPhotoParams
	var capturedMsg *telego.SendMessageParams
	svc.onSendPhoto = func(p *telego.SendPhotoParams) {
		capturedPhoto = p
	}
	svc.onSendMessage = func(p *telego.SendMessageParams) {
		capturedMsg = p
	}

	senderID := int64(12345)
	usr := user.New(senderID, "evil<script>alert(1)</script>", "John<b unclosed", "Doe<a href=\"phish\">", user.RoleUser, time.Now().UTC())
	if err := store.Users().Save(ctx, usr); err != nil {
		t.Fatalf("save user failed: %v", err)
	}

	msg := &telego.Message{
		MessageID: 101,
		Chat:      telego.Chat{ID: senderID},
		From: &telego.User{
			ID:        senderID,
			Username:  "evil<script>alert(1)</script>",
			FirstName: "John<b unclosed",
			LastName:  "Doe<a href=\"phish\">",
		},
		Photo: []telego.PhotoSize{
			{FileID: "photo-xyz", Width: 100, Height: 100},
		},
		Caption: "Receipt note with <script>alert('xss')</script> and & <b unclosed",
	}

	err := svc.handleReceiptUpload(ctx, msg, usr)
	if err != nil {
		t.Fatalf("handleReceiptUpload failed: %v", err)
	}

	var caption string
	if capturedPhoto != nil {
		caption = capturedPhoto.Caption
	} else if capturedMsg != nil {
		caption = capturedMsg.Text
	} else {
		t.Fatal("expected message or photo forwarded to admin, got none")
	}

	// Verify dangerous raw HTML tags are NOT present
	dangerousPatterns := []string{
		"<script>",
		"</script>",
		"<b unclosed",
		"<a href=\"phish\">",
	}
	for _, p := range dangerousPatterns {
		if strings.Contains(caption, p) {
			t.Errorf("receipt caption contains unescaped malicious pattern %q: %s", p, caption)
		}
	}

	// Verify safe HTML entities ARE present
	requiredEntities := []string{
		"&lt;script&gt;alert(1)&lt;/script&gt;",
		"John&lt;b unclosed",
		"&lt;a href=",
		"&amp;",
	}
	for _, e := range requiredEntities {
		if !strings.Contains(caption, e) {
			t.Errorf("receipt caption missing expected escaped entity %q: %s", e, caption)
		}
	}
}

func TestHTMLSanitization_UserSettingsView(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cfg := &config.Config{
		AdminTelegramID: 99999,
		SupportHandle:   "@support<help>",
	}
	svc := NewService(nil, cfg, store)

	var capturedText string
	svc.onSendMessage = func(p *telego.SendMessageParams) {
		capturedText = p.Text
	}

	userID := int64(223344)
	usr := user.New(userID, "user<script>", "Alice<b unclosed", "Smith<a href=\"evil\">", user.RoleUser, time.Now().UTC())
	_ = store.Users().Save(ctx, usr)

	err := svc.sendUserSettings(ctx, tu.ID(userID), usr)
	if err != nil {
		t.Fatalf("sendUserSettings failed: %v", err)
	}

	dangerousPatterns := []string{
		"<script>",
		"<b unclosed",
		"<a href=\"evil\">",
	}
	for _, p := range dangerousPatterns {
		if strings.Contains(capturedText, p) {
			t.Errorf("user settings text contains unescaped pattern %q: %s", p, capturedText)
		}
	}

	requiredEntities := []string{
		"user&lt;script&gt;",
		"Alice&lt;b unclosed",
		"Smith&lt;a href=",
	}
	for _, e := range requiredEntities {
		if !strings.Contains(capturedText, e) {
			t.Errorf("user settings text missing escaped entity %q: %s", e, capturedText)
		}
	}
}

func TestHTMLSanitization_AdminUserDetailView(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cfg := &config.Config{
		AdminTelegramID: 99999,
		SupportHandle:   "@support",
	}
	svc := NewService(nil, cfg, store)

	var capturedText string
	svc.onSendMessage = func(p *telego.SendMessageParams) {
		capturedText = p.Text
	}

	targetUID := int64(556677)
	usr := user.New(targetUID, "admin_target<script>", "Eve<b unclosed", "Hacker<svg onload=1>", user.RoleUser, time.Now().UTC())
	_ = store.Users().Save(ctx, usr)

	err := svc.sendAdminUserDetailView(ctx, tu.ID(cfg.AdminTelegramID), targetUID)
	if err != nil {
		t.Fatalf("sendAdminUserDetailView failed: %v", err)
	}

	dangerousPatterns := []string{
		"<script>",
		"<b unclosed",
		"<svg onload=1>",
	}
	for _, p := range dangerousPatterns {
		if strings.Contains(capturedText, p) {
			t.Errorf("admin user detail contains unescaped pattern %q: %s", p, capturedText)
		}
	}

	requiredEntities := []string{
		"admin_target&lt;script&gt;",
		"Eve&lt;b unclosed",
		"Hacker&lt;svg onload=1&gt;",
	}
	for _, e := range requiredEntities {
		if !strings.Contains(capturedText, e) {
			t.Errorf("admin user detail missing escaped entity %q: %s", e, capturedText)
		}
	}
}

func TestHTMLSanitization_ServerDetailView(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cfg := &config.Config{
		AdminTelegramID: 99999,
	}
	svc := NewService(nil, cfg, store)

	var capturedText string
	svc.onSendMessage = func(p *telego.SendMessageParams) {
		capturedText = p.Text
	}

	userID := int64(778899)
	res := &billing.ProvisionedResource{
		ID:           "res-evil-1",
		UserID:       userID,
		ServerName:   "srv-<script>alert(1)</script>",
		Location:     "fsn1<b unclosed",
		ImageName:    "ubuntu-24.04<script>",
		IPv4Address:  "192.168.1.1<phish>",
		IPv6Address:  "2001:db8::1<script>",
		RootPassword: "p@ss<w&o\"rd>",
		PowerState:   billing.PowerStateRunning,
		Status:       billing.ResourceStatusActive,
		CreatedAt:    time.Now().UTC(),
	}
	_ = store.Billing().SaveResource(ctx, res)

	err := svc.sendServerManagementView(ctx, tu.ID(userID), res.ID, userID)
	if err != nil {
		t.Fatalf("sendServerManagementView failed: %v", err)
	}

	dangerousPatterns := []string{
		"<script>",
		"<b unclosed",
		"<phish>",
		"<w&o\"rd>",
	}
	for _, p := range dangerousPatterns {
		if strings.Contains(capturedText, p) {
			t.Errorf("server management view contains unescaped pattern %q: %s", p, capturedText)
		}
	}

	requiredEntities := []string{
		"srv-&lt;script&gt;alert(1)&lt;/script&gt;",
		"fsn1&lt;b unclosed",
		"ubuntu-24.04&lt;script&gt;",
		"192.168.1.1&lt;phish&gt;",
		"2001:db8::1&lt;script&gt;",
		"[Encrypted/Set]",
	}
	for _, e := range requiredEntities {
		if !strings.Contains(capturedText, e) {
			t.Errorf("server management view missing escaped entity %q: %s", e, capturedText)
		}
	}

	if strings.Contains(capturedText, "p@ss") {
		t.Errorf("server management view must not expose root password: %s", capturedText)
	}
}

func TestHTMLSanitization_ProviderErrorMessage(t *testing.T) {
	ctx := context.Background()
	store := memory.NewStore()
	cfg := &config.Config{
		AdminTelegramID: 99999,
		SupportHandle:   "@support",
	}

	providerErr := errors.New("hcloud: API error <code>quota_exceeded</code> <script>phish</script> & unclosed <b")
	failProv := &failingProvider{failErr: providerErr}

	svc := NewService(nil, cfg, store, failProv)

	var capturedText string
	svc.onSendMessage = func(p *telego.SendMessageParams) {
		capturedText = p.Text
	}

	userID := int64(334455)
	usr := user.New(userID, "tester", "Test", "User", user.RoleUser, time.Now().UTC())
	_ = store.Users().Save(ctx, usr)

	w, _ := store.Wallets().GetByUserID(ctx, userID)
	w.BalanceIRT = 500_000
	_ = store.Wallets().UpdateBalances(ctx, w)

	_ = svc.handleBuyExecute(ctx, tu.ID(userID), userID, "cx23", "nbg1", "ubuntu-24.04", "pwd")

	dangerousPatterns := []string{
		"<script>",
		"</script>",
		"<code>quota_exceeded</code>",
		"unclosed <b",
	}
	for _, p := range dangerousPatterns {
		if strings.Contains(capturedText, p) {
			t.Errorf("provider error message contains unescaped pattern %q: %s", p, capturedText)
		}
	}

	requiredEntities := []string{
		"&lt;code&gt;quota_exceeded&lt;/code&gt;",
		"&lt;script&gt;phish&lt;/script&gt;",
		"&amp; unclosed &lt;b",
	}
	for _, e := range requiredEntities {
		if !strings.Contains(capturedText, e) {
			t.Errorf("provider error message missing escaped entity %q: %s", e, capturedText)
		}
	}
}

func TestUpdateLoopPanicRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := memory.NewStore()
	cfg := &config.Config{
		AdminTelegramID: 99999,
		SupportHandle:   "@support",
	}
	svc := NewService(nil, cfg, store)

	// Test 1: HandleUpdate directly on a malformed update that would cause panic (e.g., Message with nil From)
	malformedUpdate := telego.Update{
		UpdateID: 999,
		Message: &telego.Message{
			MessageID: 1,
			Chat:      telego.Chat{ID: 12345},
			From:      nil, // Will panic if dereferenced without recover
			Text:      "/start",
		},
	}

	err := svc.HandleUpdate(ctx, malformedUpdate)
	// Must NOT crash the process; must return a recovered error
	if err == nil {
		t.Fatal("expected error from malformed update causing panic, got nil")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Fatalf("expected panic error message, got: %v", err)
	}

	// Test 2: ProcessUpdateLoop simulation with panic recovery
	// Create a channel that sends a panic-inducing update followed by a valid update
	updatesChan := make(chan telego.Update, 2)
	updatesChan <- malformedUpdate

	validUserID := int64(111222)
	validUser := user.New(validUserID, "validuser", "Valid", "User", user.RoleUser, time.Now().UTC())
	_ = store.Users().Save(ctx, validUser)

	validUpdate := telego.Update{
		UpdateID: 1000,
		Message: &telego.Message{
			MessageID: 2,
			Chat:      telego.Chat{ID: validUserID},
			From: &telego.User{
				ID:        validUserID,
				Username:  "validuser",
				FirstName: "Valid",
				LastName:  "User",
			},
			Text: "/start",
		},
	}
	updatesChan <- validUpdate
	close(updatesChan)

	// Process updates loop with recovery
	processedValid := false
	for u := range updatesChan {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// panic caught
				}
			}()
			_ = svc.HandleUpdate(ctx, u)
			if u.UpdateID == 1000 {
				processedValid = true
			}
		}()
	}

	if !processedValid {
		t.Fatal("expected subsequent valid update to be processed after recovered panic")
	}
}
