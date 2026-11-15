package clinepass

import (
	"os"
	"strings"
	"testing"
)

// testdata/recommended-models.json is Cline's public answer of 2026-11-15.
func loadTestCatalog(t *testing.T) Snapshot {
	t.Helper()
	body, errRead := os.ReadFile("testdata/recommended-models.json")
	if errRead != nil {
		t.Fatal(errRead)
	}
	models, errParse := ParseCatalog(body)
	if errParse != nil {
		t.Fatal(errParse)
	}
	byID := map[string]Model{}
	for _, model := range models {
		byID[model.ID] = model
	}
	return Snapshot{Models: models, byID: byID, Revision: catalogRevision(models)}
}

func TestParseCatalogTakesTheClinePassOffers(t *testing.T) {
	snapshot := loadTestCatalog(t)
	if len(snapshot.Models) != 9 {
		t.Fatalf("models = %d", len(snapshot.Models))
	}
	for i, model := range snapshot.Models {
		if strings.Contains(model.ID, "/") || model.UpstreamID != ModelNamespace+model.ID || model.Name != model.ID {
			t.Errorf("model %+v", model)
		}
		if i > 0 && strings.ToLower(snapshot.Models[i-1].ID) > strings.ToLower(model.ID) {
			t.Errorf("not sorted at %s", model.ID)
		}
	}
	pro, ok := snapshot.Lookup("deepseek-v4-pro")
	if !ok || pro.Description != "Frontier reasoning and coding with 1M context window" {
		t.Fatalf("deepseek-v4-pro = %+v", pro)
	}
	// The other lists (recommended, free, clineCloud) are not ClinePass offers.
	if snapshot.Has("muse-ember") || snapshot.Has("meta/muse-ember") {
		t.Fatal("a recommended model leaked into the catalog")
	}
}

func TestParseCatalogSkipsAndRefuses(t *testing.T) {
	models, errParse := ParseCatalog([]byte(`{"clinePass":[
		{"id":"cline-pass/kimi-k3","name":"Kimi K3","tags":[" NEW ",""]},
		{"id":"cline-pass/kimi-k3","name":"again"},
		{"id":"other/model"},
		{"id":"cline-pass/"},
		{"id":"cline-pass/a/b"},
		{"id":" cline-pass/glm-5.3 "}
	]}`))
	if errParse != nil {
		t.Fatal(errParse)
	}
	if len(models) != 2 || models[0].ID != "glm-5.3" || models[1].ID != "kimi-k3" || models[1].Name != "Kimi K3" || len(models[1].Tags) != 1 || models[1].Tags[0] != "NEW" {
		t.Fatalf("models = %+v", models)
	}
	for _, body := range []string{`{"clinePass":[]}`, `{"recommended":[{"id":"cline-pass/x"}]}`, `not json`} {
		if _, errParse := ParseCatalog([]byte(body)); errParse == nil {
			t.Errorf("%s: accepted", body)
		}
	}
}

func TestUpstreamIDPutsTheNamespaceBack(t *testing.T) {
	snapshot := loadTestCatalog(t)
	cases := map[string]string{
		"kimi-k3":                              "cline-pass/kimi-k3",
		"clinepass/kimi-k3":                    "cline-pass/kimi-k3",
		"cline-pass/kimi-k3":                   "cline-pass/kimi-k3",
		"clinepass/cline-pass/kimi-k3":         "cline-pass/kimi-k3",
		"kimi-k3(high)":                        "cline-pass/kimi-k3",
		"old/kimi-k3":                          "cline-pass/kimi-k3",
		"deepseek-v4.1-flash-1017":             "cline-pass/deepseek-v4.1-flash-1017",
		"not-listed-yet":                       "cline-pass/not-listed-yet",
		"clinepass/not-listed-yet(low)":        "cline-pass/not-listed-yet",
		" clinepass/deepseek-v4.1-flash-1017 ": "cline-pass/deepseek-v4.1-flash-1017",
	}
	for model, want := range cases {
		if got := snapshot.UpstreamID(model, "clinepass"); got != want {
			t.Errorf("UpstreamID(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestModelInfos(t *testing.T) {
	snapshot := loadTestCatalog(t)
	all := snapshot.ModelInfos(true, nil)
	if len(all) != 9 || all[0].ID != snapshot.Models[0].ID || all[0].Type != ProviderID || !all[0].UserDefined {
		t.Fatalf("all = %+v", all[0])
	}
	picked := snapshot.ModelInfos(false, []string{"deepseek-v4-pro", "cline-pass/gone"})
	if len(picked) != 2 || picked[0].ID != "deepseek-v4-pro" || !strings.Contains(picked[0].Description, "1M context") || picked[1].ID != "gone" {
		t.Fatalf("picked = %+v", picked)
	}
	view := snapshot.ModelsView().([]modelView)
	if len(view) != 9 || view[0].UpstreamID != ModelNamespace+view[0].ID {
		t.Fatalf("view = %+v", view[0])
	}
}
