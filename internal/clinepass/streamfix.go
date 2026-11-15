package clinepass

import (
	"bytes"
	"encoding/json"

	"github.com/berry-shake/cpa-provider-core/provider"
	"github.com/berry-shake/cpa-provider-core/streamgate"
)

// Cline can end a stream with its own failure envelope,
// {"success":false,"message":...}, instead of an OpenAI error object. The
// stream fix rewrites such an event to {"error":{"message","code"}} so the
// gate reads it as the error it is (before any output it fails the call and
// CPA tries another key; aggregated calls fail either way).

// StreamFix repairs every stream (Chat Completions is the only protocol).
func (a *Adapter) StreamFix(provider.Protocol, provider.CallInput) provider.StreamFix {
	return successFix{}
}

type successFix struct{}

// Event passes events on, rewriting a success:false envelope without an
// error member. Events that cannot carry one are not decoded.
func (successFix) Event(event []byte) [][]byte {
	if !bytes.Contains(event, []byte(`"success"`)) || !bytes.Contains(event, []byte("false")) {
		return [][]byte{event}
	}
	ev := streamgate.NewEvent(event)
	if !ev.Object() || !bytes.Equal(bytes.TrimSpace(ev.Member("success")), []byte("false")) {
		return [][]byte{event}
	}
	if errValue := ev.Member("error"); errValue != nil && string(errValue) != "null" {
		return [][]byte{event}
	}
	payload := ev.Payload()
	message := errorText(payload)
	if message == "" {
		message = "ClinePass request failed"
	}
	failure := map[string]any{"message": message}
	if code, ok := payload["code"]; ok && code != nil {
		failure["code"] = code
	}
	raw, _ := json.Marshal(map[string]any{"error": failure})
	return [][]byte{append(append([]byte("data: "), raw...), '\n', '\n')}
}

// Flush has nothing held back.
func (successFix) Flush() [][]byte { return nil }

// Close leaves a stream without its end reported as truncated.
func (successFix) Close() [][]byte { return nil }
