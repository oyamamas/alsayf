package buildkit

import (
	"context"
	"debug/buildinfo"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Options struct {
	Modules                                                          []string
	SourceDir, Repository, Ref, GoBinary, CacheDir, Output, OS, Arch string
	Offline                                                          bool
}
type Result struct {
	Output   string
	Revision string
	Pending  bool
}

func TargetValid(targetOS, arch string) bool {
	return (targetOS == "linux" || targetOS == "darwin" || targetOS == "windows") && (arch == "amd64" || arch == "arm64")
}
func Build(ctx context.Context, opts Options, log io.Writer) (result Result, err error) {
	modules, err := Normalize(opts.Modules)
	if err != nil {
		return result, err
	}
	if opts.OS == "" {
		opts.OS = runtime.GOOS
	}
	if opts.Arch == "" {
		opts.Arch = runtime.GOARCH
	}
	if !TargetValid(opts.OS, opts.Arch) {
		return result, fmt.Errorf("unsupported build target %s/%s (supported: linux/darwin/windows, amd64/arm64)", opts.OS, opts.Arch)
	}
	if opts.Output == "" {
		return result, errors.New("build output is required")
	}
	source := opts.SourceDir
	revision := opts.Ref
	modified := false
	if source != "" {
		source, err = filepath.Abs(source)
		if err != nil {
			return result, err
		}
		// Capture provenance before our output lock and staging directory can
		// appear as untracked files inside a clean source checkout.
		revision, modified = gitRevision(ctx, source)
	}
	output, err := filepath.Abs(opts.Output)
	if err != nil {
		return result, err
	}
	result.Output = output
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return result, err
	}
	if err := checkOutput(output); err != nil {
		return result, err
	}
	lock, err := os.OpenFile(output+".weaponize.lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, fmt.Errorf("acquire output lock (remove a stale lock only after its builder has stopped): %w", err)
	}
	fmt.Fprintf(lock, "%d\n", os.Getpid())
	lock.Close()
	defer func() {
		if !result.Pending {
			if removeErr := os.Remove(output + ".weaponize.lock"); removeErr != nil && !os.IsNotExist(removeErr) {
				err = errors.Join(err, removeErr)
			}
		}
	}()
	stage, err := os.MkdirTemp(filepath.Dir(output), ".alsayf-stage-")
	if err != nil {
		return result, err
	}
	defer func() {
		if !result.Pending {
			err = errors.Join(err, removeWork(stage))
		}
	}()
	work, err := os.MkdirTemp("", "alsayf-build-")
	if err != nil {
		return result, err
	}
	defer func() {
		if cleanupErr := removeWork(work); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove temporary build files %s: %w", work, cleanupErr))
		}
	}()
	repository := opts.Repository
	if repository == "" {
		repository = SourceRepository
	}
	if !repositoryPattern.MatchString(repository) || strings.Contains(repository, "..") {
		return result, errors.New("repository must be a GitHub owner/name")
	}
	if source == "" {
		if opts.Offline {
			return result, errors.New("offline builds require --source-dir")
		}
		if revision == "" {
			return result, errors.New("source ref is required for downloaded sources")
		}
		source, revision, err = fetchSource(ctx, repository, revision, work, log)
		if err != nil {
			return result, err
		}
	}
	if revision != "" && !revisionPattern.MatchString(revision) {
		return result, errors.New("source revision must resolve to a commit hash")
	}
	result.Revision = revision
	data, err := os.ReadFile(filepath.Join(source, "go.mod"))
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0]) != "module alsayf" {
		return result, errors.New("source directory is not an Alsayf module")
	}
	var available []Module
	data, err = os.ReadFile(filepath.Join(source, "internal", "buildkit", "modules.json"))
	if err != nil {
		return result, fmt.Errorf("source must support the weaponize build protocol: %w", err)
	}
	if err := json.Unmarshal(data, &available); err != nil {
		return result, err
	}
	for _, module := range modules {
		found := false
		for _, m := range available {
			if m.Name == module {
				found = true
				break
			}
		}
		if !found {
			return result, fmt.Errorf("source revision does not support module %s", module)
		}
	}
	required, err := requiredGo(source)
	if err != nil {
		return result, err
	}
	compiler, err := ensureGo(ctx, opts.GoBinary, required, work, opts.Offline, log)
	if err != nil {
		return result, err
	}
	cache := opts.CacheDir
	if cache == "" {
		cache = filepath.Join(work, "cache")
	} else {
		cache, err = filepath.Abs(cache)
		if err != nil {
			return result, err
		}
	}
	for _, dir := range []string{filepath.Join(cache, "mod"), filepath.Join(cache, "build"), filepath.Join(work, "tmp"), filepath.Join(work, "gopath")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return result, err
		}
	}
	values := map[string]string{"GOOS": opts.OS, "GOARCH": opts.Arch, "GOMODCACHE": filepath.Join(cache, "mod"), "GOCACHE": filepath.Join(cache, "build"), "GOTMPDIR": filepath.Join(work, "tmp"), "GOPATH": filepath.Join(work, "gopath")}
	if opts.Offline {
		values["GOPROXY"] = "off"
		values["GOSUMDB"] = "off"
		values["GONOPROXY"] = "none"
		values["GOPRIVATE"] = ""
	}
	suffix := ""
	if opts.OS == "windows" {
		suffix = ".exe"
	}
	candidate := filepath.Join(stage, "alsayf"+suffix)
	tags := strings.Join(append([]string{"core"}, modules...), ",")
	flags := fmt.Sprintf("-s -w -X alsayf/internal/buildkit.SourceRepository=%s -X alsayf/internal/buildkit.SourceRevision=%s -X alsayf/internal/buildkit.SourceModified=%t", repository, revision, modified)
	fmt.Fprintf(log, "Building %s/%s with tags %s\n", opts.OS, opts.Arch, tags)
	command := exec.CommandContext(ctx, compiler, "build", "-mod=readonly", "-buildvcs=false", "-trimpath", "-ldflags", flags, "-tags", tags, "-o", candidate, ".")
	command.Dir = source
	command.Env, err = isolatedEnvironment(work, values)
	if err != nil {
		return result, err
	}
	command.Stdout = log
	command.Stderr = log
	prepareCommand(command)
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, fmt.Errorf("build failed; existing executable retained: %w", err)
	}
	if err := verify(ctx, candidate, opts.OS, opts.Arch, modules, revision); err != nil {
		return result, fmt.Errorf("candidate validation failed; existing executable retained: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	mode := os.FileMode(0755)
	if old, statErr := os.Stat(output); statErr == nil {
		mode = old.Mode().Perm()
	}
	if err := os.Chmod(candidate, mode); err != nil {
		return result, err
	}
	file, err := os.OpenFile(candidate, os.O_RDWR, 0)
	if err != nil {
		return result, err
	}
	syncErr := file.Sync()
	file.Close()
	if syncErr != nil {
		return result, syncErr
	}
	pending, err := install(candidate, output, stage, log)
	result.Pending = pending
	if err != nil {
		return result, err
	}
	if pending {
		fmt.Fprintln(log, "Build verified; replacement will finish after this process exits.")
	} else {
		fmt.Fprintf(log, "Installed %s\n", output)
	}
	return result, nil
}
func checkOutput(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("output must be a regular file, not a directory or symlink")
	}
	binary, err := buildinfo.ReadFile(path)
	if err != nil || binary.Path != "alsayf" {
		return errors.New("refusing to overwrite an existing file that is not an Alsayf executable")
	}
	return nil
}
func verify(ctx context.Context, path, targetOS, arch string, modules []string, revision string) error {
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return err
	}
	if info.Path != "alsayf" {
		return errors.New("candidate is not an Alsayf executable")
	}
	settings := map[string]string{}
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}
	expectedTags := strings.Join(append([]string{"core"}, modules...), ",")
	if settings["GOOS"] != targetOS || settings["GOARCH"] != arch || settings["-tags"] != expectedTags {
		return errors.New("candidate platform or module build tags do not match the requested profile")
	}
	if targetOS != runtime.GOOS || arch != runtime.GOARCH {
		return nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(checkCtx, path, "version", "--json")
	data, err := command.Output()
	if err != nil {
		return fmt.Errorf("candidate version self-check: %w", err)
	}
	var actual Info
	if err := json.Unmarshal(data, &actual); err != nil {
		return err
	}
	if actual.Schema != 1 || actual.OS != targetOS || actual.Arch != arch || strings.Join(actual.Modules, ",") != strings.Join(modules, ",") || actual.Revision != revision {
		return errors.New("candidate self-check metadata does not match the requested build")
	}
	return nil
}
