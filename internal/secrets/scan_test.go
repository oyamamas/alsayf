package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, root, name, data string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func fakeToken() string { return "ghp_" + "0123456789abcdefghijklmnopqrstuvwxyz" }

func TestRealEngineRedactionAndDeduplication(t *testing.T) {
	root := t.TempDir()
	token := fakeToken()
	first := writeFixture(t, root, "a.env", "# fixture\nключ="+token+"\n")
	writeFixture(t, root, "b.env", token+"\n")
	writeFixture(t, root, "clean.txt", "nothing to detect here\n")
	report, err := Scan(context.Background(), []string{root, first}, Options{MaxFileSize: DefaultMaxFileSize})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Complete || report.FilesScanned != 3 {
		t.Fatalf("unexpected scan summary: %+v", report)
	}
	found := false
	for _, finding := range report.Findings {
		if finding.RuleID != "np.github.1" {
			continue
		}
		found = true
		if len(finding.Occurrences) != 2 {
			t.Fatalf("want one finding across two files, got %+v", finding)
		}
		o := finding.Occurrences[0]
		if o.Line != 2 || o.Column != 6 || o.StartByte != int64(len("# fixture\nключ=")) {
			t.Fatalf("wrong UTF-8 location: %+v", o)
		}
	}
	if !found {
		t.Fatal("synthetic GitHub token not detected")
	}
	for _, format := range []string{"text", "json"} {
		var buf bytes.Buffer
		if err := WriteReport(&buf, report, format); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(buf.String(), token) || strings.Contains(buf.String(), "ключ=") {
			t.Fatal("report leaked secret or context")
		}
		if !strings.Contains(buf.String(), "[REDACTED]") {
			t.Fatal("missing redaction")
		}
		if format == "json" && !json.Valid(buf.Bytes()) {
			t.Fatal("invalid JSON")
		}
	}
	second, err := Scan(context.Background(), []string{root}, Options{MaxFileSize: DefaultMaxFileSize})
	if err != nil {
		t.Fatal(err)
	}
	if report.Findings[0].ID == second.Findings[0].ID {
		t.Fatal("finding ID must not be a reusable secret hash")
	}
}

func TestSkipPolicy(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "good.env", fakeToken())
	writeFixture(t, root, ".git/config", fakeToken())
	writeFixture(t, root, "node_modules/hidden.env", fakeToken())
	writeFixture(t, root, "large.txt", strings.Repeat("a", 101))
	writeFixture(t, root, "binary.dat", "\x00"+fakeToken())
	writeFixture(t, root, "invalid.dat", "\xff"+fakeToken())
	if err := os.Symlink(filepath.Join(root, "good.env"), filepath.Join(root, "link.env")); err != nil {
		t.Fatal(err)
	}
	report, err := Scan(context.Background(), []string{root}, Options{MaxFileSize: 100, Exclude: []string{"node_modules"}})
	if err != nil {
		t.Fatal(err)
	}
	if report.FilesScanned != 1 || len(report.Skipped) != 6 {
		t.Fatalf("wrong skip policy: %+v", report)
	}
	for _, f := range report.Findings {
		if len(f.Occurrences) != 1 || !strings.HasSuffix(f.Occurrences[0].Path, "good.env") {
			t.Fatalf("scanned excluded content: %+v", f)
		}
	}
}

type failingEngine struct{}

func (failingEngine) scan([]byte) ([]detection, error) {
	return nil, errors.New("engine error contains " + fakeToken())
}
func (failingEngine) close() error { return nil }

func TestPartialErrorsAndCancellation(t *testing.T) {
	root := t.TempDir()
	path := writeFixture(t, root, "input.txt", "some content")
	report, err := scanWithEngine(context.Background(), []string{path}, Options{MaxFileSize: 100}, failingEngine{})
	if !errors.Is(err, ErrIncomplete) || report.Complete || len(report.Issues) != 1 {
		t.Fatal("engine failure must mark results incomplete")
	}
	var buf bytes.Buffer
	if err := WriteReport(&buf, report, "json"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), fakeToken()) {
		t.Fatal("raw engine error leaked")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err = scanWithEngine(ctx, []string{root}, Options{MaxFileSize: 100}, failingEngine{})
	if !errors.Is(err, ErrIncomplete) || report.FilesScanned != 0 {
		t.Fatal("canceled scan did not stop")
	}
	report, err = Scan(context.Background(), []string{filepath.Join(root, "missing"), path}, Options{MaxFileSize: 100})
	if !errors.Is(err, ErrIncomplete) || report.FilesScanned != 1 {
		t.Fatal("missing input should report failure and continue other paths")
	}
}

func TestInvalidOptions(t *testing.T) {
	for _, opts := range []Options{{MaxFileSize: 0}, {MaxFileSize: -1}, {MaxFileSize: 100, Exclude: []string{"["}}} {
		if _, err := Scan(context.Background(), []string{t.TempDir()}, opts); err == nil {
			t.Fatalf("invalid options accepted: %+v", opts)
		}
	}
}

func TestModuleHasNoNetworkOrProcessDependencies(t *testing.T) {
	// Inspect non-test dependencies: this test itself needs os/exec to ask Go
	// for the build graph. The secrets module must not acquire these packages.
	cmd := exec.Command("go", "list", "-deps", "alsayf/internal/secrets")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range strings.Fields(string(out)) {
		if pkg == "net" || strings.HasPrefix(pkg, "net/") || pkg == "os/exec" ||
			strings.Contains(pkg, "/validator") || strings.Contains(pkg, "/scoring") ||
			pkg == "github.com/praetorian-inc/titus" {
			t.Fatalf("offline module acquired forbidden dependency: %s", pkg)
		}
	}
}

func TestRulesAreEmbeddedAndFiltered(t *testing.T) {
	rules, err := Rules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) < 100 {
		t.Fatalf("unexpectedly few secret rules: %d", len(rules))
	}
	for _, r := range rules {
		if r.ID == "np.aws.1" || r.ID == "np.aws.3" {
			t.Fatalf("non-secret identifier rule enabled: %s", r.ID)
		}
	}
}
