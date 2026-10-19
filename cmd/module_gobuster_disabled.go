//go:build core && !gobuster

package cmd

import "github.com/spf13/cobra"

func addGobusterCommand(*cobra.Command) {}
