package clinepass

import (
	"encoding/json"
	"net/http"
	"testing"
)

func decode(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAccountDataUnwraps(t *testing.T) {
	data, perr := accountData(decode(t, `{"success":true,"data":{"limits":[]}}`), EndpointUsageLimits, "clinepass: ")
	if perr != nil || data["limits"] == nil {
		t.Fatalf("data = %v, %+v", data, perr)
	}
	for raw, want := range map[string]string{
		`{"success":false,"error":"Unauthorized"}`:             "clinepass: Unauthorized",
		`{"success":false,"error":{"message":"rate limited"}}`: "clinepass: rate limited",
		`{"success":false,"message":"plan expired"}`:           "clinepass: plan expired",
		`{"success":false}`:                                    "clinepass: " + EndpointUsageLimits + " failed",
	} {
		_, perr := accountData(decode(t, raw), EndpointUsageLimits, "clinepass: ")
		if perr == nil || perr.Message != want || perr.HTTPStatus != http.StatusBadGateway || perr.Code != "upstream_error" {
			t.Errorf("%s: %+v", raw, perr)
		}
	}
	if _, perr := accountData(decode(t, `{"success":true}`), EndpointPlan, "clinepass: "); perr == nil || perr.Code != "invalid_upstream_response" {
		t.Errorf("no data: %+v", perr)
	}
}

func TestWindowsFrom(t *testing.T) {
	windows, ok := WindowsFrom(decode(t, `{"limits":[
		{"type":"five_hour","percentUsed":12.5,"resetsAt":"2026-11-15T09:00:00Z"},
		{"type":"weekly","percentUsed":140},
		{"type":"monthly","percentUsed":-1,"resetsAt":"2026-12-01T00:00:00Z"},
		{"type":"","percentUsed":3},
		"junk"
	]}`))
	if !ok || len(windows) != 3 {
		t.Fatalf("windows = %+v", windows)
	}
	if w := windows[0]; w.Type != "five_hour" || *w.PercentUsed != 12.5 || w.ResetsAt != "2026-11-15T09:00:00Z" {
		t.Errorf("five_hour = %+v", w)
	}
	if left, ok := windows[0].Left(); !ok || left != 0.875 {
		t.Errorf("five_hour left = %v %v", left, ok)
	}
	if w := windows[1]; *w.PercentUsed != 100 {
		t.Errorf("weekly is clamped: %+v", w)
	}
	if w := windows[2]; w.PercentUsed != nil {
		t.Errorf("a negative figure is no figure: %+v", w)
	}
	if _, left := windows[2].Left(); left {
		t.Error("a window without a figure has a share left")
	}
	if _, ok := WindowsFrom(decode(t, `{}`)); ok {
		t.Error("an answer without limits is accepted")
	}
}

func TestPlanFromHashesTheUser(t *testing.T) {
	plan, account := PlanFrom(decode(t, `{"userId":"private-user","subscriptionId":"private-subscription","currentPeriodEnd":"2026-12-02T14:08:10Z","plan":{"displayName":"Cline Pass (Monthly)"}}`))
	if plan.Name != "Cline Pass (Monthly)" || plan.PeriodEnd != "2026-12-02T14:08:10Z" {
		t.Fatalf("plan = %+v", plan)
	}
	if len(account) != 16 || account == "private-user" || account != AccountFingerprint("private-user") || account == AccountFingerprint("other-user") {
		t.Fatalf("account = %q", account)
	}
	if _, empty := PlanFrom(decode(t, `{}`)); empty != "" {
		t.Fatalf("no user ID gives %q", empty)
	}
}

func TestQuotaFromAccount(t *testing.T) {
	used := func(v float64) *float64 { return &v }
	quota := QuotaFromAccount(AccountInfo{
		Plan: &PlanInfo{Name: "Cline Pass"},
		Windows: []Window{
			{Type: "five_hour", PercentUsed: used(20), ResetsAt: "2026-11-15T09:00:00Z"},
			{Type: "weekly", PercentUsed: used(50)},
			{Type: "monthly"},
			{Type: "daily", PercentUsed: used(0)},
		},
	})
	if quota.Subscription == nil || quota.Subscription.Plan != "Cline Pass" || len(quota.Groups) != 1 || quota.Groups[0].DisplayName != DisplayName {
		t.Fatalf("quota = %+v", quota)
	}
	buckets := quota.Groups[0].Buckets
	if len(buckets) != 3 || buckets[0].Window != "5h" || buckets[0].RemainingFraction != 0.8 || buckets[0].ResetTime != "2026-11-15T09:00:00Z" || buckets[1].Window != "7d" || buckets[2].Window != "daily" || buckets[2].RemainingFraction != 1 {
		t.Fatalf("buckets = %+v", buckets)
	}
	if empty := QuotaFromAccount(AccountInfo{Windows: []Window{{Type: "monthly"}}}); empty.Subscription != nil || len(empty.Groups) != 0 {
		t.Fatalf("no figures = %+v", empty)
	}
}
