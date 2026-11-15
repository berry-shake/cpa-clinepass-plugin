package clinepass

import (
	"bytes"
	"encoding/json"

	"github.com/berry-shake/cpa-provider-core/jsonfield"
	"github.com/berry-shake/cpa-provider-core/provider"
	"github.com/berry-shake/cpa-provider-core/streamgate"
)

// Cline reports a failure inside a stream in two ways the gate does not read
// as one:
//
//   - its own envelope, {"success":false,"message":...}, instead of an OpenAI
//     error object;
//   - the documented mid-stream error (https://docs.cline.bot/api/errors): a
//     chunk whose choice has finish_reason "error" and an error object
//     {"code","message"}. The gate would take the finish reason for a normal
//     end, log a success and pass a cut-off answer on.
//
// The stream fix rewrites both to {"error":{"message","code"}} so the gate
// reads them as the errors they are (before any output it fails the call and
// CPA tries another key; aggregated calls fail either way). The code words
// map as usual: rate_limit to 429, context_length_exceeded to 400, the rest
// (server_error, content_filter) to 502.

// StreamFix repairs every stream (Chat Completions is the only protocol).
func (a *Adapter) StreamFix(provider.Protocol, provider.CallInput) provider.StreamFix {
	return errorFix{}
}

type errorFix struct{}

// Event passes events on, rewriting the two failure forms. Events that
// cannot be one are not decoded.
func (errorFix) Event(event []byte) [][]byte {
	if bytes.Contains(event, []byte(`"success"`)) && bytes.Contains(event, []byte("false")) {
		if out, ok := successFailure(event); ok {
			return out
		}
	}
	if bytes.Contains(event, []byte(`"error"`)) {
		if out, ok := choiceFailure(event); ok {
			return out
		}
	}
	return [][]byte{event}
}

// Flush has nothing held back.
func (errorFix) Flush() [][]byte { return nil }

// Close leaves a stream without its end reported as truncated.
func (errorFix) Close() [][]byte { return nil }

// successFailure rewrites a success:false envelope without an error member.
func successFailure(event []byte) ([][]byte, bool) {
	ev := streamgate.NewEvent(event)
	if !ev.Object() || !bytes.Equal(bytes.TrimSpace(ev.Member("success")), []byte("false")) {
		return nil, false
	}
	if errValue := ev.Member("error"); errValue != nil && string(errValue) != "null" {
		return nil, false
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
	return [][]byte{sseData(map[string]any{"error": failure})}, true
}

// choiceFailure rewrites a chunk with a finish_reason "error" choice. Output
// or usage the chunk carries as well goes first, in a copy without the
// failure, so it still reaches the client and the request log.
func choiceFailure(event []byte) ([][]byte, bool) {
	ev := streamgate.NewEvent(event)
	if !ev.Object() || ev.Member("choices") == nil {
		return nil, false
	}
	payload := ev.Payload()
	choices, _ := payload["choices"].([]any)
	var failure map[string]any
	keep := payload["usage"] != nil
	for _, raw := range choices {
		choice, _ := raw.(map[string]any)
		if choice == nil {
			continue
		}
		if failure == nil && jsonfield.Text(choice["finish_reason"]) == "error" {
			failure = choiceError(choice["error"])
		}
		if jsonfield.Text(choice["finish_reason"]) == "error" {
			choice["finish_reason"] = nil
		}
		delete(choice, "error")
		keep = keep || hasOutput(choice)
	}
	if failure == nil {
		return nil, false
	}
	out := make([][]byte, 0, 2)
	if keep {
		out = append(out, sseData(payload))
	}
	return append(out, sseData(map[string]any{"error": failure})), true
}

// choiceError reads a choice's error: {"code","message"} as documented, or a
// bare message.
func choiceError(raw any) map[string]any {
	failure := map[string]any{}
	switch value := raw.(type) {
	case map[string]any:
		if message := jsonfield.String(value, "message"); message != "" {
			failure["message"] = message
		}
		if code, ok := value["code"]; ok && code != nil {
			failure["code"] = code
		}
	case string:
		if value != "" {
			failure["message"] = value
		}
	}
	if failure["message"] == nil {
		failure["message"] = "ClinePass stream ended with finish_reason error"
	}
	return failure
}

// hasOutput reports whether a choice's delta carries anything besides the
// role.
func hasOutput(choice map[string]any) bool {
	delta, _ := choice["delta"].(map[string]any)
	for key, value := range delta {
		if key == "role" || value == nil {
			continue
		}
		if text, isText := value.(string); isText && text == "" {
			continue
		}
		return true
	}
	return false
}

func sseData(value any) []byte {
	raw, _ := json.Marshal(value)
	return append(append([]byte("data: "), raw...), '\n', '\n')
}
