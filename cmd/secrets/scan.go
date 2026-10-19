package secrets

import (
	"errors"
	"fmt"

	"alsayf/internal/secrets"
	"github.com/spf13/cobra"
)

func newScanCommand() *cobra.Command {
	opts := secrets.Options{}
	var format string
	var failOnFindings bool
	cmd := &cobra.Command{
		Use:     "scan <path> [path...]",
		Short:   "Scan local files and directories for secrets",
		Args:    cobra.MinimumNArgs(1),
		Example: "  alsayf secrets scan ./project\n  alsayf secrets scan ./project --exclude node_modules --exclude '*.lock' --format json",
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != "text" && format != "json" {
				return fmt.Errorf("unsupported format %q (use text or json)", format)
			}
			report, scanErr := secrets.Scan(cmd.Context(), args, opts)
			if scanErr != nil && !errors.Is(scanErr, secrets.ErrIncomplete) {
				return scanErr
			}
			if err := secrets.WriteReport(cmd.OutOrStdout(), report, format); err != nil {
				return err
			}
			if scanErr != nil {
				return scanErr
			}
			if failOnFindings && len(report.Findings) > 0 {
				return errors.New("secrets detected")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json (both redact secrets)")
	cmd.Flags().Int64Var(&opts.MaxFileSize, "max-file-size", secrets.DefaultMaxFileSize, "Maximum file size in bytes")
	cmd.Flags().StringArrayVar(&opts.Exclude, "exclude", nil, "Exclude a filename, relative path, or absolute path glob; repeatable")
	cmd.Flags().BoolVar(&failOnFindings, "fail-on-findings", false, "Return a nonzero exit code if secrets are detected")
	return cmd
}
