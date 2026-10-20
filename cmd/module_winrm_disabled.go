//go:build core && !winrm

package cmd

import "github.com/spf13/cobra"

func addWinRMCommand(*cobra.Command) {}
