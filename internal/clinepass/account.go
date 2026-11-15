package clinepass

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net/http"
	"strings"

	"github.com/berry-shake/cpa-provider-core/jsonfield"
	"github.com/berry-shake/cpa-provider-core/upstream"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// AccountInfo is the plan usage of the Cline account behind one key. It is
// account-wide: every key of an account reports the same figures, which also
// include the account's use in other clients.
type AccountInfo struct {
	// Account identifies the account (a hash of Cline's user ID), so the
	// management page counts an account once however many keys it has.
	// Empty when the plan could not be read.
	Account string `json:"account,omitempty"`
	// Plan is the subscription; nil when the plan query failed (PlanError).
	Plan      *PlanInfo `json:"plan,omitempty"`
	PlanError string    `json:"plan_error,omitempty"`
	// Windows are the usage windows in Cline's order: five_hour, weekly and
	// monthly today. Error is set instead when the usage query failed.
	Windows   []Window `json:"windows,omitempty"`
	Error     string   `json:"error,omitempty"`
	FetchedAt string   `json:"fetched_at"`
}

// PlanInfo is the account's plan from GET /users/me/plan.
type PlanInfo struct {
	Name string `json:"name,omitempty"`
	// PeriodEnd ends the current billing period (RFC 3339).
	PeriodEnd string `json:"period_end,omitempty"`
}

// Window is one usage window from GET /users/me/plan/usage-limits.
type Window struct {
	// Type is Cline's name for the window: five_hour, weekly or monthly.
	Type string `json:"type"`
	// PercentUsed is how much of the window is used, 0 to 100; nil when
	// Cline reports no figure, which is not the same as no use.
	PercentUsed *float64 `json:"percent_used,omitempty"`
	ResetsAt    string   `json:"resets_at,omitempty"`
}

// Left is the share of the window still available, 0 to 1; ok is false
// without a figure.
func (w Window) Left() (left float64, ok bool) {
	if w.PercentUsed == nil {
		return 0, false
	}
	return math.Max(0, math.Min(1, 1-*w.PercentUsed/100)), true
}

// QuotaWindow names a Cline window the way CPA's quota panel does: 5h, 7d,
// monthly; other names are kept.
func QuotaWindow(windowType string) string {
	switch windowType {
	case "five_hour":
		return "5h"
	case "weekly":
		return "7d"
	default:
		return windowType
	}
}

// accountData unwraps a Cline account answer {"success":true,"data":{...}}.
// success:false is an upstream error with Cline's message; an answer
// without a data object is an invalid response.
func accountData(raw map[string]any, name, prefix string) (map[string]any, *pluginabi.Error) {
	if success, ok := raw["success"].(bool); ok && !success {
		message := errorText(raw)
		if message == "" {
			message = name + " failed"
		}
		return nil, &pluginabi.Error{Code: "upstream_error", Message: prefix + message, Retryable: true, HTTPStatus: http.StatusBadGateway}
	}
	data, ok := raw["data"].(map[string]any)
	if !ok {
		return nil, &pluginabi.Error{Code: "invalid_upstream_response", Message: prefix + "decode " + name + ": no data object", HTTPStatus: http.StatusBadGateway}
	}
	return data, nil
}

// errorText reads the message of an error answer: {"error":"..."},
// {"error":{"message":"..."}} or {"message":"..."}.
func errorText(raw map[string]any) string {
	switch value := raw["error"].(type) {
	case string:
		return strings.TrimSpace(value)
	case map[string]any:
		if message := jsonfield.String(value, "message"); message != "" {
			return message
		}
	}
	return jsonfield.String(raw, "message")
}

// WindowsFrom reads the usage-limits data: {"limits":[{"type","percentUsed",
// "resetsAt"}]}. ok is false when the list is missing. A negative percentage
// is no figure.
func WindowsFrom(data map[string]any) ([]Window, bool) {
	limits, ok := data["limits"].([]any)
	if !ok {
		return nil, false
	}
	windows := make([]Window, 0, len(limits))
	for _, item := range limits {
		fields, isObject := item.(map[string]any)
		if !isObject {
			continue
		}
		window := Window{Type: jsonfield.String(fields, "type"), ResetsAt: jsonfield.String(fields, "resetsAt", "resets_at")}
		if window.Type == "" {
			continue
		}
		if percent, known := upstream.OptionalNumber(fields["percentUsed"]); known && percent >= 0 {
			percent = math.Min(100, percent)
			window.PercentUsed = &percent
		}
		windows = append(windows, window)
	}
	return windows, true
}

// PlanFrom reads the plan data: {"userId","currentPeriodEnd",
// "plan":{"displayName"}}. The user ID only names the account (hashed).
func PlanFrom(data map[string]any) (*PlanInfo, string) {
	info := &PlanInfo{PeriodEnd: jsonfield.String(data, "currentPeriodEnd")}
	if plan, ok := data["plan"].(map[string]any); ok {
		info.Name = jsonfield.String(plan, "displayName", "name")
	}
	return info, AccountFingerprint(jsonfield.String(data, "userId"))
}

// AccountFingerprint names the account behind a Cline user ID without
// keeping the ID; empty for an empty ID.
func AccountFingerprint(userID string) string {
	if userID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("clinepass-account\x00" + userID))
	return hex.EncodeToString(sum[:])[:16]
}

// QuotaFromAccount renders account info for CPA's quota panel: the plan name
// and one bar per window with a figure (5h, 7d, monthly).
func QuotaFromAccount(info AccountInfo) pluginapi.QuotaFetchResponse {
	var out pluginapi.QuotaFetchResponse
	if info.Plan != nil && info.Plan.Name != "" {
		out.Subscription = &pluginapi.QuotaSubscription{Plan: info.Plan.Name}
	}
	var buckets []pluginapi.QuotaBucket
	for _, window := range info.Windows {
		left, ok := window.Left()
		if !ok {
			continue
		}
		buckets = append(buckets, pluginapi.QuotaBucket{Window: QuotaWindow(window.Type), RemainingFraction: left, ResetTime: window.ResetsAt})
	}
	if len(buckets) > 0 {
		out.Groups = []pluginapi.QuotaGroup{{DisplayName: DisplayName, Buckets: buckets}}
	}
	return out
}
