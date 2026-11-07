package telegram

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/order"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/wallet"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/crypto"
)

type buyDraft struct {
	PlanID     string    `json:"plan_id,omitempty"`
	Location   string    `json:"location,omitempty"`
	Distro     string    `json:"distro,omitempty"`
	OSImage    string    `json:"os_image,omitempty"`
	AccessMode string    `json:"access_mode,omitempty"` // "pwd" or "ssh"
	SSHKey     string    `json:"ssh_key,omitempty"`
	Awaiting   string    `json:"awaiting,omitempty"` // "ssh_key"
	CreatedAt  time.Time `json:"created_at,omitempty"`
}

func (s *Service) getBuyDraft(userID int64) *buyDraft {
	s.mu.RLock()
	d, ok := s.buyDrafts[userID]
	s.mu.RUnlock()
	if ok && d != nil {
		if time.Since(d.CreatedAt) <= 2*time.Hour {
			return d
		}
		s.clearBuyDraft(userID)
		return nil
	}

	if conv := s.convStore(); conv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var loaded buyDraft
		step, found, err := conv.GetConversationState(ctx, userID, "buy", &loaded)
		if err == nil && found {
			if loaded.CreatedAt.IsZero() {
				loaded.CreatedAt = time.Now().UTC()
			}
			if loaded.Awaiting == "" && step != "" {
				loaded.Awaiting = step
			}
			s.mu.Lock()
			s.buyDrafts[userID] = &loaded
			s.mu.Unlock()
			return &loaded
		}
	}
	return nil
}

func (s *Service) setBuyDraft(userID int64, d *buyDraft) {
	if d == nil {
		s.clearBuyDraft(userID)
		return
	}
	s.mu.Lock()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	s.buyDrafts[userID] = d
	s.mu.Unlock()

	if conv := s.convStore(); conv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = conv.SaveConversationState(ctx, userID, "buy", d.Awaiting, d, time.Now().UTC().Add(2*time.Hour))
	}
}

func (s *Service) clearBuyDraft(userID int64) {
	s.mu.Lock()
	delete(s.buyDrafts, userID)
	s.mu.Unlock()

	if conv := s.convStore(); conv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = conv.DeleteConversationState(ctx, userID, "buy")
	}
}

func validateSSHPublicKey(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("empty SSH key")
	}
	_, _, _, _, err := ssh.ParseAuthorizedKey([]byte(trimmed))
	if err != nil {
		return "", fmt.Errorf("invalid OpenSSH public key format: %w", err)
	}
	return trimmed, nil
}

func locationName(code string) string {
	switch code {
	case "nbg1":
		return "nbg1 (Nuremberg, DE)"
	case "fsn1":
		return "fsn1 (Falkenstein, DE)"
	case "hel1":
		return "hel1 (Helsinki, FI)"
	default:
		return code
	}
}

func osImageLabel(image string) string {
	switch image {
	case "ubuntu-24.04":
		return "Ubuntu 24.04 LTS"
	case "ubuntu-22.04":
		return "Ubuntu 22.04 LTS"
	case "debian-12":
		return "Debian 12"
	case "debian-11":
		return "Debian 11"
	default:
		return image
	}
}

// Step 1: Location selection (1-minute dynamic cache from Hetzner)
func (s *Service) sendBuyStep1Location(ctx context.Context, chatID telego.ChatID, userID int64, itemID string) error {
	it, err := s.store.Catalog().GetItem(ctx, itemID)
	if err != nil {
		return err
	}

	draft := &buyDraft{
		PlanID:    itemID,
		CreatedAt: time.Now().UTC(),
	}
	s.setBuyDraft(userID, draft)

	text := fmt.Sprintf(
		"🌍 <b>Location (Datacenter)</b>\n\n"+
			"• Plan: <b>%s</b> (%d vCPU, %d GB RAM)\n"+
			"Select datacenter location:",
		html.EscapeString(it.Name), it.VCPU, it.RAMGB,
	)

	locations, _ := s.provider.ListLocations(ctx)
	var rows [][]telego.InlineKeyboardButton
	for _, loc := range locations {
		label := fmt.Sprintf("%s (%s, %s)", loc.Name, loc.City, loc.Country)
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(label).WithCallbackData(fmt.Sprintf("buy:loc:%s:%s", itemID, loc.Name)),
		))
	}
	if len(rows) == 0 {
		rows = append(rows,
			tu.InlineKeyboardRow(tu.InlineKeyboardButton("nbg1 (Nuremberg, DE)").WithCallbackData(fmt.Sprintf("buy:loc:%s:nbg1", itemID))),
			tu.InlineKeyboardRow(tu.InlineKeyboardButton("fsn1 (Falkenstein, DE)").WithCallbackData(fmt.Sprintf("buy:loc:%s:fsn1", itemID))),
			tu.InlineKeyboardRow(tu.InlineKeyboardButton("hel1 (Helsinki, FI)").WithCallbackData(fmt.Sprintf("buy:loc:%s:hel1", itemID))),
		)
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("« Back").WithCallbackData(fmt.Sprintf("plan:view:%s", itemID)),
		tu.InlineKeyboardButton("❌ Cancel").WithCallbackData("buy:cancel"),
	))

	markup := tu.InlineKeyboard(rows...)
	_, err = s.sendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

// Step 2a: OS Distribution selection
func (s *Service) sendBuyStep2Distro(ctx context.Context, chatID telego.ChatID, userID int64, itemID string, location string) error {
	draft := s.getBuyDraft(userID)
	if draft == nil {
		draft = &buyDraft{PlanID: itemID, CreatedAt: time.Now().UTC()}
	}
	draft.Location = location
	s.setBuyDraft(userID, draft)

	it, _ := s.store.Catalog().GetItem(ctx, itemID)
	name := itemID
	if it != nil {
		name = it.Name
	}

	text := fmt.Sprintf(
		"💿 <b>Operating System</b>\n\n"+
			"• Plan: <b>%s</b> | Location: <b>%s</b>\n"+
			"Select distribution:",
		html.EscapeString(name), html.EscapeString(locationName(location)),
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("Ubuntu").WithCallbackData(fmt.Sprintf("buy:os_d:%s:%s:ubuntu", itemID, location)),
			tu.InlineKeyboardButton("Debian").WithCallbackData(fmt.Sprintf("buy:os_d:%s:%s:debian", itemID, location)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« Back").WithCallbackData(fmt.Sprintf("plan:buy:%s", itemID)),
			tu.InlineKeyboardButton("❌ Cancel").WithCallbackData("buy:cancel"),
		),
	)

	_, err := s.sendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

// Step 2b: OS Version selection (1-minute dynamic cache from Hetzner)
func (s *Service) sendBuyStep2Version(ctx context.Context, chatID telego.ChatID, userID int64, itemID, location, distro string) error {
	draft := s.getBuyDraft(userID)
	if draft == nil {
		draft = &buyDraft{PlanID: itemID, Location: location, CreatedAt: time.Now().UTC()}
	}
	draft.Distro = distro
	s.setBuyDraft(userID, draft)

	distroTitle := "Ubuntu"
	if distro == "debian" {
		distroTitle = "Debian"
	}
	title := fmt.Sprintf("💿 <b>%s Version</b>\n\nSelect version:", html.EscapeString(distroTitle))

	images, _ := s.provider.ListImages(ctx)
	var rows [][]telego.InlineKeyboardButton
	for _, img := range images {
		if strings.EqualFold(img.OSFlavor, distro) {
			label := img.Description
			if label == "" {
				label = img.Name
			}
			rows = append(rows, tu.InlineKeyboardRow(
				tu.InlineKeyboardButton(label).WithCallbackData(fmt.Sprintf("buy:os_v:%s:%s:%s:%s", itemID, location, distro, img.Name)),
			))
		}
	}
	if len(rows) == 0 {
		if distro == "ubuntu" {
			rows = append(rows,
				tu.InlineKeyboardRow(tu.InlineKeyboardButton("Ubuntu 24.04 LTS").WithCallbackData(fmt.Sprintf("buy:os_v:%s:%s:%s:ubuntu-24.04", itemID, location, distro))),
				tu.InlineKeyboardRow(tu.InlineKeyboardButton("Ubuntu 22.04 LTS").WithCallbackData(fmt.Sprintf("buy:os_v:%s:%s:%s:ubuntu-22.04", itemID, location, distro))),
			)
		} else {
			rows = append(rows,
				tu.InlineKeyboardRow(tu.InlineKeyboardButton("Debian 12").WithCallbackData(fmt.Sprintf("buy:os_v:%s:%s:%s:debian-12", itemID, location, distro))),
				tu.InlineKeyboardRow(tu.InlineKeyboardButton("Debian 11").WithCallbackData(fmt.Sprintf("buy:os_v:%s:%s:%s:debian-11", itemID, location, distro))),
			)
		}
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("« Back").WithCallbackData(fmt.Sprintf("buy:loc:%s:%s", itemID, location)),
		tu.InlineKeyboardButton("❌ Cancel").WithCallbackData("buy:cancel"),
	))

	markup := tu.InlineKeyboard(rows...)
	_, err := s.sendMessage(ctx, tu.Message(chatID, title).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

// Step 3: Access Mode selection
func (s *Service) sendBuyStep3Access(ctx context.Context, chatID telego.ChatID, userID int64, itemID, location, distro, image string) error {
	draft := s.getBuyDraft(userID)
	if draft == nil {
		draft = &buyDraft{PlanID: itemID, Location: location, Distro: distro, CreatedAt: time.Now().UTC()}
	}
	draft.OSImage = image
	draft.Awaiting = ""
	s.setBuyDraft(userID, draft)

	it, _ := s.store.Catalog().GetItem(ctx, itemID)
	name := itemID
	if it != nil {
		name = it.Name
	}

	text := fmt.Sprintf(
		"🔑 <b>Authentication</b>\n\n"+
			"• Plan: <b>%s</b> | Location: <b>%s</b> | OS: <b>%s</b>\n\n"+
			"Select authentication method:",
		html.EscapeString(name), html.EscapeString(locationName(location)), html.EscapeString(osImageLabel(image)),
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🔑 Root Password (Auto-generate)").WithCallbackData(fmt.Sprintf("buy:acc_pwd:%s:%s:%s", itemID, location, image)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🛡️ SSH Public Key").WithCallbackData(fmt.Sprintf("buy:acc_ssh_prompt:%s:%s:%s", itemID, location, image)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« Back").WithCallbackData(fmt.Sprintf("buy:os_d:%s:%s:%s", itemID, location, distro)),
			tu.InlineKeyboardButton("❌ Cancel").WithCallbackData("buy:cancel"),
		),
	)

	_, err := s.sendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

// Step 3 - SSH Prompt
func (s *Service) sendBuyStep3SSHPrompt(ctx context.Context, chatID telego.ChatID, userID int64, itemID, location, image string) error {
	draft := s.getBuyDraft(userID)
	if draft == nil {
		draft = &buyDraft{PlanID: itemID, Location: location, OSImage: image, CreatedAt: time.Now().UTC()}
	}
	draft.Awaiting = "ssh_key"
	s.setBuyDraft(userID, draft)

	text := "🛡️ <b>Send SSH Public Key</b>\n\n" +
		"Paste your OpenSSH public key in this chat:\n" +
		"<code>ssh-ed25519 AAAAC3... user@host</code>\n" +
		"<code>ssh-rsa AAAAB3... user@host</code>"

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🔑 Use Root Password instead").WithCallbackData(fmt.Sprintf("buy:acc_pwd:%s:%s:%s", itemID, location, image)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("❌ Cancel").WithCallbackData("buy:cancel"),
		),
	)

	_, err := s.sendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

// Step 4: Pre-invoice summary and checkout confirmation
func (s *Service) sendBuyStep4Confirm(ctx context.Context, chatID telego.ChatID, userID int64, draft *buyDraft) error {
	it, err := s.store.Catalog().GetItem(ctx, draft.PlanID)
	if err != nil {
		return err
	}

	obs, _ := s.store.FxRates().FetchRate(ctx)
	sellRate := pricing.CalculateSellRate(obs.MarketIRTPerUSD, pricing.IRT(s.cfg.RateSpreadIRTPerUSD))
	hourlyIRT := pricing.CalculatePriceIRT(it.HourlyUSDMicro, sellRate)
	monthlyIRT := pricing.CalculatePriceIRT(it.MonthlyCapUSD, sellRate)
	initialBlockIRT := hourlyIRT

	w, _ := s.store.Wallets().GetByUserID(ctx, userID)
	combinedVal := w.CombinedValuation(sellRate)

	var accessDesc string
	if draft.AccessMode == "ssh" {
		accessDesc = "SSH Key"
	} else {
		accessDesc = "Root Password"
	}

	balanceSufficient := combinedVal >= initialBlockIRT

	text := fmt.Sprintf(
		"🧾 <b>Order Confirmation</b>\n\n"+
			"• Server: <b>%s</b> (%d vCPU, %d GB RAM, %d GB NVMe)\n"+
			"• Location: <b>%s</b>\n"+
			"• OS: <b>%s</b>\n"+
			"• Auth: <b>%s</b>\n\n"+
			"💰 <b>Pricing:</b>\n"+
			"• Hourly: <code>%s Toman</code>\n"+
			"• Monthly Cap: <code>%s Toman</code>\n"+
			"• Prepay (1st hour): <code>%s Toman</code>\n\n"+
			"💼 <b>Wallet Balance:</b> <code>%s Toman</code>",
		html.EscapeString(it.Name), it.VCPU, it.RAMGB, it.DiskGB,
		html.EscapeString(locationName(draft.Location)),
		html.EscapeString(osImageLabel(draft.OSImage)),
		html.EscapeString(accessDesc),
		formatNumber(int64(hourlyIRT)),
		formatNumber(int64(monthlyIRT)),
		formatNumber(int64(initialBlockIRT)),
		formatNumber(int64(combinedVal)),
	)

	var rows [][]telego.InlineKeyboardButton

	if balanceSufficient {
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🚀 Confirm & Deploy Server").WithCallbackData(fmt.Sprintf("buy:exec:%s:%s:%s:%s", draft.PlanID, draft.Location, draft.OSImage, draft.AccessMode)),
		))
	} else {
		deficit := initialBlockIRT - combinedVal
		text += fmt.Sprintf("\n\n⛔ <b>Insufficient balance.</b> Deficit: <code>%s Toman</code>", formatNumber(int64(deficit)))
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("💳 Top Up Wallet").WithCallbackData("wallet:topup_info"),
		))
	}

	distro := draft.Distro
	if distro == "" {
		distro = "ubuntu"
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("« Edit").WithCallbackData(fmt.Sprintf("buy:os_v:%s:%s:%s:%s", draft.PlanID, draft.Location, distro, draft.OSImage)),
		tu.InlineKeyboardButton("❌ Cancel").WithCallbackData("buy:cancel"),
	))

	markup := tu.InlineKeyboard(rows...)
	_, err = s.sendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

// Step 5: Provisioning Execution
func (s *Service) handleBuyExecute(ctx context.Context, chatID telego.ChatID, userID int64, planID, location, osImage, accessMode string) error {
	// In-flight buy lock to prevent double-click server creation
	if _, loaded := s.inFlightBuys.LoadOrStore(userID, struct{}{}); loaded {
		_, _ = s.sendMessage(ctx, tu.Message(chatID, "⏳ درخواست خرید سرور شما در حال پردازش است، لطفاً کمی صبر فرمایید..."))
		return nil
	}
	defer s.inFlightBuys.Delete(userID)

	usr, err := s.store.Users().GetByID(ctx, userID)
	if err != nil || usr == nil {
		return s.sendAccessDenied(ctx, chatID)
	}
	if usr.Blocked {
		_, _ = s.sendMessage(ctx, tu.Message(chatID, fmt.Sprintf("⛔ Your account is blocked: %s", html.EscapeString(s.cfg.SupportHandle))))
		return nil
	}

	it, err := s.store.Catalog().GetItem(ctx, planID)
	if err != nil {
		_, _ = s.sendMessage(ctx, tu.Message(chatID, "❌ Selected plan was not found in catalog."))
		return nil
	}

	// Token configuration check: never silently fake server creation if token is missing
	if !s.provider.HasToken() {
		errMsg := "⚠️ <b>Hetzner API token is not configured on this host.</b>\n\n" +
			"Live server creation cannot proceed until <code>HCLOUD_TOKEN</code> is set in <code>/etc/manova/shop-tgbot/shopbot.env</code>.\n\n" +
			"Your wallet was not debited."
		_, _ = s.sendMessage(ctx, tu.Message(chatID, errMsg).WithParseMode(telego.ModeHTML))
		return nil
	}

	obs, _ := s.store.FxRates().FetchRate(ctx)
	sellRate := pricing.CalculateSellRate(obs.MarketIRTPerUSD, pricing.IRT(s.cfg.RateSpreadIRTPerUSD))
	hourlyIRT := pricing.CalculatePriceIRT(it.HourlyUSDMicro, sellRate)
	initialBlockIRT := hourlyIRT

	orderSalt := make([]byte, 4)
	_, _ = rand.Read(orderSalt)
	orderID := fmt.Sprintf("ord-%d-%s", userID, hex.EncodeToString(orderSalt))

	w, err := s.store.Wallets().DebitAtomic(
		ctx,
		userID,
		initialBlockIRT,
		usr.Segment,
		sellRate,
		wallet.KindPurchase,
		"order",
		orderID,
	)
	if err != nil {
		if errors.Is(err, wallet.ErrBelowFloor) {
			draft := s.getBuyDraft(userID)
			if draft != nil {
				return s.sendBuyStep4Confirm(ctx, chatID, userID, draft)
			}
			_, _ = s.sendMessage(ctx, tu.Message(chatID, "⛔ موجودی کیف پول شما برای این خرید کافی نیست (به سقف اعتبار رسیده‌اید)."))
			return nil
		}
		slog.Error("failed to debit wallet atomically", "user_id", userID, "err", err)
		_, _ = s.sendMessage(ctx, tu.Message(chatID, "❌ خطا در کسر از موجودی کیف پول."))
		return nil
	}

	waitMsg, _ := s.sendMessage(ctx, tu.Message(chatID, "⏳ <b>Deploying server on Hetzner Cloud...</b>").WithParseMode(telego.ModeHTML))

	// 2. Create Order record
	ord := &order.Order{
		ID:           orderID,
		UserID:       userID,
		Status:       order.StatePaid,
		CheckoutMode: order.CheckoutModeWallet,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := s.store.Orders().Save(ctx, ord, nil); err != nil {
		slog.Error("failed to save order", "err", err, "order_id", orderID)
	}

	// 3. Call CloudProvider
	serverName := fmt.Sprintf("%s-%d-%d", it.HetznerType, userID, time.Now().Unix()%1000)
	var sshKeys []string
	var savedSSHKey string
	draft := s.getBuyDraft(userID)
	if accessMode == "ssh" && draft != nil && draft.SSHKey != "" {
		sshKeys = []string{draft.SSHKey}
		savedSSHKey = draft.SSHKey
	}

	createResult, err := s.provider.CreateServer(ctx, serverName, it.HetznerType, osImage, location, sshKeys)
	if err != nil {
		// Rollback debit on provider failure
		w.BalanceIRT += initialBlockIRT
		if err := s.store.Wallets().UpdateBalances(ctx, w); err != nil {
			slog.Error("failed to update wallet balance on rollback", "err", err, "user_id", userID)
		}

		// Compensating ledger entry to preserve accounting balance invariant: Balance == sum(Ledger Amounts)
		revSalt := make([]byte, 4)
		_, _ = rand.Read(revSalt)
		revLedgID := fmt.Sprintf("led-rev-%d-%s", time.Now().UnixNano(), hex.EncodeToString(revSalt))
		if err := s.store.Wallets().RecordLedgerEntry(ctx, &wallet.LedgerEntry{
			ID:           revLedgID,
			WalletID:     w.ID,
			Currency:     wallet.CurrencyIRT,
			Amount:       int64(initialBlockIRT),
			Kind:         wallet.KindUsageReversal,
			BalanceAfter: int64(w.BalanceIRT),
			RefType:      "order",
			RefID:        ord.ID,
			CreatedAt:    time.Now().UTC(),
		}); err != nil {
			slog.Error("failed to record compensating ledger entry", "err", err, "order_id", ord.ID)
		}

		ord.Status = order.StateFulfillmentFailed
		_ = s.store.Orders().Save(ctx, ord, nil)

		if waitMsg != nil {
			_ = s.deleteMessage(ctx, &telego.DeleteMessageParams{
				ChatID:    chatID,
				MessageID: waitMsg.MessageID,
			})
		}

		errMsg := fmt.Sprintf("❌ <b>Hetzner Server Creation Failed</b>\n\n%s\n\nYour wallet balance has been refunded.", html.EscapeString(err.Error()))
		_, _ = s.sendMessage(ctx, tu.Message(chatID, errMsg).WithParseMode(telego.ModeHTML))
		return nil
	}

	// 4. Save ProvisionedResource
	rootPasswordToSave := createResult.RootPassword
	if s.cfg != nil && !s.cfg.CredentialKEK.IsZero() && s.cfg.CredentialKEK.Expose() != "" && createResult.RootPassword != "" {
		if encPwd, err := crypto.Encrypt(createResult.RootPassword, s.cfg.CredentialKEK.Expose()); err == nil {
			rootPasswordToSave = encPwd
		} else {
			slog.Error("failed to encrypt root password", "err", err)
		}
	}

	resID := fmt.Sprintf("res-%d", time.Now().UnixNano())
	res := &billing.ProvisionedResource{
		ID:             resID,
		UserID:         userID,
		ServerName:     serverName,
		OrderItemID:    ord.ID,
		ExternalID:     createResult.ExternalID,
		Status:         billing.ResourceStatusActive,
		PowerState:     billing.PowerStateRunning,
		HourlyPriceUSD: it.HourlyUSDMicro,
		MonthlyCapUSD:  it.MonthlyCapUSD,
		IPv4Address:    createResult.IPv4Address,
		IPv6Address:    createResult.IPv6Address,
		Location:       location,
		ImageName:      osImage,
		RootPassword:   rootPasswordToSave,
		SSHKey:         savedSSHKey,
		BillingStartAt: time.Now().UTC(),
		CreatedAt:      time.Now().UTC(),
	}
	_ = s.store.Billing().SaveResource(ctx, res)

	ord.Status = order.StateDelivered
	_ = s.store.Orders().Save(ctx, ord, nil)

	s.clearBuyDraft(userID)

	if waitMsg != nil {
		_ = s.deleteMessage(ctx, &telego.DeleteMessageParams{
			ChatID:    chatID,
			MessageID: waitMsg.MessageID,
		})
	}

	// 5. Deliver Server Card (Minimal & Technical)
	var credsSection string
	if accessMode == "ssh" {
		credsSection = "• Auth: <b>SSH Key</b>\n• Username: <code>root</code>"
	} else {
		revealURL := s.generateRevealURL(createResult.RootPassword)
		if revealURL != "" {
			credsSection = fmt.Sprintf("• Username: <code>root</code>\n• 🔑 One-time Password: <a href=\"%s\">Click to reveal</a> (Valid 15m)", revealURL)
		} else {
			credsSection = "• Username: <code>root</code>\n• 🔑 One-time Password: <i>Generated</i>"
		}
	}

	successText := fmt.Sprintf(
		"🎉 <b>Server Ready</b>\n\n"+
			"• Name: <code>%s</code>\n"+
			"• Plan: <b>%s</b>\n"+
			"• Location: <b>%s</b>\n"+
			"• OS: <b>%s</b>\n\n"+
			"🌐 <b>Network:</b>\n"+
			"• IPv4: <code>%s</code>\n"+
			"• IPv6: <code>%s</code>\n\n"+
			"🔑 <b>Login:</b>\n"+
			"%s\n\n"+
			"📌 <b>Connect:</b>\n"+
			"<code>ssh root@%s</code>",
		html.EscapeString(serverName),
		html.EscapeString(it.Name),
		html.EscapeString(locationName(location)),
		html.EscapeString(osImageLabel(osImage)),
		html.EscapeString(createResult.IPv4Address),
		html.EscapeString(createResult.IPv6Address),
		credsSection,
		html.EscapeString(createResult.IPv4Address),
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🖥️ Manage Server").WithCallbackData(fmt.Sprintf("srv:manage:%s", res.ID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("📋 My Servers").WithCallbackData("menu:servers"),
			tu.InlineKeyboardButton("🏠 Main Menu").WithCallbackData("menu:main"),
		),
	)

	_, err = s.sendMessage(ctx, tu.Message(chatID, successText).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}
