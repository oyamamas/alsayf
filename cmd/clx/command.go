// Package clx embeds CloudExec's infrastructure commands under alsayf clx.
package clx

import (
	"fmt"
	"net/netip"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

// NewCommand returns a fresh command tree, with instance-local flag values.
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "clx",
		Short:             "CloudExec infrastructure discovery and service modules",
		PersistentPreRunE: validateOptions,
	}
	cmd.AddCommand(
		NewCmdClickhouse(), newConsulCommand(), newEtcdCommand(),
		newExportersCommand(), newGitlabCommand(), newGrafanaCommand(),
		newKafkaCommand(), newKubeCommand(), newPostgresCommand(),
		newRedisCommand(), newRegistryCommand(), newZkCommand(),
	)
	return cmd
}

var hostnamePattern = regexp.MustCompile(`^([a-zA-Z0-9-]{1,63}\.)+[a-zA-Z]{2,}$`)

// Validate before invoking legacy handlers: zero workers can deadlock them and
// invalid targets/input files otherwise terminate the process with os.Exit.
func validateOptions(cmd *cobra.Command, args []string) error {
	if err := cmd.Context().Err(); err != nil {
		return err
	}
	if f := cmd.Flags().Lookup("threads"); f != nil {
		threads, err := strconv.Atoi(f.Value.String())
		if err != nil || threads <= 0 {
			return fmt.Errorf("threads must be a positive integer")
		}
	}
	if f := cmd.Flags().Lookup("port"); f != nil && f.Value.String() != "" {
		port, err := strconv.Atoi(f.Value.String())
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("port must be between 1 and 65535")
		}
	}
	if f := cmd.Flags().Lookup("timeout"); f != nil && f.Value.String() != "" {
		duration, err := time.ParseDuration(f.Value.String() + "s")
		if err != nil || duration <= 0 {
			return fmt.Errorf("timeout must be a positive number of seconds")
		}
	}
	if f := cmd.Flags().Lookup("list-modules"); f != nil && f.Value.String() == "true" {
		return nil
	}
	if f := cmd.Flags().Lookup("inputlist"); f != nil && f.Value.String() != "" {
		info, err := os.Stat(f.Value.String())
		if err != nil {
			return fmt.Errorf("cannot inspect input list: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("input list must be a regular file")
		}
		file, err := os.Open(f.Value.String())
		if err != nil {
			return fmt.Errorf("cannot open input list: %w", err)
		}
		defer file.Close()
		info, err = file.Stat()
		if err != nil {
			return fmt.Errorf("cannot inspect input list: %w", err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("input list must be a regular file")
		}
		return nil
	}
	if len(args) == 0 {
		return fmt.Errorf("provide a host, network, or --inputlist file")
	}
	if _, err := netip.ParseAddr(args[0]); err == nil {
		return nil
	}
	if _, err := netip.ParsePrefix(args[0]); err == nil {
		return nil
	}
	if hostnamePattern.MatchString(args[0]) {
		return nil
	}
	return fmt.Errorf("invalid host or network %q", args[0])
}
