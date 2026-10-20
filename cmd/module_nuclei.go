//go:build !core || nuclei

package cmd

import (
	nucleicmd "alsayf/cmd/nuclei"

	"github.com/spf13/cobra"
)

func addNucleiCommand(cmd *cobra.Command) {
	cmd.AddCommand(nucleicmd.NewCommand())
}
