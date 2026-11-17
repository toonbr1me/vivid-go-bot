"""Exercise the built application's HTTP API on deploy/verification only."""
import hashlib
import http.cookies
import json
import os
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def sql(statement):
    result = subprocess.run(
        ["psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c", statement],
        check=True, capture_output=True, text=True,
    )
    return result.stdout.strip().splitlines()[0] if result.stdout.strip() else ""


class Browser:
    def __init__(self, token=None):
        self.cookies = {"vivid_session": token} if token else {}
        self.opener = urllib.request.build_opener(NoRedirect())

    def request(self, path, expected=200, form=None, headers=None):
        request_headers = dict(headers or {})
        request_headers["Cookie"] = "; ".join(f"{k}={v}" for k, v in self.cookies.items())
        if form is not None:
            form = dict(form, csrf=self.cookies["vivid_csrf"])
            data = urllib.parse.urlencode(form).encode()
        else:
            data = None
        request = urllib.request.Request("http://app:8080" + path, data=data, headers=request_headers)
        try:
            response = self.opener.open(request, timeout=20)
        except urllib.error.HTTPError as exc:
            response = exc
        body = response.read().decode("utf-8")
        for raw in response.headers.get_all("Set-Cookie", []):
            cookie = http.cookies.SimpleCookie(raw)
            self.cookies.update({k: v.value for k, v in cookie.items()})
        if response.code != expected:
            raise AssertionError(f"{path}: got {response.code}, expected {expected}: {body[:300]}")
        print(f"HTTP {response.code} {path}", flush=True)
        return body


def account(telegram_id, label):
    return int(sql(
        f"INSERT INTO accounts(telegram_id,name,ref_code) VALUES({telegram_id},'{label}','verify-{telegram_id}') "
        "ON CONFLICT(telegram_id) DO UPDATE SET name=excluded.name RETURNING id"
    ))


def browser(account_id, label):
    token = f"verification-{label}-{time.time_ns()}"
    digest = hashlib.sha256(token.encode()).hexdigest()
    sql(f"INSERT INTO web_sessions(token_hash,account_id,expires_at) VALUES('{digest}',{account_id},now()+interval '1 hour')")
    client = Browser(token)
    client.request("/login", 303)
    client.request("/account")
    return client


def main():
    if os.environ.get("PGDATABASE") != "vivid_app" or os.environ.get("PGHOST") != "db":
        raise RuntimeError("Only the isolated verification database is supported")
    guest = Browser()
    guest.request("/healthz")
    guest.request("/readyz")
    guest.request("/metrics", 404)
    guest.request("/metrics", headers={"Authorization": "Bearer verification_metrics_only"})
    for path in ["/", "/buy", "/connect", "/support", "/terms", "/privacy"]:
        guest.request(path)
    guest.request("/gifts", 303)
    guest.request("/history", 303)

    owner_id = account(100, "Verification owner")
    support_id = account(200, "Verification support")
    nonce = time.time_ns() % 1_000_000_000 + 1_000_000
    buyer_id = account(nonce, "Verification buyer")
    recipient_id = account(nonce + 1, "Verification recipient")
    sql("INSERT INTO staff(telegram_id,role,display_name,is_active) VALUES(200,'support','Verification support',true) "
        "ON CONFLICT(telegram_id) DO UPDATE SET role='support',is_active=true")
    sql(f"INSERT INTO user_balance(subject_id,telegram_id,balance) VALUES({buyer_id},{nonce},300)")
    owner, support, buyer, recipient = [
        browser(i, label) for i, label in [
            (owner_id, "owner"), (support_id, "support"),
            (buyer_id, "buyer"), (recipient_id, "recipient"),
        ]
    ]
    for path in ["/admin", "/admin/plans", "/admin/settings", "/admin/staff", "/admin/withdrawals",
                 "/admin/refunds", "/admin/operations", "/admin/orders", f"/admin/customers/{buyer_id}"]:
        owner.request(path)
    for path in ["/admin/promos", "/admin/partners", "/admin/orders", "/admin/settings", "/admin/operations"]:
        support.request(path, 403)
    support.request("/admin/tickets")

    owner.request("/admin/plans", 303, form={
        "id": "verify_gift", "title": "Docker gift", "days": 45, "price": 100,
        "devices": 3, "traffic": "10GB", "active": "on", "gifts": "on", "sort": 1,
    })
    purchase = {"action": "buy", "plan": "verify_gift", "option": "balance", "request_id": f"docker-purchase-{nonce}"}
    buyer.request("/gifts", 303, form=purchase)
    buyer.request("/gifts", 303, form=purchase)
    assert sql(f"SELECT balance::int FROM user_balance WHERE subject_id={buyer_id}") == "200"
    assert sql(f"SELECT count(*) FROM subscription_gifts WHERE buyer_id={buyer_id}") == "1"
    gift = sql(f"SELECT id FROM subscription_gifts WHERE buyer_id={buyer_id}")
    buyer.request("/gifts", 409, form={"action": "claim", "code": gift})
    recipient.request("/gifts", 303, form={"action": "claim", "code": gift})
    recipient.request("/gifts", 303, form={"action": "claim", "code": gift})
    support.request("/gifts", 409, form={"action": "claim", "code": gift})
    assert sql(f"SELECT count(*) FROM orders WHERE account_id={recipient_id} AND provider='gift_claim'") == "1"
    buyer.request("/gifts")
    buyer.request("/history")

    ticket = int(sql(f"INSERT INTO support_tickets(subject_id,telegram_id,subject) VALUES({buyer_id},{nonce},'Docker support') RETURNING id"))
    sql(f"INSERT INTO support_messages(ticket_id,subject_id,message) VALUES({ticket},{buyer_id},'Client issue')")
    path = f"/support/tickets/{ticket}"
    owner.request(path + "/staff", 303, form={"action": "take"})
    owner.request(path + "/staff", 303, form={"action": "note", "message": "docker-private-note"})
    support.request(path + "/staff", 409, form={"action": "reply", "message": "Wrong holder"})
    assert "docker-private-note" in owner.request(path)
    assert "docker-private-note" not in buyer.request(path)
    owner.request(path + "/staff", 303, form={"action": "transfer", "target": 200})
    support.request(path + "/staff", 303, form={"action": "close"})
    support.request(path + "/staff", 303, form={"action": "reopen"})

    sql(f"INSERT INTO orders(order_id,account_id,provider,plan_id,kind,amount,currency,status,created_at) "
        f"SELECT 'docker-{nonce}-'||n,{buyer_id},'wata','30d','subscription',1,'RUB','pending',now()-n*interval '1 hour' FROM generate_series(1,170)n")
    assert f"docker-{nonce}-170" in owner.request(f"/admin/orders?q=docker-{nonce}-170")
    owner.request("/admin/settings", 303, form={"TRIAL_BOT_DAYS": 4})
    assert "4" in owner.request("/admin/settings")
    owner.request("/admin/campaigns")
    support.request("/admin/campaigns", 403)
    campaign_title = f"Docker campaign {nonce}"
    owner.request("/admin/campaigns", 303, form={
        "revision": 0, "title": campaign_title, "audience": "expired",
        "message": "Docker campaign preview", "daily_limit": 2,
        "cooldown_days": 0, "send_from": 0, "send_to": 24,
    })
    campaign = sql(f"SELECT id FROM campaigns WHERE title='{campaign_title}'")
    assert sql(f"SELECT enabled FROM campaigns WHERE id='{campaign}'") == "f"
    assert "Docker campaign preview" in owner.request(f"/admin/campaigns/{campaign}")
    owner.request(f"/admin/campaigns/{campaign}", 303, form={"action": "enable", "revision": 1})
    assert sql(f"SELECT enabled FROM campaigns WHERE id='{campaign}'") == "t"
    owner.request(f"/admin/campaigns/{campaign}", 303, form={"action": "pause", "revision": 2})
    assert sql(f"SELECT enabled FROM campaigns WHERE id='{campaign}'") == "f"
    owner.request(f"/admin/campaigns/{campaign}", 409, form={"action": "enable", "revision": 1})
    print(json.dumps({"http_smoke": "passed", "buyer_id": buyer_id, "recipient_id": recipient_id,
                      "ticket_id": ticket, "gift_claim_orders": 1, "balance_after_replay": 200,
                      "campaign_preview_enable_pause": "passed"}), flush=True)


if __name__ == "__main__":
    main()
