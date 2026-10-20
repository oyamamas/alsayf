# Building and native weaponization

Alsayf supports two native-module deployment workflows. External backend
installation is a separate backlog item and is not implemented.

## Prepare a build interactively

Run the builder from a source checkout on your workstation:

```sh
./builder
```

On Windows, use `py -3 builder` (or `python builder`). The script requires
Python 3.9+ with its standard library. It asks for tools, target OS/architecture,
and output path. No manually constructed Go command is necessary. Module
selection accepts numbers, module names, command names, `core`, or `all`.

The builder reuses a compatible Go executable from PATH. If none is available,
it downloads the version required by `go.mod` from the official Go distribution,
checks the archive's size and SHA-256 against the official HTTPS metadata, and
extracts the toolchain into a temporary directory. It does not install a
system-wide compiler. Building the worker and the toolkit share a cache; all
builder-owned temporary files are removed after normal success or failure.

Non-interactive examples:

```sh
./builder --modules core
./builder --modules secrets,nuclei --os linux --arch amd64 -o bin/alsayf-linux
./builder --modules secrets,gbuster,winrm --os windows --arch amd64 -o bin/alsayf.exe
./builder --modules all --cache-dir /path/to/alsayf-build-cache
```

The supported target matrix is Linux, macOS (`darwin`), and Windows, each on
`amd64` or `arm64`. CGO is disabled. Runtime support for a tool or operation may
still depend on the target platform. Cross-compilation is not a substitute for
execution tests on that operating system.

`--cache-dir` explicitly retains a reusable dependency/build cache with `mod/`
and `build/` subdirectories. The default cache is private and temporary.
`--go /path/to/go` selects a compatible compiler. A newer installed compiler is
accepted to avoid unnecessary downloads; this does not promise bit-for-bit
identical output across Go versions. Pin the same compiler and source commit
when reproducibility matters. Actual compiler and source information are
available through `alsayf version --json`.

## Weaponize an existing executable

Bring a core build to the administration host, then run:

```sh
alsayf weaponize --list
alsayf weaponize nuclei
alsayf weaponize secrets gbuster
alsayf version --json
```

`gbuster` is the command alias for the `gobuster` module tag. Previously compiled
modules are preserved; requesting an already present module is a no-op. This
workflow requires write permission to the executable's directory and adequate
memory and temporary disk space for a Go build. The temporary filesystem must
allow execution of a downloaded Go toolchain. Nuclei's dependency graph and
compiler/linker workload are substantial even though the starting core is small.

The command obtains the Alsayf source revision embedded in the running binary,
reuses or downloads a compatible Go compiler, and builds the union of the old
and requested native modules. Sources are downloaded as a pinned GitHub commit
archive, without requiring Git. Go dependencies are checked against committed
`go.sum`; `go.mod` and `go.sum` are not rewritten. No generators or test suites
are run on the administration host.

A candidate is built in a private staging directory beside the destination.
The build's target and module tags are inspected. Native candidates must also
pass `version --json` with matching source revision, platform, and enabled
modules before replacement. On build or validation failure, the existing
executable is retained. Concurrent builders for the same output are rejected
by a `.weaponize.lock` file. No automatic privilege elevation is attempted.

Linux/macOS replacement uses a same-filesystem rename. Windows cannot overwrite
the running image: an allowed Windows PowerShell helper waits for this process
to exit, replaces the binary, and removes its staging files. No execution policy
is changed. Policies that disallow the local helper are reported; use `--output`
for an executable that can be deployed through an approved mechanism instead.
Windows replacement is asynchronous, with failures recorded in the staging
`update.log`; its location is printed before exit. Other processes holding the
binary open can prevent replacement. Failed replacement preserves the old image
or its `previous.exe` recovery copy. Actual Windows helper execution still needs
validation on Windows; cross-builds alone do not verify it.

Useful options:

```sh
# Write a separate executable instead of replacing this one.
alsayf weaponize nuclei --output ./alsayf-with-nuclei

# Explicitly choose a newer source ref, resolved to an exact commit first.
alsayf weaponize nuclei --ref codex-branch

# Build local changes without downloading the source tree.
alsayf weaponize nuclei --source-dir /path/to/alsayf

# Reuse a populated cache and local prerequisites, with dependency networking off.
alsayf weaponize nuclei --source-dir /path/to/alsayf \
  --go /path/to/go --cache-dir /path/to/alsayf-build-cache --offline
```

The default source repository is `oyamamas/alsayf`. `--repository owner/name`
selects a fork; `GITHUB_TOKEN` optionally authenticates source downloads.
A clean build with embedded revision requires no explicit ref. A build from
modified sources requires an explicit `--source-dir` or `--ref` so local edits
are not silently discarded. Source refs must contain the weaponize build
protocol and every module that must be preserved. New module types unknown to
an old core require updating that core first.

`--timeout` defaults to 30 minutes for weaponize and the builder's worker.
Ctrl+C cancels downloads and Go compilation; owned temporary data is removed
when normal cancellation unwinds. A second interrupt, SIGKILL, power loss, or
OS termination can leave a temporary directory, staging directory, or stale
lock. Remove such leftovers only after confirming their builder has stopped.
Normal cleanup does not remove user-supplied sources, the system Go toolchain,
explicitly retained caches, system logs, shell history, or monitoring records.

## Network and build isolation

A public pinned commit uses one source archive download from GitHub codeload.
An explicit branch/tag first requires commit resolution through the GitHub API.
Authenticated source archives use GitHub's API redirect flow. Toolchain bootstrap
uses official HTTPS metadata and an official Go archive. Module downloads use
`https://proxy.golang.org` without direct VCS fallback and `sum.golang.org` by
default. Only dependencies needed by the selected import graph are built; the
workflow does not run `go mod download all` or download wordlists/templates.

Set `ALSAYF_GOPROXY` and `ALSAYF_GOSUMDB` explicitly to use approved mirrors.
These are deliberate overrides of the controlled defaults; ordinary Go proxy,
private-module, GOFLAGS, workspace, and GOENV settings are not inherited into
managed builds. `--offline` requires local sources, a compatible installed Go,
and populated caches; it disables proxy and checksum-database networking while
retaining checks against the supplied `go.sum`. Offline does not generate a
source/dependency/toolchain distribution pack for you.

Child Go processes are given a private telemetry directory with mode `off`,
without changing the user's global Go telemetry settings. This uses Go's internal
`TEST_TELEMETRY_DIR` mechanism, verified with Go 1.26.2; it is not a public Go
compatibility contract and needs checking when upgrading the supported toolchain.
HTTPS downloads and source trust remain visible administrative operations.
Toolchain checksums are verified; source archives currently rely on HTTPS and
commit pinning rather than a separately signed release manifest.

## Manual compilation

Install a Go toolchain compatible with the `go`/`toolchain` lines in `go.mod`.
Plain `go build` enables all native modules. Include the `core` tag to select a
subset; without it, individual module tags do not restrict the full build.

| Module tag | Command |
| --- | --- |
| `secrets` | `alsayf secrets` |
| `clx` | `alsayf clx` |
| `gobuster` | `alsayf gbuster` |
| `nuclei` | `alsayf nuclei` |
| `winrm` | `alsayf winrm` |

```sh
# Core includes weaponize, version, and help.
CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags="-s -w" -tags core -o bin/alsayf-core .

# A selected toolkit.
CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags="-s -w" -tags core,secrets,nuclei -o bin/alsayf .

# Full toolkit.
CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags="-s -w" -o bin/alsayf-full .

# Cross-compile a selected Windows executable from a Unix shell.
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags="-s -w" \
  -tags core,secrets,winrm -o bin/alsayf.exe .
```

In PowerShell, set environment variables with `$env:CGO_ENABLED='0'`,
`$env:GOOS='windows'`, and `$env:GOARCH='amd64'`, then run the same `go build`
arguments. `make core`, `make full`, and `make builder` are convenience targets
on systems with Make. Manual builds made inside a clean Git checkout retain Go's
VCS revision metadata. Archives without Git metadata cannot infer their source
commit; explicitly set
`alsayf/internal/buildkit.SourceRepository`, `SourceRevision`, and
`SourceModified` through `-ldflags -X` to supply accurate build provenance.

## Backlog

External weaponization (installing prebuilt non-Go backends and their runtimes),
ready-made offline build packs, and signed source/distribution manifests are not
implemented. No external tool runtime is installed by native weaponization.
