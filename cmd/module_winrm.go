//go:build !core || winrm

package cmd

import (
	winrmcmd "alsayf/cmd/winrm"

	"github.com/spf13/cobra"
)

func addWinRMCommand(cmd *cobra.Command) {
	cmd.AddCommand(winrmcmd.NewCommand())
}
