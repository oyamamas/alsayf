package winrm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	upstream "github.com/masterzen/winrm"
)

// PowerShell reads the script from stdin rather than putting its contents into
// cmd.exe's limited command line. Console streams use UTF-8 on either client OS.
const powershellBootstrap = `$utf8 = New-Object System.Text.UTF8Encoding($false); [Console]::InputEncoding = $utf8; [Console]::OutputEncoding = $utf8; $OutputEncoding = $utf8; $ErrorActionPreference = 'Stop'; $global:LASTEXITCODE = 0; try { & ([ScriptBlock]::Create([Console]::In.ReadToEnd())); if ($LASTEXITCODE) { exit $LASTEXITCODE } } catch { [Console]::Error.WriteLine($_.ToString()); exit 1 }`

func Run(ctx context.Context, opts Options, mode Mode, text, file string, input io.Reader, stdout, stderr io.Writer) (err error) {
	if err := opts.validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	command, stdin, err := prepare(mode, text, file, input)
	if err != nil {
		return err
	}
	password, err := opts.password(ctx, input, stderr)
	if err != nil {
		return err
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	port := opts.Port
	if port == 0 {
		port = 5986
		if opts.HTTP {
			port = 5985
		}
	}
	var ca []byte
	if opts.CACert != "" {
		ca, err = os.ReadFile(opts.CACert)
		if err != nil {
			return fmt.Errorf("read CA certificate: %w", err)
		}
		if len(ca) == 0 {
			return errors.New("CA certificate file is empty")
		}
	}
	endpoint := upstream.NewEndpoint(opts.Host, port, !opts.HTTP, opts.Insecure, ca, nil, nil, opts.RequestTimeout)
	endpoint.TLSServerName = opts.TLSServerName
	// Short output polls keep interactive input responsive on the single NTLM
	// connection. The network timeout remains independently configurable.
	params := upstream.NewParameters("PT1S", "en-US", 153600)
	transport := newTransport(ctx, opts.Auth, opts.RequestTimeout)
	defer transport.close()
	params.TransportDecorator = func() upstream.Transporter { return transport }
	client, err := upstream.NewClientWithParameters(endpoint, opts.User, password, params)
	if err != nil {
		return fmt.Errorf("configure WinRM: %w", err)
	}
	shell, err := client.CreateShell()
	if err != nil {
		return fmt.Errorf("create WinRM shell: %w", err)
	}
	defer func() {
		cleanupCancel := transport.cleanupContext()
		defer cleanupCancel()
		// NTLM encryption state belongs to the previous TCP connection.
		if resetErr := transport.Transport(endpoint); resetErr != nil {
			err = errors.Join(err, fmt.Errorf("prepare WinRM cleanup: %w", resetErr))
			return
		}
		if closeErr := shell.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close WinRM shell: %w", closeErr))
		}
	}()
	// Cancellation is handled by the transport, then by deleting the shell. The
	// upstream context handler calls Command.Close concurrently with its caller.
	remote, err := shell.ExecuteWithContext(context.Background(), command)
	if err != nil {
		return fmt.Errorf("start remote command: %w", err)
	}
	var outputWG sync.WaitGroup
	var outputMu sync.Mutex
	outputErrors := make(chan error, 2)
	for _, stream := range []struct {
		src io.Reader
		dst io.Writer
	}{{remote.Stdout, stdout}, {remote.Stderr, stderr}} {
		outputWG.Add(1)
		go func(src io.Reader, dst io.Writer) {
			defer outputWG.Done()
			_, copyErr := io.Copy(lockedWriter{mu: &outputMu, dst: dst}, src)
			if copyErr != nil {
				outputErrors <- copyErr
				cancel()
				// Unblock upstream's pipe writer if a destination failed.
				io.Copy(io.Discard, src)
			}
		}(stream.src, stream.dst)
	}
	inputDone := make(chan error, 1)
	inputStopped := make(chan struct{})
	inputPipe := &inputWriter{dst: remote.Stdin}
	go func() {
		var inputErr error
		if stdin != nil {
			if mode == Shell {
				inputErr = copyLines(ctx, inputPipe, stdin)
			} else {
				_, inputErr = io.Copy(inputPipe, stdin)
			}
		}
		if inputErr == nil {
			inputErr = inputPipe.Close()
		}
		inputDone <- inputErr
		if inputErr != nil && mode != Exec {
			select {
			case <-inputStopped:
			default:
				cancel()
			}
		}
	}()
	remote.Wait()
	outputWG.Wait()
	ctxErr := ctx.Err()
	close(inputStopped)
	cancel()
	// Prevent an input reader that unblocks late from sending data during shell
	// cleanup, and wait for any network write already in progress to finish.
	inputPipe.stop()
	close(outputErrors)
	for outputErr := range outputErrors {
		err = errors.Join(err, fmt.Errorf("read remote output: %w", outputErr))
	}
	if mode != Shell {
		inputErr := <-inputDone
		if inputErr != nil && mode != Exec {
			err = errors.Join(err, fmt.Errorf("write remote input: %w", inputErr))
		}
	} else {
		// Interactive terminal input may still be blocked after remote exit.
		// Closing a supplied file/pipe stops it without waiting for another line.
		select {
		case inputErr := <-inputDone:
			if inputErr != nil {
				err = errors.Join(err, fmt.Errorf("write remote input: %w", inputErr))
			}
		default:
			if closer, ok := input.(io.ReadCloser); ok {
				closer.Close()
			}
		}
	}
	if remote.Error() != nil {
		err = errors.Join(err, fmt.Errorf("remote command: %w", remote.Error()))
	}
	if ctxErr != nil {
		err = errors.Join(err, ctxErr)
	}
	if err == nil && remote.ExitCode() != 0 {
		err = fmt.Errorf("remote command exited with code %d", remote.ExitCode())
	}
	return err
}

// Serialize input against cleanup without waiting for a blocked terminal Read.
type inputWriter struct {
	mu      sync.Mutex
	dst     io.WriteCloser
	stopped bool
}

func (w *inputWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return 0, io.ErrClosedPipe
	}
	return w.dst.Write(data)
}

func (w *inputWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return io.ErrClosedPipe
	}
	return w.dst.Close()
}

func (w *inputWriter) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
}

func prepare(mode Mode, text, file string, input io.Reader) (string, io.Reader, error) {
	switch mode {
	case Exec:
		if strings.TrimSpace(text) == "" || strings.ContainsRune(text, '\x00') {
			return "", nil, errors.New("command must not be empty or contain NUL characters")
		}
		return text, nil, nil
	case PowerShell:
		if (file == "") == (text == "") {
			return "", nil, ErrScriptSource
		}
		if file != "" {
			var data []byte
			var err error
			if file == "-" {
				data, err = io.ReadAll(input)
			} else {
				data, err = os.ReadFile(file)
			}
			if err != nil {
				return "", nil, fmt.Errorf("read PowerShell script: %w", err)
			}
			text = string(data)
		}
		text = strings.TrimPrefix(text, "\xef\xbb\xbf")
		if strings.TrimSpace(text) == "" || !utf8.ValidString(text) || strings.ContainsRune(text, '\x00') {
			return "", nil, errors.New("PowerShell script must be nonempty UTF-8 text without NUL characters")
		}
		command := upstream.Powershell(powershellBootstrap)
		command = strings.Replace(command, "powershell.exe ", "powershell.exe -NoLogo -NoProfile -NonInteractive ", 1)
		return command, strings.NewReader(text), nil
	case Shell:
		return "cmd.exe /Q", input, nil
	default:
		return "", nil, errors.New("unsupported WinRM mode")
	}
}

func copyLines(ctx context.Context, dst io.Writer, src io.Reader) error {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := io.WriteString(dst, scanner.Text()+"\r\n"); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type lockedWriter struct {
	mu  *sync.Mutex
	dst io.Writer
}

func (w lockedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dst.Write(data)
}
