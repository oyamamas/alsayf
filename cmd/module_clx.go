//go:build !core || clx

package cmd

import (
	"alsayf/cmd/clx"

	"github.com/spf13/cobra"
)

func addCloudExecCommand(cmd *cobra.Command) {
	cmd.AddCommand(clx.NewCommand())
}
