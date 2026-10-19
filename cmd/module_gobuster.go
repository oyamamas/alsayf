//go:build !core || gobuster

package cmd

import (
	gobustercmd "alsayf/cmd/gobuster"

	"github.com/spf13/cobra"
)

func addGobusterCommand(cmd *cobra.Command) {
	cmd.AddCommand(gobustercmd.NewCommand())
}
