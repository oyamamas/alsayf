package nuclei

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/logrusorgru/aurora/v4"
	"github.com/miekg/dns"
	"github.com/projectdiscovery/goflags"
	"github.com/projectdiscovery/gologger"
	"github.com/projectdiscovery/gologger/formatter"
	"github.com/projectdiscovery/gologger/levels"
	"github.com/projectdiscovery/nuclei/v3/pkg/catalog/config"
	"github.com/projectdiscovery/nuclei/v3/pkg/catalog/disk"
	"github.com/projectdiscovery/nuclei/v3/pkg/catalog/loader"
	"github.com/projectdiscovery/nuclei/v3/pkg/core"
	"github.com/projectdiscovery/nuclei/v3/pkg/input"
	"github.com/projectdiscovery/nuclei/v3/pkg/input/provider"
	"github.com/projectdiscovery/nuclei/v3/pkg/loader/workflow"
	"github.com/projectdiscovery/nuclei/v3/pkg/protocols"
	"github.com/projectdiscovery/nuclei/v3/pkg/protocols/common/interactsh"
	"github.com/projectdiscovery/nuclei/v3/pkg/protocols/common/protocolinit"
	"github.com/projectdiscovery/nuclei/v3/pkg/templates"
	"github.com/projectdiscovery/nuclei/v3/pkg/types"
	"github.com/projectdiscovery/nuclei/v3/pkg/utils/stats"
	"github.com/projectdiscovery/ratelimit"
)

// Public Nuclei protocol pools contain process-global state. The CLI runs one
// scan per process; serialize engine setup/execution for embedded callers too.
var engineMu sync.Mutex

func Run(ctx context.Context, opts Options, in io.Reader, out, errOut io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := opts.validate(); err != nil {
		return err
	}
	targets, err := opts.targets(in)
	if err != nil {
		return err
	}
	native, err := engineOptions(opts)
	if err != nil {
		return err
	}
	engineMu.Lock()
	defer engineMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	logs := &logWriter{out: errOut}
	logger := &gologger.Logger{}
	logger.SetFormatter(formatter.NewCLI(false))
	logger.SetWriter(logs)
	logger.SetMaxLevel(levels.LevelWarning)
	native.Logger = logger
	root := templateRoot(native)
	if opts.TemplateRoot != "" {
		root, err = filepath.Abs(opts.TemplateRoot)
		if err != nil {
			return err
		}
	}
	previousRoot := config.DefaultConfig.TemplatesDirectory
	config.DefaultConfig.SetTemplatesDir(root)
	defer config.DefaultConfig.SetTemplatesDir(previousRoot)

	// Do not import lib (SDK), installer, or internal/runner: their setup can
	// check for updates. This adapter wires only the public execution packages.
	config.DefaultConfig.DisableUpdateCheck()
	if err := ensureLocalIgnoreFile(); err != nil {
		return err
	}
	if err := protocolinit.Init(native); err != nil {
		protocolinit.Close(native.ExecutionId)
		return fmt.Errorf("cannot initialize nuclei protocols: %w", err)
	}
	defer protocolinit.Close(native.ExecutionId)
	tmp, err := os.MkdirTemp("", "alsayf-nuclei-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	parser := templates.NewParser()
	defer parser.Purge()
	native.Parser = parser
	progress := &scanProgress{}
	writer := &resultWriter{out: out, jsonl: opts.JSONL, colorizer: aurora.New(aurora.WithColors(false))}
	limiter := ratelimit.New(ctx, uint(native.RateLimit), time.Second)
	defer limiter.Stop()
	iopts := interactsh.DefaultOptions(writer, nil, progress)
	iopts.NoInteractsh = true
	iopts.Logger = logger
	iopts.CooldownPeriod = 0
	interactions, err := interactsh.New(iopts)
	if err != nil {
		return err
	}
	defer interactions.Close()
	catalog := &localCatalog{DiskCatalog: disk.NewCatalog(root), skipped: make(map[string]bool)}
	execution := &protocols.ExecutorOptions{
		Options: native, Output: writer, Progress: progress,
		Catalog: catalog, RateLimiter: limiter, Interactsh: interactions,
		Colorizer: writer.colorizer, ResumeCfg: types.NewResumeCfg(),
		Parser: parser, InputHelper: input.NewHelper(),
		TemporaryDirectory: tmp, Logger: logger,
	}
	workflowLoader, err := workflow.NewLoader(execution)
	if err != nil {
		return err
	}
	execution.WorkflowLoader = workflowLoader
	store, err := loader.New(loader.NewConfig(native, catalog, execution))
	if err != nil {
		return fmt.Errorf("cannot create nuclei template loader: %w", err)
	}
	if err := store.ValidateTemplates(); err != nil {
		return fmt.Errorf("invalid local nuclei templates: %w", err)
	}
	parseErrorsBefore := templateErrors()
	if err := store.Load(); err != nil {
		return fmt.Errorf("cannot load nuclei templates: %w", err)
	}
	if logs.errorCount() > 0 || templateErrors() > parseErrorsBefore {
		return errors.New("nuclei reported template loading errors; see diagnostics")
	}
	if skipped := catalog.skippedCount(); skipped > 0 {
		fmt.Fprintf(errOut, "Skipped %d template files using disabled Interactsh.\n", skipped)
	}
	runnable := append(append([]*templates.Template{}, store.Templates()...), store.Workflows()...)
	if len(runnable) == 0 {
		return errors.New("no runnable templates matched; check local paths and filters (OAST, headless, code, and self-contained templates are disabled)")
	}
	var outputFile *os.File
	if opts.Output != "" {
		outputFile, err = os.OpenFile(opts.Output, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return fmt.Errorf("cannot create output file: %w", err)
		}
		defer outputFile.Close()
		out = io.MultiWriter(out, outputFile)
		writer.out = out
	}
	if opts.ListTemplates {
		return listTemplates(out, runnable, opts.JSONL)
	}
	inputs := provider.NewSimpleInputProviderWithUrls(native.ExecutionId, targets...)
	defer inputs.Close()
	engine := core.New(native)
	engine.SetExecuterOptions(execution)
	engine.ExecuteScanWithOpts(ctx, runnable, inputs, false)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writer.resultError(); err != nil {
		return err
	}
	if progress.errors.Load() > 0 || logs.errorCount() > 0 {
		return fmt.Errorf("nuclei scan incomplete: %d request errors; see diagnostics", progress.errors.Load())
	}
	if outputFile != nil {
		if err := outputFile.Close(); err != nil {
			return fmt.Errorf("cannot close output file: %w", err)
		}
	}
	if opts.FailOnFindings && writer.ResultCount() > 0 {
		return ErrFindings
	}
	return nil
}

func templateRoot(opts *types.Options) string {
	var path string
	if len(opts.Templates) > 0 {
		path = opts.Templates[0]
	} else {
		path = opts.Workflows[0]
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

func templateErrors() int64 {
	return stats.GetValue(templates.TemplateSyntaxErrorStats) + stats.GetValue(templates.TemplateRuntimeWarningStats)
}

func ensureLocalIgnoreFile() error {
	path := config.DefaultConfig.GetIgnoreFilePath()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot initialize local nuclei ignore file: %w", err)
	}
	return file.Close()
}

func engineOptions(o Options) (*types.Options, error) {
	n := types.DefaultOptions()
	n.TemplateThreads, n.BulkSize, n.RateLimit = o.Concurrency, o.BulkSize, o.RateLimit
	n.Timeout, n.Retries = o.Timeout, o.Retries
	n.PayloadConcurrency = o.Concurrency
	n.NoInteractsh, n.NoColor, n.OmitTemplate = true, true, true
	n.OmitRawRequests = true
	n.PublicTemplateDisableDownload, n.GitHubTemplateDisableDownload = true, true
	n.GitLabTemplateDisableDownload, n.AwsTemplateDisableDownload, n.AzureTemplateDisableDownload = true, true, true
	n.CustomHeaders = append(goflags.StringSlice{}, o.Headers...)
	n.Tags = append(goflags.StringSlice{}, o.Tags...)
	n.IncludeIds = append(goflags.StringSlice{}, o.IDs...)
	if err := n.Severities.Set(o.Severity); err != nil {
		return nil, fmt.Errorf("invalid severity: %w", err)
	}
	for _, variable := range o.Variables {
		key, _, ok := strings.Cut(variable, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("template variable must use key=value: %q", variable)
		}
		if err := n.Vars.Set(variable); err != nil {
			return nil, fmt.Errorf("invalid template variable: %w", err)
		}
	}
	for _, path := range o.Templates {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		n.Templates = append(n.Templates, abs)
	}
	for _, path := range o.Workflows {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		n.Workflows = append(n.Workflows, abs)
	}
	resolvers := append([]string{}, o.Resolvers...)
	if len(resolvers) == 0 {
		config, err := dns.ClientConfigFromFile("/etc/resolv.conf")
		if err != nil || len(config.Servers) == 0 {
			return nil, errors.New("cannot discover system DNS resolvers; provide --resolver explicitly")
		}
		for _, server := range config.Servers {
			resolvers = append(resolvers, net.JoinHostPort(server, config.Port))
		}
	}
	n.InternalResolversList = resolvers
	n.SystemResolvers = false
	return n, nil
}

func listTemplates(out io.Writer, loaded []*templates.Template, jsonl bool) error {
	for _, template := range loaded {
		if jsonl {
			if err := json.NewEncoder(out).Encode(struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				Severity string `json:"severity"`
			}{template.ID, template.Info.Name, template.Info.SeverityHolder.Severity.String()}); err != nil {
				return err
			}
		} else if _, err := fmt.Fprintf(out, "%s\t%s\t%q\n", template.ID, template.Info.SeverityHolder.Severity.String(), template.Info.Name); err != nil {
			return err
		}
	}
	return nil
}
