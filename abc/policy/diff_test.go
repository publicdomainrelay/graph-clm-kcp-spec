package policy

import "testing"

const sampleDiff = `diff --git a/hono-bidder/mod.ts b/hono-bidder/mod.ts
index 1111111..2222222 100644
--- a/hono-bidder/mod.ts
+++ b/hono-bidder/mod.ts
@@ -10,2 +10,3 @@ export function bid() {
 const guest = provider.address();
+  await Deno.connect({ hostname: guest, port: 22 });
 }
diff --git a/lib/requester/mod.ts b/lib/requester/mod.ts
deleted file mode 100644
--- a/lib/requester/mod.ts
+++ /dev/null
@@ -1,2 +0,0 @@
-export function dial() {
-}
diff --git a/lib/cloud-init/mod.ts b/lib/cloud-init/mod.ts
new file mode 100644
--- /dev/null
+++ b/lib/cloud-init/mod.ts
@@ -0,0 +1,1 @@
+export const module = "user_data";
`

func TestParseUnifiedDiffNamesFilesAndLines(t *testing.T) {
	diff := ParseUnifiedDiff(sampleDiff)
	if len(diff.Spec.Files) != 3 {
		t.Fatalf("files = %d, want 3", len(diff.Spec.Files))
	}
	byPath := map[string]CodeDiffFile{}
	for _, file := range diff.Spec.Files {
		byPath[file.Path] = file
	}
	modified, ok := byPath["hono-bidder/mod.ts"]
	if !ok || modified.Status != DiffStatusModified {
		t.Fatalf("modified = %+v", modified)
	}
	added := modified.Added
	if len(added) != 1 || added[0].Line != 11 || added[0].Text != "  await Deno.connect({ hostname: guest, port: 22 });" {
		t.Errorf("added = %+v", added)
	}
	if len(modified.Removed) != 0 {
		t.Errorf("removed = %+v", modified.Removed)
	}
	deleted, ok := byPath["lib/requester/mod.ts"]
	if !ok || deleted.Status != DiffStatusDeleted || len(deleted.Removed) != 2 || deleted.Removed[1].Line != 2 {
		t.Errorf("deleted = %+v", deleted)
	}
	created, ok := byPath["lib/cloud-init/mod.ts"]
	if !ok || created.Status != DiffStatusAdded || len(created.Added) != 1 {
		t.Errorf("added file = %+v", created)
	}
}

const contentDiff = `diff --git a/db/schema.sql b/db/schema.sql
index 3333333..4444444 100644
--- a/db/schema.sql
+++ b/db/schema.sql
@@ -1,3 +1,3 @@
--- a removed SQL comment line
+-- an added SQL comment line
 select 1;
-+++ not a header
+++ a content line
diff --git a/hono-bidder/mod.ts b/hono-bidder/mod.ts
similarity index 80%
rename from hono-bidder/mod.ts
rename to hono-bidder/bidder.ts
@@ -5,1 +5,1 @@
-old
+new
`

func TestParseUnifiedDiffKeepsHeaderLikeContent(t *testing.T) {
	diff := ParseUnifiedDiff(contentDiff)
	byPath := map[string]CodeDiffFile{}
	for _, file := range diff.Spec.Files {
		byPath[file.Path] = file
	}
	schema, ok := byPath["db/schema.sql"]
	if !ok {
		t.Fatalf("the schema file was lost: %+v", diff.Spec.Files)
	}
	wantRemoved := []string{"-- a removed SQL comment line", "+++ not a header"}
	if len(schema.Removed) != 2 {
		t.Fatalf("removed = %+v, want the two content lines", schema.Removed)
	}
	for index, want := range wantRemoved {
		if schema.Removed[index].Text != want {
			t.Errorf("removed[%d] = %q, want %q", index, schema.Removed[index].Text, want)
		}
	}
	if len(schema.Added) != 2 || schema.Added[0].Text != "-- an added SQL comment line" || schema.Added[1].Text != "++ a content line" {
		t.Errorf("added = %+v", schema.Added)
	}
	renamed, ok := byPath["hono-bidder/bidder.ts"]
	if !ok {
		t.Fatalf("the renamed file is missing: %+v", diff.Spec.Files)
	}
	if len(renamed.Removed) != 1 || len(renamed.Added) != 1 {
		t.Errorf("renamed = %+v", renamed)
	}
}

func TestParseUnifiedDiffIsEmptyWithoutAPatch(t *testing.T) {
	diff := ParseUnifiedDiff("")
	if len(diff.Spec.Files) != 0 {
		t.Fatalf("files = %+v", diff.Spec.Files)
	}
}
