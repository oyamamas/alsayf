package buildkit

import (
	"context"
	"errors"
	"fmt"
	"go/version"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var goLine = regexp.MustCompile(`(?m)^go\s+([0-9]+\.[0-9]+(?:\.[0-9]+)?)\s*$`)
var toolchainLine = regexp.MustCompile(`(?m)^toolchain\s+(go[0-9]+\.[0-9]+(?:\.[0-9]+)?)\s*$`)

func requiredGo(source string) (string, error) {
	data, err := os.ReadFile(filepath.Join(source, "go.mod"))
	if err != nil {
		return "", err
	}
	match := goLine.FindSubmatch(data)
	if len(match) != 2 {
		return "", errors.New("source go.mod is missing its Go version")
	}
	required := "go" + string(match[1])
	if other := toolchainLine.FindSubmatch(data); len(other) == 2 && version.Compare(string(other[1]), required) > 0 {
		required = string(other[1])
	}
	return required, nil
}
func environment(overrides map[string]string) []string {
	values := map[string]string{}
	for _, s := range os.Environ() {
		if key, value, ok := strings.Cut(s, "="); ok {
			if runtime.GOOS == "windows" {
				key = strings.ToUpper(key)
			}
			values[key] = value
		}
	}
	// Ignore workstation configuration that changes module/workspace selection.
	for _, key := range []string{"GOROOT", "GOTOOLDIR", "GOFLAGS", "GOWORK", "GOENV", "GOTOOLCHAIN", "GOOS", "GOARCH", "GOAMD64", "GOARM64", "GOEXPERIMENT", "TEST_TELEMETRY_DIR", "GO_TELEMETRY_CHILD", "GO_TELEMETRY_CHILD_UPLOAD"} {
		delete(values, key)
	}
	values["GOENV"] = "off"
	values["GOWORK"] = "off"
	values["GOTOOLCHAIN"] = "local"
	values["CGO_ENABLED"] = "0"
	values["GOPROXY"] = "https://proxy.golang.org"
	values["GOSUMDB"] = "sum.golang.org"
	values["GONOPROXY"] = "none"
	values["GONOSUMDB"] = "none"
	values["GOPRIVATE"] = ""
	for _, name := range []string{"GOPROXY", "GOSUMDB"} {
		if value := os.Getenv("ALSAYF_" + name); value != "" {
			values[name] = value
		}
	}
	for key, value := range overrides {
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		values[key] = value
	}
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}
func ensureGo(ctx context.Context, binary, required, work string, offline bool, log io.Writer) (string, error) {
	if binary == "" {
		binary, _ = exec.LookPath("go")
	}
	if binary != "" {
		path, err := exec.LookPath(binary)
		if err == nil {
			command := exec.CommandContext(ctx, path, "version")
			command.Env, err = isolatedEnvironment(work, nil)
			if err != nil {
				return "", err
			}
			if data, err := command.Output(); err == nil {
				fields := strings.Fields(string(data))
				if len(fields) >= 3 && version.Compare(fields[2], required) >= 0 {
					fmt.Fprintf(log, "Using %s (%s)\n", fields[2], path)
					return filepath.Abs(path)
				}
			}
		}
	}
	if offline {
		return "", fmt.Errorf("offline build requires an installed Go toolchain >= %s", required)
	}
	var releases []struct {
		Version string `json:"version"`
		Files   []struct {
			Filename string `json:"filename"`
			OS       string `json:"os"`
			Arch     string `json:"arch"`
			SHA256   string `json:"sha256"`
			Size     int64  `json:"size"`
			Kind     string `json:"kind"`
		} `json:"files"`
	}
	if err := readJSON(ctx, "https://go.dev/dl/?mode=json&include=all", "", &releases); err != nil {
		return "", err
	}
	for _, release := range releases {
		if release.Version != required {
			continue
		}
		for _, file := range release.Files {
			if file.OS != runtime.GOOS || file.Arch != runtime.GOARCH || file.Kind != "archive" {
				continue
			}
			if filepath.Base(file.Filename) != file.Filename || len(file.SHA256) != 64 || file.Size <= 0 {
				return "", errors.New("invalid official Go archive metadata")
			}
			fmt.Fprintf(log, "Downloading %s for %s/%s; verifying SHA-256\n", required, file.OS, file.Arch)
			archive := filepath.Join(work, file.Filename)
			if err := download(ctx, "https://go.dev/dl/"+file.Filename, "", archive, file.SHA256, file.Size); err != nil {
				return "", err
			}
			root := filepath.Join(work, "toolchain")
			var err error
			if strings.HasSuffix(file.Filename, ".zip") {
				err = extractZIP(ctx, archive, root)
			} else {
				err = extractTarGZ(ctx, archive, root)
			}
			if err != nil {
				return "", err
			}
			os.Remove(archive)
			suffix := ""
			if runtime.GOOS == "windows" {
				suffix = ".exe"
			}
			path := filepath.Join(root, "go", "bin", "go"+suffix)
			command := exec.CommandContext(ctx, path, "version")
			command.Env, err = isolatedEnvironment(work, nil)
			if err != nil {
				return "", err
			}
			if data, err := command.Output(); err != nil || !strings.Contains(string(data), required+" ") {
				return "", errors.New("downloaded Go toolchain failed its version check")
			}
			return path, nil
		}
	}
	return "", fmt.Errorf("no official %s archive for %s/%s", required, runtime.GOOS, runtime.GOARCH)
}

func isolatedEnvironment(work string, overrides map[string]string) ([]string, error) {
	dir := filepath.Join(work, "telemetry")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "mode"), []byte("off\n"), 0600); err != nil {
		return nil, err
	}
	values := map[string]string{"TEST_TELEMETRY_DIR": dir}
	for key, value := range overrides {
		values[key] = value
	}
	return environment(values), nil
}
