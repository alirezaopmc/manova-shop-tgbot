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
		text := "🖥️ <b>سرورهای فعال</b>\n\n" +
			"شما هیچ سرور فعالی ندارید."

		markup := tu.InlineKeyboard(
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("🛒 خرید سرور ابری").WithCallbackData("menu:catalog"),
			),
			tu.InlineKeyboardRow(
				tu.InlineKeyboardButton("« منوی اصلی").WithCallbackData("menu:main"),
			),
		)

		_, err = s.sendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
		return err
	}

	text := fmt.Sprintf("🖥️ <b>سرورهای فعال (%d)</b>\n\nسرور مورد نظر را انتخاب نمایید:", len(resources))

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
		tu.InlineKeyboardButton("« منوی اصلی").WithCallbackData("menu:main"),
	))

	markup := tu.InlineKeyboard(rows...)
	_, err = s.sendMessage(ctx, tu.Message(chatID, text).WithParseMode(telego.ModeHTML).WithReplyMarkup(markup))
	return err
}

// checkServerOwnership validates that the user owns the server or is an administrator.
func (s *Service) checkServerOwnership(ctx context.Context, chatID int64, userID int64, res *billing.ProvisionedResource) (bool, error) {
	if res != nil && res.UserID == userID {
		return true, nil
	}
	usr, err := s.store.Users().GetByID(ctx, userID)
	if err == nil && usr != nil && usr.Role == user.RoleAdmin {
		return true, nil
	}
	if s.cfg != nil && s.cfg.AdminTelegramID != 0 && userID == s.cfg.AdminTelegramID {
		return true, nil
	}
	_ = s.sendAccessDenied(ctx, tu.ID(chatID))
	return false, nil
}

// Server management view for a specific server
func (s *Service) sendServerManagementView(ctx context.Context, chatID telego.ChatID, resID string, userID int64) error {
	res, err := s.store.Billing().GetResource(ctx, resID)
	if err != nil || res == nil || res.Status != billing.ResourceStatusActive {
		_, _ = s.sendMessage(ctx, tu.Message(chatID, "❌ سرور مورد نظر یافت نشد."))
		return s.sendUserServersList(ctx, chatID, userID)
	}

	// Verify ownership or admin
	if ok, err := s.checkServerOwnership(ctx, chatID.ID, userID, res); !ok {
		return err
	}

	stateLabel := "🟢 Running"
	if res.PowerState == billing.PowerStateOff {
		stateLabel = "🔴 Powered Off"
	}

	var accessInfo string
	if res.SSHKey != "" {
		accessInfo = "🛡️ <b>SSH Key</b>"
	} else if res.RootPassword != "" {
		accessInfo = fmt.Sprintf("🔑 <b>Root Password:</b> <code>%s</code>", res.RootPassword)
	} else {
		accessInfo = "🔑 <i>Password generated (use Reset Password if needed)</i>"
	}

	text := fmt.Sprintf(
		"🖥️ <b>%s</b>\n\n"+
			"• Status: <b>%s</b>\n"+
			"• Location: <code>%s</code>\n"+
			"• Image: <code>%s</code>\n"+
			"• IPv4: <code>%s</code>\n"+
			"• IPv6: <code>%s</code>\n"+
			"%s\n\n"+
			"SSH: <code>ssh root@%s</code>",
		res.ServerName,
		stateLabel,
		res.Location,
		res.ImageName,
		res.IPv4Address,
		res.IPv6Address,
		accessInfo,
		res.IPv4Address,
	)

	var rows [][]telego.InlineKeyboardButton

	// Power controls
	if res.PowerState == billing.PowerStateOff {
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("⚡ Power On").WithCallbackData(fmt.Sprintf("srv:act:poweron:%s", res.ID)),
			tu.InlineKeyboardButton("🔄 Reboot").WithCallbackData(fmt.Sprintf("srv:act:reboot:%s", res.ID)),
		))
	} else {
		rows = append(rows, tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🔌 Power Off").WithCallbackData(fmt.Sprintf("srv:act:poweroff:%s", res.ID)),
			tu.InlineKeyboardButton("🔄 Reboot").WithCallbackData(fmt.Sprintf("srv:act:reboot:%s", res.ID)),
		))
	}

	// Password and Rescue
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("🔑 Reset Root Password").WithCallbackData(fmt.Sprintf("srv:act:reset_pwd:%s", res.ID)),
		tu.InlineKeyboardButton("🛟 Rescue Mode").WithCallbackData(fmt.Sprintf("srv:act:rescue:%s", res.ID)),
	))

	// Delete server
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("🗑️ Delete Server").WithCallbackData(fmt.Sprintf("srv:act:delete_prompt:%s", res.ID)),
	))

	// Navigation
	rows = append(rows, tu.InlineKeyboardRow(
		tu.InlineKeyboardButton("« سرورها").WithCallbackData("menu:servers"),
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

	if ok, err := s.checkServerOwnership(ctx, cb.Message.GetChat().ID, userID, res); !ok {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("دسترسی غیرمجاز."))
		return err
	}

	_ = s.provider.PowerOn(ctx, res.ExternalID)
	res.PowerState = billing.PowerStateRunning
	_ = s.store.Billing().SaveResource(ctx, res)

	_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("⚡ Power On sent."))
	return s.sendServerManagementView(ctx, chatID, resID, userID)
}

func (s *Service) handleServerPowerOff(ctx context.Context, cb *telego.CallbackQuery, resID string, userID int64) error {
	chatID := tu.ID(cb.Message.GetChat().ID)
	res, err := s.store.Billing().GetResource(ctx, resID)
	if err != nil || res == nil {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("سرور یافت نشد."))
		return nil
	}

	if ok, err := s.checkServerOwnership(ctx, cb.Message.GetChat().ID, userID, res); !ok {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("دسترسی غیرمجاز."))
		return err
	}

	_ = s.provider.PowerOff(ctx, res.ExternalID)
	res.PowerState = billing.PowerStateOff
	_ = s.store.Billing().SaveResource(ctx, res)

	_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("🔌 Power Off sent."))
	return s.sendServerManagementView(ctx, chatID, resID, userID)
}

func (s *Service) handleServerReboot(ctx context.Context, cb *telego.CallbackQuery, resID string, userID int64) error {
	chatID := tu.ID(cb.Message.GetChat().ID)
	res, err := s.store.Billing().GetResource(ctx, resID)
	if err != nil || res == nil {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("سرور یافت نشد."))
		return nil
	}

	if ok, err := s.checkServerOwnership(ctx, cb.Message.GetChat().ID, userID, res); !ok {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("دسترسی غیرمجاز."))
		return err
	}

	_ = s.provider.Reboot(ctx, res.ExternalID)
	res.PowerState = billing.PowerStateRunning
	_ = s.store.Billing().SaveResource(ctx, res)

	_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("🔄 Reboot sent."))
	return s.sendServerManagementView(ctx, chatID, resID, userID)
}

func (s *Service) handleServerResetPassword(ctx context.Context, cb *telego.CallbackQuery, resID string, userID int64) error {
	chatID := tu.ID(cb.Message.GetChat().ID)
	res, err := s.store.Billing().GetResource(ctx, resID)
	if err != nil || res == nil {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("سرور یافت نشد."))
		return nil
	}

	if ok, err := s.checkServerOwnership(ctx, cb.Message.GetChat().ID, userID, res); !ok {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("دسترسی غیرمجاز."))
		return err
	}

	newPwd, err := s.provider.ResetPassword(ctx, res.ExternalID)
	if err != nil {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("Error resetting root password."))
		return nil
	}

	res.RootPassword = newPwd
	_ = s.store.Billing().SaveResource(ctx, res)

	_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("🔑 Root password reset."))

	notice := fmt.Sprintf(
		"🔑 <b>New Root Password (%s):</b>\n<code>%s</code>",
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

	if ok, err := s.checkServerOwnership(ctx, cb.Message.GetChat().ID, userID, res); !ok {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("دسترسی غیرمجاز."))
		return err
	}

	rescuePwd, err := s.provider.EnableRescue(ctx, res.ExternalID)
	if err != nil {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("Error enabling rescue mode."))
		return nil
	}

	_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("🛟 Rescue mode enabled."))

	notice := fmt.Sprintf(
		"🛟 <b>Rescue Mode enabled (%s):</b>\n<code>%s</code>\n\nReboot server to boot into rescue system.",
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

	if ok, err := s.checkServerOwnership(ctx, chatID.ID, userID, res); !ok {
		return err
	}

	text := fmt.Sprintf(
		"⚠️ <b>Delete Server (%s - %s)</b>\n\n"+
			"All data will be permanently erased. Billing stops immediately.",
		res.ServerName, res.IPv4Address,
	)

	markup := tu.InlineKeyboard(
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("🗑️ Confirm Delete").WithCallbackData(fmt.Sprintf("srv:act:delete_confirm:%s", res.ID)),
		),
		tu.InlineKeyboardRow(
			tu.InlineKeyboardButton("« Cancel").WithCallbackData(fmt.Sprintf("srv:manage:%s", res.ID)),
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

	if ok, err := s.checkServerOwnership(ctx, cb.Message.GetChat().ID, userID, res); !ok {
		_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("دسترسی غیرمجاز."))
		return err
	}

	_ = s.provider.DeleteServer(ctx, res.ExternalID)
	_ = s.store.Billing().UpdateResourceBillingEnd(ctx, res.ID, time.Now().UTC(), billing.ResourceStatusDeleted)

	_ = s.answerCallbackQuery(ctx, tu.CallbackQuery(cb.ID).WithText("🗑️ Server deleted."))

	msg := fmt.Sprintf("✅ <b>%s deleted.</b> Billing stopped.", res.ServerName)
	_, _ = s.sendMessage(ctx, tu.Message(chatID, msg).WithParseMode(telego.ModeHTML))

	return s.sendUserServersList(ctx, chatID, userID)
}
