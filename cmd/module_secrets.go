//go:build !core || secrets

package cmd

import (
	secretscmd "alsayf/cmd/secrets"

	"github.com/spf13/cobra"
)

func addSecretsCommand(cmd *cobra.Command) {
	cmd.AddCommand(secretscmd.NewCommand())
}
