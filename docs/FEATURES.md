# Feature snapshot

Recorded on 2026-10-20.

Alsayf is a modular command-line security toolkit for legitimate administration
and assessment of authorized systems. Commands and runtime code are separated
by module under `cmd/<module>` and `internal/<module>`.

## Build profiles and deployment

| Build | Output | Included commands | Measured size |
| --- | --- | --- | --- |
| `make core` | `bin/alsayf-core` | Core, `weaponize`, `version`, help | 6.73 MiB |
| `make full` | `bin/alsayf` | Core, `secrets`, `clx`, `gbuster`, `nuclei`, `winrm` | 124.91 MiB |

Sizes are for stripped, pure-Go macOS arm64 builds on the development machine;
other targets may differ. Both Make targets disable CGO by default.

Plain `go build` produces the full toolkit. `go build -tags core` excludes all
five modules from the executable's import graph. The core profile includes
`weaponize`, `version`, and help. Native modules can be added by an on-host
rebuild; external backend installation is not implemented.

Module tags can be combined with `core` to build a selected toolkit. For example,
`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -tags core,gobuster -o bin/alsayf-gobuster .`
builds only Gobuster, measuring 9.57 MiB on the same platform. Use
`-tags core,secrets,gobuster` to include both secret scanning and Gobuster. The
available module tags are `secrets`, `clx`, `gobuster`, `nuclei`, and `winrm`; without `core`, all
modules remain enabled by default.

`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -tags core,nuclei -o bin/alsayf-nuclei .`
builds only the Nuclei module, measuring 106.71 MiB on the same platform. To retain
the earlier tools without Nuclei or WinRM, select `-tags core,secrets,clx,gobuster`.
That selected build measured 37.03 MiB before the WinRM integration.

`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -tags core,winrm -o bin/alsayf-winrm .`
builds the WinRM client alone, measuring approximately 7.41 MiB on macOS arm64.
The client is pure Go and can run on macOS, Linux, or Windows while managing
remote Windows hosts. Windows and Linux builds are compile-checked; actual
Windows server interoperability still needs a live-host test.

Build an executable for the target OS and architecture before deployment. The
target host does not need Go or source code; modules are already compiled in and
require no downloads to run already compiled tools. Optional native weaponization
can download build prerequisites. CloudExec commands connect to their selected
infrastructure services. Its GitLab repository cloning module additionally
requires the system `git` executable.

## Native build workflows

- `alsayf weaponize <module> [module...]` downloads matching sources and, if
  needed, Go; builds the union of selected and already compiled native modules;
  verifies the candidate before replacement; removes owned temporary build data.
- `./builder` (Python 3.9+) selects tools and a target platform interactively,
  bootstraps Go if necessary, and prepares a deployable executable.
- `alsayf version --json` records source revision, modification state, platform,
  compiler version, and enabled modules.
- Local-source, retained-cache, offline, and explicit-output variants are
  documented in [BUILDING.md](BUILDING.md), alongside manual Go commands.

## Local secret discovery: `alsayf secrets`

- `scan <path> [path...]` recursively scans local files and directories, including
  binary and non-UTF-8 files.
- 223 embedded secret detection rules, loaded through Titus v1.1.33, excluding
  rules marked noisy. The portable regular-expression matcher runs locally.
- Full captured secret values in text and JSON output; no masking, online
  credential validation, or external rule downloads.
- Findings include rule identity, file location, and captured values. Identical
  captures are grouped without merging different passwords sharing a username.
- Reports include completeness, scanned file/byte counts, skipped paths, and
  errors. Incomplete scans return a failure status.
- Repeatable `--exclude`, configurable `--max-file-size` (100 MiB by default),
  and `--fail-on-findings` for automation.
- `rules list`, with text or JSON output, lists enabled rules.

The earlier local comparison against Nosey Parker 0.24.0 detected the expected
rule in all 369 positive examples from shared rules, with no target-rule hits in
114 negative examples and no findings in seven clean controls. Six positive
examples differ in capture representation. This fixture result does not
establish complete real-world detection or performance parity.

Current limitations: no Git-history scanning; symlinks, special files, `.git`
metadata, and oversized files are skipped. Repeated identical captures under a
rule within one file retain the first location, rather than every occurrence.

## Infrastructure commands: `alsayf clx`

CloudExec is integrated into the command tree, with adapted runtime code in
`cmd/clx` and `internal/clx`.

The twelve service commands are `clickhouse`, `consul`, `etcd`, `exporters`,
`gitlab`, `grafana`, `kafka`, `kube`, `postgres`, `redis`, `registry`, and `zk`.

Capabilities include service discovery, authentication and access checks,
service-specific data inspection, and inherited assessment modules. Available
actions and flags depend on the service; some modules perform active actions
such as command execution or SSRF checks. Exporter scanning retains its own
CloudExec secret-detection rules.

Commands accept hosts, networks, or input lists as supported by each handler.
Shared validation checks targets, readable input files, positive worker counts
and timeouts, and valid port ranges. Command constructors keep flag state local
to each command tree.

Integration validation covered builds, command registration, invalid arguments,
and selected loopback mock scenarios. It did not exercise every service module
against a live backend. Some inherited handlers do not observe context
cancellation; a second Ctrl+C restores process termination behavior.

## Wordlist-based discovery: `alsayf gbuster`

Gobuster v3.8.2 is embedded through its upstream Go packages. It does not require
a separately installed Gobuster executable. The adapter is separated into
`cmd/gobuster` and `internal/gobuster`.

- `dir`: HTTP directory and file discovery, including extension expansion.
- `dns`: DNS subdomain discovery, including an optional custom resolver.
- `vhost`: virtual host discovery.
- `fuzz`: wordlist substitution in URLs, headers, and request bodies.
- `s3` and `gcs`: cloud storage bucket enumeration.
- `tftp`: TFTP file discovery.

Native mode flags are preserved, including concurrency, delays, timeouts,
supported filters, and output files. Wordlists are supplied by file or stdin;
none are bundled or automatically downloaded. Use
`alsayf gbuster <mode> --help` for mode-specific options.

The adapter propagates cancellation and validates worker counts, timeouts,
delays, and unexpected arguments before invoking mode handlers. Upstream scan
output uses process standard streams, and inherited logger fatal paths can exit
the process on output errors.

Validation covered help for all seven modes; local HTTP directory, extension,
fuzz, and virtual host scenarios; a local UDP DNS resolver; stdin wordlists;
file output; invalid options; and Ctrl+C cancellation. S3, GCS, and TFTP backend
behavior was not exercised. Dependency isolation was checked for all 32 module
combinations.

## Template-based assessment: `alsayf nuclei`

Nuclei v3.11.1's public parser, catalog/loader, protocols, and execution engine
are integrated under `cmd/nuclei` and `internal/nuclei`. The SDK wrapper and its
unconditional version check are not imported. Template matching and execution
use upstream implementations.

- Explicit local template files/directories (`-t`) and workflows (`-w`). No
  bundled templates, remote template source flags, or automatic downloads.
- `--template-root` selects the local root for relative workflow references and
  tag-based workflow selection. By default the first source directory (or its
  containing directory for a file) is used.
- Targets from repeated `-u`, a list file (`-l`), or stdin (`-l -`); duplicates
  are removed before scanning.
- Severity, tag, and template-ID filters; custom headers and variables.
- Configurable template/target concurrency, request rate, timeout, and retries.
- Text or JSONL findings, optional output file, and `--fail-on-findings`.
  Extracted values are fully preserved.
- `--list-templates` lists runnable templates without scanning targets.
- Invalid templates fail validation before scanning. Cancellation, output errors,
  and recorded request failures return an unsuccessful exit status.

Example:

```sh
alsayf nuclei -u https://service.internal -t ./templates --severity high,critical --jsonl
```

Automatic updates, Interactsh, and cloud reporting are disabled. Template files
containing Interactsh URL markers are excluded, including workflow children.
Headless, code, self-contained, DAST, global-matcher, and file-template
capabilities are not enabled. No browser or external Nuclei executable is needed.
This is a focused Alsayf command, not a flag-for-flag copy of the upstream CLI.

DNS uses `--resolver` addresses or `/etc/resolv.conf`; systems without that file
must supply resolvers explicitly. The engine retains process-global protocol
pools, so the supported CLI lifecycle is one scan per process. Configuration
uses upstream `NUCLEI_CONFIG_DIR`; an absent ignore file is initialized empty
without downloading one. Existing local ignore settings remain in effect.
Template content can specify additional network destinations; disabling
background integrations does not constitute a network sandbox.

Integration checks use synthetic templates and loopback HTTP/DNS services, not
real vulnerability targets. They cover matching/extraction, negative controls,
filters, workflow execution, lists/stdin, output files, OAST exclusion, invalid
templates/options, and cancellation. A local capture proxy checks for unintended
update/OAST/cloud requests. Compile-time dependency isolation is checked for all
32 module combinations.

## Remote Windows administration: `alsayf winrm`

WinRM for Go is integrated under `cmd/winrm` and `internal/winrm`, using WinRS
SOAP shells and commands. No Ruby, Evil-WinRM, external WinRM executable, or
local Windows installation is required on the client. The target Windows host
must already have a reachable WinRM listener and grant the supplied account
remote access. The module does not configure the remote service or firewall.

- `exec '<command>'`: execute one CMD command, streaming stdout and stderr.
- `powershell '<script>'` (alias `ps`): execute PowerShell code, a local UTF-8
  script (`--file script.ps1`), or a script read from stdin (`--file -`). Scripts
  are sent through the remote process's stdin, avoiding command-line size limits.
  Script errors and the last native program's nonzero exit status fail the command.
- `shell`: one persistent `cmd.exe /Q` process with line-oriented stdin and
  streamed output. Working directory and environment survive between lines.
  Type `exit` to finish. This is not a PTY or PSRP runspace; full-screen console
  applications and Evil-WinRM's PowerShell completion are not provided.
- NTLM authentication by default, including upstream WinRM message encryption.
  Basic authentication is available with `--auth basic` over HTTPS only.
- HTTPS on port 5986 by default, certificate verification, `--ca-cert`, and
  `--tls-server-name`. `--insecure` explicitly disables certificate validation.
  `--http` selects NTLM on port 5985; `--port` overrides either default.
- Passwords from `--password`, `--password-env`, `--password-file`, the default
  `ALSAYF_WINRM_PASSWORD` environment variable, or a hidden terminal prompt.
  Explicit password sources are mutually exclusive. File input removes only
  one trailing line ending and preserves other whitespace.
- Network I/O timeout (`--request-timeout`, default 60s), optional overall
  session timeout (`--timeout`), and Ctrl+C cancellation. Cleanup attempts to
  delete the remote shell on a fresh connection, bounded by five seconds;
  failed cleanup is reported. A lost connection cannot guarantee remote cleanup.
- Nonzero remote exit codes, failed output/input, non-idle SOAP faults, and
  authentication failures return an unsuccessful CLI status.

Examples (password is prompted for or supplied through the environment):

```sh
alsayf winrm exec -H server.internal -u 'CONTOSO\admin' 'whoami /all'
alsayf winrm ps -H server.internal -u 'CONTOSO\admin' 'Get-Service'
alsayf winrm ps -H server.internal -u admin --file inventory.ps1
alsayf winrm shell -H server.internal -u admin --ca-cert company-ca.pem
```

Kerberos, CredSSP, certificate authentication, PSRP sessions, and file-transfer
commands are not exposed. CMD output retains the remote console encoding;
the PowerShell adapter explicitly configures UTF-8 streams.

Adapter validation uses a loopback HTTPS/SOAP server: streams, exit status,
PowerShell input from all three sources, long scripts, shell input, password
sources, TLS verification, idle and error faults, output failures, cancellation,
and cleanup, with the race detector. Upstream loopback tests separately verify
NTLM handshakes, message encryption, reauthentication, and custom dialing.
These tests do not establish interoperability with every Windows version or
domain policy. Module dependency isolation is checked for all 32 combinations.

## Module integration policy

Future integrations must remain separate modules with their own command
constructor, isolated adapter/runtime code, pinned upstream dependencies, and
compile-time inclusion through an independent module tag, and an entry in
`internal/buildkit/modules.json` for builder/weaponize selection. Keep provenance
and licenses in `third_party`. README contains the authorized-use notice,
disclaimer, and third-party attribution; operational documentation lives in docs/.
Large optional assets such as wordlists should remain outside the executable.

Native modules can be selected with the interactive `builder` or added through
`alsayf weaponize` on a host with build resources. See [BUILDING.md](BUILDING.md)
for automatic downloads, manual compilation, platform behavior, and cleanup.

## Deferred backlog

External weaponization (prebuilt backends and their runtimes), ready-made
offline build packs, and signed release manifests are deferred. Prepared builds
remain available for hosts that cannot or should not compile tools locally.
