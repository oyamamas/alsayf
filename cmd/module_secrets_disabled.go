//go:build core && !secrets

package cmd

import "github.com/spf13/cobra"

func addSecretsCommand(*cobra.Command) {}
