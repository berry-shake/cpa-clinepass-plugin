package clinepass

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/berry-shake/cpa-clinepass-plugin/internal/web"
	"github.com/berry-shake/cpa-provider-core/config"
	"github.com/berry-shake/cpa-provider-core/credential"
	"github.com/berry-shake/cpa-provider-core/provider"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// Adapter is the ClinePass provider for cpa-provider-core.
type Adapter struct {
	catalog *Catalog
}

// New returns the ClinePass adapter with an empty catalog.
func New() *Adapter {
	return &Adapter{catalog: NewCatalog()}
}

var (
	_ provider.Adapter         = (*Adapter)(nil)
	_ provider.StreamFixer     = (*Adapter)(nil)
	_ provider.ErrorRemapper   = (*Adapter)(nil)
	_ provider.HeaderDecorator = (*Adapter)(nil)
)

// Spec describes the plugin. Cline serves Chat Completions only (CPA
// translates the other protocols), and its non-stream answers are wrapped
// and occasionally empty, so non-stream calls are aggregated from a stream.
func (a *Adapter) Spec() provider.Spec {
	return provider.Spec{
		ID:          ProviderID,
		DisplayName: DisplayName,
		Author:      "berry-shake",
		Repository:  "https://github.com/berry-shake/cpa-clinepass-plugin",
		Config: config.Profile{
			BaseURL:           DefaultBaseURL,
			ModelPrefix:       ProviderID,
			DataDir:           "plugins/clinepass-data",
			Database:          "clinepass.db",
			BaseURLSuffixes:   []string{"/v1", "/api"},
			BaseURLHelp:       "Cline API origin (https://api.cline.bot); a trailing /api/v1 is dropped. Auth files may override it with base_url.",
			ModelPrefixHelp:   `Default CPA prefix for ClinePass keys ("clinepass" -> clinepass/kimi-k3). Empty adds no prefix. An auth file's own prefix field wins.`,
			ModelsRefreshHelp: "How often to reload the ClinePass model list; 0 disables polling.",
		},
		Page: web.Page,
		Logo: web.Logo,
		Texts: provider.Texts{
			LoginStart:          "ClinePass uses API keys: add one on the ClinePass plugin page or upload a clinepass auth file",
			ModelsRoute:         "ClinePass model list (Cline's recommended ClinePass offers).",
			ModelsRefreshRoute:  "Reload the ClinePass model catalog and re-register changed models.",
			AddCredentialRoute:  `Add a ClinePass API key. Body: {"api_key","label","note","models","prefix","proxy_url"|"proxy_id"}.`,
			EditCredentialRoute: `Edit a ClinePass credential. Body: {"auth_index","label","note","models","prefix","proxy_url"|"proxy_id","priority"}.`,
			QuotaRoute:          "Plan and 5-hour / weekly / monthly usage of the Cline account behind one credential (?auth_index=).",
		},
		Protocols:     []provider.Protocol{provider.Chat},
		AggregateChat: true,
	}
}

// Decorate asks Cline for the standard OpenAI shape of non-stream answers
// (X-Response-Format: openai, https://docs.cline.bot/api/chat-completions);
// without it they come wrapped as {"success":true,"data":{...}}. Aggregated
// calls go upstream as streams, which the header leaves as they are; it
// covers an upstream that answers one of them with a plain body anyway.
func (a *Adapter) Decorate(header http.Header, _ provider.CallInput) {
	header.Set(HeaderResponseFormat, "openai")
}

// Endpoint is where Cline serves Chat Completions, the only protocol
// declared.
func (a *Adapter) Endpoint(provider.Protocol) string {
	return EndpointChat
}

// Catalog is the ClinePass model list.
func (a *Adapter) Catalog() provider.CatalogSource {
	return a.catalog
}

// VerifyKey checks a new key with the plan usage endpoint, which every
// ClinePass key may read.
func (a *Adapter) VerifyKey(ctx context.Context, env provider.Env, client *http.Client, cred credential.Credential) *pluginabi.Error {
	raw, perr := env.Errors.GetJSON(ctx, client, env.BaseURL(cred)+EndpointUsageLimits, cred.APIKey, env.UserAgent, EndpointUsageLimits)
	if perr != nil {
		return perr
	}
	_, perr = accountData(raw, EndpointUsageLimits, env.Errors.Prefix)
	return perr
}

// AccountKey caches account answers per key and origin.
func (a *Adapter) AccountKey(env provider.Env, cred credential.Credential) string {
	return credential.Fingerprint(cred.APIKey) + " " + env.BaseURL(cred)
}

// FetchAccount reads the plan usage, then the plan, of the account behind a
// key, one call after the other in the key's egress turn. A failed usage
// call fails the answer (and the plan is not asked); a failed plan call only
// leaves the plan out, and such an answer is not cached.
func (a *Adapter) FetchAccount(env provider.Env, client *http.Client, cred credential.Credential) (any, bool, *pluginabi.Error) {
	baseURL := env.BaseURL(cred)
	get := func(path string) (map[string]any, *pluginabi.Error) {
		ctx, cancel := context.WithTimeout(env.Context, provider.AccountCallTimeout)
		defer cancel()
		raw, perr := env.Errors.GetJSON(ctx, client, baseURL+path, cred.APIKey, env.UserAgent, path)
		if perr != nil {
			return nil, perr
		}
		return accountData(raw, path, env.Errors.Prefix)
	}
	info := AccountInfo{FetchedAt: env.Now().UTC().Format(time.RFC3339)}
	limits, perr := get(EndpointUsageLimits)
	if perr == nil {
		var ok bool
		if info.Windows, ok = WindowsFrom(limits); !ok {
			perr = &pluginabi.Error{Code: "invalid_upstream_response", Message: env.Errors.Prefix + "decode " + EndpointUsageLimits + ": no limits", HTTPStatus: http.StatusBadGateway}
		}
	}
	if perr != nil {
		info.Error = perr.Message
		return info, false, perr
	}
	plan, perr := get(EndpointPlan)
	if perr != nil {
		info.PlanError = perr.Message
		return info, false, nil
	}
	info.Plan, info.Account = PlanFrom(plan)
	return info, true, nil
}

// Quota turns an account answer into CPA's quota panel response.
func (a *Adapter) Quota(info any) pluginapi.QuotaFetchResponse {
	account, _ := info.(AccountInfo)
	return QuotaFromAccount(account)
}

// RemapError reports Cline's refusal of client routing options
// (providerOptions / provider) as the request error it is (400) rather than
// a 403, which CPA would count against the key.
func (a *Adapter) RemapError(status int, _ string, message string) (int, string) {
	if status == http.StatusForbidden && strings.Contains(message, "providerOptions") {
		return http.StatusBadRequest, message
	}
	return 0, message
}
