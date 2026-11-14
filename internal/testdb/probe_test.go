package testdb

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestSearchPathIsApplied(t *testing.T) {
	url := URL()
	if url == "" {
		t.Skip("нет тестовой базы")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, withSearchPath(url, "probe_test"))
	if err != nil {
		t.Fatalf("подключение: %v", err)
	}
	defer conn.Close(ctx)
	var path string
	if err := conn.QueryRow(ctx, `SHOW search_path`).Scan(&path); err != nil {
		t.Fatalf("search_path: %v", err)
	}
	t.Logf("search_path = %q", path)
	if path != "probe_test" {
		t.Errorf("search_path = %q, ждали probe_test", path)
	}
}
