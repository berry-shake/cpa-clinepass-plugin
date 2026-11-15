package clinepass

// The catalog caches the ClinePass model list and turns it into CPA model
// registrations.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/berry-shake/cpa-provider-core/provider"
	"github.com/berry-shake/cpa-provider-core/upstream"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// maxCatalogBytes bounds the recommended-models response (the real one is
// ~3 KB).
const maxCatalogBytes = 4 << 20

// Model is one ClinePass offer from the recommended-models list.
type Model struct {
	// ID is the ID CPA registers, the upstream ID without ModelNamespace
	// ("kimi-k3").
	ID string
	// UpstreamID is the ID requests carry upstream ("cline-pass/kimi-k3").
	UpstreamID  string
	Name        string
	Description string
	Tags        []string
}

type catalogResponse struct {
	ClinePass []struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	} `json:"clinePass"`
}

// Snapshot is an immutable view of the cached catalog.
type Snapshot struct {
	Models    []Model
	byID      map[string]Model
	Revision  string
	FetchedAt time.Time
	CheckedAt time.Time
	LastError string
}

// Empty reports whether no catalog has been loaded.
func (c Snapshot) Empty() bool { return len(c.Models) == 0 }

// Summary describes the snapshot on the management page.
func (c Snapshot) Summary() provider.CatalogSummary {
	return provider.CatalogSummary{Count: len(c.Models), Revision: c.Revision, FetchedAt: c.FetchedAt, CheckedAt: c.CheckedAt, LastError: c.LastError}
}

// Has reports whether a model ID (without ModelNamespace) is in the catalog.
func (c Snapshot) Has(id string) bool {
	_, ok := c.byID[id]
	return ok
}

// Lookup finds a model by the ID CPA registers.
func (c Snapshot) Lookup(id string) (Model, bool) {
	model, ok := c.byID[id]
	return model, ok
}

// Catalog caches the ClinePass model list.
type Catalog struct {
	mu       sync.RWMutex
	snapshot Snapshot
	// fetchMu serializes upstream fetches so concurrent model.for_auth calls
	// during startup share one request.
	fetchMu sync.Mutex
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog {
	return &Catalog{snapshot: Snapshot{byID: map[string]Model{}}}
}

// Current returns the cached snapshot.
func (c *Catalog) Current() provider.Catalog {
	return c.current()
}

func (c *Catalog) current() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

// Fetch reloads the catalog through client. The list is small and always
// reloaded in full, so force changes nothing.
func (c *Catalog) Fetch(ctx context.Context, client *http.Client, baseURL, userAgent string, _ bool, now time.Time) (provider.Catalog, error) {
	c.fetchMu.Lock()
	defer c.fetchMu.Unlock()

	req, errReq := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+EndpointCatalog, nil)
	if errReq != nil {
		return c.current(), errReq
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, errDo := client.Do(req)
	if errDo != nil {
		return c.recordError(now, errDo)
	}
	body, errRead := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes))
	_ = resp.Body.Close()
	if errRead != nil {
		return c.recordError(now, errRead)
	}
	if resp.StatusCode != http.StatusOK {
		return c.recordError(now, fmt.Errorf("GET %s returned %d: %s", EndpointCatalog, resp.StatusCode, upstream.Truncate(string(body), 300)))
	}
	models, errParse := ParseCatalog(body)
	if errParse != nil {
		return c.recordError(now, errParse)
	}
	byID := make(map[string]Model, len(models))
	for _, model := range models {
		byID[model.ID] = model
	}
	next := Snapshot{
		Models:    models,
		byID:      byID,
		Revision:  catalogRevision(models),
		FetchedAt: now,
		CheckedAt: now,
	}
	c.mu.Lock()
	c.snapshot = next
	c.mu.Unlock()
	return next, nil
}

// ParseCatalog reads the ClinePass offers of a recommended-models response,
// sorted by ID. Entries outside ModelNamespace and repeated IDs are skipped;
// a list without any offer is an error.
func ParseCatalog(body []byte) ([]Model, error) {
	var list catalogResponse
	if errUnmarshal := json.Unmarshal(body, &list); errUnmarshal != nil {
		return nil, fmt.Errorf("decode %s: %w", EndpointCatalog, errUnmarshal)
	}
	models := make([]Model, 0, len(list.ClinePass))
	seen := make(map[string]bool, len(list.ClinePass))
	for _, item := range list.ClinePass {
		upstreamID := strings.TrimSpace(item.ID)
		id := strings.TrimPrefix(upstreamID, ModelNamespace)
		if !strings.HasPrefix(upstreamID, ModelNamespace) || id == "" || strings.ContainsAny(id, "/ \t\r\n") || seen[id] {
			continue
		}
		seen[id] = true
		name := strings.TrimSpace(item.Name)
		if name == "" || name == upstreamID {
			name = id
		}
		var tags []string
		for _, tag := range item.Tags {
			if tag = strings.TrimSpace(tag); tag != "" {
				tags = append(tags, tag)
			}
		}
		models = append(models, Model{ID: id, UpstreamID: upstreamID, Name: name, Description: strings.TrimSpace(item.Description), Tags: tags})
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("%s returned no ClinePass models", EndpointCatalog)
	}
	sort.Slice(models, func(i, j int) bool { return strings.ToLower(models[i].ID) < strings.ToLower(models[j].ID) })
	return models, nil
}

func (c *Catalog) recordError(now time.Time, err error) (provider.Catalog, error) {
	c.mu.Lock()
	c.snapshot.CheckedAt = now
	c.snapshot.LastError = err.Error()
	snapshot := c.snapshot
	c.mu.Unlock()
	return snapshot, err
}

// catalogRevision hashes what ends up in CPA's model registry.
func catalogRevision(models []Model) string {
	hash := sha256.New()
	for _, model := range models {
		_, _ = fmt.Fprintf(hash, "%s\x00%s\x00%s\x00%s\n", model.ID, model.UpstreamID, model.Name, model.Description)
	}
	return hex.EncodeToString(hash.Sum(nil))[:16]
}

// ModelInfos lists the CPA models of a credential: every catalog model when
// all is set, otherwise the selected IDs. Selected IDs missing from the
// catalog are still registered (with minimal metadata) so a catalog outage
// or a renamed model does not silently drop routes the user picked.
func (c Snapshot) ModelInfos(all bool, ids []string) []pluginapi.ModelInfo {
	var selected []Model
	if all {
		selected = c.Models
	} else {
		selected = make([]Model, 0, len(ids))
		for _, id := range ids {
			if model, ok := c.Lookup(id); ok {
				selected = append(selected, model)
				continue
			}
			id = strings.TrimPrefix(id, ModelNamespace)
			selected = append(selected, Model{ID: id, UpstreamID: ModelNamespace + id, Name: id})
		}
	}
	out := make([]pluginapi.ModelInfo, 0, len(selected))
	for _, model := range selected {
		out = append(out, toModelInfo(model))
	}
	return out
}

func toModelInfo(model Model) pluginapi.ModelInfo {
	description := DisplayName + " · " + model.Name
	if model.Description != "" {
		description += " · " + model.Description
	}
	return pluginapi.ModelInfo{
		ID:          model.ID,
		Object:      "model",
		OwnedBy:     ProviderID,
		Type:        ProviderID,
		DisplayName: model.Name,
		Name:        model.ID,
		Description: description,
		// UserDefined keeps CPA from stripping reasoning settings for models
		// it has no thinking metadata for; Cline validates them upstream.
		UserDefined: true,
	}
}

// UpstreamID maps the model CPA hands the executor to the ClinePass ID: the
// credential's prefix and a reasoning suffix are dropped (provider.UpstreamID;
// the registered IDs contain no slash, so any other prefix goes too, the
// namespace included), then ModelNamespace is put back. A model the catalog
// does not list (yet) still gets the namespace.
func (c Snapshot) UpstreamID(model, prefix string) string {
	// The credential's prefix is dropped first and returned as is, so a full
	// ID behind it ("clinepass/cline-pass/kimi-k3") still has the namespace.
	id := strings.TrimPrefix(provider.UpstreamID(model, prefix, c.Has, provider.FlatIDs), ModelNamespace)
	if found, ok := c.Lookup(id); ok {
		return found.UpstreamID
	}
	return ModelNamespace + id
}

// modelView is one model of the management page's models view.
type modelView struct {
	ID          string   `json:"id"`
	UpstreamID  string   `json:"upstream_id"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// ModelsView lists the catalog for the management page's models view.
func (c Snapshot) ModelsView() any {
	models := make([]modelView, 0, len(c.Models))
	for _, model := range c.Models {
		models = append(models, modelView{ID: model.ID, UpstreamID: model.UpstreamID, Name: model.Name, Description: model.Description, Tags: model.Tags})
	}
	return models
}
