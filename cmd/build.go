package cmd

import (
	"alsayf/internal/buildkit"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func addBuildCommands(root *cobra.Command) {
	enabled := []string{}
	for _, module := range buildkit.Modules() {
		for _, command := range root.Commands() {
			if command.Name() == module.Command {
				enabled = append(enabled, module.Name)
			}
		}
	}
	info := buildkit.CurrentInfo(enabled)
	var jsonOutput bool
	version := &cobra.Command{Use: "version", Short: "Show source revision, platform, and compiled modules", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if jsonOutput {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(info)
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Alsayf %s (modified=%t)\n%s/%s; %s\nModules: %s\n", info.Revision, info.Modified, info.OS, info.Arch, info.GoVersion, strings.Join(info.Modules, ", "))
		return err
	}}
	version.Flags().BoolVar(&jsonOutput, "json", false, "Write build metadata as JSON")
	root.AddCommand(version)
	var opts buildkit.Options
	var list bool
	var timeout time.Duration
	weaponize := &cobra.Command{
		Use:     "weaponize <module> [module...]",
		Short:   "Download build prerequisites and rebuild Alsayf with additional native modules",
		Long:    "Preserve compiled modules, obtain matching Alsayf sources and Go if needed, build and verify a replacement executable, then remove temporary build files. Requires network access unless local sources, a compatible Go toolchain, and a populated dependency cache are provided. External backends are not implemented.",
		Example: "  alsayf weaponize nuclei\n  alsayf weaponize secrets gbuster\n  alsayf weaponize --list\n  alsayf weaponize nuclei --source-dir ./alsayf --offline --cache-dir ./build-cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			if list {
				if len(args) > 0 {
					return errors.New("--list does not accept module names")
				}
				for _, m := range buildkit.Modules() {
					state := "available"
					for _, name := range info.Modules {
						if m.Name == name {
							state = "enabled"
						}
					}
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%-10s %-9s %s\n", m.Command, state, m.Description); err != nil {
						return err
					}
				}
				return nil
			}
			if len(args) == 0 {
				return errors.New("specify at least one native module, or --list")
			}
			if timeout <= 0 {
				return errors.New("--timeout must be positive")
			}
			requested, err := buildkit.Normalize(args)
			if err != nil {
				return err
			}
			combined, err := buildkit.Normalize(append(append([]string{}, info.Modules...), requested...))
			if err != nil {
				return err
			}
			if len(combined) == len(info.Modules) && opts.Ref == "" && opts.SourceDir == "" && opts.Output == "" {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "Requested modules are already compiled in.")
				return err
			}
			if opts.SourceDir != "" && opts.Ref != "" {
				return errors.New("--source-dir and --ref are mutually exclusive")
			}
			if opts.SourceDir == "" && opts.Ref == "" {
				if info.Modified {
					return errors.New("this executable was built from modified sources; supply --source-dir or explicitly choose --ref")
				}
				if info.Revision == "" {
					return errors.New("this executable has no source revision; supply --source-dir or --ref")
				}
				opts.Ref = info.Revision
			}
			opts.Modules = combined
			opts.OS = runtime.GOOS
			opts.Arch = runtime.GOARCH
			if opts.Output == "" {
				path, err := os.Executable()
				if err != nil {
					return err
				}
				path, err = filepath.EvalSymlinks(path)
				if err != nil {
					return err
				}
				opts.Output = path
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			_, err = buildkit.Build(ctx, opts, cmd.ErrOrStderr())
			return err
		},
	}
	flags := weaponize.Flags()
	flags.BoolVar(&list, "list", false, "List native modules and their compiled status")
	flags.StringVar(&opts.Repository, "repository", info.Repository, "GitHub owner/repository for sources")
	flags.StringVar(&opts.Ref, "ref", "", "Source commit, tag, or branch (default: this executable's revision)")
	flags.StringVar(&opts.SourceDir, "source-dir", "", "Use local Alsayf sources instead of downloading")
	flags.StringVar(&opts.GoBinary, "go", "", "Go executable (automatically downloaded if missing or too old)")
	flags.StringVar(&opts.CacheDir, "cache-dir", "", "Persistent dependency/build cache directory (default: temporary, removed afterwards)")
	flags.BoolVar(&opts.Offline, "offline", false, "Disable prerequisite/dependency downloads; requires local sources and caches")
	flags.StringVarP(&opts.Output, "output", "o", "", "Write a verified executable here instead of replacing this one")
	flags.DurationVar(&timeout, "timeout", 30*time.Minute, "Overall download/build timeout")
	root.AddCommand(weaponize)
}
