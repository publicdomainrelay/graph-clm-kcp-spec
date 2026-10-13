package graphproc

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/impl/boltgraph"
)

func TestParseListenersReadsTheJSONLine(t *testing.T) {
	line := `{"admin_addr":"127.0.0.1:41673","binary":"graph-node","bolt_addr":"127.0.0.1:45393","http_addr":"127.0.0.1:34839","level":"INFO","message":"graph node listeners started","node_id":"graph-node-0"}`
	addresses, ok := ParseListeners(line)
	if !ok || addresses.Bolt != "127.0.0.1:45393" || addresses.HTTP != "127.0.0.1:34839" || addresses.Admin != "127.0.0.1:41673" {
		t.Fatalf("ParseListeners = %+v %v", addresses, ok)
	}
}

func TestParseListenersReadsTheTextLine(t *testing.T) {
	line := "\x1b[2m2026-10-12T22:07:43Z\x1b[0m \x1b[32m INFO\x1b[0m graph_node: graph node listeners started \x1b[3mnode_id\x1b[0m\x1b[2m=\x1b[0mgraph-node-0 bolt_addr=127.0.0.1:43207 http_addr=127.0.0.1:39415"
	addresses, ok := ParseListeners(line)
	if !ok || addresses.Bolt != "127.0.0.1:43207" || addresses.HTTP != "127.0.0.1:39415" {
		t.Fatalf("ParseListeners = %+v %v", addresses, ok)
	}
}

func TestParseListenersIgnoresOtherLines(t *testing.T) {
	if _, ok := ParseListeners(`{"message":"starting graph node"}`); ok {
		t.Fatal("a line that is not the listeners line was read as one")
	}
}

func TestStartBindsPortZeroAndReportsTheAddress(t *testing.T) {
	if _, err := NodeBin(); err != nil {
		if os.Getenv("SPECD_REQUIRE_LIVE") == "1" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	root := t.TempDir()
	instance, err := Start(ctx, Options{Root: root, DieWithStarter: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Stop(instance) })
	if instance.BoltAddr == "" || instance.BoltAddr == "127.0.0.1:0" || instance.BoltURL != "bolt://"+instance.BoltAddr {
		t.Fatalf("instance = %+v, want the bound bolt address", instance)
	}
	loaded, err := LoadEndpoint(root)
	if err != nil || loaded.BoltURL != instance.BoltURL || loaded.Pid != instance.Pid {
		t.Fatalf("endpoint = %+v %v", loaded, err)
	}

	query(t, ctx, instance)

	again, err := Start(ctx, Options{Root: root})
	if err != nil || again.Pid != instance.Pid {
		t.Fatalf("a second Start = %+v %v, want the serving node reused", again, err)
	}

	Stop(instance)
	if _, ok := Probe(root); ok {
		t.Fatal("the node still serves after Stop")
	}
	restarted, err := Start(ctx, Options{Root: root, DieWithStarter: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Stop(restarted) })
	if restarted.TokenFile != instance.TokenFile {
		t.Errorf("token file moved: %s -> %s", instance.TokenFile, restarted.TokenFile)
	}
	query(t, ctx, restarted)

	other, err := Start(ctx, Options{Root: t.TempDir(), DieWithStarter: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Stop(other) })
	if other.BoltAddr == restarted.BoltAddr {
		t.Fatalf("two roots share %s", other.BoltAddr)
	}
}

func query(t *testing.T, ctx context.Context, instance Instance) {
	t.Helper()
	password, err := instance.Password()
	if err != nil {
		t.Fatal(err)
	}
	client, err := boltgraph.Connect(ctx, boltgraph.Options{URL: instance.BoltURL, User: instance.User, Password: password}, 3)
	if err != nil {
		t.Fatalf("connect %s: %v", instance.BoltURL, err)
	}
	defer client.Close(ctx)
	rows, err := client.Run(ctx, "MATCH (n:GraphprocProbe) RETURN count(*) AS total", nil)
	if err != nil || len(rows) != 1 {
		t.Fatalf("probe query = %v %v", rows, err)
	}
}
