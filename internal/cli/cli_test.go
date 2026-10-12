package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sadasant/aesthetics/internal/report"
)

// fakeSystemOne answers like a decision model: guidelines whose text
// contains FAILME are unmet, every attribution is "preexisting", and any
// request about bad.go fails with a server error.
func fakeSystemOne(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			State     map[string]any `json:"state"`
			Questions map[string]struct {
				Type         string            `json:"type"`
				Instructions string            `json:"instructions"`
				Criteria     map[string]string `json:"criteria"`
			} `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if meta, _ := req.State["artifact"].(map[string]any); meta["path"] == "bad.go" {
			http.Error(w, "boom", http.StatusBadRequest)
			return
		}
		answers := map[string]any{}
		for id, q := range req.Questions {
			if q.Type == "noul" {
				p := 0.9
				if strings.Contains(q.Instructions, "FAILME") {
					p = 0.1
				}
				answers[id] = map[string]any{"type": "noul", "noul": p}
				continue
			}
			choice := ""
			for k := range q.Criteria {
				if choice == "" || k == "preexisting" {
					choice = k
				}
			}
			answers[id] = map[string]any{"type": "choice", "choice": choice, "probabilities": map[string]float64{choice: 0.9}, "confidence": 0.9}
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "fake", "answers": answers, "usage": map[string]int{}})
	}))
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func setupRepo(t *testing.T, serverURL string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		".aesthetics.yaml": fmt.Sprintf(`provider:
  type: systemone
  base_url: %s/v1
  model: fake
guidelines:
  - id: tidy
    text: FAILME The file is tidy.
    remedies: [Tidy it up.]
  - The file is readable.
ci:
  fail_on: warning
`, serverURL),
		"good.go": "package a\n\nfunc A() {}\n",
		"bad.go":  "package a\n\nfunc B() {}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "init", "-q")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	return dir
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })
}

func snapshot(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			b, _ := os.ReadFile(p)
			fmt.Fprintf(h, "%s %x\n", p, sha256.Sum256(b))
		}
		return nil
	})
	return fmt.Sprintf("%x", h.Sum(nil)) + git(t, dir, "status", "--porcelain")
}

func runJSON(t *testing.T, args ...string) (int, report.Report) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr)
	var rep report.Report
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("exit %d, invalid JSON: %v\nstdout: %s\nstderr: %s", code, err, stdout.String(), stderr.String())
	}
	return code, rep
}

func artifactByPath(rep report.Report, path string) *report.ArtifactResult {
	for i := range rep.Artifacts {
		if rep.Artifacts[i].Path == path {
			return &rep.Artifacts[i]
		}
	}
	return nil
}

func TestCheckIsolatesFailuresAndDoesNotModifyTheRepository(t *testing.T) {
	srv := fakeSystemOne(t)
	defer srv.Close()
	dir := setupRepo(t, srv.URL)
	chdir(t, dir)
	before := snapshot(t, dir)

	code, rep := runJSON(t, "check", "--format", "json", "--scope", "file", "good.go", "bad.go")
	if code != report.ExitFailed {
		t.Errorf("exit = %d, want %d", code, report.ExitFailed)
	}
	good, bad := artifactByPath(rep, "good.go"), artifactByPath(rep, "bad.go")
	if good == nil || good.Status != report.StatusComplete || len(good.Results) != 2 {
		t.Fatalf("good.go = %+v", good)
	}
	for _, c := range good.Results {
		switch c.Guideline {
		case "config/tidy":
			if c.Outcome != report.Fail || c.Recommendation != "Tidy it up." || !c.Counted {
				t.Errorf("tidy = %+v", c)
			}
		case "config/the-file-is-readable":
			if c.Outcome != report.Pass {
				t.Errorf("readable = %+v", c)
			}
		default:
			t.Errorf("unexpected guideline %s", c.Guideline)
		}
	}
	if bad == nil || bad.Status != report.StatusError || len(bad.Results) != 2 || bad.Results[0].Outcome != report.Indeterminate {
		t.Errorf("bad.go should be an identifiable error with indeterminate results: %+v", bad)
	}
	if !rep.Summary.Incomplete || rep.Summary.Errors != 1 {
		t.Errorf("summary = %+v", rep.Summary)
	}

	code, _ = runJSON(t, "check", "--format", "json", "--scope", "file", "--fail-on", "never", "bad.go")
	if code != report.ExitIncomplete {
		t.Errorf("incomplete run exit = %d, want %d", code, report.ExitIncomplete)
	}
	if after := snapshot(t, dir); after != before {
		t.Error("check modified the repository")
	}
}

func TestCheckDiffAttributesFailures(t *testing.T) {
	srv := fakeSystemOne(t)
	defer srv.Close()
	dir := setupRepo(t, srv.URL)
	chdir(t, dir)
	os.WriteFile("good.go", []byte("package a\n\nfunc A() {}\n\nfunc C() {}\n"), 0o644)
	os.WriteFile("new.go", []byte("package a\n"), 0o644)

	code, rep := runJSON(t, "check", "--diff", "--format", "json")
	if code != report.ExitFailed {
		t.Errorf("exit = %d, want %d", code, report.ExitFailed)
	}
	if rep.Run.Diff == nil || rep.Run.Diff.Files != 2 {
		t.Fatalf("diff = %+v", rep.Run.Diff)
	}
	tidy := func(path string) report.Criterion {
		a := artifactByPath(rep, path)
		if a == nil {
			t.Fatalf("%s not evaluated", path)
		}
		for _, c := range a.Results {
			if c.Guideline == "config/tidy" {
				return c
			}
		}
		t.Fatalf("%s: tidy not evaluated", path)
		return report.Criterion{}
	}
	if c := tidy("good.go"); c.Attribution == nil || c.Attribution.Value != "preexisting" || c.Counted {
		t.Errorf("modified file: %+v", c)
	}
	// A new file cannot have preexisting problems, whatever the model says.
	if c := tidy("new.go"); c.Attribution == nil || c.Attribution.Value != "introduced" || c.Attribution.Basis != "new file" || !c.Counted {
		t.Errorf("new file: %+v", c)
	}
	if code, _ := runJSON(t, "check", "--diff", "--format", "json", "good.go"); code != report.ExitOK {
		t.Errorf("only preexisting failures: exit = %d, want 0", code)
	}
}

func TestExplainShowsApplicableGuidelines(t *testing.T) {
	dir := setupRepo(t, "http://unused")
	chdir(t, dir)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"explain", "good.go", "--use", "readme,kiss"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"config/tidy", "kiss/direct", "Active but out of scope", "readme/states-purpose"} {
		if !strings.Contains(out, want) {
			t.Errorf("explain output lacks %q:\n%s", want, out)
		}
	}
}
