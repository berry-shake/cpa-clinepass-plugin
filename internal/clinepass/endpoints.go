// Package clinepass is the ClinePass adapter of cpa-provider-core: what the
// plugin knows about the Cline API itself — endpoints, the ClinePass model
// catalog and the plan usage it reports.
package clinepass

// ProviderID is the CPA provider key and the auth file type. It differs from
// ClinePassBridge's "cline-pass" so the two plugins never claim each other's
// auth files.
const ProviderID = "clinepass"

// DisplayName is the provider's name in texts and the menu.
const DisplayName = "ClinePass"

// ModelNamespace starts every ClinePass model ID upstream
// ("cline-pass/kimi-k3"). CPA registers the IDs without it, so clients ask
// for clinepass/kimi-k3 rather than clinepass/cline-pass/kimi-k3.
const ModelNamespace = "cline-pass/"

// DefaultBaseURL is the Cline API origin; the endpoints below carry the
// /api/v1 path, which a configured base URL may repeat.
const DefaultBaseURL = "https://api.cline.bot"

// EndpointChat is the only inference endpoint: Cline serves the OpenAI Chat
// Completions API; CPA translates Messages and Responses requests to it.
const EndpointChat = "/api/v1/chat/completions"

// HeaderResponseFormat set to "openai" makes Cline answer non-stream
// requests in the standard OpenAI shape instead of {"success","data"}.
const HeaderResponseFormat = "X-Response-Format"

// Account and catalog endpoints. Their answers are wrapped as
// {"success":true,"data":...}.
const (
	// EndpointCatalog lists the recommended models; the ClinePass offers are
	// under "clinePass". It needs no API key.
	EndpointCatalog = "/api/v1/ai/cline/recommended-models"
	// EndpointUsageLimits reports the plan's 5-hour, weekly and monthly
	// usage of the account behind a key.
	EndpointUsageLimits = "/api/v1/users/me/plan/usage-limits"
	// EndpointPlan reports the account's plan and billing period.
	EndpointPlan = "/api/v1/users/me/plan"
)
