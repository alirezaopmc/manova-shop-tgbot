package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/adapters/memory"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/invite"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/platform/config"
)

// Service runs the Telegram bot adapter over long polling.
type Service struct {
	bot         *telego.Bot
	cfg         *config.Config
	store       *memory.Store
	botUsername string
}

func NewService(bot *telego.Bot, cfg *config.Config, store *memory.Store) *Service {
	return &Service{
		bot:   bot,
		cfg:   cfg,
		store: store,
	}
}

// Start initiates long polling and handles incoming updates until ctx is cancelled.
func (s *Service) Start(ctx context.Context) error {
	me, err := s.bot.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("getMe failed: %w", err)
	}
	s.botUsername = me.Username
	slog.Info("telegram bot connected", "username", s.botUsername, "id", me.ID)

	updates, err := s.bot.UpdatesViaLongPolling(ctx, nil)
	if err != nil {
		return fmt.Errorf("updates long polling failed: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case u, ok := <-updates:
			if !ok {
				return nil
			}
			if err := s.HandleUpdate(ctx, u); err != nil {
				slog.Error("error handling update", "update_id", u.UpdateID, "error", err)
			}
		}
	}
}

// HandleUpdate processes a single Telegram update (message or callback query).
func (s *Service) HandleUpdate(ctx context.Context, u telego.Update) error {
	if u.Message != nil {
		return s.handleMessage(ctx, u.Message)
	}
	if u.CallbackQuery != nil {
		return s.handleCallback(ctx, u.CallbackQuery)
	}
	return nil
}

func (s *Service) handleMessage(ctx context.Context, msg *telego.Message) error {
	chatID := tu.ID(msg.Chat.ID)
	userID := msg.From.ID
	text := strings.TrimSpace(msg.Text)
	now := time.Now().UTC()

	// Check if message is contact sharing
	if msg.Contact != nil {
		return s.handleContact(ctx, msg)
	}

	// 1. Fetch or create user
	usr, err := s.store.Users().GetByID(ctx, userID)
	if err != nil && !errorsIsNotFound(err) {
		return err
	}

	// If user is configured Admin, ensure admin record exists
	if usr == nil && userID == s.cfg.AdminTelegramID {
		usr = user.New(userID, msg.From.Username, msg.From.FirstName, msg.From.LastName, user.RoleAdmin, now)
		_ = s.store.Users().Save(ctx, usr)
	}

	// 2. Handle /start command (may contain deep-link invite code)
	if strings.HasPrefix(text, "/start") {
		return s.handleStart(ctx, msg, usr, text, now)
	}

	// 3. Handle /admin command
	if text == "/admin" {
		if usr == nil || usr.Role != user.RoleAdmin {
			_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, "⛔ شما به این بخش دسترسی ندارید."))
			return nil
		}
		return s.sendAdminPortal(ctx, chatID)
	}

	// Unenrolled user attempting other commands
	if usr == nil {
		return s.sendAccessDenied(ctx, chatID)
	}

	// Default: show main menu
	return s.sendMainMenu(ctx, chatID, usr)
}

func (s *Service) handleStart(ctx context.Context, msg *telego.Message, usr *user.User, text string, now time.Time) error {
	chatID := tu.ID(msg.Chat.ID)
	userID := msg.From.ID

	// If user already enrolled:
	if usr != nil {
		return s.sendMainMenu(ctx, chatID, usr)
	}

	// New user attempting to enroll: check invite code
	rawCode := invite.CleanCode(text)
	if rawCode == "" {
		return s.sendAccessDenied(ctx, chatID)
	}

	// Validate and claim invite
	inv, err := s.store.Invites().Claim(ctx, rawCode, userID, now)
	if err != nil {
		errMsg := fmt.Sprintf("❌ <b>کد دعوت نامعتبر است</b>\n\nکد وارد شده معتبر نیست، منقضی شده یا ظرفیت آن تکمیل شده است.\n\nبرای دریافت دعوت‌نامه جدید با پشتیبانی تماس بگیرید: %s", s.cfg.SupportHandle)
		_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, errMsg).WithParseMode(telego.ModeHTML))
		return nil
	}

	// Successful claim: create user record
	newUser := user.New(userID, msg.From.Username, msg.From.FirstName, msg.From.LastName, user.RoleUser, now)
	newUser.InvitedBy = &inv.CreatedBy
	newUser.InviteCode = &inv.Code
	if err := s.store.Users().Save(ctx, newUser); err != nil {
		return err
	}

	welcomeText := fmt.Sprintf(
		"🎉 <b>به فروشگاه مانوا خوش آمدید!</b>\n\n" +
			"دعوت‌نامه شما با موفقیت تایید شد. هم‌اکنون می‌توانید سرورهای ابری Hetzner را با تحویل آنی و پرداخت ریالی خریداری فرمایید.",
	)
	_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, welcomeText).WithParseMode(telego.ModeHTML))

	return s.sendMainMenu(ctx, chatID, newUser)
}

func (s *Service) handleContact(ctx context.Context, msg *telego.Message) error {
	chatID := tu.ID(msg.Chat.ID)
	userID := msg.From.ID
	now := time.Now().UTC()

	usr, err := s.store.Users().GetByID(ctx, userID)
	if err != nil || usr == nil {
		return s.sendAccessDenied(ctx, chatID)
	}

	// Verification check: contact must belong to sender
	if msg.Contact.UserID != userID {
		_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, "❌ شماره‌ای که ارسال کردید متعلق به حساب شما نیست. لطفاً فقط شماره خودتان را به اشتراک بگذارید."))
		return nil
	}

	// Normalize Iranian number
	phoneE164, err := user.NormalizeIranianPhone(msg.Contact.PhoneNumber)
	if err != nil {
		_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, "❌ شماره ارسال‌شده معتبر نیست. فقط شماره‌های همراه ایران (+989) پذیرفته می‌شوند."))
		return nil
	}

	// Check if already used by someone else
	phoneHash := user.HashPhoneNumber(phoneE164, []byte(s.cfg.CredentialKEK.Expose()))
	existing, _ := s.store.Users().FindByPhoneHash(ctx, phoneHash)
	if existing != nil && existing.TelegramUserID != userID {
		_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, fmt.Sprintf("❌ این شماره قبلاً با حساب دیگری ثبت شده است. جهت بررسی با پشتیبانی %s تماس بگیرید.", s.cfg.SupportHandle)))
		return nil
	}

	_ = usr.SetVerifiedPhone(phoneE164, phoneHash, now)
	_ = s.store.Users().Save(ctx, usr)

	successText := fmt.Sprintf("✅ <b>شماره موبایل شما با موفقیت تایید شد:</b>\n<code>%s</code>\n\nاکنون می‌توانید کیف پول خود را شارژ کرده یا سرور سفارش دهید.", phoneE164)
	removeKeyboard := tu.ReplyKeyboardRemove()
	_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, successText).WithParseMode(telego.ModeHTML).WithReplyMarkup(removeKeyboard))

	return s.sendMainMenu(ctx, chatID, usr)
}

func (s *Service) handleCallback(ctx context.Context, cb *telego.CallbackQuery) error {
	_ = s.bot.AnswerCallbackQuery(ctx, tu.CallbackQuery(cb.ID))

	chatID := tu.ID(cb.Message.GetChat().ID)
	data := cb.Data
	userID := cb.From.ID
	now := time.Now().UTC()

	usr, err := s.store.Users().GetByID(ctx, userID)
	if err != nil || usr == nil {
		if userID == s.cfg.AdminTelegramID {
			usr = user.New(userID, cb.From.Username, cb.From.FirstName, cb.From.LastName, user.RoleAdmin, now)
			_ = s.store.Users().Save(ctx, usr)
		} else {
			return s.sendAccessDenied(ctx, chatID)
		}
	}

	switch {
	case data == "menu:main":
		return s.sendMainMenu(ctx, chatID, usr)
	case data == "menu:catalog":
		return s.sendCatalog(ctx, chatID)
	case data == "menu:wallet":
		return s.sendWallet(ctx, chatID, userID)
	case data == "menu:phone":
		return s.sendPhonePrompt(ctx, chatID)
	case data == "menu:support":
		return s.sendSupport(ctx, chatID)
	case data == "admin:home":
		if usr.Role != user.RoleAdmin {
			return nil
		}
		return s.sendAdminPortal(ctx, chatID)
	case data == "admin:invite:create":
		if usr.Role != user.RoleAdmin {
			return nil
		}
		return s.sendInviteCreationPrompt(ctx, chatID)
	case strings.HasPrefix(data, "admin:invite:gen:"):
		if usr.Role != user.RoleAdmin {
			return nil
		}
		parts := strings.Split(data, ":")
		uses, _ := strconv.Atoi(parts[3])
		return s.handleGenerateInvite(ctx, chatID, userID, uses)
	case strings.HasPrefix(data, "plan:view:"):
		itemID := strings.TrimPrefix(data, "plan:view:")
		return s.sendServerDetail(ctx, chatID, itemID)
	case data == "wallet:topup_info":
		return s.sendTopupInfo(ctx, chatID)
	}

	return nil
}

func (s *Service) sendMainMenu(ctx context.Context, chatID telego.ChatID, usr *user.User) error {
	text := fmt.Sprintf(
		"🌟 <b>منوی اصلی فروشگاه مانوا</b>\n\n"+
			"• شناسه کاربری: <code>%d</code>\n"+
			"• سطح کاربری: <b>%s</b>\n"+
			"• وضعیت شماره موبایل: %s\n\n"+
			"جهت خرید سرور ابری، شارژ حساب یا مدیریت خدمات از دکمه‌های زیر استفاده کنید:",
		usr.TelegramUserID,
		usr.Segment,
		phoneStatusLabel(usr.IsPhoneVerified()),
	)

	rows := [][]telego.InlineKeyboardButton{
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🛒 خرید سرور ابری Hetzner").WithCallbackData("menu:catalog"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("💰 کیف پول و موجودی").WithCallbackData("menu:wallet"),
			tu.InlineKeyboardRow(tu.InlineKeyboardButton("📱 تایید موبایل").WithCallbackData("menu:phone"))[0],
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("ℹ️ پشتیبانی و راهنما").WithCallbackData("menu:support"),
		),
	}

	if usr.Role == user.RoleAdmin {
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("⚙️ پنل مدیریت (Admin)").WithCallbackData("admin:home"),
		))
	}

	markup := tu.InlineKeyboard(rows...)
	_, err := s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) sendAdminPortal(ctx context.Context, chatID telego.ChatID) error {
	userCount, _ := s.store.Users().Count(ctx)
	text := fmt.Sprintf(
		"⚙️ <b>پنل مدیریت فروشگاه مانوا</b>\n\n"+
			"• کل کاربران ثبت‌نام شده: <code>%d</code>\n"+
			"• دارنده کارت بانکی: <code>%s</code>\n"+
			"• مارجین نرخ دلار: <code>%d تومان</code>\n\n"+
			"یکی از گزینه‌های مدیریتی زیر را انتخاب فرمایید:",
		userCount,
		s.cfg.CardHolderName,
		s.cfg.RateSpreadIRTPerUSD,
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🎫 ایجاد دعوت‌نامه جدید").WithCallbackData("admin:invite:create"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« بازگشت به منوی اصلی").WithCallbackData("menu:main"),
		),
	)

	_, err := s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) sendInviteCreationPrompt(ctx context.Context, chatID telego.ChatID) error {
	text := "🎟️ <b>ایجاد دعوت‌نامه جدید ورود به ربات</b>\n\n" +
		"تعداد کاربر مجاز برای این لینک دعوت را انتخاب کنید:"

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("۱ کاربره (یک‌بار مصرف)").WithCallbackData("admin:invite:gen:1"),
			tu.InlineKeyboardButton("۵ کاربره").WithCallbackData("admin:invite:gen:5"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("نامحدود (عمومی)").WithCallbackData("admin:invite:gen:0"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« انصراف و بازگشت").WithCallbackData("admin:home"),
		),
	)

	_, err := s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) handleGenerateInvite(ctx context.Context, chatID telego.ChatID, adminID int64, uses int) error {
	now := time.Now().UTC()
	inv := invite.New(adminID, uses, nil, now)
	if err := s.store.Invites().Create(ctx, inv); err != nil {
		return err
	}

	botUser := s.botUsername
	if botUser == "" {
		botUser = "manova_shop_bot"
	}
	botUser = strings.TrimPrefix(botUser, "@")

	deepLink := fmt.Sprintf("https://t.me/%s?start=invite_%s", botUser, inv.Code)
	shareText := "دعوت‌نامه ورود به ربات فروشگاه ابری مانوا 🚀"
	shareURL := fmt.Sprintf("https://t.me/share/url?url=%s&text=%s", url.QueryEscape(deepLink), url.QueryEscape(shareText))

	usesDesc := "نامحدود"
	if uses > 0 {
		usesDesc = fmt.Sprintf("%d کاربر", uses)
	}

	cardText := fmt.Sprintf(
		"🎫 <b>دعوت‌نامه جدید صادر شد!</b>\n\n"+
			"• <b>کد دعوت:</b> <code>%s</code>\n"+
			"• <b>تعداد مجاز:</b> <code>%s</code>\n"+
			"• <b>وضعیت:</b> 🟢 فعال\n\n"+
			"🔗 <b>لینک ورود مستقیم:</b>\n"+
			"<blockquote><code>%s</code></blockquote>\n\n"+
			"💡 <i>می‌توانید با زدن دکمه «ارسال برای دوستان»، این کارت را مستقیماً همراه دکمه ورود برای کاربران بفرستید.</i>",
		inv.Code,
		usesDesc,
		deepLink,
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("↗️ ورود به ربات (Join Bot)").WithURL(deepLink),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("↗️ ارسال برای دوستان (Share)").WithURL(shareURL),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« بازگشت به پنل مدیریت").WithCallbackData("admin:home"),
		),
	)

	_, err := s.bot.SendMessage(ctx, tu.Message(chatID, cardText).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) sendCatalog(ctx context.Context, chatID telego.ChatID) error {
	items, err := s.store.Catalog().ListActiveItems(ctx)
	if err != nil {
		return err
	}

	obs, _ := s.store.FxRates().FetchRate(ctx)
	sellRate := pricing.CalculateSellRate(obs.MarketIRTPerUSD, pricing.IRT(s.cfg.RateSpreadIRTPerUSD))

	text := fmt.Sprintf(
		"🛒 <b>سرورهای ابری Hetzner Cloud (تحویل آنی)</b>\n\n"+
			"نرخ محاسبه دلار امروز: <code>%s تومان</code>\n"+
			"تمامی سرورها شامل ترافیک ۲۰ ترابایت رایگان، پردازنده‌های پرسرعت AMD/Intel و هارد NVMe می‌باشند.\n\n"+
			"پلن مورد نظر خود را انتخاب کنید:",
		formatNumber(int64(sellRate)),
	)

	var rows [][]telego.InlineKeyboardButton
	for _, it := range items {
		hourlyIRT := pricing.CalculatePriceIRT(it.HourlyUSDMicro, sellRate)
		monthlyIRT := pricing.CalculatePriceIRT(it.MonthlyCapUSD, sellRate)
		label := fmt.Sprintf("%s (%d vCPU / %d GB RAM) — %s ت/ماه",
			it.Name, it.VCPU, it.RAMGB, formatNumber(int64(monthlyIRT)))
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(label).WithCallbackData("plan:view:"+it.ID),
		))
		_ = hourlyIRT
	}
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("« بازگشت به منوی اصلی").WithCallbackData("menu:main"),
	))

	markup := tu.InlineKeyboard(rows...)
	_, err = s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) sendServerDetail(ctx context.Context, chatID telego.ChatID, itemID string) error {
	it, err := s.store.Catalog().GetItem(ctx, itemID)
	if err != nil {
		return err
	}

	obs, _ := s.store.FxRates().FetchRate(ctx)
	sellRate := pricing.CalculateSellRate(obs.MarketIRTPerUSD, pricing.IRT(s.cfg.RateSpreadIRTPerUSD))
	hourlyIRT := pricing.CalculatePriceIRT(it.HourlyUSDMicro, sellRate)
	monthlyIRT := pricing.CalculatePriceIRT(it.MonthlyCapUSD, sellRate)

	text := fmt.Sprintf(
		"🖥️ <b>مشخصات سرور %s</b>\n\n"+
			"• <b>پردازنده:</b> <code>%d vCPU</code>\n"+
			"• <b>رم:</b> <code>%d GB RAM</code>\n"+
			"• <b>دیسک:</b> <code>%d GB NVMe SSD</code>\n"+
			"• <b>ترافیک:</b> <code>20 TB Included</code>\n\n"+
			"💰 <b>هزینه مصرف:</b>\n"+
			"• ساعتی: <code>%s تومان</code> (دقیقه‌ای %s ت)\n"+
			"• سقف ماهیانه: <code>%s تومان</code>\n\n"+
			"⚠️ <i>طبق قوانین هتزنر، محاسبه هزینه بر مبنای دقایق وجود سرور است و با خاموش بودن سرور متوقف نمی‌شود.</i>",
		it.Name, it.VCPU, it.RAMGB, it.DiskGB,
		formatNumber(int64(hourlyIRT)), formatNumber(int64(hourlyIRT/60)),
		formatNumber(int64(monthlyIRT)),
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« بازگشت به لیست پلن‌ها").WithCallbackData("menu:catalog"),
		),
	)

	_, err = s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) sendWallet(ctx context.Context, chatID telego.ChatID, userID int64) error {
	w, err := s.store.Wallets().GetByUserID(ctx, userID)
	if err != nil {
		return err
	}

	obs, _ := s.store.FxRates().FetchRate(ctx)
	sellRate := pricing.CalculateSellRate(obs.MarketIRTPerUSD, pricing.IRT(s.cfg.RateSpreadIRTPerUSD))
	combined := w.CombinedValuation(sellRate)

	text := fmt.Sprintf(
		"💰 <b>کیف پول شما</b>\n\n"+
			"• موجودی تومانی: <code>%s تومان</code>\n"+
			"• موجودی دلاری: <code>%0.2f USD</code>\n"+
			"• ارزش کل موجودی: <code>%s تومان</code>\n\n"+
			"• نرخ تبدیل دلار امروز: <code>%s تومان</code>",
		formatNumber(int64(w.BalanceIRT)),
		float64(w.BalanceUSDMicro)/1_000_000.0,
		formatNumber(int64(combined)),
		formatNumber(int64(sellRate)),
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("💳 شارژ موجودی با کارت بانکی").WithCallbackData("wallet:topup_info"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« بازگشت به منوی اصلی").WithCallbackData("menu:main"),
		),
	)

	_, err = s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) sendTopupInfo(ctx context.Context, chatID telego.ChatID) error {
	text := fmt.Sprintf(
		"💳 <b>اطلاعات پرداخت جهت شارژ حساب</b>\n\n"+
			"مبلغ مورد نظر را به شماره کارت زیر واریز فرمایید:\n\n"+
			"• شماره کارت: <code>1234123412341234</code>\n"+
			"• بنام: <b>%s</b>\n\n"+
			"پس از واریز، تصویر رسید یا کد رهگیری را برای پشتیبانی ارسال فرمایید تا کیف پول شما در لحظه شارژ شود.\n\n"+
			"ارتباط با پشتیبانی: %s",
		s.cfg.CardHolderName,
		s.cfg.SupportHandle,
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« بازگشت به کیف پول").WithCallbackData("menu:wallet"),
		),
	)

	_, err := s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) sendPhonePrompt(ctx context.Context, chatID telego.ChatID) error {
	text := "📱 <b>احراز شماره موبایل</b>\n\n" +
		"طبق ضوابط امنیتی و تایید هویت، برای خرید سرور یا شارژ حساب اشتراک شماره موبایل ضروری است.\n\n" +
		"لطفاً روی دکمه زیر کلیک کنید تا شماره سیم‌کارت ثبت شده روی تلگرام شما تایید گردد:"

	contactBtn := tu.KeyboardButton("📱 ارسال شماره موبایل من").WithRequestContact()
	markup := tu.Keyboard(
		tu.KeyboardRow(contactBtn),
	).WithResizeKeyboard()

	_, err := s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) sendSupport(ctx context.Context, chatID telego.ChatID) error {
	text := fmt.Sprintf(
		"ℹ️ <b>پشتیبانی و راهنمایی</b>\n\n"+
			"در صورت بروز هرگونه سوال، نیاز به مشاوره یا گزارش مشکل، می‌توانید با پشتیبانی رسمی در ارتباط باشید:\n\n"+
			"👤 پشتیبانی: %s",
		s.cfg.SupportHandle,
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« بازگشت").WithCallbackData("menu:main"),
		),
	)

	_, err := s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) sendAccessDenied(ctx context.Context, chatID telego.ChatID) error {
	text := fmt.Sprintf(
		"🔒 <b>ورود اختصاصی با دعوت‌نامه</b>\n\n"+
			"دسترسی به خدمات فروشگاه مانوا در فاز خصوصی فقط با داشتن لینک دعوت امکان‌پذیر است.\n\n"+
			"اگر لینک دعوت دارید، لطفاً روی لینک خود کلیک کرده و دکمه Start را بزنید.\n"+
			"پشتیبانی: %s",
		s.cfg.SupportHandle,
	)
	_, err := s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML))
	return err
}

func phoneStatusLabel(verified bool) string {
	if verified {
		return "🟢 تایید شده"
	}
	return "🔴 تایید نشده (نیاز به احراز)"
}

func formatNumber(n int64) string {
	in := strconv.FormatInt(n, 10)
	var out []byte
	l := len(in)
	for i, c := range in {
		if i > 0 && (l-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out)
}

func errorsIsNotFound(err error) bool {
	return err == memory.ErrNotFound
}
