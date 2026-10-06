package orgfixture

import (
	"os"
	"testing"
)

// TestMaterialize builds the default polyrepo in $ORGFIXTURE_OUT and leaves it
// there, for scripts/example-org-run.sh and for anyone who wants to explore a
// polyrepo by hand: remotes/<name>.git are the bare repositories, seeds/<name>
// the work repositories history was made in. It does nothing without the
// variable.
func TestMaterialize(t *testing.T) {
	out := os.Getenv("ORGFIXTURE_OUT")
	if out == "" {
		t.Skip("set ORGFIXTURE_OUT to materialize the fixture polyrepo")
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := Default()
	spec.Dir = out
	f := Build(t, spec)
	// A little history, so there is a root log to read.
	f.Advance("market", map[string]string{"lib/requester/retry.ts": "export const retries = 3;\n"}, "market: retry the request")
	f.Advance("relay", map[string]string{"lib/relay-server/ping.ts": "export const ping = true;\n"}, "relay: answer pings")
	t.Logf("polyrepo at %s; root remote %s", out, f.RemoteURL(spec.Root))
}
