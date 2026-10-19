package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alsayf",
		Short: "A modular security toolkit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	addModuleCommands(cmd)
	return cmd
}

func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	// Restore the default signal action after cancellation so a second Ctrl+C
	// can terminate legacy CloudExec handlers that do not observe the context.
	go func() {
		<-ctx.Done()
		stop()
	}()
	cmd := NewRootCommand()
	if err := cmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
		os.Exit(1)
	}
}
