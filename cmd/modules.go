package cmd

import "github.com/spf13/cobra"

func addModuleCommands(cmd *cobra.Command) {
	addSecretsCommand(cmd)
	addCloudExecCommand(cmd)
	addGobusterCommand(cmd)
}
