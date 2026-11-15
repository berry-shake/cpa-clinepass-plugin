package clinepass_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

// snapshotCases generates the inputs of fixtures that do not exist yet
// (go test ./internal/clinepass -run TestSnapshots -update). Once a fixture
// is written it is the source of truth; editing a case here does not change
// an existing fixture, delete the file to regenerate it.

const (
	snapshotVersion = "9.9.9-snapshot"

	// Fake ClinePass keys; none of them is real.
	fakeKey1 = "cline_test_fake_key_0001"
	fakeKey2 = "cline_test_fake_key_0002"
	fakeKey3 = "cline_test_fake_key_0003"

	pathChat    = "/api/v1/chat/completions"
	pathCatalog = "/api/v1/ai/cline/recommended-models"
	pathLimits  = "/api/v1/users/me/plan/usage-limits"
	pathPlan    = "/api/v1/users/me/plan"

	mgmtBase = "/v0/management/plugins/clinepass"
)

type snapshotCase struct {
	name        string
	description string
	// config is the plugin config object; nil means snapshotConfig().
	config      map[string]any
	credentials []caseCredential
	script      []snapshotReply
	steps       []caseStep
}

// caseCredential is an auth file seeded in the fake host; index must be the
// auth index the host gives it (idx-<position>).
type caseCredential struct {
	name   string
	index  string
	fields map[string]any
}

func (c caseCredential) raw() []byte {
	raw, _ := json.Marshal(c.fields)
	return raw
}

func (c caseCredential) metadata() map[string]any {
	out := make(map[string]any, len(c.fields))
	for key, value := range c.fields {
		out[key] = value
	}
	return out
}

type caseStep struct {
	method  string
	request any
}

func (c snapshotCase) fixture(t *testing.T) snapshotFixture {
	t.Helper()
	config := c.config
	if config == nil {
		config = snapshotConfig(nil)
	}
	fixture := snapshotFixture{
		Case:           c.name,
		Description:    c.description,
		Setup:          snapshotSetup{Version: snapshotVersion, Config: caseJSON(t, config), Credentials: []snapshotCredential{}},
		UpstreamScript: []snapshotReply{},
		Steps:          []snapshotStep{},
	}
	for i, cred := range c.credentials {
		if want := fmt.Sprintf("idx-%d", i+1); cred.index != want {
			t.Fatalf("case %s: credential %s has index %s, want %s", c.name, cred.name, cred.index, want)
		}
		fixture.Setup.Credentials = append(fixture.Setup.Credentials, snapshotCredential{Name: cred.name, Fields: caseJSON(t, cred.fields)})
	}
	for _, reply := range c.script {
		if reply.Header == nil {
			reply.Header = http.Header{}
		}
		if reply.Chunks == nil {
			reply.Chunks = []string{}
		}
		fixture.UpstreamScript = append(fixture.UpstreamScript, reply)
	}
	for _, step := range c.steps {
		fixture.Steps = append(fixture.Steps, snapshotStep{RPC: snapshotRPC{Method: step.method, Request: caseJSON(t, step.request)}})
	}
	return fixture
}

func snapshotConfig(extra map[string]any) map[string]any {
	config := map[string]any{
		"enabled":  true,
		"priority": 0,
		"base-url": "{{UPSTREAM}}",
		"data-dir": "{{DATA_DIR}}",
	}
	for key, value := range extra {
		config[key] = value
	}
	return config
}

// RPC request shapes as CPA sends them (CPA's rpc_schema.go).

type cpaExecutorRequest struct {
	pluginapi.ExecutorRequest
	StreamID       string `json:"stream_id,omitempty"`
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type cpaManagementRequest struct {
	pluginapi.ManagementRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type cpaQuotaFetchRequest struct {
	pluginapi.QuotaFetchRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

const hostCallbackID = "host-callback-1"

func clinepassCred(index int, name string, fields map[string]any) caseCredential {
	all := map[string]any{"type": "clinepass"}
	for key, value := range fields {
		all[key] = value
	}
	return caseCredential{name: name, index: fmt.Sprintf("idx-%d", index), fields: all}
}

func credK1(index int) caseCredential {
	return clinepassCred(index, "clinepass-k1.json", map[string]any{"api_key": fakeKey1})
}

type execOption func(*cpaExecutorRequest)

// execRequest builds an executor request the way CPA does after routing a
// client request with model "clinepass/<model>" to cred. CPA reports the
// requested model in the metadata, so OriginalRequest is not decoded.
func execRequest(cred caseCredential, format, payload string, options ...execOption) cpaExecutorRequest {
	var body struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal([]byte(payload), &body)
	requested := body.Model
	if requested != "" && !strings.Contains(requested, "/") {
		requested = "clinepass/" + requested
	}
	original := strings.Replace(payload, `"model":"`+body.Model+`"`, `"model":"`+requested+`"`, 1)
	paths := map[string]string{"openai": "/v1/chat/completions", "openai-response": "/v1/responses", "claude": "/v1/messages"}
	req := cpaExecutorRequest{
		ExecutorRequest: pluginapi.ExecutorRequest{
			AuthID:       cred.name,
			AuthProvider: "clinepass",
			Model:        body.Model,
			Format:       format,
			Headers: http.Header{
				"Accept":        {"application/json"},
				"Authorization": {"Bearer sk-cpa-client-fake"},
				"Content-Type":  {"application/json"},
				"User-Agent":    {"snapshot-client/1.0"},
			},
			OriginalRequest: []byte(original),
			SourceFormat:    format,
			Payload:         []byte(payload),
			Metadata: map[string]any{
				"request_path":        paths[format],
				"requested_model":     requested,
				"selected_auth_id":    cred.name,
				"selected_auth_index": cred.index,
			},
			StorageJSON:  cred.raw(),
			AuthMetadata: cred.metadata(),
		},
		HostCallbackID: hostCallbackID,
	}
	for _, option := range options {
		option(&req)
	}
	return req
}

func streaming(streamID string) execOption {
	return func(req *cpaExecutorRequest) {
		req.Stream = true
		req.StreamID = streamID
		req.Headers.Set("Accept", "text/event-stream")
	}
}

func mgmtRequest(method, route string, query url.Values, body string) cpaManagementRequest {
	req := cpaManagementRequest{
		ManagementRequest: pluginapi.ManagementRequest{
			Method:  method,
			Path:    route,
			Headers: http.Header{"Accept": {"application/json"}},
			Query:   query,
		},
		HostCallbackID: hostCallbackID,
	}
	if body != "" {
		req.Headers.Set("Content-Type", "application/json")
		req.Body = []byte(body)
	}
	return req
}

func mgmt(method, route string, body string) caseStep {
	return caseStep{method: pluginabi.MethodManagementHandle, request: mgmtRequest(method, route, nil, body)}
}

func mgmtQuery(method, route string, query url.Values) caseStep {
	return caseStep{method: pluginabi.MethodManagementHandle, request: mgmtRequest(method, route, query, "")}
}

func execStep(method string, req cpaExecutorRequest) caseStep {
	return caseStep{method: method, request: req}
}

func modelsForAuthStep(cred caseCredential, force bool) caseStep {
	return caseStep{method: pluginabi.MethodModelForAuth, request: cpaAuthModelRequest{AuthModelRequest: pluginapi.AuthModelRequest{
		AuthID:       cred.name,
		AuthProvider: "clinepass",
		StorageJSON:  cred.raw(),
		Metadata:     cred.metadata(),
		Host:         pluginapi.HostConfigSummary{AuthDir: snapshotAuthDir, ForceModelPrefix: force},
	}, HostCallbackID: hostCallbackID}}
}

func quotaStep(cred caseCredential, withStorage bool) caseStep {
	req := cpaQuotaFetchRequest{QuotaFetchRequest: pluginapi.QuotaFetchRequest{AuthIndex: cred.index, AuthID: cred.name, Provider: "clinepass"}, HostCallbackID: hostCallbackID}
	if withStorage {
		req.StorageJSON = cred.raw()
		req.Metadata = cred.metadata()
	}
	return caseStep{method: pluginabi.MethodQuotaFetch, request: req}
}

func lifecycleStep(method string, schema uint32, config map[string]any) caseStep {
	raw, _ := json.Marshal(config)
	var plain map[string]any
	_ = json.Unmarshal(raw, &plain)
	text, errYAML := yaml.Marshal(plain)
	if errYAML != nil {
		panic(errYAML)
	}
	return caseStep{method: method, request: cpaLifecycleRequest{ConfigYAML: text, SchemaVersion: schema}}
}

func bare(method string) caseStep {
	return caseStep{method: method}
}

func reply(method, path string, status int, contentType, body string) snapshotReply {
	header := http.Header{}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return snapshotReply{Method: method, Path: path, Status: status, Header: header, Body: body}
}

func jsonReply(path string, status int, body string) snapshotReply {
	return reply(http.MethodPost, path, status, "application/json", body)
}

func withReplyHeader(r snapshotReply, name, value string) snapshotReply {
	r.Header = r.Header.Clone()
	r.Header.Set(name, value)
	return r
}

func sseReply(path string, events ...string) snapshotReply {
	r := reply(http.MethodPost, path, http.StatusOK, "text/event-stream", "")
	for _, event := range events {
		r.Chunks = append(r.Chunks, event+"\n\n")
	}
	return r
}

func getReply(path string, status int, body string) snapshotReply {
	return reply(http.MethodGet, path, status, "application/json", body)
}

// Canned bodies, shaped like Cline's answers.

const (
	chatPayload       = `{"model":"kimi-k3","messages":[{"role":"user","content":"Say hello"}],"max_tokens":64,"temperature":0.2}`
	chatFalsePayload  = `{"model":"kimi-k3","messages":[{"role":"user","content":"Say hello"}],"stream":false,"reasoning_effort":"high"}`
	chatToolsPayload  = `{"model":"deepseek-v4.1-flash-1017","messages":[{"role":"user","content":"Read a.go"}],"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}}]}`
	chatStreamPayload = `{"model":"kimi-k3","messages":[{"role":"user","content":"Say hello"}],"max_tokens":64,"stream":true}`
	msgPayload        = `{"model":"kimi-k3","max_tokens":256,"messages":[{"role":"user","content":"Say hello"}]}`
	respPayload       = `{"model":"kimi-k3","input":"Say hello"}`

	catalogBody = `{"recommended":[{"id":"anthropic/claude-sonnet-5.5","name":"claude-sonnet-5.5","description":"","tags":[]}],` +
		`"free":[],` +
		`"clinePass":[` +
		`{"id":"cline-pass/kimi-k3","name":"cline-pass/kimi-k3","description":"Moonshot Kimi K3","tags":["NEW"]},` +
		`{"id":"cline-pass/deepseek-v4.1-flash-1017","name":"cline-pass/deepseek-v4.1-flash-1017","description":"","tags":[]},` +
		`{"id":"cline-pass/glm-5.3","name":"cline-pass/glm-5.3","description":"Z-AI's new top open-weights model","tags":[]},` +
		`{"id":"cline-pass/kimi-k3","name":"duplicate","description":"","tags":[]},` +
		`{"id":"other/model","name":"other","description":"","tags":[]}],` +
		`"clineCloud":[]}`

	limitsBody = `{"success":true,"data":{"limits":[{"type":"five_hour","percentUsed":12.5,"resetsAt":"2026-01-02T07:00:00Z"},{"type":"weekly","percentUsed":40,"resetsAt":"2026-01-05T00:00:00Z"},{"type":"monthly","percentUsed":75,"resetsAt":"2026-01-20T09:30:00Z"}]}}`
	planBody   = `{"success":true,"data":{"userId":"user-fake-1","subscriptionId":"sub-fake-1","currentPeriodEnd":"2026-01-20T09:30:00Z","plan":{"displayName":"Cline Pass (Monthly)"}}}`
)

func chatChunk(delta string, finish string) string {
	choice := `{"index":0,"delta":` + delta
	if finish != "" {
		choice += `,"finish_reason":"` + finish + `"`
	}
	return `data: {"id":"gen-snap-1","object":"chat.completion.chunk","created":1767225600,"model":"cline-pass/kimi-k3","provider":"moonshotai","choices":[` + choice + `}]}`
}

const usageChunk = `data: {"id":"gen-snap-1","object":"chat.completion.chunk","created":1767225600,"model":"cline-pass/kimi-k3","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":6,"total_tokens":17,"prompt_tokens_details":{"cached_tokens":3},"completion_tokens_details":{"reasoning_tokens":2},"cost":0.0001}}`

var (
	chatStreamEvents = []string{
		": OPENROUTER PROCESSING",
		chatChunk(`{"role":"assistant","content":""}`, ""),
		chatChunk(`{"reasoning":"A greeting."}`, ""),
		chatChunk(`{"content":"Hello"}`, ""),
		chatChunk(`{"content":" there!"}`, ""),
		chatChunk(`{}`, "stop"),
		usageChunk,
		"data: [DONE]",
	}
	toolStreamEvents = []string{
		chatChunk(`{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_snap_1","type":"function","function":{"name":"read","arguments":""}}]}`, ""),
		chatChunk(`{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}`, ""),
		chatChunk(`{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\"}"}}]}`, ""),
		chatChunk(`{}`, "tool_calls"),
		usageChunk,
		"data: [DONE]",
	}
)

func snapshotCases() []snapshotCase {
	k1 := credK1(1)
	var cases []snapshotCase
	cases = append(cases, execCases(k1)...)
	cases = append(cases, errorCases(k1)...)
	cases = append(cases, catalogCases()...)
	cases = append(cases, credentialCases()...)
	cases = append(cases, quotaCases()...)
	cases = append(cases, lifecycleCases()...)
	return cases
}

func execCases(k1 caseCredential) []snapshotCase {
	k2 := clinepassCred(2, "clinepass-k2.json", map[string]any{"api_key": fakeKey2, "base_url": "{{UPSTREAM}}/api/v1", "prefix": "cp"})
	return []snapshotCase{
		{
			name:        "exec_chat_nonstream_aggregated",
			description: "A non-stream chat call goes upstream as a stream (stream:true, include_usage, Accept: text/event-stream) and is answered with one chat.completion: reasoning and text joined, usage kept; the request log row is non-stream with TTFT",
			credentials: []caseCredential{k1},
			script:      []snapshotReply{withReplyHeader(sseReply(pathChat, chatStreamEvents...), "X-Request-Id", "cp-req-1")},
			steps: []caseStep{
				execStep(pluginabi.MethodExecutorExecute, execRequest(k1, "openai", chatPayload)),
				mgmt(http.MethodGet, mgmtBase+"/logs", ""),
			},
		},
		{
			name:        "exec_chat_nonstream_tool_calls",
			description: "An aggregated call with a tool call split over chunks; stream:false in the request becomes true, a base_url with /api/v1 and a prefix are honored",
			credentials: []caseCredential{k1, k2},
			script: []snapshotReply{
				sseReply(pathChat, toolStreamEvents...),
				sseReply(pathChat, chatStreamEvents...),
			},
			steps: []caseStep{
				execStep(pluginabi.MethodExecutorExecute, execRequest(k1, "openai", chatToolsPayload)),
				execStep(pluginabi.MethodExecutorExecute, execRequest(k2, "openai", chatFalsePayload)),
			},
		},
		{
			name:        "exec_chat_nonstream_plain_json",
			description: "An upstream that answers an aggregated call with a plain chat.completion is passed through",
			credentials: []caseCredential{k1},
			script:      []snapshotReply{jsonReply(pathChat, 200, `{"id":"gen-snap-2","object":"chat.completion","model":"cline-pass/kimi-k3","choices":[{"index":0,"message":{"role":"assistant","content":"Hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`)},
			steps:       []caseStep{execStep(pluginabi.MethodExecutorExecute, execRequest(k1, "openai", chatPayload))},
		},
		{
			name:        "exec_chat_stream",
			description: "Chat completions over the host stream bridge pass through: include_usage added, keepalive comment dropped, [DONE] kept",
			credentials: []caseCredential{k1},
			script:      []snapshotReply{withReplyHeader(sseReply(pathChat, chatStreamEvents...), "X-Request-Id", "cp-req-2")},
			steps: []caseStep{
				execStep(pluginabi.MethodExecutorExecuteStream, execRequest(k1, "openai", chatStreamPayload, streaming("stream-chat-1"))),
				mgmt(http.MethodGet, mgmtBase+"/logs", ""),
			},
		},
		{
			name:        "exec_unsupported",
			description: "Messages and Responses requests (CPA translates them for a chat-only plugin) and count_tokens are refused without calling Cline",
			credentials: []caseCredential{k1},
			steps: []caseStep{
				execStep(pluginabi.MethodExecutorExecute, execRequest(k1, "claude", msgPayload)),
				execStep(pluginabi.MethodExecutorExecuteStream, execRequest(k1, "openai-response", respPayload, streaming("stream-resp-1"))),
				execStep(pluginabi.MethodExecutorCountTokens, execRequest(k1, "claude", msgPayload)),
			},
		},
	}
}

func errorCases(k1 caseCredential) []snapshotCase {
	chat := func() cpaExecutorRequest { return execRequest(k1, "openai", chatPayload) }
	stream := func(id string) cpaExecutorRequest {
		return execRequest(k1, "openai", chatStreamPayload, streaming(id))
	}
	return []snapshotCase{
		{
			name:        "error_401",
			description: "Upstream 401 keeps its status",
			credentials: []caseCredential{k1},
			script:      []snapshotReply{jsonReply(pathChat, 401, `{"error":{"message":"Invalid API key","code":401}}`)},
			steps:       []caseStep{execStep(pluginabi.MethodExecutorExecute, chat())},
		},
		{
			name:        "error_403_provider_options",
			description: "Cline's 403 refusal of client routing options is a request error (400); another 403 stays 403",
			credentials: []caseCredential{k1},
			script: []snapshotReply{
				jsonReply(pathChat, 403, `{"error":{"message":"Error 403: client-supplied \"providerOptions\" model routing is not supported"}}`),
				jsonReply(pathChat, 403, `{"error":{"message":"Your plan does not include this model"}}`),
			},
			steps: []caseStep{
				execStep(pluginabi.MethodExecutorExecute, execRequest(k1, "openai", `{"model":"kimi-k3","messages":[],"providerOptions":{"gateway":{"order":["x"]}}}`)),
				execStep(pluginabi.MethodExecutorExecute, chat()),
			},
		},
		{
			name:        "error_429_retry_after",
			description: "Upstream 429 with Retry-After, on an aggregated call and on a stream request",
			credentials: []caseCredential{k1},
			script: []snapshotReply{
				withReplyHeader(jsonReply(pathChat, 429, `{"success":false,"error":"5-hour usage limit reached"}`), "Retry-After", "30"),
				withReplyHeader(jsonReply(pathChat, 429, `{"error":{"message":"Weekly usage limit reached"}}`), "Retry-After", "7"),
			},
			steps: []caseStep{
				execStep(pluginabi.MethodExecutorExecute, chat()),
				execStep(pluginabi.MethodExecutorExecuteStream, stream("stream-429-1")),
			},
		},
		{
			name:        "error_stream_success_false",
			description: "Cline's success:false envelope inside a stream is read as an error: before output it fails both a stream request and an aggregated call",
			credentials: []caseCredential{k1},
			script: []snapshotReply{
				sseReply(pathChat, chatChunk(`{"role":"assistant","content":""}`, ""), `data: {"success":false,"message":"Plan limit reached","code":429}`),
				sseReply(pathChat, `data: {"success":false,"message":"Upstream provider error"}`),
			},
			steps: []caseStep{
				execStep(pluginabi.MethodExecutorExecuteStream, stream("stream-sf-1")),
				execStep(pluginabi.MethodExecutorExecute, chat()),
				mgmt(http.MethodGet, mgmtBase+"/logs", ""),
			},
		},
		{
			name:        "error_aggregated",
			description: "Aggregated calls fail on an error after output, on an empty completion and on a truncated stream",
			credentials: []caseCredential{k1},
			script: []snapshotReply{
				sseReply(pathChat, chatChunk(`{"role":"assistant","content":"Partial"}`, ""), `data: {"error":{"message":"Provider returned error","code":502}}`),
				sseReply(pathChat, chatChunk(`{"role":"assistant","content":""}`, ""), chatChunk(`{}`, "stop"), usageChunk, "data: [DONE]"),
				sseReply(pathChat, chatChunk(`{"role":"assistant","content":"Hel"}`, "")),
			},
			steps: []caseStep{
				execStep(pluginabi.MethodExecutorExecute, chat()),
				execStep(pluginabi.MethodExecutorExecute, chat()),
				execStep(pluginabi.MethodExecutorExecute, chat()),
				mgmt(http.MethodGet, mgmtBase+"/logs", ""),
			},
		},
	}
}

func catalogCases() []snapshotCase {
	k1 := credK1(1)
	k2 := clinepassCred(2, "clinepass-k2.json", map[string]any{"api_key": fakeKey2, "prefix": "cp", "models": []string{"retired-model", "kimi-k3"}})
	catalog := getReply(pathCatalog, 200, catalogBody)
	return []snapshotCase{
		{
			name:        "catalog_models_for_auth",
			description: "model.for_auth loads the ClinePass offers (namespace stripped, duplicates and other lists skipped) once; a model selection with a retired model; the management models view",
			credentials: []caseCredential{k1, k2},
			script:      []snapshotReply{catalog},
			steps: []caseStep{
				bare(pluginabi.MethodModelStatic),
				modelsForAuthStep(k1, false),
				modelsForAuthStep(k2, true),
				mgmt(http.MethodGet, mgmtBase+"/models", ""),
			},
		},
		{
			name:        "catalog_fetch_error",
			description: "Catalog failures: model.for_auth registers nothing for an all-models key, the models view answers 502 and the status shows the error",
			credentials: []caseCredential{k1},
			script: []snapshotReply{
				reply(http.MethodGet, pathCatalog, 500, "text/plain", "upstream down"),
				getReply(pathCatalog, 200, `{"recommended":[],"clinePass":[]}`),
			},
			steps: []caseStep{
				modelsForAuthStep(k1, false),
				mgmt(http.MethodGet, mgmtBase+"/models", ""),
				mgmt(http.MethodGet, mgmtBase+"/status", ""),
			},
		},
	}
}

func credentialCases() []snapshotCase {
	catalog := getReply(pathCatalog, 200, catalogBody)
	return []snapshotCase{
		{
			name:        "credential_add_key_check",
			description: "Adding a key checks it against the plan usage: accepted (then saved with the catalog loaded), 401 (400 to the page), success:false (502)",
			script: []snapshotReply{
				getReply(pathLimits, 200, limitsBody),
				catalog,
				getReply(pathLimits, 401, `{"success":false,"error":"Unauthorized"}`),
				getReply(pathLimits, 200, `{"success":false,"error":"No active ClinePass subscription"}`),
			},
			steps: []caseStep{
				mgmt(http.MethodPost, mgmtBase+"/credentials", `{"api_key":"`+fakeKey1+`","label":"Primary","models":["kimi-k3"]}`),
				mgmt(http.MethodPost, mgmtBase+"/credentials", `{"api_key":"`+fakeKey2+`"}`),
				mgmt(http.MethodPost, mgmtBase+"/credentials", `{"api_key":"`+fakeKey3+`"}`),
			},
		},
	}
}

func quotaCases() []snapshotCase {
	k1 := credK1(1)
	k2 := clinepassCred(2, "clinepass-k2.json", map[string]any{"api_key": fakeKey2})
	k3 := clinepassCred(3, "clinepass-k3.json", map[string]any{"api_key": fakeKey3})
	quotaPath := mgmtBase + "/credentials/quota"
	byIndex := func(index string) caseStep {
		return mgmtQuery(http.MethodGet, quotaPath, url.Values{"auth_index": {index}})
	}
	return []snapshotCase{
		{
			name:        "quota_fetch_plan_usage",
			description: "quota.fetch reads the usage limits and the plan: 5h / 7d / monthly bars and the plan name; repeats come from the cache",
			credentials: []caseCredential{k1},
			script:      []snapshotReply{getReply(pathLimits, 200, limitsBody), getReply(pathPlan, 200, planBody)},
			steps: []caseStep{
				bare(pluginabi.MethodQuotaDescribe),
				quotaStep(k1, true),
				quotaStep(k1, false),
			},
		},
		{
			name:        "quota_fetch_partial",
			description: "A failed plan query leaves the plan out and is not cached; a window without a figure gets no bar",
			credentials: []caseCredential{k1},
			script: []snapshotReply{
				getReply(pathLimits, 200, `{"success":true,"data":{"limits":[{"type":"five_hour","percentUsed":0,"resetsAt":"2026-01-02T07:00:00Z"},{"type":"weekly"}]}}`),
				reply(http.MethodGet, pathPlan, 500, "text/plain", "boom"),
				getReply(pathLimits, 200, limitsBody),
				getReply(pathPlan, 200, planBody),
			},
			steps: []caseStep{quotaStep(k1, true), quotaStep(k1, true)},
		},
		{
			name:        "quota_fetch_errors",
			description: "Usage errors fail quota.fetch and skip the plan: 401, success:false, an answer without limits",
			credentials: []caseCredential{k1, k2, k3},
			script: []snapshotReply{
				getReply(pathLimits, 401, `{"success":false,"error":"Unauthorized"}`),
				getReply(pathLimits, 200, `{"success":false,"error":"No active ClinePass subscription"}`),
				getReply(pathLimits, 200, `{"success":true,"data":{}}`),
			},
			steps: []caseStep{quotaStep(k1, true), quotaStep(k2, true), quotaStep(k3, true)},
		},
		{
			name:        "quota_management_view",
			description: "The management quota view: plan usage with the account hash, a rejected key (400), an unknown credential (404)",
			credentials: []caseCredential{k1, k2},
			script: []snapshotReply{
				getReply(pathLimits, 200, limitsBody),
				getReply(pathPlan, 200, planBody),
				getReply(pathLimits, 401, `{"success":false,"error":"Unauthorized"}`),
			},
			steps: []caseStep{byIndex("idx-1"), byIndex("idx-2"), byIndex("idx-99"), byIndex("idx-1")},
		},
	}
}

func lifecycleCases() []snapshotCase {
	catalog := getReply(pathCatalog, 200, catalogBody)
	return []snapshotCase{
		{
			name:        "lifecycle_register_and_identifiers",
			description: "plugin.register declares chat-completions only, identifiers, unsupported operations, shutdown",
			script:      []snapshotReply{catalog},
			steps: []caseStep{
				lifecycleStep(pluginabi.MethodPluginRegister, pluginabi.SchemaVersion, snapshotConfig(nil)),
				bare(pluginabi.MethodAuthIdentifier),
				bare(pluginabi.MethodExecutorIdentifier),
				bare(pluginabi.MethodQuotaIdentifier),
				{method: pluginabi.MethodAuthLoginStart, request: map[string]any{}},
				bare(pluginabi.MethodQuotaReset),
				bare(pluginabi.MethodPluginShutdown),
			},
		},
	}
}
