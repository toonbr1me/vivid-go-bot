package bot

import (
	"context"
	"encoding/json"
	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
	"strings"
	"testing"
)

type miniAppCaller struct {
	target  string
	changes int
}

func (c *miniAppCaller) Call(_ context.Context, endpoint string, data *telegoapi.RequestData) (*telegoapi.Response, error) {
	result := json.RawMessage(`true`)
	if strings.HasSuffix(endpoint, "getChatMenuButton") {
		result, _ = json.Marshal(map[string]any{"type": "web_app", "text": "Кабинет", "web_app": map[string]string{"url": c.target}})
	} else if strings.HasSuffix(endpoint, "setChatMenuButton") {
		var p struct {
			Menu struct {
				WebApp struct {
					URL string `json:"url"`
				} `json:"web_app"`
			} `json:"menu_button"`
		}
		if err := json.Unmarshal(data.BodyRaw, &p); err != nil {
			return nil, err
		}
		c.target = p.Menu.WebApp.URL
		c.changes++
	}
	return &telegoapi.Response{Ok: true, Result: result}, nil
}
func TestMiniAppMenuRepairsOldDomainWithoutRepeatedWrites(t *testing.T) {
	caller := &miniAppCaller{target: "https://obsolete.example/app"}
	api, err := telego.NewBot("123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", telego.WithAPICaller(caller))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Settings{SiteEnabled: true, SiteBaseURL: "https://vivid.example/"}
	b := New(api, nil, cfg, nil)
	for i := 0; i < 2; i++ {
		if err = b.syncMiniAppMenu(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if caller.changes != 1 || caller.target != "https://vivid.example/app" {
		t.Fatal(caller)
	}
	cfg.SiteBaseURL = "https://new.example"
	if err = b.syncMiniAppMenu(context.Background()); err != nil {
		t.Fatal(err)
	}
	if caller.changes != 2 || caller.target != "https://new.example/app" {
		t.Fatal(caller)
	}
	cfg.SiteBaseURL = "http://localhost:8080"
	if miniAppURL(cfg) != "" {
		t.Fatal("insecure menu URL")
	}
}
