package bot

import (
	"fmt"
	"github.com/mymmrac/telego"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"strconv"
)

type ticketListPosition struct {
	scope string
	page  int
	staff bool
}

func (b *Bot) rememberTicketList(id int64, page int, staff bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ticketLists == nil {
		b.ticketLists = map[int64]ticketListPosition{}
	}
	b.ticketLists[id] = ticketListPosition{page: page, staff: staff}
}

func (b *Bot) ticketListBack(id int64, staff bool) string {
	b.mu.Lock()
	position, ok := b.ticketLists[id]
	b.mu.Unlock()
	prefix := "support:list"
	if staff {
		prefix = "admin:tickets"
	}
	if ok && position.staff == staff {
		if staff && position.scope != "" {
			return fmt.Sprintf("queue:%s:%d", position.scope, position.page)
		}
		return fmt.Sprintf("%s:%d", prefix, position.page)
	}
	return prefix
}

func ticketListPager(p storage.TicketPage, prefix string) []telego.InlineKeyboardButton {
	if p.Pages <= 1 {
		return nil
	}
	row := []telego.InlineKeyboardButton{}
	if p.Page > 0 {
		row = append(row, button("«", prefix+":0"), button("←", fmt.Sprintf("%s:%d", prefix, p.Page-1)))
	}
	row = append(row, button(fmt.Sprintf("%d / %d", p.Page+1, p.Pages), fmt.Sprintf("%s:%d", prefix, p.Page)))
	if p.Page+1 < p.Pages {
		row = append(row, button("→", fmt.Sprintf("%s:%d", prefix, p.Page+1)), button("»", prefix+":-1"))
	}
	return row
}

func ticketListPage(raw string) int { page, _ := strconv.Atoi(raw); return page }
