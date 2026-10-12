package artifact

import "testing"

func TestParseDiffTracksAddedLines(t *testing.T) {
	text := `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,4 +1,5 @@
 package a
-func Old() {}
+func New() {}
+func Extra() {}
 
 // end
@@ -20,2 +21,3 @@ func X() {
 x
+y
 z
diff --git a/old.txt b/new.txt
similarity index 100%
rename from old.txt
rename to new.txt
`
	files := parseDiff(text)
	if len(files) != 2 {
		t.Fatalf("got %d files", len(files))
	}
	a := files[0]
	if a.Path != "a.go" || a.Additions != 3 || a.Deletions != 1 {
		t.Errorf("a.go = %+v", a)
	}
	for line, want := range map[int]bool{1: false, 2: true, 3: true, 4: false, 21: false, 22: true, 23: false} {
		if a.Added[line] != want {
			t.Errorf("line %d added = %v, want %v", line, a.Added[line], want)
		}
	}
	if r := files[1]; r.Status != "renamed" || r.Path != "new.txt" || r.OldPath != "old.txt" {
		t.Errorf("rename = %+v", r)
	}
}
