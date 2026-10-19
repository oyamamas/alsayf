// Package secrets exposes the secrets module through Cobra commands.
package secrets

import "github.com/spf13/cobra"

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Find secrets locally, without network access",
		Long:  "Scan local UTF-8 files with embedded detection rules. Credentials are never validated or sent to external services.",
	}
	cmd.AddCommand(newScanCommand(), newRulesCommand())
	return cmd
}
