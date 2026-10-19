// Package gobuster exposes the embedded Gobuster CLI as an optional module.
package gobuster

import (
	"alsayf/internal/gobuster"

	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "gbuster <mode> [flags]",
		Short:              "Gobuster: directory, DNS, virtual host, and wordlist-based discovery",
		DisableFlagParsing: true,
		Example:            "  alsayf gbuster dir --help\n  alsayf gbuster dns --help",
		RunE: func(cmd *cobra.Command, args []string) error {
			return gobuster.Run(cmd.Context(), args, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
}
