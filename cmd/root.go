package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"alsayf/cmd/clx"
	secretscmd "alsayf/cmd/secrets"
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "alsayf",
		Short:             "A modular security toolkit",
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	cmd.AddCommand(secretscmd.NewCommand(), clx.NewCommand())
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
