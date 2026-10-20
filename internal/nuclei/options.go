// Package nuclei integrates the pinned public Nuclei engine packages without
// the SDK wrapper's unconditional network update check.
package nuclei

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const Version = "3.11.1"

var ErrFindings = errors.New("nuclei findings detected")

type Options struct {
	Targets, Templates, Workflows, Tags, IDs   []string
	Headers, Variables, Resolvers              []string
	TargetList, Severity, Output, TemplateRoot string
	Concurrency, BulkSize, RateLimit           int
	Timeout, Retries                           int
	JSONL, ListTemplates, FailOnFindings       bool
}

func DefaultOptions() Options {
	return Options{Concurrency: 25, BulkSize: 25, RateLimit: 150, Timeout: 5, Retries: 1}
}

func (o Options) validate() error {
	if len(o.Templates)+len(o.Workflows) == 0 {
		return errors.New("provide local --templates or --workflows; templates are not downloaded automatically")
	}
	if o.Concurrency <= 0 || o.BulkSize <= 0 || o.RateLimit <= 0 || o.Timeout <= 0 || o.Retries < 0 {
		return errors.New("concurrency, bulk-size, rate-limit, and timeout must be positive; retries must not be negative")
	}
	if o.TemplateRoot != "" {
		info, err := os.Stat(o.TemplateRoot)
		if err != nil {
			return fmt.Errorf("cannot inspect template root: %w", err)
		}
		if !info.IsDir() {
			return errors.New("template-root must be a local directory")
		}
	}
	for _, path := range append(append([]string{}, o.Templates...), o.Workflows...) {
		if strings.Contains(path, "://") {
			return fmt.Errorf("template sources must be local paths: %q", path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("cannot inspect template path %q: %w", path, err)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("template path must be a regular file or directory: %q", path)
		}
	}
	if o.ListTemplates && (len(o.Targets) > 0 || o.TargetList != "") {
		return errors.New("--list-templates does not accept targets")
	}
	if !o.ListTemplates && len(o.Targets) == 0 && o.TargetList == "" {
		return errors.New("provide --target or --list")
	}
	if o.Output != "" {
		outputPath, err := filepath.Abs(o.Output)
		if err != nil {
			return err
		}
		if resolved, err := filepath.EvalSymlinks(outputPath); err == nil {
			outputPath = resolved
		}
		outputInfo, _ := os.Stat(outputPath)
		inputs := append(append([]string{}, o.Templates...), o.Workflows...)
		if o.TargetList != "" && o.TargetList != "-" {
			inputs = append(inputs, o.TargetList)
		}
		for _, path := range inputs {
			inputPath, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			if resolved, err := filepath.EvalSymlinks(inputPath); err == nil {
				inputPath = resolved
			}
			inputInfo, _ := os.Stat(inputPath)
			if inputPath == outputPath || (inputInfo != nil && outputInfo != nil && os.SameFile(inputInfo, outputInfo)) {
				return errors.New("output file must not overwrite an input file")
			}
			if inputInfo != nil && inputInfo.IsDir() {
				rel, err := filepath.Rel(inputPath, outputPath)
				ext := strings.ToLower(filepath.Ext(outputPath))
				if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && (ext == ".yaml" || ext == ".yml" || ext == ".json") {
					return errors.New("output file must not overwrite template files inside an input directory; use a separate output path or .jsonl")
				}
			}
		}
	}
	return nil
}

func (o Options) targets(in io.Reader) ([]string, error) {
	targets := append([]string{}, o.Targets...)
	if o.TargetList != "" {
		if o.TargetList != "-" {
			info, err := os.Stat(o.TargetList)
			if err != nil {
				return nil, fmt.Errorf("cannot inspect target list: %w", err)
			}
			if !info.Mode().IsRegular() {
				return nil, errors.New("target list must be a regular file")
			}
			file, err := os.Open(o.TargetList)
			if err != nil {
				return nil, fmt.Errorf("cannot open target list: %w", err)
			}
			defer file.Close()
			info, err = file.Stat()
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() {
				return nil, errors.New("target list must be a regular file")
			}
			in = file
		}
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			if line := strings.TrimSpace(scanner.Text()); line != "" && !strings.HasPrefix(line, "#") {
				targets = append(targets, line)
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("cannot read target list: %w", err)
		}
	}
	seen := make(map[string]bool)
	var unique []string
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" || strings.ContainsAny(target, "\r\n\t ") {
			return nil, fmt.Errorf("invalid target %q", target)
		}
		if strings.Contains(target, "://") {
			u, err := url.Parse(target)
			if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
				return nil, fmt.Errorf("target URL must use http or https: %q", target)
			}
		}
		if !seen[target] {
			unique = append(unique, target)
			seen[target] = true
		}
	}
	if len(unique) == 0 && !o.ListTemplates {
		return nil, errors.New("target list is empty")
	}
	return unique, nil
}
