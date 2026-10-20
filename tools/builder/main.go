// The interactive entry point is the repository's ./builder Python script.
// This worker shares its build, validation, and installation code with Alsayf.
package main

import (
	"alsayf/internal/buildkit"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"time"
)

func main() {
	var opts buildkit.Options
	var modules string
	var timeout time.Duration
	flag.StringVar(&modules, "modules", "", "core, all, or comma-separated native module names")
	flag.StringVar(&opts.SourceDir, "source-dir", ".", "local source directory")
	flag.StringVar(&opts.Output, "output", "", "output executable")
	flag.StringVar(&opts.OS, "os", runtime.GOOS, "target operating system")
	flag.StringVar(&opts.Arch, "arch", runtime.GOARCH, "target architecture")
	flag.StringVar(&opts.GoBinary, "go", "", "Go executable")
	flag.StringVar(&opts.CacheDir, "cache-dir", "", "persistent cache directory")
	flag.BoolVar(&opts.Offline, "offline", false, "disable downloads")
	flag.DurationVar(&timeout, "timeout", 30*time.Minute, "overall build timeout")
	flag.Parse()
	if flag.NArg() != 0 || timeout <= 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments or non-positive timeout")
		os.Exit(1)
	}
	selected, err := buildkit.ParseModules(modules)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	opts.Modules = selected
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	go func() { <-signalCtx.Done(); stop() }()
	ctx, cancel := context.WithTimeout(signalCtx, timeout)
	_, err = buildkit.Build(ctx, opts, os.Stderr)
	cancel()
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Build failed:", err)
		os.Exit(1)
	}
}
