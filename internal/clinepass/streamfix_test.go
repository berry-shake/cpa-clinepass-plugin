package clinepass

import (
	"net/http"
	"testing"

	"github.com/berry-shake/cpa-provider-core/provider"
)

func TestEveryStreamGetsTheFix(t *testing.T) {
	if _, ok := New().StreamFix(provider.Chat, provider.CallInput{}).(successFix); !ok {
		t.Fatal("StreamFix does not return successFix")
	}
}

func TestSuccessFix(t *testing.T) {
	fix := successFix{}
	cases := []struct {
		in, want string
	}{
		{"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"success is false\"}}]}\n\n", ""},
		{"data: {\"success\":false,\"message\":\"Plan limit reached\",\"code\":429}\n\n", "data: {\"error\":{\"code\":429,\"message\":\"Plan limit reached\"}}\n\n"},
		{"event: error\ndata: {\"success\": false}\n\n", "data: {\"error\":{\"message\":\"ClinePass request failed\"}}\n\n"},
		{"data: {\"success\":false,\"error\":{\"message\":\"already an error\"}}\n\n", ""},
		{"data: {\"success\":true,\"data\":false}\n\n", ""},
		{"data: [DONE]\n\n", ""},
	}
	for _, tc := range cases {
		out := fix.Event([]byte(tc.in))
		want := tc.want
		if want == "" {
			want = tc.in
		}
		if len(out) != 1 || string(out[0]) != want {
			t.Errorf("Event(%q) = %q, want %q", tc.in, out, want)
		}
	}
	if fix.Flush() != nil || fix.Close() != nil {
		t.Error("successFix holds events back")
	}
}

func TestRemapError(t *testing.T) {
	a := New()
	if status, message := a.RemapError(http.StatusForbidden, "", `Error 403: client-supplied "providerOptions" model routing is not supported`); status != http.StatusBadRequest || message == "" {
		t.Errorf("providerOptions refusal = %d %q", status, message)
	}
	for _, status := range []int{http.StatusForbidden, http.StatusUnauthorized} {
		if mapped, _ := a.RemapError(status, "", "Forbidden"); mapped != 0 {
			t.Errorf("%d remapped to %d", status, mapped)
		}
	}
}
