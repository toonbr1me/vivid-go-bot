package bot

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"github.com/toonbr1me/vivid-go-bot/internal/payments"
	"github.com/toonbr1me/vivid-go-bot/internal/remnawave"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
)

func TestSubscriptionWidgetFitsTelegramLimits(t *testing.T) {
	var compressed bytes.Buffer
	writer, err := zlib.NewWriterLevel(&compressed, zlib.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(subscriptionWidgetHTML)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if len(subscriptionWidgetHTML) > 32768 || compressed.Len() > 4096 {
		t.Fatalf("виджет слишком большой: html=%d zlib=%d", len(subscriptionWidgetHTML), compressed.Len())
	}
	if strings.Contains(subscriptionWidgetHTML, "innerHTML") || strings.Contains(subscriptionWidgetHTML, "fetch(") {
		t.Fatal("виджет не должен вставлять HTML из данных или ходить в сеть")
	}
}

func TestSubscriptionWidgetQueryRejectsOtherContexts(t *testing.T) {
	valid := telego.WidgetQuery{ID: "query", From: telego.User{ID: 7}, Chat: &telego.Chat{ID: 7, Type: "private"}, MessageID: 10,
		Type: &telego.WidgetQueryTypeData{Type: "data"}, Data: subscriptionWidgetRefresh}
	if !privateSubscriptionWidgetQuery(valid) {
		t.Fatal("свой виджет отклонён")
	}
	for _, change := range []func(*telego.WidgetQuery){
		func(q *telego.WidgetQuery) { q.Chat = &telego.Chat{ID: 8, Type: "private"} },
		func(q *telego.WidgetQuery) { q.Chat = &telego.Chat{ID: 7, Type: "group"} },
		func(q *telego.WidgetQuery) { q.InlineMessageID = "inline" },
		func(q *telego.WidgetQuery) { q.WidgetID = "shared" },
		func(q *telego.WidgetQuery) { q.Chat = nil },
		func(q *telego.WidgetQuery) { q.Data = "subscription:refresh:8" },
		func(q *telego.WidgetQuery) { q.Type = nil },
	} {
		query := valid
		change(&query)
		if privateSubscriptionWidgetQuery(query) {
			t.Fatalf("небезопасный контекст принят: %+v", query)
		}
	}
}

func TestSubscriptionWidgetSendsOnlyPrivateState(t *testing.T) {
	caller := &scriptedCaller{}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	b := &Bot{api: api}
	data := subscriptionWidgetData{Status: "Активна", Until: "20.11.2026", Devices: "1 из 3", Traffic: "1 ГБ", Updated: "15:00"}
	if err := b.sendSubscriptionWidgetCard(context.Background(), 7, data); err != nil {
		t.Fatal(err)
	}
	var request struct {
		ChatID       int64  `json:"chat_id"`
		ChatData     string `json:"chat_data"`
		PublicData   string `json:"public_data"`
		IsShared     bool   `json:"is_shared"`
		AllowQueries bool   `json:"allow_queries"`
	}
	if err := json.Unmarshal([]byte(caller.bodies[0]), &request); err != nil {
		t.Fatal(err)
	}
	if caller.methods[0] != "sendWidget" || request.ChatID != 7 || request.PublicData != "" || request.IsShared || !request.AllowQueries {
		t.Fatalf("неверная отправка: %+v", request)
	}
	var decoded subscriptionWidgetData
	if err := json.Unmarshal([]byte(request.ChatData), &decoded); err != nil || decoded != data {
		t.Fatalf("потеряны данные: %+v %v", decoded, err)
	}
}

func TestSubscriptionWidgetRejectsForeignQueryBeforeReadingDatabase(t *testing.T) {
	caller := &scriptedCaller{}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	b := &Bot{api: api} // Базы нет: проверка прав должна сработать до её чтения.
	if err := b.answerSubscriptionWidget(context.Background(), telego.WidgetQuery{ID: "foreign", From: telego.User{ID: 7},
		Chat: &telego.Chat{ID: 8, Type: "private"}, MessageID: 1, Type: &telego.WidgetQueryTypeData{Type: "data"}, Data: subscriptionWidgetRefresh}); err != nil {
		t.Fatal(err)
	}
	if len(caller.methods) != 1 || caller.methods[0] != "answerWidgetQuery" {
		t.Fatalf("попытка изменить чужое сообщение: %v", caller.methods)
	}
}

func TestSubscriptionWidgetDoesNotClaimInactiveDuringOutage(t *testing.T) {
	data := widgetDataForAccess(domain.Access{}, errors.New("panel offline with secret"))
	if data.Status != "Не удалось проверить" || strings.Contains(data.Error, "secret") {
		t.Fatalf("неверное состояние сбоя: %+v", data)
	}
	expiry := time.Now().Add(time.Hour)
	data = widgetDataForAccess(domain.Access{Active: true, ExpireAt: &expiry}, errors.New("offline"))
	if data.Status != "Активна" || data.Until == "—" || data.Error == "" {
		t.Fatalf("утерян известный срок: %+v", data)
	}
}

type widgetSnapshotPanel struct{ payments.Panel }

func (widgetSnapshotPanel) UsersByTelegramID(context.Context, int64) ([]remnawave.User, error) {
	return []remnawave.User{{ID: 1, Status: "ACTIVE", ExpireAt: remnawave.Timestamp{Time: time.Now().Add(24 * time.Hour)}}}, nil
}
func (widgetSnapshotPanel) HWIDDevices(context.Context, int64) ([]remnawave.HWIDDevice, error) {
	return nil, nil
}

func TestSubscriptionWidgetRefreshPersistsPrivateState(t *testing.T) {
	db := testdb.Open(t)
	caller := &scriptedCaller{}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Settings{}
	b := &Bot{api: api, db: db, cfg: cfg, pay: payments.New(db, widgetSnapshotPanel{}, nil, cfg)}
	query := telego.WidgetQuery{ID: "refresh", From: telego.User{ID: 7}, Chat: &telego.Chat{ID: 7, Type: "private"}, MessageID: 10,
		Type: &telego.WidgetQueryTypeData{Type: "data"}, Data: subscriptionWidgetRefresh}
	if err := b.answerSubscriptionWidget(context.Background(), query); err != nil {
		t.Fatal(err)
	}
	if len(caller.methods) != 2 || caller.methods[0] != "answerWidgetQuery" || caller.methods[1] != "editMessageWidgetData" {
		t.Fatalf("неверная последовательность: %v", caller.methods)
	}
	var update struct {
		ChatID     int64  `json:"chat_id"`
		MessageID  int    `json:"message_id"`
		ChatData   string `json:"chat_data"`
		PublicData string `json:"public_data"`
	}
	if err := json.Unmarshal([]byte(caller.bodies[1]), &update); err != nil {
		t.Fatal(err)
	}
	var state subscriptionWidgetData
	if err := json.Unmarshal([]byte(update.ChatData), &state); err != nil {
		t.Fatal(err)
	}
	if update.ChatID != 7 || update.MessageID != 10 || update.PublicData != "" || state.Status != "Активна" {
		t.Fatalf("обновлён не тот виджет или неверный статус: %+v %+v", update, state)
	}
	query.ID = "refresh-too-soon"
	if err := b.answerSubscriptionWidget(context.Background(), query); err != nil {
		t.Fatal(err)
	}
	if len(caller.methods) != 3 || caller.methods[2] != "answerWidgetQuery" {
		t.Fatal("частые запросы не ограничены")
	}
}
