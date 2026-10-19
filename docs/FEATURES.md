# Feature snapshot

Recorded on 2026-10-19.

Alsayf is a modular command-line security toolkit for legitimate administration
and assessment of authorized systems. Commands and runtime code are separated
by module under `cmd/<module>` and `internal/<module>`.

## Build profiles and deployment

| Build | Output | Included commands | Measured size |
| --- | --- | --- | --- |
| `make core` | `bin/alsayf-core` | CLI core and help | 2.49 MiB |
| `make full` | `bin/alsayf` | Core, `secrets`, `clx`, `gbuster` | 36.74 MiB |

Sizes are for stripped, pure-Go macOS arm64 builds on the development machine;
other targets may differ. Both Make targets disable CGO by default.

Plain `go build` produces the full toolkit. `go build -tags core` excludes all
three modules from the executable's import graph. The core profile currently
provides no scanning functionality or runtime module installation.

Module tags can be combined with `core` to build a selected toolkit. For example,
`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -tags core,gobuster -o bin/alsayf-gobuster .`
builds only Gobuster, measuring 9.57 MiB on the same platform. Use
`-tags core,secrets,gobuster` to include both secret scanning and Gobuster. The
available module tags are `secrets`, `clx`, and `gobuster`; without `core`, all
modules remain enabled by default.

Build an executable for the target OS and architecture before deployment. The
target host does not need Go or source code; modules are already compiled in and
require no downloads. CloudExec commands connect to their selected infrastructure
services. Its GitLab repository cloning module additionally requires the system
`git` executable.

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
behavior was not exercised. Dependency isolation was checked for all eight module
combinations.

## Module integration policy

Future integrations must remain separate modules with their own command
constructor, isolated adapter/runtime code, pinned upstream dependencies, and
compile-time inclusion through an independent module tag. Keep provenance and
licenses in `third_party`; keep README limited to third-party attribution.
Large optional assets such as wordlists should remain outside the executable.

This separation provides a foundation for selecting modules in a possible future
`weaponize` build workflow. It does not install modules at runtime or require
compilation on administration targets.

## Deferred backlog

`alsayf weaponize <module>` is not implemented. Runtime source extraction,
toolchain preparation, rebuilding, and executable replacement are deferred:
administration targets may have neither Go nor internet access. Current
deployment uses executables built in advance.
