// Package winrm exposes the optional Windows remote administration module.
package winrm

import (
	"alsayf/internal/winrm"

	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	opts := winrm.DefaultOptions()
	root := &cobra.Command{
		Use:   "winrm",
		Short: "WinRM: remote Windows commands, PowerShell, and CMD sessions",
		Long:  "Administer Windows hosts through WinRM/WinRS using a built-in Go client. Supports NTLM and Basic authentication. HTTPS is the default; --http selects NTLM with WinRM message encryption. The remote WinRM service must already be configured.",
		Args:  cobra.NoArgs,
	}
	f := root.PersistentFlags()
	f.StringVarP(&opts.Host, "host", "H", "", "Remote hostname or IP (without a scheme or port)")
	f.IntVar(&opts.Port, "port", 0, "WinRM port (default: 5986 for HTTPS, 5985 for HTTP)")
	f.StringVarP(&opts.User, "user", "u", "", `Username, DOMAIN\user, or user@domain`)
	f.StringVarP(&opts.Password, "password", "p", "", "Password (default: ALSAYF_WINRM_PASSWORD, or a hidden terminal prompt)")
	f.StringVar(&opts.PasswordEnv, "password-env", "", "Read password from this environment variable")
	f.StringVar(&opts.PasswordFile, "password-file", "", "Read password from a file; remove one trailing newline")
	f.StringVar(&opts.Auth, "auth", opts.Auth, "Authentication: ntlm or basic (Basic requires HTTPS)")
	f.BoolVar(&opts.HTTP, "http", false, "Use HTTP with NTLM message encryption")
	f.StringVar(&opts.CACert, "ca-cert", "", "PEM CA bundle for verifying the HTTPS server")
	f.StringVar(&opts.TLSServerName, "tls-server-name", "", "Expected TLS certificate hostname")
	f.BoolVar(&opts.Insecure, "insecure", false, "Skip HTTPS certificate verification")
	f.DurationVar(&opts.RequestTimeout, "request-timeout", opts.RequestTimeout, "Network I/O timeout per request")
	f.DurationVar(&opts.Timeout, "timeout", 0, "Overall session timeout (0 means no deadline)")
	root.MarkFlagsMutuallyExclusive("password", "password-env", "password-file")
	run := func(cmd *cobra.Command, mode winrm.Mode, text, file string) error {
		o := opts
		o.PasswordSet = cmd.Flags().Changed("password")
		return winrm.Run(cmd.Context(), o, mode, text, file, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
	}
	exec := &cobra.Command{
		Use:     "exec <command>",
		Short:   "Execute one CMD command and stream stdout/stderr",
		Args:    cobra.ExactArgs(1),
		Example: `  alsayf winrm exec -H server.internal -u 'CONTOSO\admin' 'whoami /all'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, winrm.Exec, args[0], "")
		},
	}
	var scriptFile string
	ps := &cobra.Command{
		Use:     "powershell [script]",
		Aliases: []string{"ps"},
		Short:   "Run PowerShell code, a local script file, or stdin (--file -)",
		Args:    cobra.MaximumNArgs(1),
		Example: "  alsayf winrm powershell -H server.internal -u admin 'Get-Service'\n  alsayf winrm ps -H server.internal -u admin --file inventory.ps1",
		RunE: func(cmd *cobra.Command, args []string) error {
			var script string
			if len(args) != 0 {
				script = args[0]
			}
			if cmd.Flags().Changed("file") && len(args) != 0 {
				return winrm.ErrScriptSource
			}
			return run(cmd, winrm.PowerShell, script, scriptFile)
		},
	}
	ps.Flags().StringVarP(&scriptFile, "file", "f", "", "Local UTF-8 PowerShell script, or - for stdin")
	shell := &cobra.Command{
		Use:   "shell",
		Short: "Open a persistent streaming CMD session (type exit to finish)",
		Long:  "Open one remote cmd.exe process with streamed input/output. Working directory and environment persist until exit. This is a line-oriented WinRS session, not a PTY or a PSRP PowerShell runspace; full-screen console applications are not supported.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd, winrm.Shell, "", "")
		},
	}
	root.AddCommand(exec, ps, shell)
	return root
}
