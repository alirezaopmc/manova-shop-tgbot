package telegram

import (
	"context"
	"fmt"
	"time"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
)

// List user's active servers
func (s *Service) sendUserServersList(ctx context.Context, chatID telego.ChatID, userID int64) error {
	resources, err := s.store.Billing().ListUserResources(ctx, userID)
	if err != nil {
		return err
	}

	if len(resources) == 0 {
		text := "🖥️ <b>سرورهای فعال شما</b>\n\n" +
			"شما در حال حاضر هیچ سرور ابری فعالی در حساب کاربری خود ندارید.\n\n" +
			"جهت انتخاب پلن و راه‌اندازی فوری سرور از دکمه زیر استفاده نمایید:"

		markup := tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("🛒 خرید سرور ابری Hetzner").WithCallbackData("menu:catalog"),
			),
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("« بازگشت به منوی اصلی").WithCallbackData("menu:main"),
			),
		)

		_, err = s.sendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
		return err
	}

	text := fmt.Sprintf(
		"🖥️ <b>سرورهای فعال شما (%d سرور)</b>\n\n"+
			"برای مشاهده اطلاعات اتصال، دریافت رمز یا انجام عملیات روی هر سرور، آن را از لیست زیر انتخاب کنید:",
		len(resources),
	)

	var rows [][]telego.InlineKeyboardButton
	for _, res := range resources {
		stateEmoji := "🟢"
		if res.PowerState == billing.PowerStateOff {
			stateEmoji = "🔴"
		}
		btnLabel := fmt.Sprintf("%s %s (%s)", stateEmoji, res.ServerName, res.IPv4Address)
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton(btnLabel).WithCallbackData(fmt.Sprintf("srv:manage:%s", res.ID)),
		))
	}

	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("🛒 خرید سرور جدید").WithCallbackData("menu:catalog"),
		tu.InlineKeyboardButton("« بازگشت به منوی اصلی").WithCallbackData("menu:main"),
	))

	markup := tu.InlineKeyboard(rows...)
	_, err = s.sendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

// Server management view for a specific server
func (s *Service) sendServerManagementView(ctx context.Context, chatID telego.ChatID, resID string, userID int64) error {
	res, err := s.store.Billing().GetResource(ctx, resID)
	if err != nil || res == nil || res.Status != billing.ResourceStatusActive {
		_, _ = s.sendMessage(ctx, tu.Message(chatID, "❌ سرور مورد نظر یافت نشد یا قبلاً حذف گردیده است."))
		return s.sendUserServersList(ctx, chatID, userID)
	}

	// Verify ownership or admin
	usr, _ := s.store.Users().GetByID(ctx, userID)
	if res.UserID != userID && (usr == nil || usr.Role != user.RoleAdmin) {
		return s.sendAccessDenied(ctx, chatID)
	}

	stateLabel := "🟢 روشن (Running)"
	if res.PowerState == billing.PowerStateOff {
		stateLabel = "🔴 خاموش (Powered Off)"
	}

	var accessInfo string
	if res.SSHKey != "" {
		accessInfo = "🛡️ <b>ورود با کلید عمومی SSH</b>"
	} else if res.RootPassword != "" {
		accessInfo = fmt.Sprintf("🔑 <b>کلمه عبور Root:</b>\n<code>%s</code>", res.RootPassword)
	} else {
		accessInfo = "🔑 <i>کلمه عبور Root تولید شده است. در صورت نیاز از دکمه ریست پسورد استفاده نمایید.</i>"
	}

	text := fmt.Sprintf(
		"🖥️ <b>مدیریت سرور %s</b>\n\n"+
			"• وضعیت کارکرد: <b>%s</b>\n"+
			"• موقعیت دیتاسنتر: <b>%s</b>\n"+
			"• سیستم‌عامل: <b>%s</b>\n"+
			"• زمان ساخت: <code>%s UTC</code>\n\n"+
			"🌐 <b>آدرس‌های شبکه:</b>\n"+
			"• آی‌پی (IPv4): <code>%s</code>\n"+
			"• آی‌پی (IPv6): <code>%s</code>\n\n"+
			"🔑 <b>احراز هویت:</b>\n"+
			"• نام کاربری: <code>root</code>\n"+
			"%s\n\n"+
			"📌 <b>دستور اتصال در ترمینال:</b>\n"+
			"<code>ssh root@%s</code>\n\n"+
			"عملیات مورد نظر خود را انتخاب نمایید:",
		res.ServerName,
		stateLabel,
		locationName(res.Location),
		osImageLabel(res.ImageName),
		res.CreatedAt.Format("2006-01-02 15:04"),
		res.IPv4Address,
		res.IPv6Address,
		accessInfo,
		res.IPv4Address,
	)

	var rows [][]telego.InlineKeyboardButton

	// Power controls
	if res.PowerState == billing.PowerStateOff {
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("⚡ روشن کردن سرور").WithCallbackData(fmt.Sprintf("srv:act:poweron:%s", res.ID)),
			tu.InlineKeyboardButton("🔄 ریبوت سرور").WithCallbackData(fmt.Sprintf("srv:act:reboot:%s", res.ID)),
		))
	} else {
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🔌 خاموش کردن سرور").WithCallbackData(fmt.Sprintf("srv:act:poweroff:%s", res.ID)),
			tu.InlineKeyboardButton("🔄 ریبوت سرور").WithCallbackData(fmt.Sprintf("srv:act:reboot:%s", res.ID)),
		))
	}

	// Password and Rescue
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("🔑 ریست رمز Root").WithCallbackData(fmt.Sprintf("srv:act:reset_pwd:%s", res.ID)),
		tu.InlineKeyboardButton("🛟 حالت نجات (Rescue)").WithCallbackData(fmt.Sprintf("srv:act:rescue:%s", res.ID)),
	))

	// Delete server
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("🗑️ حذف کامل سرور").WithCallbackData(fmt.Sprintf("srv:act:delete_prompt:%s", res.ID)),
	))

	// Navigation
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("« بازگشت به لیست سرورها").WithCallbackData("menu:servers"),
		tu.InlineKeyboardButton("🏠 منوی اصلی").WithCallbackData("menu:main"),
	))

	markup := tu.InlineKeyboard(rows...)
	_, err = s.sendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) handleServerPowerOn(ctx context.Context, cb *telego.CallbackQuery, resID string, userID int64) error {
	chatID := tu.ID(cb.Message.GetChat().ID)
	res, err := s.store.Billing().GetResource(ctx, resID)
	if err != nil || res == nil {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("سرور یافت نشد."))
		return nil
	}

	_ = s.provider.PowerOn(ctx, res.ExternalID)
	res.PowerState = billing.PowerStateRunning
	_ = s.store.Billing().SaveResource(ctx, res)

	_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("⚡ سرور با موفقیت روشن شد."))
	return s.sendServerManagementView(ctx, chatID, resID, userID)
}

func (s *Service) handleServerPowerOff(ctx context.Context, cb *telego.CallbackQuery, resID string, userID int64) error {
	chatID := tu.ID(cb.Message.GetChat().ID)
	res, err := s.store.Billing().GetResource(ctx, resID)
	if err != nil || res == nil {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("سرور یافت نشد."))
		return nil
	}

	_ = s.provider.PowerOff(ctx, res.ExternalID)
	res.PowerState = billing.PowerStateOff
	_ = s.store.Billing().SaveResource(ctx, res)

	_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("🔌 سرور خاموش شد."))
	return s.sendServerManagementView(ctx, chatID, resID, userID)
}

func (s *Service) handleServerReboot(ctx context.Context, cb *telego.CallbackQuery, resID string, userID int64) error {
	chatID := tu.ID(cb.Message.GetChat().ID)
	res, err := s.store.Billing().GetResource(ctx, resID)
	if err != nil || res == nil {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("سرور یافت نشد."))
		return nil
	}

	_ = s.provider.Reboot(ctx, res.ExternalID)
	res.PowerState = billing.PowerStateRunning
	_ = s.store.Billing().SaveResource(ctx, res)

	_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("🔄 دستور ریبوت به سرور ارسال گردید."))
	return s.sendServerManagementView(ctx, chatID, resID, userID)
}

func (s *Service) handleServerResetPassword(ctx context.Context, cb *telego.CallbackQuery, resID string, userID int64) error {
	chatID := tu.ID(cb.Message.GetChat().ID)
	res, err := s.store.Billing().GetResource(ctx, resID)
	if err != nil || res == nil {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("سرور یافت نشد."))
		return nil
	}

	newPwd, err := s.provider.ResetPassword(ctx, res.ExternalID)
	if err != nil {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("خطا در بازنشانی رمز عبور."))
		return nil
	}

	res.RootPassword = newPwd
	_ = s.store.Billing().SaveResource(ctx, res)

	_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("🔑 کلمه عبور با موفقیت ریست شد."))

	notice := fmt.Sprintf(
		"🔑 <b>کلمه عبور جدید Root برای سرور %s تنظیم شد:</b>\n\n"+
			"<code>%s</code>\n\n"+
			"لطفاً این رمز را در جای امن ذخیره فرمایید.",
		res.ServerName, newPwd,
	)
	_, _ = s.sendMessage(ctx, tu.Message(chatID, notice).WithParseMode(telego.ModeHTML))
	return s.sendServerManagementView(ctx, chatID, resID, userID)
}

func (s *Service) handleServerRescue(ctx context.Context, cb *telego.CallbackQuery, resID string, userID int64) error {
	chatID := tu.ID(cb.Message.GetChat().ID)
	res, err := s.store.Billing().GetResource(ctx, resID)
	if err != nil || res == nil {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("سرور یافت نشد."))
		return nil
	}

	rescuePwd, err := s.provider.EnableRescue(ctx, res.ExternalID)
	if err != nil {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("خطا در فعال‌سازی Rescue Mode."))
		return nil
	}

	_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("🛟 حالت نجات فعال گردید."))

	notice := fmt.Sprintf(
		"🛟 <b>حالت نجات (Rescue Mode) برای سرور %s فعال شد</b>\n\n"+
			"• رمز عبور موقت محیط نجات:\n<code>%s</code>\n\n"+
			"⚠️ <i>برای بوت شدن سرور به سیستم نجات لینوکس، هم‌اکنون سرور را ریبوت نمایید.</i>",
		res.ServerName, rescuePwd,
	)
	_, _ = s.sendMessage(ctx, tu.Message(chatID, notice).WithParseMode(telego.ModeHTML))
	return s.sendServerManagementView(ctx, chatID, resID, userID)
}

func (s *Service) handleServerDeletePrompt(ctx context.Context, chatID telego.ChatID, resID string, userID int64) error {
	res, err := s.store.Billing().GetResource(ctx, resID)
	if err != nil || res == nil {
		return s.sendUserServersList(ctx, chatID, userID)
	}

	text := fmt.Sprintf(
		"⚠️ <b>هشدار: تایید حذف سرور ابری</b>\n\n"+
			"آیا از حذف کامل سرور <b>%s</b> (%s) اطمینان قطعی دارید؟\n\n"+
			"• تمامی اطلاعات، فایل‌ها و تنظیمات روی دیسک این سرور برای همیشه پاک خواهند شد.\n"+
			"• با حذف سرور، محاسبه هزینه آن بلافاصله متوقف می‌شود.\n"+
			"• این عملیات به هیچ عنوان قابل بازگشت نیست.",
		res.ServerName, res.IPv4Address,
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🗑️ بله، سرور را برای همیشه حذف کن").WithCallbackData(fmt.Sprintf("srv:act:delete_confirm:%s", res.ID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« انصراف و بازگشت به مشخصات سرور").WithCallbackData(fmt.Sprintf("srv:manage:%s", res.ID)),
		),
	)

	_, err = s.sendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

func (s *Service) handleServerDeleteConfirm(ctx context.Context, cb *telego.CallbackQuery, resID string, userID int64) error {
	chatID := tu.ID(cb.Message.GetChat().ID)
	res, err := s.store.Billing().GetResource(ctx, resID)
	if err != nil || res == nil {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("سرور قبلاً حذف شده است."))
		return s.sendUserServersList(ctx, chatID, userID)
	}

	_ = s.provider.DeleteServer(ctx, res.ExternalID)
	_ = s.store.Billing().UpdateResourceBillingEnd(ctx, res.ID, time.Now().UTC(), billing.ResourceStatusDeleted)

	_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("🗑️ سرور با موفقیت حذف گردید."))

	msg := fmt.Sprintf("✅ <b>سرور %s با موفقیت حذف شد.</b>\n\nمحاسبه هزینه متوقف گردید و باقیمانده اعتبار در کیف پول شما محفوظ است.", res.ServerName)
	_, _ = s.sendMessage(ctx, tu.Message(chatID, msg).WithParseMode(telego.ModeHTML))

	return s.sendUserServersList(ctx, chatID, userID)
}
