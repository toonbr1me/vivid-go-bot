package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"

	"github.com/toonbr1me/vivid-go-bot/internal/domain"
)

// handlePartnersCommand — партнёры программы: список, карточка, приглашение.
//
// Управление партнёрами есть и в админке на сайте; здесь — быстрый
// доступ из бота: владельцу не нужно открывать браузер.
func (b *Bot) handlePartnersCommand(ctx *telegohandler.Context, message telego.Message) error {
	tgID := message.From.ID
	if !b.isOwner(ctx, tgID) {
		b.send(ctx, telego.ChatID{ID: message.Chat.ID}, "Недостаточно прав.", keyboard(b.backRow()))
		return nil
	}
	text, kb := b.partnersScreen(ctx, tgID)
	b.send(ctx, telego.ChatID{ID: message.Chat.ID}, text, kb)
	return nil
}

// partnersScreen — список партнёров.
func (b *Bot) partnersScreen(ctx context.Context, tgID int64) (string, *telego.InlineKeyboardMarkup) {
	partners, err := b.db.ListPartners(ctx)
	if err != nil {
		return "⚠️ Не удалось загрузить партнёров.", keyboard(b.backRow())
	}
	lines := []string{"🤝 <b>Партнёры</b>", ""}
	var rows [][]telego.InlineKeyboardButton
	for _, partner := range partners {
		mark := "🟢"
		if partner.Status != domain.PartnerActive {
			mark = "⏸"
		}
		lines = append(lines, fmt.Sprintf("%s <b>%s</b> — кодов: %d, клиентов: %d, начислено: %s",
			mark, escapeHTML(partner.Name), partner.CodesCount, partner.ClientsCount,
			priceText(partner.Rewarded, b.cfg.Current().Currency)))
		rows = append(rows, []telego.InlineKeyboardButton{
			button(escapeHTML(firstRunes(partner.Name, 20)), "pw:p:"+strconv.FormatInt(partner.ID, 10)),
		})
	}
	if len(partners) == 0 {
		lines = append(lines, "Партнёров пока нет. Создать: в админке на сайте → Партнёры.")
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button("🏠 Меню", "nav:main"),
	})
	return strings.Join(lines, "\n"), keyboard(rows...)
}

// partnerCard — карточка партнёра.
func (b *Bot) partnerCard(ctx context.Context, partnerID int64) (string, *telego.InlineKeyboardMarkup) {
	partner, err := b.db.GetPartner(ctx, partnerID)
	if err != nil || partner == nil {
		return "Партнёр не найден.", keyboard(b.backRow())
	}
	codes, _ := b.db.PartnerCodes(ctx, partnerID)
	rewards, _ := b.db.PartnerRewards(ctx, partnerID, 10)

	state := "активен"
	if partner.Status != domain.PartnerActive {
		state = "на паузе"
	}
	lines := []string{
		"🤝 <b>" + escapeHTML(partner.Name) + "</b>",
		"",
		"Контакт: " + orDash(partner.Contact),
		"Статус: " + state,
	}
	if partner.SubjectID != nil {
		lines = append(lines, "Кабинет привязан — начисления идут на баланс партнёра.")
	} else {
		lines = append(lines, "Кабинет не привязан: начисления копятся.")
	}
	if len(codes) > 0 {
		lines = append(lines, "", "Коды:")
		for _, code := range codes {
			lines = append(lines, "• <code>"+escapeHTML(code.Code)+"</code> — "+
				fmt.Sprintf("применений: %d", code.UsedCount))
		}
	}
	if len(rewards) > 0 {
		lines = append(lines, "", "Начисления:")
		for _, reward := range rewards {
			lines = append(lines, "• "+priceText(reward.Amount, b.cfg.Current().Currency)+" — "+reward.Status)
		}
	}
	rows := [][]telego.InlineKeyboardButton{
		{button("🎫 Ссылка-приглашение", "pw:invite:"+strconv.FormatInt(partnerID, 10))},
	}
	if partner.Status == domain.PartnerActive {
		rows = append(rows, []telego.InlineKeyboardButton{
			button("⏸ На паузу", "pw:pause:"+strconv.FormatInt(partnerID, 10)),
		})
	} else {
		rows = append(rows, []telego.InlineKeyboardButton{
			button("▶️ Включить", "pw:on:"+strconv.FormatInt(partnerID, 10)),
		})
	}
	rows = append(rows, []telego.InlineKeyboardButton{
		button("← Партнёры", "pw:list"),
		button("🏠 Меню", "nav:main"),
	})
	return strings.Join(lines, "\n"), keyboard(rows...)
}

// handlePartnerCallback — кнопки карточки партнёра.
func (b *Bot) handlePartnerCallback(ctx context.Context, queryID string, chatID telego.ChatID, messageID int, tgID int64, action, rest string) {
	if !b.isOwner(ctx, tgID) {
		b.answer(ctx, queryID, "Недостаточно прав", true)
		return
	}
	partnerID, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		b.answer(ctx, queryID, "Партнёр не найден", true)
		return
	}
	switch action {
	case "p":
		b.answer(ctx, queryID, "", false)
		text, kb := b.partnerCard(ctx, partnerID)
		b.upsert(ctx, chatID, messageID, text, kb)
	case "invite":
		b.answer(ctx, queryID, "", false)
		code, err := b.db.CreatePartnerInviteCode(ctx, partnerID, 7*24*time.Hour)
		if err != nil {
			b.answer(ctx, queryID, "Не удалось создать приглашение", true)
			return
		}
		link := b.cfg.Current().SiteBaseURL + "/partner/claim?token=" + code
		b.upsert(ctx, chatID, messageID,
			"🎫 Ссылка-приглашение (действует неделю):\n<code>"+escapeHTML(link)+"</code>",
			keyboard([]telego.InlineKeyboardButton{button("← Партнёр", "pw:p:"+rest)}))
	case "pause":
		if _, err := b.db.UpdatePartner(ctx, partnerID, "", "", "", "", domain.PartnerPaused); err != nil {
			b.answer(ctx, queryID, "Не удалось изменить партнёра", true)
			return
		}
		stopped, _ := b.db.DeactivatePartnerCodes(ctx, partnerID)
		b.answer(ctx, queryID, fmt.Sprintf("Партнёр на паузе, выключено кодов: %d", stopped), false)
		text, kb := b.partnerCard(ctx, partnerID)
		b.upsert(ctx, chatID, messageID, text, kb)
	case "on":
		if _, err := b.db.UpdatePartner(ctx, partnerID, "", "", "", "", domain.PartnerActive); err != nil {
			b.answer(ctx, queryID, "Не удалось изменить партнёра", true)
			return
		}
		b.answer(ctx, queryID, "Партнёр включён", false)
		text, kb := b.partnerCard(ctx, partnerID)
		b.upsert(ctx, chatID, messageID, text, kb)
	case "list":
		b.answer(ctx, queryID, "", false)
		text, kb := b.partnersScreen(ctx, tgID)
		b.upsert(ctx, chatID, messageID, text, kb)
	default:
		b.answer(ctx, queryID, "Неизвестное действие", true)
	}
}

// orDash — пустое значение как прочерк.
func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return escapeHTML(value)
}
