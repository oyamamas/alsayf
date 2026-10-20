// Package nuclei exposes the optional Nuclei module.
package nuclei

import (
	"alsayf/internal/nuclei"

	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	opts := nuclei.DefaultOptions()
	cmd := &cobra.Command{
		Use:     "nuclei",
		Short:   "Nuclei: vulnerability checks with local templates",
		Long:    "Run Nuclei with explicitly supplied local templates and workflows. Automatic updates, cloud uploads, and Interactsh are disabled. Headless and code templates are not enabled.",
		Version: nuclei.Version,
		Args:    cobra.NoArgs,
		Example: "  alsayf nuclei -u https://service.internal -t ./templates --severity high,critical\n  alsayf nuclei -l targets.txt -t ./templates --jsonl\n  alsayf nuclei -t ./templates --list-templates",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return nuclei.Run(cmd.Context(), opts, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	f := cmd.Flags()
	f.StringArrayVarP(&opts.Targets, "target", "u", nil, "Target URL, hostname, or IP; repeatable")
	f.StringVarP(&opts.TargetList, "list", "l", "", "Target list file, or - for stdin")
	f.StringArrayVarP(&opts.Templates, "templates", "t", nil, "Local template file or directory; repeatable")
	f.StringArrayVarP(&opts.Workflows, "workflows", "w", nil, "Local workflow file or directory; repeatable")
	f.StringVar(&opts.TemplateRoot, "template-root", "", "Local template root for relative workflow references and tag selection")
	f.StringVar(&opts.Severity, "severity", "", "Comma-separated severities: info, low, medium, high, critical, unknown")
	f.StringSliceVar(&opts.Tags, "tags", nil, "Template tags to include")
	f.StringSliceVar(&opts.IDs, "id", nil, "Template IDs to include")
	f.IntVarP(&opts.Concurrency, "concurrency", "c", opts.Concurrency, "Maximum concurrent templates")
	f.IntVar(&opts.BulkSize, "bulk-size", opts.BulkSize, "Maximum concurrent targets per template")
	f.IntVar(&opts.RateLimit, "rate-limit", opts.RateLimit, "Maximum requests per second")
	f.IntVar(&opts.Timeout, "timeout", opts.Timeout, "Connection timeout in seconds")
	f.IntVar(&opts.Retries, "retries", opts.Retries, "Retries after request failures")
	f.StringArrayVarP(&opts.Headers, "header", "H", nil, "Custom HTTP header; repeatable")
	f.StringArrayVar(&opts.Variables, "var", nil, "Template variable as key=value; repeatable")
	f.StringArrayVar(&opts.Resolvers, "resolver", nil, "DNS resolver address; repeatable (default: system resolvers)")
	f.BoolVar(&opts.JSONL, "jsonl", false, "Write findings as JSON Lines")
	f.StringVarP(&opts.Output, "output", "o", "", "Also write results to this file")
	f.BoolVar(&opts.ListTemplates, "list-templates", false, "List runnable templates without scanning targets")
	f.BoolVar(&opts.FailOnFindings, "fail-on-findings", false, "Return a nonzero exit status when findings are detected")
	return cmd
}
