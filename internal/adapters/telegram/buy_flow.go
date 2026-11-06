package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/order"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/wallet"
)

type buyDraft struct {
	PlanID     string
	Location   string
	Distro     string
	OSImage    string
	AccessMode string // "pwd" or "ssh"
	SSHKey     string
	Awaiting   string // "ssh_key"
	CreatedAt  time.Time
}

func (s *Service) getBuyDraft(userID int64) *buyDraft {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.buyDrafts[userID]
	if !ok || d == nil {
		return nil
	}
	if time.Since(d.CreatedAt) > 2*time.Hour {
		return nil
	}
	return d
}

func (s *Service) setBuyDraft(userID int64, d *buyDraft) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	s.buyDrafts[userID] = d
}

func (s *Service) clearBuyDraft(userID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.buyDrafts, userID)
}

func validateSSHPublicKey(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("کلید SSH خالی است")
	}
	_, _, _, _, err := ssh.ParseAuthorizedKey([]byte(trimmed))
	if err != nil {
		return "", fmt.Errorf("فرمت کلید عمومی OpenSSH نامعتبر است: %w", err)
	}
	return trimmed, nil
}

func locationName(code string) string {
	switch code {
	case "nbg1":
		return "🇩🇪 نورنبرگ، آلمان (nbg1)"
	case "fsn1":
		return "🇩🇪 فالکن‌اشتاین، آلمان (fsn1)"
	case "hel1":
		return "🇫🇮 هلسینکی، فنلاند (hel1)"
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
		return "Debian 12 (Bookworm)"
	case "debian-11":
		return "Debian 11 (Bullseye)"
	default:
		return image
	}
}

// Step 1: Location selection
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
		"🌍 <b>مرحله ۱ از ۳: انتخاب موقعیت دیتاسنتر</b>\n\n"+
			"• سرور انتخابی: <b>%s</b> (%d vCPU, %d GB RAM)\n\n"+
			"لطفاً لوکیشن دیتاسنتر مورد نظر خود در اروپا را انتخاب کنید:",
		it.Name, it.VCPU, it.RAMGB,
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🇩🇪 نورنبرگ، آلمان (Nuremberg - nbg1)").WithCallbackData(fmt.Sprintf("buy:loc:%s:nbg1", itemID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🇩🇪 فالکن‌اشتاین، آلمان (Falkenstein - fsn1)").WithCallbackData(fmt.Sprintf("buy:loc:%s:fsn1", itemID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🇫🇮 هلسینکی، فنلاند (Helsinki - hel1)").WithCallbackData(fmt.Sprintf("buy:loc:%s:hel1", itemID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« بازگشت به مشخصات سرور").WithCallbackData(fmt.Sprintf("plan:view:%s", itemID)),
			tu.InlineKeyboardButton("❌ انصراف").WithCallbackData("buy:cancel"),
		),
	)

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
		"💿 <b>مرحله ۲ از ۳: انتخاب سیستم‌عامل سرور</b>\n\n"+
			"• سرور: <b>%s</b>\n"+
			"• دیتاسنتر: <b>%s</b>\n\n"+
			"لطفاً توزیع لینوکس مورد نظر خود را انتخاب نمایید:",
		name, locationName(location),
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🐧 اوبونتو (Ubuntu)").WithCallbackData(fmt.Sprintf("buy:os_d:%s:%s:ubuntu", itemID, location)),
			tu.InlineKeyboardButton("🔴 دبیان (Debian)").WithCallbackData(fmt.Sprintf("buy:os_d:%s:%s:debian", itemID, location)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« بازگشت به انتخاب لوکیشن").WithCallbackData(fmt.Sprintf("plan:buy:%s", itemID)),
			tu.InlineKeyboardButton("❌ انصراف").WithCallbackData("buy:cancel"),
		),
	)

	_, err := s.sendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

// Step 2b: OS Version selection
func (s *Service) sendBuyStep2Version(ctx context.Context, chatID telego.ChatID, userID int64, itemID, location, distro string) error {
	draft := s.getBuyDraft(userID)
	if draft == nil {
		draft = &buyDraft{PlanID: itemID, Location: location, CreatedAt: time.Now().UTC()}
	}
	draft.Distro = distro
	s.setBuyDraft(userID, draft)

	var title string
	var rows [][]telego.InlineKeyboardButton

	if distro == "ubuntu" {
		title = "🐧 <b>انتخاب نسخه اوبونتو (Ubuntu)</b>\n\nنسخه مورد نظر را برای نصب روی سرور مشخص فرمایید:"
		rows = [][]telego.InlineKeyboardButton{
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("Ubuntu 24.04 LTS (جدیدترین نسخه پایدار)").WithCallbackData(fmt.Sprintf("buy:os_v:%s:%s:%s:ubuntu-24.04", itemID, location, distro)),
			),
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("Ubuntu 22.04 LTS (پایدار)").WithCallbackData(fmt.Sprintf("buy:os_v:%s:%s:%s:ubuntu-22.04", itemID, location, distro)),
			),
		}
	} else {
		title = "🔴 <b>انتخاب نسخه دبیان (Debian)</b>\n\nنسخه مورد نظر را برای نصب روی سرور مشخص فرمایید:"
		rows = [][]telego.InlineKeyboardButton{
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("Debian 12 (Bookworm)").WithCallbackData(fmt.Sprintf("buy:os_v:%s:%s:%s:debian-12", itemID, location, distro)),
			),
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("Debian 11 (Bullseye)").WithCallbackData(fmt.Sprintf("buy:os_v:%s:%s:%s:debian-11", itemID, location, distro)),
			),
		}
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("« بازگشت به انتخاب توزیع").WithCallbackData(fmt.Sprintf("buy:loc:%s:%s", itemID, location)),
		tu.InlineKeyboardButton("❌ انصراف").WithCallbackData("buy:cancel"),
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
		"🔑 <b>مرحله ۳ از ۳: شیوه دسترسی و احراز هویت</b>\n\n"+
			"• سرور: <b>%s</b>\n"+
			"• لوکیشن: <b>%s</b>\n"+
			"• سیستم‌عامل: <b>%s</b>\n\n"+
			"لطفاً نحوه دسترسی به حساب root سرور را تعیین نمایید:\n\n"+
			"• <b>کلمه عبور Root:</b> یک رمز عبور تصادفی و بسیار امن تولید شده و بلافاصله پس از ساخت سرور به شما تحویل داده می‌شود.\n\n"+
			"• <b>کلید عمومی SSH:</b> ورود بسیار امن بدون نیاز به کلمه عبور با کلید اختصاصی OpenSSH شما.",
		name, locationName(location), osImageLabel(image),
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🔑 کلمه عبور Root (تولید خودکار و امن)").WithCallbackData(fmt.Sprintf("buy:acc_pwd:%s:%s:%s", itemID, location, image)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🛡️ ثبت کلید عمومی SSH (Public Key)").WithCallbackData(fmt.Sprintf("buy:acc_ssh_prompt:%s:%s:%s", itemID, location, image)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« بازگشت به انتخاب سیستم‌عامل").WithCallbackData(fmt.Sprintf("buy:os_d:%s:%s:%s", itemID, location, distro)),
			tu.InlineKeyboardButton("❌ انصراف").WithCallbackData("buy:cancel"),
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

	text := "🛡️ <b>ثبت کلید عمومی SSH (OpenSSH Public Key)</b>\n\n" +
		"لطفاً کلید عمومی خود را کپی کرده و به عنوان یک پیام در همین چت ارسال نمایید.\n\n" +
		"نمونه فرمت‌های معتبر:\n" +
		"<code>ssh-ed25519 AAAAC3NzaC1lZDI1NTE5... user@host</code>\n" +
		"<code>ssh-rsa AAAAB3NzaC1yc2E... user@host</code>\n\n" +
		"⚠️ <i>توجه: کلید خصوصی (Private Key) خود را هرگز برای هیچ‌کس ارسال نکنید! فقط کلید عمومی با پیشوند ssh-ed25519 یا ssh-rsa پذیرفته می‌شود.</i>"

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🔑 انصراف و استفاده از رمز عبور Root").WithCallbackData(fmt.Sprintf("buy:acc_pwd:%s:%s:%s", itemID, location, image)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("❌ انصراف از خرید").WithCallbackData("buy:cancel"),
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
		accessDesc = "🛡️ کلید عمومی SSH (ورود بدون نیاز به پسورد)"
	} else {
		accessDesc = "🔑 کلمه عبور Root (تولید امن پس از ساخت)"
	}

	balanceSufficient := combinedVal >= initialBlockIRT

	text := fmt.Sprintf(
		"🧾 <b>پیش‌فاکتور و تایید نهایی سفارش سرور</b>\n\n"+
			"🖥️ <b>مشخصات فنی سرور:</b>\n"+
			"• مدل سرور: <b>%s</b> (%d vCPU, %d GB RAM, %d GB NVMe)\n"+
			"• موقعیت دیتاسنتر: <b>%s</b>\n"+
			"• سیستم‌عامل انتخابی: <b>%s</b>\n"+
			"• شیوه احراز هویت: <b>%s</b>\n\n"+
			"💰 <b>تعرفه و هزینه مصرف:</b>\n"+
			"• نرخ تبدیل دلار روز: <code>%s تومان</code>\n"+
			"• هزینه ساعتی: <code>%s تومان</code> (دقیقه‌ای %s ت)\n"+
			"• سقف هزینه ماهیانه: <code>%s تومان</code>\n"+
			"• مبلغ پیش‌پرداخت اولیه (ساعت اول): <code>%s تومان</code>\n\n"+
			"💼 <b>وضعیت کیف پول شما:</b>\n"+
			"• موجودی قابل استفاده: <code>%s تومان</code>\n\n"+
			"⚠️ <i>توجه: طبق ساختار تعرفه کلود هتزنر، محاسبه هزینه بر مبنای دقایق وجود سرور بوده و حتی در حالت خاموش تا زمان حذف سرور ادامه دارد.</i>",
		it.Name, it.VCPU, it.RAMGB, it.DiskGB,
		locationName(draft.Location),
		osImageLabel(draft.OSImage),
		accessDesc,
		formatNumber(int64(sellRate)),
		formatNumber(int64(hourlyIRT)), formatNumber(int64(hourlyIRT/60)),
		formatNumber(int64(monthlyIRT)),
		formatNumber(int64(initialBlockIRT)),
		formatNumber(int64(combinedVal)),
	)

	var rows [][]telego.InlineKeyboardButton

	if balanceSufficient {
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🚀 تایید سفارش و ساخت فوری سرور").WithCallbackData(fmt.Sprintf("buy:exec:%s:%s:%s:%s", draft.PlanID, draft.Location, draft.OSImage, draft.AccessMode)),
		))
	} else {
		deficit := initialBlockIRT - combinedVal
		text += fmt.Sprintf("\n\n⛔ <b>موجودی کیف پول شما برای پیش‌پرداخت ساعت اول کافی نیست!</b>\nکسری موجودی: <code>%s تومان</code>\nلطفاً ابتدا کیف پول خود را شارژ فرمایید.", formatNumber(int64(deficit)))
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("💳 شارژ کیف پول").WithCallbackData("wallet:topup_info"),
		))
	}

	distro := draft.Distro
	if distro == "" {
		distro = "ubuntu"
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("« ویرایش مشخصات دسترسی").WithCallbackData(fmt.Sprintf("buy:os_v:%s:%s:%s:%s", draft.PlanID, draft.Location, distro, draft.OSImage)),
		tu.InlineKeyboardButton("❌ انصراف").WithCallbackData("buy:cancel"),
	))

	markup := tu.InlineKeyboard(rows...)
	_, err = s.sendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

// Step 5: Provisioning Execution
func (s *Service) handleBuyExecute(ctx context.Context, chatID telego.ChatID, userID int64, planID, location, osImage, accessMode string) error {
	usr, err := s.store.Users().GetByID(ctx, userID)
	if err != nil || usr == nil {
		return s.sendAccessDenied(ctx, chatID)
	}
	if usr.Blocked {
		_, _ = s.sendMessage(ctx, tu.Message(chatID, fmt.Sprintf("⛔ حساب شما مسدود است: %s", s.cfg.SupportHandle)))
		return nil
	}

	it, err := s.store.Catalog().GetItem(ctx, planID)
	if err != nil {
		_, _ = s.sendMessage(ctx, tu.Message(chatID, "❌ پلن مورد نظر در کاتالوگ یافت نشد."))
		return nil
	}

	obs, _ := s.store.FxRates().FetchRate(ctx)
	sellRate := pricing.CalculateSellRate(obs.MarketIRTPerUSD, pricing.IRT(s.cfg.RateSpreadIRTPerUSD))
	hourlyIRT := pricing.CalculatePriceIRT(it.HourlyUSDMicro, sellRate)
	initialBlockIRT := hourlyIRT

	w, err := s.store.Wallets().GetByUserID(ctx, userID)
	if err != nil {
		return err
	}

	if w.CombinedValuation(sellRate) < initialBlockIRT {
		draft := s.getBuyDraft(userID)
		if draft != nil {
			return s.sendBuyStep4Confirm(ctx, chatID, userID, draft)
		}
		_, _ = s.sendMessage(ctx, tu.Message(chatID, "⛔ موجودی کیف پول کافی نیست. لطفاً کیف پول را شارژ فرمایید."))
		return nil
	}

	// Progress indicator
	waitMsg, _ := s.sendMessage(ctx, tu.Message(chatID, "⏳ <b>در حال کسر پیش‌پرداخت و ارسال دستور ساخت سرور به دیتاسنتر...</b>").WithParseMode(telego.ModeHTML))

	// 1. Debit initial block from wallet
	w.BalanceIRT -= initialBlockIRT
	_ = s.store.Wallets().UpdateBalances(ctx, w)

	orderID := fmt.Sprintf("ord-%d-%d", userID, time.Now().UnixNano()%1000000)
	_ = s.store.Wallets().RecordLedgerEntry(ctx, &wallet.LedgerEntry{
		ID:           fmt.Sprintf("led-%d", time.Now().UnixNano()),
		WalletID:     w.ID,
		Currency:     wallet.CurrencyIRT,
		Amount:       -int64(initialBlockIRT),
		Kind:         wallet.KindPurchase,
		BalanceAfter: int64(w.BalanceIRT),
		RefType:      "order",
		RefID:        orderID,
		CreatedAt:    time.Now().UTC(),
	})

	// 2. Create Order record
	ord := &order.Order{
		ID:           orderID,
		UserID:       userID,
		Status:       order.StatePaid,
		CheckoutMode: order.CheckoutModeWallet,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	_ = s.store.Orders().Save(ctx, ord, nil)

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
		// Rollback debit on critical provider failure
		w.BalanceIRT += initialBlockIRT
		_ = s.store.Wallets().UpdateBalances(ctx, w)
		ord.Status = order.StateFulfillmentFailed
		_ = s.store.Orders().Save(ctx, ord, nil)

		errMsg := fmt.Sprintf("❌ <b>خطا در ساخت سرور!</b>\n\nارتباط با دیتاسنتر با خطا مواجه شد و مبلغ کسر شده به کیف پول شما برگشت داده شد.\nخطا: <code>%v</code>", err)
		_, _ = s.sendMessage(ctx, tu.Message(chatID, errMsg).WithParseMode(telego.ModeHTML))
		return nil
	}

	// 4. Save ProvisionedResource
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
		RootPassword:   createResult.RootPassword,
		SSHKey:         savedSSHKey,
		BillingStartAt: time.Now().UTC(),
		CreatedAt:      time.Now().UTC(),
	}
	_ = s.store.Billing().SaveResource(ctx, res)

	ord.Status = order.StateDelivered
	_ = s.store.Orders().Save(ctx, ord, nil)

	// 5. Clear draft
	s.clearBuyDraft(userID)

	if waitMsg != nil {
		_ = s.deleteMessage(ctx, &telego.DeleteMessageParams{
			ChatID:    chatID,
			MessageID: waitMsg.MessageID,
		})
	}

	// 6. Deliver Server Card
	var credsSection string
	if accessMode == "ssh" {
		credsSection = "• شیوه دسترسی: 🛡️ <b>ورود با کلید SSH عمومی ثبت‌شده</b>\n• نام کاربری: <code>root</code>"
	} else {
		credsSection = fmt.Sprintf("• نام کاربری: <code>root</code>\n• کلمه عبور Root:\n<code>%s</code>", createResult.RootPassword)
	}

	successText := fmt.Sprintf(
		"🎉 <b>تبریک! سرور ابری شما با موفقیت راه‌اندازی و تحویل داده شد.</b>\n\n"+
			"🖥️ <b>اطلاعات سرور:</b>\n"+
			"• نام سرور: <code>%s</code>\n"+
			"• مدل: <b>%s</b> (%d vCPU, %d GB RAM)\n"+
			"• موقعیت دیتاسنتر: <b>%s</b>\n"+
			"• سیستم‌عامل: <b>%s</b>\n\n"+
			"🌐 <b>آدرس‌های شبکه:</b>\n"+
			"• آی‌پی اصلی (IPv4):\n<code>%s</code>\n"+
			"• آی‌پی (IPv6):\n<code>%s</code>\n\n"+
			"🔑 <b>اطلاعات ورود به سرور:</b>\n"+
			"%s\n\n"+
			"📌 <b>دستور اتصال سریع در ترمینال:</b>\n"+
			"<code>ssh root@%s</code>\n\n"+
			"💡 <i>جهت مدیریت، ریبوت، خاموش کردن یا بازنشانی کلمه عبور، به بخش «🖥️ سرورهای من» مراجعه فرمایید.</i>",
		serverName, it.Name, it.VCPU, it.RAMGB,
		locationName(location),
		osImageLabel(osImage),
		createResult.IPv4Address,
		createResult.IPv6Address,
		credsSection,
		createResult.IPv4Address,
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🖥️ مدیریت این سرور").WithCallbackData(fmt.Sprintf("srv:manage:%s", res.ID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("📋 لیست سرورهای من").WithCallbackData("menu:servers"),
			tu.InlineKeyboardButton("🏠 منوی اصلی").WithCallbackData("menu:main"),
		),
	)

	_, err = s.sendMessage(ctx, tu.Message(chatID, successText).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}
