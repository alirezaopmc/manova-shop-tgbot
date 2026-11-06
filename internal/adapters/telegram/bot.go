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
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/fee"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/invite"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/receipt"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/wallet"
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

// HandleUpdate processes a single Telegram update (message, callback query, or inline query).
func (s *Service) HandleUpdate(ctx context.Context, u telego.Update) error {
	if u.Message != nil {
		return s.handleMessage(ctx, u.Message)
	}
	if u.CallbackQuery != nil {
		return s.handleCallback(ctx, u.CallbackQuery)
	}
	if u.InlineQuery != nil {
		return s.handleInlineQuery(ctx, u.InlineQuery)
	}
	return nil
}

func (s *Service) handleMessage(ctx context.Context, msg *telego.Message) error {
	chatID := tu.ID(msg.Chat.ID)
	userID := msg.From.ID
	text := strings.TrimSpace(msg.Text)
	now := time.Now().UTC()

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

	// Reject blocked users
	if usr != nil && usr.Blocked {
		_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, fmt.Sprintf("⛔ حساب کاربری شما مسدود است. لطفاً با پشتیبانی در ارتباط باشید: %s", s.cfg.SupportHandle)))
		return nil
	}

	// Check if message is contact sharing
	if msg.Contact != nil {
		return s.handleContact(ctx, msg)
	}

	// Check if message is a payment receipt photo or document upload
	if len(msg.Photo) > 0 || msg.Document != nil {
		return s.handleReceiptUpload(ctx, msg, usr)
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

	// 4. Handle /wallet command (admin only)
	if strings.HasPrefix(text, "/wallet") {
		if usr == nil || usr.Role != user.RoleAdmin {
			_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, "⛔ شما به این بخش دسترسی ندارید."))
			return nil
		}
		return s.handleAdminWalletCommand(ctx, chatID, text)
	}

	// 5. Handle /user command (admin only)
	if strings.HasPrefix(text, "/user") {
		if usr == nil || usr.Role != user.RoleAdmin {
			_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, "⛔ شما به این بخش دسترسی ندارید."))
			return nil
		}
		return s.handleAdminUserCommand(ctx, chatID, text)
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
		if usr.Blocked {
			_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, fmt.Sprintf("⛔ حساب کاربری شما مسدود است. لطفاً با پشتیبانی در ارتباط باشید: %s", s.cfg.SupportHandle)))
			return nil
		}
		return s.sendMainMenu(ctx, chatID, usr)
	}

	// New user attempting to enroll: check invite code
	rawCode := invite.CleanCode(text)
	if rawCode == "" {
		return s.sendAccessDenied(ctx, chatID)
	}

	// Validate and claim invite (strict verification)
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

	welcomeText := "🎉 <b>به فروشگاه مانوا خوش آمدید!</b>"
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

	if usr.Blocked {
		_ = s.bot.AnswerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("⛔ حساب کاربری شما مسدود است.").WithShowAlert())
		return nil
	}

	switch {
	case data == "menu:main":
		return s.sendMainMenu(ctx, chatID, usr)
	case data == "menu:settings":
		return s.sendUserSettings(ctx, chatID, usr)
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
	case data == "admin:users":
		if usr.Role != user.RoleAdmin {
			return nil
		}
		return s.sendAdminUsersList(ctx, chatID)
	case strings.HasPrefix(data, "admin:uview:"):
		if usr.Role != user.RoleAdmin {
			return nil
		}
		targetUID, _ := strconv.ParseInt(strings.TrimPrefix(data, "admin:uview:"), 10, 64)
		return s.sendAdminUserDetailView(ctx, chatID, targetUID)
	case strings.HasPrefix(data, "admin:uset:seg:"):
		if usr.Role != user.RoleAdmin {
			return nil
		}
		targetUID, _ := strconv.ParseInt(strings.TrimPrefix(data, "admin:uset:seg:"), 10, 64)
		return s.sendAdminUserSegmentMenu(ctx, chatID, targetUID)
	case strings.HasPrefix(data, "admin:uset:set_seg:"):
		if usr.Role != user.RoleAdmin {
			return nil
		}
		parts := strings.Split(data, ":")
		if len(parts) >= 5 {
			targetUID, _ := strconv.ParseInt(parts[3], 10, 64)
			seg := parts[4]
			return s.handleAdminUserSetSegment(ctx, cb, chatID, targetUID, seg)
		}
	case strings.HasPrefix(data, "admin:uset:block:"):
		if usr.Role != user.RoleAdmin {
			return nil
		}
		parts := strings.Split(data, ":")
		if len(parts) >= 5 {
			targetUID, _ := strconv.ParseInt(parts[3], 10, 64)
			blocked := parts[4] == "1"
			return s.handleAdminUserSetBlock(ctx, cb, chatID, targetUID, blocked)
		}
	case strings.HasPrefix(data, "admin:uset:role:"):
		if usr.Role != user.RoleAdmin {
			return nil
		}
		parts := strings.Split(data, ":")
		if len(parts) >= 5 {
			targetUID, _ := strconv.ParseInt(parts[3], 10, 64)
			role := parts[4]
			return s.handleAdminUserSetRole(ctx, cb, chatID, targetUID, role)
		}
	case strings.HasPrefix(data, "admin:uset:reset_phone:"):
		if usr.Role != user.RoleAdmin {
			return nil
		}
		targetUID, _ := strconv.ParseInt(strings.TrimPrefix(data, "admin:uset:reset_phone:"), 10, 64)
		return s.handleAdminUserResetPhone(ctx, cb, chatID, targetUID)
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
	case data == "admin:wallets":
		if usr.Role != user.RoleAdmin {
			return nil
		}
		return s.sendAdminWalletsList(ctx, chatID)
	case strings.HasPrefix(data, "admin:wview:"):
		if usr.Role != user.RoleAdmin {
			return nil
		}
		targetUID, _ := strconv.ParseInt(strings.TrimPrefix(data, "admin:wview:"), 10, 64)
		return s.sendAdminUserWalletView(ctx, chatID, targetUID)
	case strings.HasPrefix(data, "admin:wadj:add_menu:"):
		if usr.Role != user.RoleAdmin {
			return nil
		}
		targetUID, _ := strconv.ParseInt(strings.TrimPrefix(data, "admin:wadj:add_menu:"), 10, 64)
		return s.sendAdminWalletAdjustMenu(ctx, chatID, targetUID, "add")
	case strings.HasPrefix(data, "admin:wadj:sub_menu:"):
		if usr.Role != user.RoleAdmin {
			return nil
		}
		targetUID, _ := strconv.ParseInt(strings.TrimPrefix(data, "admin:wadj:sub_menu:"), 10, 64)
		return s.sendAdminWalletAdjustMenu(ctx, chatID, targetUID, "sub")
	case strings.HasPrefix(data, "admin:wadj:zero:"):
		if usr.Role != user.RoleAdmin {
			return nil
		}
		targetUID, _ := strconv.ParseInt(strings.TrimPrefix(data, "admin:wadj:zero:"), 10, 64)
		return s.handleAdminWalletAdjustExec(ctx, cb, targetUID, "zero", 0)
	case strings.HasPrefix(data, "admin:wadj:exec:"):
		if usr.Role != user.RoleAdmin {
			return nil
		}
		parts := strings.Split(data, ":")
		if len(parts) >= 6 {
			targetUID, _ := strconv.ParseInt(parts[3], 10, 64)
			op := parts[4]
			amount, _ := strconv.ParseInt(parts[5], 10, 64)
			return s.handleAdminWalletAdjustExec(ctx, cb, targetUID, op, amount)
		}
	case strings.HasPrefix(data, "admin:rcpt:app:"):
		if usr.Role != user.RoleAdmin {
			return nil
		}
		parts := strings.Split(data, ":")
		if len(parts) >= 5 {
			rcptID := parts[3]
			amount, _ := strconv.ParseInt(parts[4], 10, 64)
			return s.handleReceiptApproval(ctx, cb, rcptID, amount)
		}
	case strings.HasPrefix(data, "admin:rcpt:rej:"):
		if usr.Role != user.RoleAdmin {
			return nil
		}
		rcptID := strings.TrimPrefix(data, "admin:rcpt:rej:")
		return s.handleReceiptRejection(ctx, cb, rcptID)
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
			tu.InlineKeyboardButton("⚙️ مشخصات و تنظیمات").WithCallbackData("menu:settings"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("📱 تایید موبایل").WithCallbackData("menu:phone"),
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
			tu.InlineKeyboardButton("👥 مدیریت کاربران و تنظیمات").WithCallbackData("admin:users"),
			tu.InlineKeyboardButton("💳 مدیریت کیف پول‌ها").WithCallbackData("admin:wallets"),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🎫 ایجاد دعوت‌نامه جدید").WithCallbackData("admin:invite:create"),
			tu.InlineKeyboardButton("📤 ارسال دعوت اینلاین به چت‌ها").WithSwitchInlineQuery(""),
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
		"🎫 <b>دعوت‌نامه فروشگاه مانوا</b>\n\n"+
			"• <b>ظرفیت:</b> %s\n"+
			"• <b>لینک ورود مستقیم:</b>\n"+
			"<code>%s</code>",
		usesDesc,
		deepLink,
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🛍️ ورود به فروشگاه").WithURL(deepLink),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("📤 ارسال اینلاین به چت‌ها").WithSwitchInlineQuery(""),
			tu.InlineKeyboardButton("↗️ اشتراک سریع").WithURL(shareURL),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« بازگشت").WithCallbackData("admin:home"),
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
		"💳 <b>اطلاعات پرداخت جهت شارژ کیف پول</b>\n\n"+
			"مبلغ مورد نظر خود را به شماره کارت زیر واریز فرمایید:\n\n"+
			"• <b>شماره کارت:</b> <code>6037997512345678</code>\n"+
			"• <b>بنام:</b> <b>%s</b>\n\n"+
			"📸 <b>ارسال تصویر فیش (اسکرین‌شات):</b>\n"+
			"پس از واریز، کافیست <b>تصویر فیش یا اسکرین‌شات رسید</b> را مستقیماً همین‌جا در ربات ارسال فرمایید.\n"+
			"رسید شما به صورت خودکار برای مدیریت ارسال و پس از تایید، کیف پول شما شارژ خواهد شد.\n\n"+
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

func (s *Service) handleInlineQuery(ctx context.Context, q *telego.InlineQuery) error {
	senderID := q.From.ID
	now := time.Now().UTC()

	isAdmin := senderID == s.cfg.AdminTelegramID
	if !isAdmin {
		usr, _ := s.store.Users().GetByID(ctx, senderID)
		if usr != nil && usr.Role == user.RoleAdmin {
			isAdmin = true
		}
	}

	if !isAdmin {
		article := (&telego.InlineQueryResultArticle{
			Type:        "article",
			ID:          "access_denied",
			Title:       "⛔ دسترسی غیرمجاز",
			Description: "تنها مدیریت مجاز به صدور کارت دعوت است.",
			InputMessageContent: (&telego.InputTextMessageContent{
				MessageText: "⛔ شما دسترسی لازم برای صدور کارت دعوت را ندارید.",
			}),
		})
		return s.bot.AnswerInlineQuery(ctx, &telego.AnswerInlineQueryParams{
			InlineQueryID: q.ID,
			Results:       []telego.InlineQueryResult{article},
			CacheTime:     0,
			IsPersonal:    true,
		})
	}

	botUser := s.botUsername
	if botUser == "" {
		botUser = "shop_manovabot"
	}
	botUser = strings.TrimPrefix(botUser, "@")

	var results []telego.InlineQueryResult

	// 1. Single-use invitation (Default, strict security: 1 user only)
	inv1 := invite.New(senderID, 1, nil, now)
	if err := s.store.Invites().Create(ctx, inv1); err == nil {
		deepLink1 := fmt.Sprintf("https://t.me/%s?start=invite_%s", botUser, inv1.Code)
		text1 := "🛍️ <b>فروشگاه مانوا | Manova Shop</b>\n\n" +
			"دعوت‌نامه اختصاصی ورود به فروشگاه سرورهای ابری"
		markup1 := tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("🛍️ ورود به فروشگاه").WithURL(deepLink1),
			),
		)
		results = append(results, &telego.InlineQueryResultArticle{
			Type:        "article",
			ID:          fmt.Sprintf("inv_1_%s", inv1.Code),
			Title:       "🎫 کارت دعوت یک‌بار مصرف (تک‌کاربره)",
			Description: fmt.Sprintf("کد اختصاصی: %s (ظرفیت ۱ نفر)", inv1.Code),
			InputMessageContent: &telego.InputTextMessageContent{
				MessageText: text1,
				ParseMode:   telego.ModeHTML,
			},
			ReplyMarkup: markup1,
		})
	}

	// 2. 5-use invitation (For inviting small team)
	inv5 := invite.New(senderID, 5, nil, now)
	if err := s.store.Invites().Create(ctx, inv5); err == nil {
		deepLink5 := fmt.Sprintf("https://t.me/%s?start=invite_%s", botUser, inv5.Code)
		text5 := "🛍️ <b>فروشگاه مانوا | Manova Shop</b>\n\n" +
			"دعوت‌نامه اختصاصی ورود به فروشگاه سرورهای ابری"
		markup5 := tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("🛍️ ورود به فروشگاه").WithURL(deepLink5),
			),
		)
		results = append(results, &telego.InlineQueryResultArticle{
			Type:        "article",
			ID:          fmt.Sprintf("inv_5_%s", inv5.Code),
			Title:       "🎫 کارت دعوت ۵ کاربره",
			Description: fmt.Sprintf("کد اختصاصی: %s (ظرفیت ۵ نفر)", inv5.Code),
			InputMessageContent: &telego.InputTextMessageContent{
				MessageText: text5,
				ParseMode:   telego.ModeHTML,
			},
			ReplyMarkup: markup5,
		})
	}

	return s.bot.AnswerInlineQuery(ctx, &telego.AnswerInlineQueryParams{
		InlineQueryID: q.ID,
		Results:       results,
		CacheTime:     0,
		IsPersonal:    true,
	})
}

func (s *Service) handleReceiptUpload(ctx context.Context, msg *telego.Message, usr *user.User) error {
	chatID := tu.ID(msg.Chat.ID)
	userID := msg.From.ID

	if usr == nil {
		return s.sendAccessDenied(ctx, chatID)
	}

	var fileID string
	if len(msg.Photo) > 0 {
		fileID = msg.Photo[len(msg.Photo)-1].FileID
	} else if msg.Document != nil {
		fileID = msg.Document.FileID
	}

	if fileID == "" {
		_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, "❌ فایل ارسالی معتبر نیست."))
		return nil
	}

	rcptID := fmt.Sprintf("rcpt-%d", time.Now().UnixNano()/1e6)
	now := time.Now().UTC()

	rcp := &receipt.Receipt{
		ID:        rcptID,
		UserID:    userID,
		Purpose:   receipt.PurposeWalletTopup,
		ImageRef:  fileID,
		Status:    receipt.StatusPendingReview,
		CreatedAt: now,
	}

	_ = s.store.Receipts().Save(ctx, rcp)

	// Acknowledge user
	ackText := fmt.Sprintf(
		"✅ <b>رسید پرداخت شما دریافت شد!</b>\n\n"+
			"• <b>شناسه پیگیری:</b> <code>%s</code>\n"+
			"• <b>وضعیت:</b> در انتظار بررسی مدیریت ⏳\n\n"+
			"رسید شما جهت بررسی و تایید برای مدیریت ارسال گردید. به محض تایید، کیف پول شما به صورت خودکار شارژ خواهد شد.",
		rcptID,
	)
	_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, ackText).WithParseMode(telego.ModeHTML))

	// Forward to Admin
	if s.cfg.AdminTelegramID != 0 {
		adminChatID := tu.ID(s.cfg.AdminTelegramID)
		caption := fmt.Sprintf(
			"🧾 <b>رسید پرداخت جدید برای شارژ کیف پول</b>\n\n"+
				"• <b>کاربر:</b> <code>%d</code> (@%s)\n"+
				"• <b>نام:</b> %s %s\n"+
				"• <b>شناسه رسید:</b> <code>%s</code>\n"+
				"• <b>زمان:</b> <code>%s</code>\n\n"+
				"مبلغ واریزی را جهت تایید و شارژ خودکار انتخاب فرمایید:",
			userID,
			msg.From.Username,
			msg.From.FirstName,
			msg.From.LastName,
			rcptID,
			now.Format("2006-01-02 15:04:05 UTC"),
		)
		if msg.Caption != "" {
			caption += fmt.Sprintf("\n• <b>توضیحات کاربر:</b> <i>%s</i>\n", msg.Caption)
		}

		adminMarkup := tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("💰 ۱۰۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:rcpt:app:%s:100000", rcptID)),
				tu.InlineKeyboardButton("💰 ۲۵۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:rcpt:app:%s:250000", rcptID)),
			),
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("💰 ۵۰۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:rcpt:app:%s:500000", rcptID)),
				tu.InlineKeyboardButton("💰 ۱,۰۰۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:rcpt:app:%s:1000000", rcptID)),
			),
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("💰 ۲,۰۰۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:rcpt:app:%s:2000000", rcptID)),
				tu.InlineKeyboardButton("💰 ۵,۰۰۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:rcpt:app:%s:5000000", rcptID)),
			),
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("❌ رد رسید (Reject)").WithCallbackData(fmt.Sprintf("admin:rcpt:rej:%s", rcptID)),
			),
		)

		if len(msg.Photo) > 0 {
			_, _ = s.bot.SendPhoto(ctx, tu.Photo(adminChatID, tu.FileFromID(fileID)).
				WithCaption(caption).
				WithParseMode(telego.ModeHTML).
				WithReplyMarkup(adminMarkup))
		} else {
			_, _ = s.bot.SendMessage(ctx, tu.Message(adminChatID, caption).
				WithParseMode(telego.ModeHTML).
				WithReplyMarkup(adminMarkup))
		}
	}

	return nil
}

func (s *Service) handleReceiptApproval(ctx context.Context, cb *telego.CallbackQuery, rcptID string, amountIRT int64) error {
	adminID := cb.From.ID
	now := time.Now().UTC()

	rcp, err := s.store.Receipts().GetByID(ctx, rcptID)
	if err != nil || rcp == nil {
		_ = s.bot.AnswerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("❌ رسید یافت نشد."))
		return nil
	}

	if rcp.Status == receipt.StatusApproved || rcp.Status == receipt.StatusRejected {
		_ = s.bot.AnswerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("⚠️ این رسید قبلاً تعیین وضعیت شده است."))
		return nil
	}

	if err := rcp.Approve(adminID, now); err != nil {
		return err
	}
	rcp.AmountIRT = pricing.IRT(amountIRT)
	_ = s.store.Receipts().Save(ctx, rcp)

	// Credit user's wallet
	w, err := s.store.Wallets().GetByUserID(ctx, rcp.UserID)
	if err != nil {
		return err
	}
	w.BalanceIRT += pricing.IRT(amountIRT)
	if err := s.store.Wallets().UpdateBalances(ctx, w); err != nil {
		return err
	}

	_ = s.store.Wallets().RecordLedgerEntry(ctx, &wallet.LedgerEntry{
		ID:           fmt.Sprintf("ledg-rcpt-%d", time.Now().UnixNano()),
		WalletID:     w.ID,
		Currency:     wallet.CurrencyIRT,
		Amount:       amountIRT,
		Kind:         wallet.KindTopupReceipt,
		BalanceAfter: int64(w.BalanceIRT),
		CreatedAt:    now,
	})

	// 1. Notify User
	userNotice := fmt.Sprintf(
		"🎉 <b>رسید پرداخت شما تایید شد!</b>\n\n"+
			"• <b>مبلغ شارژ شده:</b> <code>%s تومان</code>\n"+
			"• <b>شناسه رسید:</b> <code>%s</code>\n"+
			"• <b>موجودی جدید کیف پول:</b> <code>%s تومان</code>\n\n"+
			"هم‌اکنون می‌توانید سرور ابری مورد نظر خود را خریداری فرمایید.",
		formatNumber(amountIRT),
		rcp.ID,
		formatNumber(int64(w.BalanceIRT)),
	)
	_, _ = s.bot.SendMessage(ctx, tu.Message(tu.ID(rcp.UserID), userNotice).WithParseMode(telego.ModeHTML))

	// 2. Update Admin message
	adminConfirm := fmt.Sprintf(
		"✅ <b>رسید %s تایید شد</b>\n\n"+
			"• مبلغ: <code>%s تومان</code>\n"+
			"• به کیف پول کاربر <code>%d</code> افزوده شد.\n"+
			"• موجودی فعلی: <code>%s تومان</code>\n"+
			"• زمان تایید: %s",
		rcptID,
		formatNumber(amountIRT),
		rcp.UserID,
		formatNumber(int64(w.BalanceIRT)),
		now.Format("15:04:05 UTC"),
	)
	if cb.Message != nil {
		chatID := tu.ID(cb.Message.GetChat().ID)
		msgID := cb.Message.GetMessageID()
		if m := cb.Message.Message(); m != nil && len(m.Photo) > 0 {
			_, _ = s.bot.EditMessageCaption(ctx, tu.EditMessageCaption(chatID, msgID, adminConfirm).WithParseMode(telego.ModeHTML))
		} else {
			_, _ = s.bot.EditMessageText(ctx, tu.EditMessageText(chatID, msgID, adminConfirm).WithParseMode(telego.ModeHTML))
		}
	}

	_ = s.bot.AnswerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText(fmt.Sprintf("رسید تایید و %s تومان شارژ شد.", formatNumber(amountIRT))))
	return nil
}

func (s *Service) handleReceiptRejection(ctx context.Context, cb *telego.CallbackQuery, rcptID string) error {
	adminID := cb.From.ID
	now := time.Now().UTC()

	rcp, err := s.store.Receipts().GetByID(ctx, rcptID)
	if err != nil || rcp == nil {
		_ = s.bot.AnswerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("❌ رسید یافت نشد."))
		return nil
	}

	if rcp.Status == receipt.StatusApproved || rcp.Status == receipt.StatusRejected {
		_ = s.bot.AnswerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("⚠️ این رسید قبلاً تعیین وضعیت شده است."))
		return nil
	}

	if err := rcp.Reject(adminID, receipt.RejectOther, "Rejected by admin", now); err != nil {
		return err
	}
	_ = s.store.Receipts().Save(ctx, rcp)

	// Notify User
	userNotice := fmt.Sprintf(
		"❌ <b>رسید پرداخت شما تایید نشد</b>\n\n"+
			"• <b>شناسه رسید:</b> <code>%s</code>\n\n"+
			"رسید ارسالی توسط مدیریت تایید نگردید. در صورت کسر وجه از حسابتان یا نیاز به پیگیری، با پشتیبانی (%s) در ارتباط باشید.",
		rcp.ID,
		s.cfg.SupportHandle,
	)
	_, _ = s.bot.SendMessage(ctx, tu.Message(tu.ID(rcp.UserID), userNotice).WithParseMode(telego.ModeHTML))

	// Update Admin message
	adminReject := fmt.Sprintf(
		"❌ <b>رسید %s رد شد</b>\nکاربر: <code>%d</code>",
		rcptID,
		rcp.UserID,
	)
	if cb.Message != nil {
		chatID := tu.ID(cb.Message.GetChat().ID)
		msgID := cb.Message.GetMessageID()
		if m := cb.Message.Message(); m != nil && len(m.Photo) > 0 {
			_, _ = s.bot.EditMessageCaption(ctx, tu.EditMessageCaption(chatID, msgID, adminReject).WithParseMode(telego.ModeHTML))
		} else {
			_, _ = s.bot.EditMessageText(ctx, tu.EditMessageText(chatID, msgID, adminReject).WithParseMode(telego.ModeHTML))
		}
	}

	_ = s.bot.AnswerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("رسید رد شد."))
	return nil
}

func (s *Service) sendAdminWalletsList(ctx context.Context, chatID telego.ChatID) error {
	users, err := s.store.Users().ListUsers(ctx, 25)
	if err != nil {
		return err
	}

	text := "💳 <b>مدیریت کیف پول کاربران</b>\n\n" +
		"جهت مشاهده موجودی، شارژ یا کسر از کیف پول، کاربر مورد نظر را انتخاب فرمایید:\n" +
		"<i>(همچنین می‌توانید دستور <code>/wallet [شناسه_کاربر]</code> را ارسال کنید)</i>"

	var rows [][]telego.InlineKeyboardButton
	for _, u := range users {
		w, _ := s.store.Wallets().GetByUserID(ctx, u.TelegramUserID)
		var balIRT int64
		if w != nil {
			balIRT = int64(w.BalanceIRT)
		}
		displayName := u.FirstName
		if u.Username != "" {
			displayName = "@" + u.Username
		}
		label := fmt.Sprintf("%s (%d) — %s ت", displayName, u.TelegramUserID, formatNumber(balIRT))
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(label).WithCallbackData(fmt.Sprintf("admin:wview:%d", u.TelegramUserID)),
		))
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("« بازگشت به پنل مدیریت").WithCallbackData("admin:home"),
	))

	markup := tu.InlineKeyboard(rows...)
	_, err = s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) sendAdminUserWalletView(ctx context.Context, chatID telego.ChatID, targetUserID int64) error {
	u, err := s.store.Users().GetByID(ctx, targetUserID)
	if err != nil || u == nil {
		_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, fmt.Sprintf("❌ کاربری با شناسه <code>%d</code> یافت نشد.", targetUserID)).WithParseMode(telego.ModeHTML))
		return nil
	}

	w, err := s.store.Wallets().GetByUserID(ctx, targetUserID)
	if err != nil {
		return err
	}

	text := fmt.Sprintf(
		"👤 <b>مدیریت کیف پول کاربر</b>\n\n"+
			"• <b>نام:</b> %s %s\n"+
			"• <b>نام کاربری:</b> @%s\n"+
			"• <b>شناسه تلگرام:</b> <code>%d</code>\n"+
			"• <b>سطح کاربری:</b> %s\n\n"+
			"💰 <b>موجودی فعلی:</b>\n"+
			"• تومانی: <code>%s تومان</code>\n"+
			"• دلاری: <code>%0.2f USD</code>\n\n"+
			"عملیات مورد نظر را انتخاب فرمایید:",
		u.FirstName, u.LastName,
		u.Username,
		u.TelegramUserID,
		u.Segment,
		formatNumber(int64(w.BalanceIRT)),
		float64(w.BalanceUSDMicro)/1_000_000.0,
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("➕ شارژ کیف پول (افزایش)").WithCallbackData(fmt.Sprintf("admin:wadj:add_menu:%d", targetUserID)),
			tu.InlineKeyboardButton("➖ کسر از موجودی (کاهش)").WithCallbackData(fmt.Sprintf("admin:wadj:sub_menu:%d", targetUserID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🔄 صفر کردن موجودی").WithCallbackData(fmt.Sprintf("admin:wadj:zero:%d", targetUserID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« بازگشت به لیست کیف پول‌ها").WithCallbackData("admin:wallets"),
		),
	)

	_, err = s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) sendAdminWalletAdjustMenu(ctx context.Context, chatID telego.ChatID, targetUserID int64, op string) error {
	w, err := s.store.Wallets().GetByUserID(ctx, targetUserID)
	if err != nil {
		return err
	}

	var title string
	var rows [][]telego.InlineKeyboardButton

	if op == "add" {
		title = fmt.Sprintf("➕ <b>افزایش موجودی کاربر <code>%d</code></b>\n\nموجودی فعلی: <code>%s تومان</code>\n\nمبلغ شارژ را انتخاب فرمایید:",
			targetUserID, formatNumber(int64(w.BalanceIRT)))
		rows = append(rows,
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("+۵۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:wadj:exec:%d:add:50000", targetUserID)),
				tu.InlineKeyboardButton("+۱۰۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:wadj:exec:%d:add:100000", targetUserID)),
			),
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("+۲۵۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:wadj:exec:%d:add:250000", targetUserID)),
				tu.InlineKeyboardButton("+۵۰۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:wadj:exec:%d:add:500000", targetUserID)),
			),
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("+۱,۰۰۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:wadj:exec:%d:add:1000000", targetUserID)),
				tu.InlineKeyboardButton("+۲,۰۰۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:wadj:exec:%d:add:2000000", targetUserID)),
			),
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("+۵,۰۰۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:wadj:exec:%d:add:5000000", targetUserID)),
			),
		)
	} else {
		title = fmt.Sprintf("➖ <b>کسر از موجودی کاربر <code>%d</code></b>\n\nموجودی فعلی: <code>%s تومان</code>\n\nمبلغ کسر را انتخاب فرمایید:",
			targetUserID, formatNumber(int64(w.BalanceIRT)))
		rows = append(rows,
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("-۵۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:wadj:exec:%d:sub:50000", targetUserID)),
				tu.InlineKeyboardButton("-۱۰۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:wadj:exec:%d:sub:100000", targetUserID)),
			),
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("-۲۵۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:wadj:exec:%d:sub:250000", targetUserID)),
				tu.InlineKeyboardButton("-۵۰۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:wadj:exec:%d:sub:500000", targetUserID)),
			),
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("-۱,۰۰۰,۰۰۰ ت").WithCallbackData(fmt.Sprintf("admin:wadj:exec:%d:sub:1000000", targetUserID)),
			),
		)
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("« انصراف و بازگشت").WithCallbackData(fmt.Sprintf("admin:wview:%d", targetUserID)),
	))

	markup := tu.InlineKeyboard(rows...)
	_, err = s.bot.SendMessage(ctx, tu.Message(chatID, title).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) handleAdminWalletAdjustExec(ctx context.Context, cb *telego.CallbackQuery, targetUserID int64, op string, amountIRT int64) error {
	chatID := tu.ID(cb.Message.GetChat().ID)
	now := time.Now().UTC()

	w, err := s.store.Wallets().GetByUserID(ctx, targetUserID)
	if err != nil {
		return err
	}

	var opDesc string
	var deltaText string

	switch op {
	case "add":
		w.BalanceIRT += pricing.IRT(amountIRT)
		opDesc = "افزایش موجودی"
		deltaText = fmt.Sprintf("+%s تومان", formatNumber(amountIRT))
	case "sub":
		w.BalanceIRT -= pricing.IRT(amountIRT)
		opDesc = "کاهش موجودی"
		deltaText = fmt.Sprintf("-%s تومان", formatNumber(amountIRT))
	case "zero":
		w.BalanceIRT = 0
		opDesc = "صفر کردن موجودی"
		deltaText = "موجودی صفر شد"
	}

	if err := s.store.Wallets().UpdateBalances(ctx, w); err != nil {
		return err
	}

	_ = s.store.Wallets().RecordLedgerEntry(ctx, &wallet.LedgerEntry{
		ID:           fmt.Sprintf("ledg-adm-%d", time.Now().UnixNano()),
		WalletID:     w.ID,
		Currency:     wallet.CurrencyIRT,
		Amount:       amountIRT,
		Kind:         wallet.KindAdminAdjustment,
		BalanceAfter: int64(w.BalanceIRT),
		CreatedAt:    now,
	})

	// 1. Notify target user in Telegram
	userNotice := fmt.Sprintf(
		"🔔 <b>بروزرسانی موجودی کیف پول</b>\n\n"+
			"موجودی کیف پول شما توسط مدیریت بروزرسانی شد:\n"+
			"• <b>عملیات:</b> %s\n"+
			"• <b>میزان تغییر:</b> <code>%s</code>\n"+
			"• <b>موجودی جدید شما:</b> <code>%s تومان</code>",
		opDesc,
		deltaText,
		formatNumber(int64(w.BalanceIRT)),
	)
	_, _ = s.bot.SendMessage(ctx, tu.Message(tu.ID(targetUserID), userNotice).WithParseMode(telego.ModeHTML))

	// 2. Alert admin and return to wallet view
	confirmMsg := fmt.Sprintf("موجودی با موفقیت بروزرسانی شد. جدید: %s تومان", formatNumber(int64(w.BalanceIRT)))
	_ = s.bot.AnswerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText(confirmMsg))

	return s.sendAdminUserWalletView(ctx, chatID, targetUserID)
}

func (s *Service) handleAdminWalletCommand(ctx context.Context, chatID telego.ChatID, text string) error {
	parts := strings.Fields(text)
	if len(parts) < 2 {
		return s.sendAdminWalletsList(ctx, chatID)
	}

	targetUID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, "❌ شناسه کاربری نامعتبر است. فرمت صحیح: <code>/wallet 12345678</code>").WithParseMode(telego.ModeHTML))
		return nil
	}

	return s.sendAdminUserWalletView(ctx, chatID, targetUID)
}

func (s *Service) sendUserSettings(ctx context.Context, chatID telego.ChatID, usr *user.User) error {
	discount := "۱۰٪ (عادی)"
	switch usr.Segment {
	case fee.SegmentCustomer:
		discount = "۳٪ (ویژه)"
	case fee.SegmentLoyal:
		discount = "۱٪ (وفادار)"
	}

	phoneText := "❌ تایید نشده"
	if usr.IsPhoneVerified() && usr.PhoneE164 != nil {
		phoneText = fmt.Sprintf("✅ تایید شده (%s)", *usr.PhoneE164)
	}

	regDate := usr.CreatedAt.Format("2006-01-02 15:04")

	text := fmt.Sprintf(
		"⚙️ <b>مشخصات و حساب کاربری شما</b>\n\n"+
			"• <b>شناسه کاربری:</b> <code>%d</code>\n"+
			"• <b>نام:</b> %s %s\n"+
			"• <b>نام کاربری:</b> @%s\n"+
			"• <b>سطح کاربری:</b> <code>%s</code>\n"+
			"• <b>کارمزد خرید:</b> %s\n"+
			"• <b>وضعیت موبایل:</b> %s\n"+
			"• <b>تاریخ عضویت:</b> %s",
		usr.TelegramUserID,
		usr.FirstName, usr.LastName,
		usr.Username,
		usr.Segment,
		discount,
		phoneText,
		regDate,
	)

	var rows [][]telego.InlineKeyboardButton
	if !usr.IsPhoneVerified() {
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("📱 تایید شماره موبایل").WithCallbackData("menu:phone"),
		))
	}
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("« بازگشت به منوی اصلی").WithCallbackData("menu:main"),
	))

	markup := tu.InlineKeyboard(rows...)
	_, err := s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) sendAdminUsersList(ctx context.Context, chatID telego.ChatID) error {
	users, err := s.store.Users().ListUsers(ctx, 30)
	if err != nil {
		return err
	}

	text := "👥 <b>مدیریت کاربران و تنظیمات</b>\n\n" +
		"جهت مشاهده پرونده، تغییر سطح کارمزد، مسدودسازی، تغییر نقش یا مدیریت کیف پول، کاربر مورد نظر را انتخاب نمایید:\n" +
		"<i>(همچنین می‌توانید از دستور <code>/user [شناسه_کاربر]</code> استفاده کنید)</i>"

	var rows [][]telego.InlineKeyboardButton
	for _, u := range users {
		displayName := u.FirstName
		if u.Username != "" {
			displayName = "@" + u.Username
		}
		statusIcon := "🟢"
		if u.Blocked {
			statusIcon = "⛔"
		}
		roleIcon := "👤"
		if u.Role == user.RoleAdmin {
			roleIcon = "🛡️"
		}

		label := fmt.Sprintf("%s %s %s (%s)", statusIcon, roleIcon, displayName, u.Segment)
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(label).WithCallbackData(fmt.Sprintf("admin:uview:%d", u.TelegramUserID)),
		))
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("« بازگشت به پنل مدیریت").WithCallbackData("admin:home"),
	))

	markup := tu.InlineKeyboard(rows...)
	_, err = s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) sendAdminUserDetailView(ctx context.Context, chatID telego.ChatID, targetUserID int64) error {
	u, err := s.store.Users().GetByID(ctx, targetUserID)
	if err != nil || u == nil {
		_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, fmt.Sprintf("❌ کاربری با شناسه <code>%d</code> یافت نشد.", targetUserID)).WithParseMode(telego.ModeHTML))
		return nil
	}

	w, _ := s.store.Wallets().GetByUserID(ctx, targetUserID)
	var balIRT int64
	if w != nil {
		balIRT = int64(w.BalanceIRT)
	}

	statusStr := "🟢 فعال (Active)"
	if u.Blocked {
		statusStr = "⛔ مسدود (Blocked)"
	}

	roleStr := "کاربر عادی (USER)"
	if u.Role == user.RoleAdmin {
		roleStr = "🛡️ مدیر (ADMIN)"
	}

	phoneStr := "❌ ثبت نشده"
	if u.IsPhoneVerified() && u.PhoneE164 != nil {
		phoneStr = fmt.Sprintf("✅ %s", *u.PhoneE164)
	}

	var invitedByStr string
	if u.InvitedBy != nil {
		invitedByStr = fmt.Sprintf("<code>%d</code>", *u.InvitedBy)
	} else {
		invitedByStr = "سیستم"
	}

	var inviteCodeStr string
	if u.InviteCode != nil {
		inviteCodeStr = fmt.Sprintf("<code>%s</code>", *u.InviteCode)
	} else {
		inviteCodeStr = "-"
	}

	text := fmt.Sprintf(
		"👤 <b>پرونده و تنظیمات کاربر</b>\n\n"+
			"• <b>شناسه تلگرام:</b> <code>%d</code>\n"+
			"• <b>نام:</b> %s %s\n"+
			"• <b>نام کاربری:</b> @%s\n"+
			"• <b>وضعیت حساب:</b> %s\n"+
			"• <b>نقش سیستم:</b> %s\n"+
			"• <b>سطح کارمزد:</b> <code>%s</code>\n"+
			"• <b>وضعیت شماره همراه:</b> %s\n"+
			"• <b>موجودی کیف پول:</b> <code>%s تومان</code>\n"+
			"• <b>کد دعوت معرف:</b> %s (توسط %s)\n"+
			"• <b>تاریخ عضویت:</b> %s\n\n"+
			"جهت اعمال تغییرات در مشخصات کاربر، گزینه‌های زیر را انتخاب فرمایید:",
		u.TelegramUserID,
		u.FirstName, u.LastName,
		u.Username,
		statusStr,
		roleStr,
		u.Segment,
		phoneStr,
		formatNumber(balIRT),
		inviteCodeStr, invitedByStr,
		u.CreatedAt.Format("2006-01-02 15:04"),
	)

	// Block toggle button
	var blockBtn telego.InlineKeyboardButton
	if u.Blocked {
		blockBtn = tu.InlineKeyboardButton("🟢 رفع مسدودیت (Unblock)").WithCallbackData(fmt.Sprintf("admin:uset:block:%d:0", targetUserID))
	} else {
		blockBtn = tu.InlineKeyboardButton("⛔ مسدود کردن (Block)").WithCallbackData(fmt.Sprintf("admin:uset:block:%d:1", targetUserID))
	}

	// Role toggle button
	var roleBtn telego.InlineKeyboardButton
	if u.Role == user.RoleAdmin {
		roleBtn = tu.InlineKeyboardButton("👤 تنزل به کاربر عادی").WithCallbackData(fmt.Sprintf("admin:uset:role:%d:user", targetUserID))
	} else {
		roleBtn = tu.InlineKeyboardButton("🛡️ ارتقا به مدیر (Admin)").WithCallbackData(fmt.Sprintf("admin:uset:role:%d:admin", targetUserID))
	}

	rows := [][]telego.InlineKeyboardButton{
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🏷️ تغییر سطح کاربری (Segment)").WithCallbackData(fmt.Sprintf("admin:uset:seg:%d", targetUserID)),
			tu.InlineKeyboardButton("💳 مدیریت کیف پول").WithCallbackData(fmt.Sprintf("admin:wview:%d", targetUserID)),
		),
		tu.InlineKeyboardRow(
			blockBtn,
			roleBtn,
		),
	}

	if u.IsPhoneVerified() {
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("📱 ریست تایید شماره موبایل").WithCallbackData(fmt.Sprintf("admin:uset:reset_phone:%d", targetUserID)),
		))
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("« بازگشت به لیست کاربران").WithCallbackData("admin:users"),
	))

	markup := tu.InlineKeyboard(rows...)
	_, err = s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) sendAdminUserSegmentMenu(ctx context.Context, chatID telego.ChatID, targetUserID int64) error {
	u, err := s.store.Users().GetByID(ctx, targetUserID)
	if err != nil || u == nil {
		return nil
	}

	text := fmt.Sprintf(
		"🏷️ <b>تغییر سطح کاربری (Segment)</b>\n\n"+
			"• کاربر: <b>%s</b> (<code>%d</code>)\n"+
			"• سطح فعلی: <code>%s</code>\n\n"+
			"سطح جدید کاربری را انتخاب کنید:\n"+
			"• <b>NEW:</b> کارمزد پایه ۱۰٪ (کاربران جدید)\n"+
			"• <b>CUSTOMER:</b> کارمزد ویژه ۳٪ (مشتریان عادی)\n"+
			"• <b>LOYAL:</b> کارمزد وفادار ۱٪ (مشتریان برتر)",
		u.FirstName, u.TelegramUserID, u.Segment,
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🥉 NEW (۱۰٪)").WithCallbackData(fmt.Sprintf("admin:uset:set_seg:%d:NEW", targetUserID)),
			tu.InlineKeyboardButton("🥈 CUSTOMER (۳٪)").WithCallbackData(fmt.Sprintf("admin:uset:set_seg:%d:CUSTOMER", targetUserID)),
			tu.InlineKeyboardButton("🥇 LOYAL (۱٪)").WithCallbackData(fmt.Sprintf("admin:uset:set_seg:%d:LOYAL", targetUserID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« بازگشت به پرونده کاربر").WithCallbackData(fmt.Sprintf("admin:uview:%d", targetUserID)),
		),
	)

	_, err = s.bot.SendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) handleAdminUserSetSegment(ctx context.Context, cb *telego.CallbackQuery, chatID telego.ChatID, targetUserID int64, segStr string) error {
	u, err := s.store.Users().GetByID(ctx, targetUserID)
	if err != nil || u == nil {
		return nil
	}

	var seg fee.Segment
	switch segStr {
	case "CUSTOMER":
		seg = fee.SegmentCustomer
	case "LOYAL":
		seg = fee.SegmentLoyal
	default:
		seg = fee.SegmentNew
	}

	now := time.Now().UTC()
	u.SetSegment(seg, now)
	if err := s.store.Users().Save(ctx, u); err != nil {
		return err
	}

	_ = s.bot.AnswerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText(fmt.Sprintf("سطح کاربر به %s تغییر یافت.", seg)))
	return s.sendAdminUserDetailView(ctx, chatID, targetUserID)
}

func (s *Service) handleAdminUserSetBlock(ctx context.Context, cb *telego.CallbackQuery, chatID telego.ChatID, targetUserID int64, blocked bool) error {
	if targetUserID == s.cfg.AdminTelegramID {
		_ = s.bot.AnswerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("❌ امکان مسدود کردن مدیر اصلی سیستم وجود ندارد.").WithShowAlert())
		return nil
	}

	u, err := s.store.Users().GetByID(ctx, targetUserID)
	if err != nil || u == nil {
		return nil
	}

	now := time.Now().UTC()
	u.SetBlocked(blocked, now)
	if err := s.store.Users().Save(ctx, u); err != nil {
		return err
	}

	var toast string
	if blocked {
		toast = "کاربر مسدود شد."
	} else {
		toast = "مسدودیت کاربر لغو شد."
	}
	_ = s.bot.AnswerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText(toast))
	return s.sendAdminUserDetailView(ctx, chatID, targetUserID)
}

func (s *Service) handleAdminUserSetRole(ctx context.Context, cb *telego.CallbackQuery, chatID telego.ChatID, targetUserID int64, roleStr string) error {
	if targetUserID == s.cfg.AdminTelegramID && roleStr != "admin" {
		_ = s.bot.AnswerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("❌ امکان تنزل مقام مدیر اصلی سیستم وجود ندارد.").WithShowAlert())
		return nil
	}

	u, err := s.store.Users().GetByID(ctx, targetUserID)
	if err != nil || u == nil {
		return nil
	}

	role := user.RoleUser
	if roleStr == "admin" {
		role = user.RoleAdmin
	}

	now := time.Now().UTC()
	u.SetRole(role, now)
	if err := s.store.Users().Save(ctx, u); err != nil {
		return err
	}

	_ = s.bot.AnswerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText(fmt.Sprintf("نقش کاربر به %s تغییر یافت.", role)))
	return s.sendAdminUserDetailView(ctx, chatID, targetUserID)
}

func (s *Service) handleAdminUserResetPhone(ctx context.Context, cb *telego.CallbackQuery, chatID telego.ChatID, targetUserID int64) error {
	u, err := s.store.Users().GetByID(ctx, targetUserID)
	if err != nil || u == nil {
		return nil
	}

	now := time.Now().UTC()
	u.ResetPhone(now)
	if err := s.store.Users().Save(ctx, u); err != nil {
		return err
	}

	_ = s.bot.AnswerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("تایید شماره همراه کاربر ریست شد."))
	return s.sendAdminUserDetailView(ctx, chatID, targetUserID)
}

func (s *Service) handleAdminUserCommand(ctx context.Context, chatID telego.ChatID, text string) error {
	parts := strings.Fields(text)
	if len(parts) < 2 {
		return s.sendAdminUsersList(ctx, chatID)
	}

	targetUID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		_, _ = s.bot.SendMessage(ctx, tu.Message(chatID, "❌ شناسه کاربری نامعتبر است. فرمت صحیح: <code>/user 12345678</code>").WithParseMode(telego.ModeHTML))
		return nil
	}

	return s.sendAdminUserDetailView(ctx, chatID, targetUID)
}
