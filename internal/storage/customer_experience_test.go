package storage_test

import (
	"context"
	"github.com/toonbr1me/vivid-go-bot/internal/storage"
	"github.com/toonbr1me/vivid-go-bot/internal/testdb"
	"sync"
	"testing"
	"time"
)

func TestCustomerJourneyRetriesDoNotInflateFunnel(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s, err := db.EnsureTelegramSubject(ctx, 501)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- db.RecordJourney(ctx, s.ID, "import_requested", "web", "ios") }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	stats, err := db.CustomerExperience(ctx, 30)
	if err != nil || stats.Import != 1 || stats.Connected != 0 {
		t.Fatal(stats, err)
	}
	var hits int
	if err = db.Pool.QueryRow(ctx, `SELECT hits FROM funnel_daily WHERE step='import_requested'`).Scan(&hits); err != nil || hits != 1 {
		t.Fatal(hits, err)
	}
	first := time.Now().Add(-time.Minute)
	for i := 0; i < 3; i++ {
		if err = db.ObserveConnection(ctx, s.ID, &first); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.RecordPanelEvent(ctx, storage.PanelEvent{ID: "later-delivery", Event: "user.first_connected", TelegramID: 501}); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `SELECT hits FROM funnel_daily WHERE step='first_connected'`).Scan(&hits); err != nil || hits != 1 {
		t.Fatal("connection counted twice", hits, err)
	}
	if err = db.ObserveConnection(ctx, s.ID, nil); err != nil {
		t.Fatal(err)
	}
	h, err := db.CustomerConnection(ctx, s.ID)
	if err != nil || h.FirstConnectedAt == nil || h.FirstConnectedAt.Sub(first) > time.Millisecond || !h.Known {
		t.Fatal(h, err)
	}
}
