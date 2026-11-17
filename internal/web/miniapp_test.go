package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/toonbr1me/vivid-go-bot/internal/domain"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

const miniAppTestToken = "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func signMiniApp(v url.Values, token string) string {
	var keys, parts []string
	for k := range v {
		if k != "hash" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, k+"="+v.Get(k))
	}
	key := hmac.New(sha256.New, []byte("WebAppData"))
	key.Write([]byte(token))
	mac := hmac.New(sha256.New, key.Sum(nil))
	mac.Write([]byte(strings.Join(parts, "\n")))
	v.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return v.Encode()
}
func miniAppValues(now time.Time) url.Values {
	return url.Values{"auth_date": {fmt.Sprint(now.Unix())}, "user": {`{"id":777,"first_name":"Клиент"}`}, "query_id": {"signed-query"}, "signature": {"signed-extra-field"}}
}
func TestMiniAppSignatureAndFreshness(t *testing.T) {
	now := time.Now()
	valid := signMiniApp(miniAppValues(now), miniAppTestToken)
	user, _, err := validateMiniAppData(valid, miniAppTestToken, now)
	if err != nil || user.ID != 777 {
		t.Fatal(user, err)
	}
	for name, raw := range map[string]string{"tampered": strings.Replace(valid, "777", "778", 1), "duplicate": valid + "&user=%7B%22id%22%3A778%7D", "old": signMiniApp(miniAppValues(now.Add(-2*time.Hour)), miniAppTestToken), "future": signMiniApp(miniAppValues(now.Add(time.Minute)), miniAppTestToken), "other-bot": signMiniApp(miniAppValues(now), miniAppTestToken+"x"), "malformed": "user=%zz"} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := validateMiniAppData(raw, miniAppTestToken, now); err == nil {
				t.Fatal("untrusted identity accepted")
			}
		})
	}
}
func TestMiniAppLoginUsesSignedAccountAndPartitionedCookies(t *testing.T) {
	s, db := testServer(t)
	s.cfg.TelegramToken = miniAppTestToken
	ctx := context.Background()
	signed, err := db.EnsureTelegramSubject(ctx, 777)
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.EnsureTelegramSubject(ctx, 888)
	if err != nil {
		t.Fatal(err)
	}
	otherCookie := sessionCookieFor(t, db, other.ID)
	form := url.Values{"csrf": {"csrf-test"}, "init_data": {signMiniApp(miniAppValues(time.Now()), miniAppTestToken)}, "next": {"https://evil.invalid/"}, "account_id": {fmt.Sprint(other.ID)}}
	request := func(csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "https://example.com/auth/telegram", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.AddCookie(otherCookie)
		if csrf {
			r.AddCookie(&http.Cookie{Name: csrfCookie, Value: "csrf-test"})
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, r)
		return rec
	}
	if rec := request(false); rec.Code != 403 {
		t.Fatal("missing csrf", rec.Code)
	}
	rec := request(true)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"next":"/account"`) {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == miniAppSessionCookie {
			cookie = c
		}
	}
	if cookie == nil || !cookie.Secure || !cookie.HttpOnly || !cookie.Partitioned || cookie.SameSite != http.SameSiteNoneMode {
		t.Fatalf("unsafe app cookie %+v", cookie)
	}
	actual, err := db.SubjectBySession(ctx, hashToken(cookie.Value))
	if err != nil || actual.ID != signed.ID {
		t.Fatal("wrong account", actual, err)
	}
	original, err := db.SubjectBySession(ctx, hashToken(otherCookie.Value))
	if err != nil || original.ID != other.ID {
		t.Fatal("browser account changed", err)
	}
	r := httptest.NewRequest("GET", "https://example.com/account", nil)
	r.AddCookie(cookie)
	page := httptest.NewRecorder()
	s.Handler().ServeHTTP(page, r)
	if page.Code != 200 {
		t.Fatal("app session lost", page.Code)
	}
	form.Set("init_data", strings.Replace(form.Get("init_data"), "777", "778", 1))
	if bad := request(true); bad.Code != 401 {
		t.Fatal("tampered auth", bad.Code)
	}
}
func TestMiniAppFramePolicyAndEntry(t *testing.T) {
	s, _ := testServer(t)
	for _, path := range []string{"/app", "/account", "/admin/campaigns"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "https://example.com"+path, nil))
		csp := rec.Header().Get("Content-Security-Policy")
		if path == "/admin/campaigns" {
			if !strings.Contains(csp, "frame-ancestors 'self';") || rec.Header().Get("X-Frame-Options") != "SAMEORIGIN" {
				t.Fatal("admin embedding allowed")
			}
			continue
		}
		if !strings.Contains(csp, "frame-ancestors 'self' https://web.telegram.org") || rec.Header().Get("X-Frame-Options") != "" {
			t.Fatal("Telegram Web iframe blocked", csp)
		}
		if path == "/app" {
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), "miniapp-auth") || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("entry missing")
			}
			for _, c := range rec.Result().Cookies() {
				if c.Name == csrfCookie && (!c.Partitioned || c.SameSite != http.SameSiteNoneMode) {
					t.Fatal("iframe csrf blocked")
				}
			}
		}
	}
}
func TestMiniAppPaymentOpensGatewayOutsideTelegramFrame(t *testing.T) {
	s, _ := testServer(t)
	for _, app := range []bool{false, true} {
		r := httptest.NewRequest("POST", "https://example.com/buy", nil)
		if app {
			r.AddCookie(&http.Cookie{Name: miniAppSessionCookie, Value: "signed-session"})
		}
		rec := httptest.NewRecorder()
		s.finishCheckout(rec, r, domain.Checkout{URL: "https://gateway.example/pay"})
		if app {
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), "data-miniapp-external") || rec.Header().Get("Location") != "" {
				t.Fatal("gateway embedded")
			}
		} else if rec.Code != 303 {
			t.Fatal("browser checkout changed")
		}
	}
}
func TestLegacyPromoCanBeUsedOnBuyPage(t *testing.T) {
	s, db := testServer(t)
	ctx := context.Background()
	subject, err := db.EnsureTelegramSubject(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SavePlan(ctx, domain.Plan{ID: "30d", Title: "Месяц", Days: 30, Price: 10000, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO promocodes(code,discount_percent,reward_type) VALUES('LEGACY35',35,NULL)`); err != nil {
		t.Fatal(err)
	}
	if err = db.RememberPromoIntent(ctx, subject.ID, "LEGACY35"); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/buy", nil)
	r.AddCookie(sessionCookieFor(t, db, subject.ID))
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "scan") || strings.Contains(rec.Body.String(), "Код не подходит") {
		t.Fatal(rec.Code, rec.Body.String())
	}
	promos, err := db.ListPromos(ctx, 10)
	if err != nil || len(promos) != 1 || promos[0].RewardType != "" {
		t.Fatal("promo list", promos, err)
	}
	quotes, err := s.pay.QuotePlans(ctx, subject.ID, "LEGACY35")
	if err != nil || len(quotes) != 1 || quotes[0].Amount != 6500 {
		t.Fatal("wrong discount", quotes, err)
	}
}
