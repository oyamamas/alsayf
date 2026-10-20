//go:build core && !nuclei

package cmd

import "github.com/spf13/cobra"

func addNucleiCommand(*cobra.Command) {}
