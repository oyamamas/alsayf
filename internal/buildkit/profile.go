// Package buildkit implements prepared builds and native module weaponization.
package buildkit

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
)

//go:embed modules.json
var moduleJSON []byte

type Module struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Description string `json:"description"`
}

func Modules() []Module {
	var modules []Module
	if err := json.Unmarshal(moduleJSON, &modules); err != nil {
		panic(err)
	}
	return modules
}
func Normalize(names []string) ([]string, error) {
	seen := map[string]bool{}
	for _, name := range names {
		found := false
		for _, m := range Modules() {
			if name == m.Name || name == m.Command {
				seen[m.Name] = true
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown native module %q", name)
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}
func ParseModules(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "core" {
		return []string{}, nil
	}
	if value == "all" {
		var names []string
		for _, m := range Modules() {
			names = append(names, m.Name)
		}
		return Normalize(names)
	}
	if value == "" {
		return nil, errors.New("select core, all, or comma-separated module names")
	}
	var names []string
	for _, name := range strings.Split(value, ",") {
		names = append(names, strings.TrimSpace(name))
	}
	return Normalize(names)
}

// Set by the builder for source archives, which have no Git checkout metadata.
var SourceRepository = "oyamamas/alsayf"
var SourceRevision string
var SourceModified string

type Info struct {
	Schema     int      `json:"schema"`
	Repository string   `json:"repository"`
	Revision   string   `json:"revision"`
	Modified   bool     `json:"modified"`
	GoVersion  string   `json:"go_version"`
	OS         string   `json:"os"`
	Arch       string   `json:"arch"`
	Modules    []string `json:"modules"`
}

func CurrentInfo(modules []string) Info {
	modules, _ = Normalize(modules)
	info := Info{Schema: 1, Repository: SourceRepository, Revision: SourceRevision, Modified: SourceModified == "true", GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, Modules: modules}
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, s := range build.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Revision == "" {
					info.Revision = s.Value
				}
			case "vcs.modified":
				if SourceModified == "" {
					info.Modified = s.Value == "true"
				}
			}
		}
	}
	return info
}
func gitRevision(ctx context.Context, dir string) (string, bool) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	data, err := cmd.Output()
	if err != nil {
		return "", true
	}
	status := exec.CommandContext(ctx, "git", "status", "--porcelain")
	status.Dir = dir
	dirty, err := status.Output()
	return strings.TrimSpace(string(data)), err != nil || len(dirty) > 0
}
