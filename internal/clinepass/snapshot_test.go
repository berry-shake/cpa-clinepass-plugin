package clinepass_test

import (
	"testing"
	"time"

	"github.com/berry-shake/cpa-clinepass-plugin/internal/clinepass"
	"github.com/berry-shake/cpa-provider-core/core"
	"github.com/berry-shake/cpa-provider-core/coretest"
)

// TestSnapshots replays testdata/snapshots through the plugin (see
// cpa-provider-core's coretest package and README).
//
//	go test ./internal/clinepass -run TestSnapshots           # compare
//	go test ./internal/clinepass -run TestSnapshots -update   # rewrite outputs
func TestSnapshots(t *testing.T) {
	coretest.RunSnapshots(t, coretest.Binding{
		ProviderID: clinepass.ProviderID,
		NewService: func(version string, host func(string, []byte) ([]byte, error), upstreamURL string) coretest.Plugin {
			return core.NewWithOptions(clinepass.New(), version, host, core.Options{
				Now:             func() time.Time { return coretest.Clock },
				ManualRefresh:   true,
				EgressCheckURLs: []string{upstreamURL + "/json"},
			})
		},
		Cases: func(t *testing.T) []coretest.Fixture {
			var out []coretest.Fixture
			for _, c := range snapshotCases() {
				out = append(out, c.fixture(t))
			}
			return out
		},
	})
}

type (
	snapshotFixture     = coretest.Fixture
	snapshotSetup       = coretest.Setup
	snapshotCredential  = coretest.Credential
	snapshotStep        = coretest.Step
	snapshotRPC         = coretest.RPC
	snapshotReply       = coretest.Reply
	cpaLifecycleRequest = coretest.LifecycleRequest
	cpaAuthModelRequest = coretest.AuthModelRequest
)

const snapshotAuthDir = coretest.AuthDir

var caseJSON = coretest.CaseJSON
