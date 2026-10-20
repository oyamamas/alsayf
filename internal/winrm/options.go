package winrm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

type Mode string

const (
	Exec       Mode = "exec"
	PowerShell Mode = "powershell"
	Shell      Mode = "shell"
)

var ErrScriptSource = errors.New("provide exactly one PowerShell script or --file (use --file - for stdin)")

type Options struct {
	Host, User, Password        string
	PasswordEnv, PasswordFile   string
	Auth, CACert, TLSServerName string
	Port                        int
	HTTP, Insecure, PasswordSet bool
	RequestTimeout, Timeout     time.Duration
}

func DefaultOptions() Options {
	return Options{Auth: "ntlm", RequestTimeout: 60 * time.Second}
}

func (o Options) validate() error {
	if o.Host == "" || strings.TrimSpace(o.Host) != o.Host || strings.ContainsAny(o.Host, "/\\?#@ \t\r\n\x00") {
		return errors.New("--host must be a hostname or IP without a scheme, path, or port")
	}
	if strings.ContainsAny(o.Host, ":[]") && net.ParseIP(o.Host) == nil {
		return errors.New("--host must be a hostname or unbracketed IP; supply the port with --port")
	}
	if o.Port < 0 || o.Port > 65535 {
		return errors.New("--port must be between 1 and 65535 (or 0 for the default)")
	}
	if strings.TrimSpace(o.User) == "" || strings.ContainsAny(o.User, "\r\n\x00") {
		return errors.New("--user is required and must not contain control characters")
	}
	if o.Auth != "ntlm" && o.Auth != "basic" {
		return errors.New("--auth must be ntlm or basic")
	}
	if o.HTTP && o.Auth == "basic" {
		return errors.New("Basic authentication requires HTTPS; remove --http or use --auth ntlm")
	}
	if o.HTTP && (o.Insecure || o.CACert != "" || o.TLSServerName != "") {
		return errors.New("--ca-cert, --tls-server-name, and --insecure require HTTPS")
	}
	if o.RequestTimeout < time.Second {
		return errors.New("--request-timeout must be at least 1s")
	}
	if o.Timeout < 0 {
		return errors.New("--timeout must not be negative")
	}
	sources := 0
	for _, set := range []bool{o.PasswordSet, o.PasswordEnv != "", o.PasswordFile != ""} {
		if set {
			sources++
		}
	}
	if sources > 1 {
		return errors.New("--password, --password-env, and --password-file are mutually exclusive")
	}
	return nil
}

func (o Options) password(ctx context.Context, input io.Reader, prompt io.Writer) (string, error) {
	if o.PasswordSet {
		return o.Password, nil
	}
	if o.PasswordFile != "" {
		data, err := os.ReadFile(o.PasswordFile)
		if err != nil {
			return "", fmt.Errorf("read password file: %w", err)
		}
		return strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r"), nil
	}
	name := o.PasswordEnv
	if name == "" {
		name = "ALSAYF_WINRM_PASSWORD"
	}
	if password, ok := os.LookupEnv(name); ok {
		return password, nil
	}
	if o.PasswordEnv != "" {
		return "", fmt.Errorf("password environment variable %s is not set", name)
	}
	f, ok := input.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return "", errors.New("no password supplied; use --password-env, --password-file, --password, or ALSAYF_WINRM_PASSWORD")
	}
	fd := int(f.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", fmt.Errorf("read terminal state: %w", err)
	}
	// Change terminal state before starting the reader so cancellation cannot
	// race with disabling echo. Terminal.ReadPassword handles keyboard Ctrl+C;
	// the context also handles externally delivered interrupts.
	defer term.Restore(fd, state)
	type result struct {
		data string
		err  error
	}
	done := make(chan result, 1)
	terminal := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{input, prompt}, "")
	go func() {
		data, err := terminal.ReadPassword("WinRM password: ")
		done <- result{data, err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-done:
		if r.err != nil {
			if errors.Is(r.err, io.EOF) {
				return "", context.Canceled
			}
			return "", fmt.Errorf("read password: %w", r.err)
		}
		return r.data, nil
	}
}
