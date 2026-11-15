package clinepass

import (
	"net/http"
	"strings"
	"testing"

	"github.com/berry-shake/cpa-provider-core/provider"
)

func TestEveryStreamGetsTheFix(t *testing.T) {
	if _, ok := New().StreamFix(provider.Chat, provider.CallInput{}).(errorFix); !ok {
		t.Fatal("StreamFix does not return errorFix")
	}
}

func TestSuccessFailure(t *testing.T) {
	fix := errorFix{}
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
		t.Error("errorFix holds events back")
	}
}

func TestChoiceFailure(t *testing.T) {
	fix := errorFix{}
	cases := []struct {
		name string
		in   string
		want []string // nil: passed on unchanged
	}{
		{
			name: "documented mid-stream error",
			in:   "data: {\"id\":\"gen-1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"error\",\"error\":{\"code\":\"rate_limit\",\"message\":\"Rate limit hit during generation\"}}]}\n\n",
			want: []string{"data: {\"error\":{\"code\":\"rate_limit\",\"message\":\"Rate limit hit during generation\"}}\n\n"},
		},
		{
			name: "output and usage in the failing chunk go first",
			in:   "data: {\"id\":\"gen-1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"cut\"},\"finish_reason\":\"error\",\"error\":{\"code\":\"server_error\",\"message\":\"Upstream provider failed\"}}],\"usage\":{\"completion_tokens\":1}}\n\n",
			want: []string{
				"data: {\"choices\":[{\"delta\":{\"content\":\"cut\"},\"finish_reason\":null,\"index\":0}],\"id\":\"gen-1\",\"usage\":{\"completion_tokens\":1}}\n\n",
				"data: {\"error\":{\"code\":\"server_error\",\"message\":\"Upstream provider failed\"}}\n\n",
			},
		},
		{
			name: "finish_reason error without an error object",
			in:   "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":\"error\"}]}\n\n",
			want: []string{"data: {\"error\":{\"message\":\"ClinePass stream ended with finish_reason error\"}}\n\n"},
		},
		{
			name: "bare error message",
			in:   "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"error\",\"error\":\"context too long\"}]}\n\n",
			want: []string{"data: {\"error\":{\"message\":\"context too long\"}}\n\n"},
		},
		{name: "normal stop", in: "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"},
		{name: "the word error in the text", in: "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\\\"error\\\" is a word\"}}]}\n\n"},
		{name: "a key named error elsewhere", in: "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"},\"logprobs\":{\"error\":1}}]}\n\n"},
		{name: "top-level error is the gate's", in: "data: {\"error\":{\"message\":\"already an error\"}}\n\n"},
	}
	for _, tc := range cases {
		out := fix.Event([]byte(tc.in))
		want := tc.want
		if want == nil {
			want = []string{tc.in}
		}
		got := make([]string, len(out))
		for i, event := range out {
			got[i] = string(event)
		}
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, want)
		}
	}
}

func TestDecorateAsksForOpenAIShape(t *testing.T) {
	header := http.Header{}
	New().Decorate(header, provider.CallInput{})
	if got := header.Get("X-Response-Format"); got != "openai" {
		t.Errorf("X-Response-Format = %q", got)
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
