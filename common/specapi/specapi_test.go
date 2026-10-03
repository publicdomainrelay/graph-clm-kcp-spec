package specapi

import "testing"

func TestKindAndResourceResolveBothWays(t *testing.T) {
	for _, kind := range []string{RepositoryKind, SystemContextKind, SpecChangeKind} {
		gvr, err := GVRForKind(kind)
		if err != nil {
			t.Fatalf("GVRForKind(%q): %v", kind, err)
		}
		if gvr.Group != Group || gvr.Version != Version {
			t.Fatalf("gvr = %+v", gvr)
		}
		back, err := KindForResource(gvr.Resource)
		if err != nil {
			t.Fatalf("KindForResource(%q): %v", gvr.Resource, err)
		}
		if back != kind {
			t.Fatalf("round trip = %q, want %q", back, kind)
		}
		byResource, err := GVRForKind(gvr.Resource)
		if err != nil || byResource != gvr {
			t.Fatalf("GVRForKind(%q) = %+v, %v", gvr.Resource, byResource, err)
		}
	}
	if _, err := GVRForKind("Widget"); err == nil {
		t.Fatal("an unknown kind must not resolve")
	}
	if _, err := KindForResource("widgets"); err == nil {
		t.Fatal("an unknown resource must not resolve")
	}
	if ResourceForKind("Widget") != "" {
		t.Fatal("an unknown kind has no resource")
	}
}

func TestKindForArgAcceptsAliases(t *testing.T) {
	cases := map[string]string{
		"repository": RepositoryKind, "repositories": RepositoryKind, "repo": RepositoryKind, "repos": RepositoryKind,
		"systemcontext": SystemContextKind, "systemcontexts": SystemContextKind, "sc": SystemContextKind,
		"specchange": SpecChangeKind, "specchanges": SpecChangeKind, "change": SpecChangeKind, "changes": SpecChangeKind,
		"SystemContext": SystemContextKind,
	}
	for arg, want := range cases {
		got, err := KindForArg(arg)
		if err != nil {
			t.Fatalf("KindForArg(%q): %v", arg, err)
		}
		if got != want {
			t.Fatalf("KindForArg(%q) = %q, want %q", arg, got, want)
		}
	}
	if _, err := KindForArg("widget"); err == nil {
		t.Fatal("an unknown argument must not resolve")
	}
}

func TestHashJSON(t *testing.T) {
	first, err := HashJSON(map[string]any{"b": 2, "a": 1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := HashJSON(map[string]any{"a": 1, "b": 2})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("map key order must not change the hash: %q then %q", first, second)
	}
	if !IsHash(first) {
		t.Fatalf("%q must look like a sha256 hex digest", first)
	}
	other, err := HashJSON(map[string]any{"a": 2, "b": 2})
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Fatal("different content must hash differently")
	}
	for _, bad := range []string{"", "deadbeef", first + "0", "z" + first[1:]} {
		if IsHash(bad) {
			t.Fatalf("%q must not look like a sha256 hex digest", bad)
		}
	}
}
