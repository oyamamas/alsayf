// Package gobuster adapts the pinned upstream CLI without launching a subprocess.
package gobuster

import (
	"context"
	"fmt"
	"io"

	"github.com/OJ/gobuster/v3/cli/dir"
	"github.com/OJ/gobuster/v3/cli/dns"
	"github.com/OJ/gobuster/v3/cli/fuzz"
	"github.com/OJ/gobuster/v3/cli/gcs"
	"github.com/OJ/gobuster/v3/cli/s3"
	"github.com/OJ/gobuster/v3/cli/tftp"
	"github.com/OJ/gobuster/v3/cli/vhost"
	"github.com/OJ/gobuster/v3/libgobuster"
	"github.com/urfave/cli/v2"
)

// Run creates fresh upstream commands for each invocation. Readers and writers
// control CLI help and parsing; upstream scan output still uses process stdio.
func Run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	app := &cli.App{
		Name:      "alsayf gbuster",
		Usage:     "Embedded Gobuster discovery modes",
		UsageText: "alsayf gbuster <mode> [options]",
		Version:   libgobuster.VERSION,
		Reader:    in,
		Writer:    out,
		ErrWriter: errOut,
		// Return errors to alsayf rather than letting the CLI framework exit.
		ExitErrHandler: func(*cli.Context, error) {},
		Action: func(c *cli.Context) error {
			if c.Args().Present() {
				return fmt.Errorf("unknown gbuster mode %q", c.Args().First())
			}
			return cli.ShowAppHelp(c)
		},
		Commands: []*cli.Command{
			dir.Command(), vhost.Command(), dns.Command(), fuzz.Command(),
			tftp.Command(), s3.Command(), gcs.Command(),
		},
	}
	for _, command := range app.Commands {
		command.Before = validateOptions
	}
	err := app.RunContext(ctx, append([]string{app.Name}, args...))
	if cancelErr := ctx.Err(); cancelErr != nil {
		return cancelErr
	}
	return err
}

func validateOptions(c *cli.Context) error {
	if err := c.Context.Err(); err != nil {
		return err
	}
	if c.Args().Present() {
		return fmt.Errorf("unexpected argument %q", c.Args().First())
	}
	if c.Int("threads") <= 0 {
		return fmt.Errorf("threads must be a positive integer")
	}
	if c.Duration("delay") < 0 {
		return fmt.Errorf("delay must not be negative")
	}
	if c.Duration("timeout") <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	return nil
}
