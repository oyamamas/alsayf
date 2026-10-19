package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewCommand()
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), err
}

func TestCommands(t *testing.T) {
	root := t.TempDir()
	token := "ghp_" + "0123456789abcdefghijklmnopqrstuvwxyz"
	path := filepath.Join(root, "input.env")
	if err := os.WriteFile(path, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := run(t, "scan", path, "--format", "json")
	if err != nil || stderr != "" || !json.Valid([]byte(out)) || strings.Contains(out, token) || !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("unexpected JSON scan output: %v %s %s", err, out, stderr)
	}
	out, _, err = run(t, "scan", path, "--format", "json", "--fail-on-findings")
	if err == nil || !json.Valid([]byte(out)) {
		t.Fatal("fail-on-findings must fail after writing JSON")
	}
	out, _, err = run(t, "scan", filepath.Join(root, "missing"), "--format", "json")
	if err == nil || !json.Valid([]byte(out)) || !strings.Contains(out, `"complete": false`) {
		t.Fatal("missing input must return an incomplete JSON report")
	}
	for _, args := range [][]string{{"scan"}, {"scan", path, "--format", "yaml"}, {"scan", path, "--validate"}, {"scan", path, "--reveal"}, {"scan", path, "--max-file-size", "0"}} {
		if _, _, err := run(t, args...); err == nil {
			t.Fatalf("invalid command accepted: %v", args)
		}
	}
	out, _, err = run(t, "rules", "list", "--format", "json")
	if err != nil || !json.Valid([]byte(out)) || !strings.Contains(out, "np.github.1") {
		t.Fatalf("rules list failed: %v", err)
	}
}
