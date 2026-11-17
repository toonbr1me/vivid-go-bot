package web

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWebCampaignRequiresReviewAndRejectsStaleEnable(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	actor, err := db.EnsureTelegramSubject(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	token, cookies := csrfFrom(t, handler)
	cookies = append(cookies, sessionCookieFor(t, db, actor.ID))
	form := url.Values{"csrf": {token}, "revision": {"0"}, "title": {"Trial conversion"}, "audience": {"trial_only"}, "message": {"<script>literal text</script>"}, "daily_limit": {"10"}, "cooldown_days": {"0"}, "send_from": {"9"}, "send_to": {"21"}}
	response := postForm(t, handler, "/admin/campaigns", form, cookies)
	if response.Code != 303 {
		t.Fatalf("save %d %s", response.Code, response.Body.String())
	}
	path := response.Header().Get("Location")
	id := strings.TrimPrefix(path, "/admin/campaigns/")
	campaign, err := db.Campaign(ctx, id)
	if err != nil || campaign.Enabled {
		t.Fatalf("draft enabled %+v %v", campaign, err)
	}
	request := httptest.NewRequest("GET", path, nil)
	request.AddCookie(cookies[len(cookies)-1])
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "Включить автоматическую отправку") || strings.Contains(response.Body.String(), "<script>literal text</script>") {
		t.Fatal("preview missing or text not escaped")
	}
	enable := url.Values{"csrf": {token}, "revision": {"1"}, "action": {"enable"}}
	response = postForm(t, handler, path, enable, cookies)
	if response.Code != 303 {
		t.Fatalf("enable %d %s", response.Code, response.Body.String())
	}
	campaign, err = db.Campaign(ctx, id)
	if err != nil || !campaign.Enabled {
		t.Fatal("not enabled", err)
	}
	campaign.Text = "Revised message"
	campaign, err = db.SaveCampaign(ctx, campaign, 100)
	if err != nil {
		t.Fatal(err)
	}
	response = postForm(t, handler, path, enable, cookies)
	if response.Code != 409 {
		t.Fatalf("stale version enabled: %d", response.Code)
	}
	campaign, err = db.Campaign(ctx, id)
	if err != nil || campaign.Enabled {
		t.Fatal("edited campaign sent without review")
	}
}

func TestCampaignPagesRejectSupportAndRequireCSRF(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	if err := db.AddStaff(ctx, 200, "support", "Agent", 100); err != nil {
		t.Fatal(err)
	}
	agent, err := db.EnsureTelegramSubject(ctx, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/admin/campaigns", "/admin/campaigns/unknown"} {
		request := httptest.NewRequest("GET", path, nil)
		request.AddCookie(sessionCookieFor(t, db, agent.ID))
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, request)
		if response.Code != 403 {
			t.Fatalf("support access: %s %d", path, response.Code)
		}
	}
	owner, err := db.EnsureTelegramSubject(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/admin/campaigns", strings.NewReader("action=enable"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(sessionCookieFor(t, db, owner.ID))
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal("CSRF bypass", response.Code)
	}
}
