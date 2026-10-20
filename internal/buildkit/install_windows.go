//go:build windows

package buildkit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Windows cannot overwrite the running image. A visible, bounded PowerShell
// helper waits for this process to exit, replaces it, and cleans its own stage.
const updateScript = `param([Parameter(Mandatory=$true)][string]$Plan)
$ErrorActionPreference = 'Stop'
$p = Get-Content -LiteralPath $Plan -Encoding UTF8 -Raw | ConvertFrom-Json
$backup = Join-Path $p.Stage 'previous.exe'
$logPath = Join-Path $p.Stage 'update.log'
try {
    $parent = Get-Process -Id $p.Parent -ErrorAction SilentlyContinue
    if ($parent -and -not $parent.WaitForExit(120000)) { throw 'Parent did not exit within two minutes' }
    $complete = $false
    for ($attempt = 0; $attempt -lt 60; $attempt++) {
        try {
            if (Test-Path -LiteralPath $p.Target) { [IO.File]::Move($p.Target, $backup) }
            try { [IO.File]::Move($p.Candidate, $p.Target) }
            catch {
                if ((Test-Path -LiteralPath $backup) -and -not (Test-Path -LiteralPath $p.Target)) { [IO.File]::Move($backup, $p.Target) }
                throw
            }
            $complete = $true
            break
        } catch { Start-Sleep -Milliseconds 500 }
    }
    if (-not $complete) { throw 'Could not replace executable within 30 seconds; original retained or stored as previous.exe' }
    if (Test-Path -LiteralPath $backup) { Remove-Item -LiteralPath $backup -Force }
    Remove-Item -LiteralPath $p.Lock -Force
    Remove-Item -LiteralPath $p.Stage -Recurse -Force
} catch {
    $_.ToString() | Set-Content -LiteralPath $logPath
    Remove-Item -LiteralPath $p.Lock -Force -ErrorAction SilentlyContinue
    exit 1
}
`

func install(candidate, target, stage string, log io.Writer) (bool, error) {
	self, err := os.Executable()
	if err != nil {
		return false, err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return false, err
	}
	if !strings.EqualFold(self, target) {
		if err := os.Rename(candidate, target); err != nil {
			return false, fmt.Errorf("replace output executable: %w", err)
		}
		return false, nil
	}
	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		return false, fmt.Errorf("Windows self-replacement requires Windows PowerShell: %w", err)
	}
	policyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	policy, policyErr := exec.CommandContext(policyCtx, powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "[string](Get-ExecutionPolicy)").Output()
	if policyErr != nil {
		return false, fmt.Errorf("read Windows update helper policy: %w", policyErr)
	}
	switch strings.TrimSpace(string(policy)) {
	case "RemoteSigned", "Unrestricted", "Bypass":
	default:
		return false, fmt.Errorf("Windows policy %s does not permit the local update helper; use --output to prepare a separate executable for approved deployment", strings.TrimSpace(string(policy)))
	}
	plan, err := json.Marshal(struct {
		Parent                         int
		Target, Candidate, Stage, Lock string
	}{os.Getpid(), target, candidate, stage, target + ".weaponize.lock"})
	if err != nil {
		return false, err
	}
	planPath := filepath.Join(stage, "update.json")
	scriptPath := filepath.Join(stage, "update.ps1")
	if err := os.WriteFile(planPath, plan, 0600); err != nil {
		return false, err
	}
	if err := os.WriteFile(scriptPath, []byte(updateScript), 0600); err != nil {
		return false, err
	}
	command := exec.Command(powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-File", scriptPath, "-Plan", planPath)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200} // CREATE_NEW_PROCESS_GROUP
	if err := command.Start(); err != nil {
		return false, err
	}
	command.Process.Release()
	fmt.Fprintf(log, "Windows update helper started; failures are recorded in %s\n", filepath.Join(stage, "update.log"))
	return true, nil
}
