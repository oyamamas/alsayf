#!/usr/bin/env python3
"""Prepare an Alsayf build; Python 3.9+ and no installed Go required."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import signal
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import urllib.parse
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parent
MAX_ARCHIVE = 1 << 30
MAX_EXTRACTED = 4 << 30
MAX_ENTRIES = 100000

class HTTPSRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        if urllib.parse.urlparse(newurl).scheme != 'https':
            raise RuntimeError('refusing a non-HTTPS download redirect')
        return super().redirect_request(req, fp, code, msg, headers, newurl)

OPENER = urllib.request.build_opener(HTTPSRedirect())
def fetch(url):
    return OPENER.open(urllib.request.Request(url, headers={'User-Agent': 'alsayf-builder'}), timeout=120)

def clean_env():
    env = {k.upper() if os.name == 'nt' else k: v for k, v in os.environ.items()}
    for key in ('GOROOT', 'GOTOOLDIR', 'GOFLAGS', 'GOWORK', 'GOENV', 'GOTOOLCHAIN',
                'GOOS', 'GOARCH', 'GOAMD64', 'GOARM64', 'GOEXPERIMENT',
                'TEST_TELEMETRY_DIR', 'GO_TELEMETRY_CHILD', 'GO_TELEMETRY_CHILD_UPLOAD'):
        env.pop(key, None)
    env.update(GOENV='off', GOWORK='off', GOTOOLCHAIN='local', CGO_ENABLED='0',
               GOPROXY=env.get('ALSAYF_GOPROXY', 'https://proxy.golang.org'),
               GOSUMDB=env.get('ALSAYF_GOSUMDB', 'sum.golang.org'),
               GONOPROXY='none', GONOSUMDB='none', GOPRIVATE='')
    return env

def version_tuple(text):
    match = re.fullmatch(r'(?:go)?(\d+)\.(\d+)(?:\.(\d+))?', text)
    return tuple(map(int, match.groups(default='0'))) if match else (0, 0, 0)

def required_go():
    source = (ROOT / 'go.mod').read_text(encoding='utf-8')
    match = re.search(r'^go\s+(\d+\.\d+(?:\.\d+)?)\s*$', source, re.M)
    if not match:
        raise RuntimeError('go.mod does not specify a Go version')
    versions = ['go' + match[1]]
    toolchain = re.search(r'^toolchain\s+(go\d+\.\d+(?:\.\d+)?)\s*$', source, re.M)
    if toolchain:
        versions.append(toolchain[1])
    return max(versions, key=version_tuple)

def host_target():
    system = {'Darwin': 'darwin', 'Linux': 'linux', 'Windows': 'windows'}.get(platform.system())
    machine = platform.machine().lower()
    arch = {'x86_64': 'amd64', 'amd64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64'}.get(machine)
    if not system or not arch:
        raise RuntimeError('builder supports Linux/macOS/Windows on amd64/arm64 hosts')
    return system, arch

def safe_path(root, name):
    pure = PurePosixPath(name)
    if pure.is_absolute() or '..' in pure.parts or '\\' in name or ':' in name:
        raise RuntimeError(f'unsafe archive path: {name!r}')
    return root.joinpath(*pure.parts)

def extract(archive, root):
    total = 0
    def entry(name, size, is_dir, mode, source):
        nonlocal total
        path = safe_path(root, name)
        total += size
        if size < 0 or total > MAX_EXTRACTED:
            raise RuntimeError('archive exceeds extracted size limit')
        if is_dir:
            path.mkdir(parents=True, exist_ok=True)
        else:
            path.parent.mkdir(parents=True, exist_ok=True)
            with path.open('xb') as out:
                shutil.copyfileobj(source, out, 1 << 20)
            if path.stat().st_size != size:
                raise RuntimeError('archive file size mismatch')
            path.chmod(0o700 if mode & 0o111 else 0o600)
    if zipfile.is_zipfile(archive):
        with zipfile.ZipFile(archive) as package:
            for count, item in enumerate(package.infolist(), 1):
                if count > MAX_ENTRIES:
                    raise RuntimeError('too many archive entries')
                mode = item.external_attr >> 16
                if stat.S_ISLNK(mode):
                    raise RuntimeError('archive symlinks are not supported')
                with package.open(item) as src:
                    entry(item.filename, item.file_size, item.is_dir(), mode, src)
    else:
        with tarfile.open(archive, 'r:gz') as package:
            for count, item in enumerate(package, 1):
                if count > MAX_ENTRIES or not (item.isdir() or item.isfile()):
                    raise RuntimeError('unsupported archive entry or too many entries')
                if item.isdir():
                    entry(item.name, 0, True, item.mode, None)
                else:
                    with package.extractfile(item) as src:
                        entry(item.name, item.size, False, item.mode, src)

def compiler(requested, workspace, offline, env):
    required = required_go()
    found = shutil.which(requested or 'go')
    if found:
        result = subprocess.run([found, 'version'], capture_output=True, text=True, env=env, timeout=30)
        fields = result.stdout.split()
        if result.returncode == 0 and len(fields) > 2 and version_tuple(fields[2]) >= version_tuple(required):
            return str(Path(found).resolve())
    if offline:
        raise RuntimeError(f'offline builder requires an installed Go >= {required}')
    system, arch = host_target()
    print(f'Downloading {required} for {system}/{arch}; verifying SHA-256', flush=True)
    with fetch('https://go.dev/dl/?mode=json&include=all') as response:
        raw = response.read((8 << 20) + 1)
    if len(raw) > 8 << 20:
        raise RuntimeError('Go metadata exceeds size limit')
    releases = json.loads(raw)
    item = next((f for r in releases if r['version'] == required for f in r['files']
                 if f['os'] == system and f['arch'] == arch and f['kind'] == 'archive'), None)
    if not item or len(item['sha256']) != 64 or Path(item['filename']).name != item['filename']:
        raise RuntimeError('no valid official Go archive for this host')
    archive = workspace / item['filename']
    digest = hashlib.sha256()
    size = 0
    with fetch('https://go.dev/dl/' + item['filename']) as response, archive.open('xb') as out:
        while chunk := response.read(1 << 20):
            size += len(chunk)
            if size > MAX_ARCHIVE:
                raise RuntimeError('Go download exceeds size limit')
            digest.update(chunk)
            out.write(chunk)
    if size != item['size'] or digest.hexdigest() != item['sha256']:
        raise RuntimeError(f"Go archive size or SHA-256 mismatch: got {size} bytes / {digest.hexdigest()}, expected {item['size']} bytes / {item['sha256']}")
    extract(archive, workspace / 'toolchain')
    archive.unlink()
    path = workspace / 'toolchain' / 'go' / 'bin' / ('go.exe' if os.name == 'nt' else 'go')
    result = subprocess.run([str(path), 'version'], capture_output=True, text=True, env=env, timeout=30)
    if result.returncode or required + ' ' not in result.stdout:
        raise RuntimeError('downloaded Go failed its version check')
    return str(path)

def select_modules(modules, value):
    value = value.strip()
    if value in ('core', '0'):
        return 'core'
    if value == 'all':
        return 'all'
    selected = []
    for token in re.split(r'[,\s]+', value.strip()):
        found = next((m for i, m in enumerate(modules, 1)
                      if token in (m['name'], m['command'], str(i))), None)
        if not found:
            raise RuntimeError(f'unknown module selection: {token!r}')
        selected.append(found['name'])
    return ','.join(sorted(set(selected)))

def main():
    system, arch = host_target()
    parser = argparse.ArgumentParser(description='Interactive Alsayf builder. Bootstraps Go if necessary.')
    parser.add_argument('--modules', help='core, all, or comma-separated module names/numbers')
    parser.add_argument('--os', choices=('linux', 'darwin', 'windows'))
    parser.add_argument('--arch', choices=('amd64', 'arm64'))
    parser.add_argument('-o', '--output', help='output executable (default: bin/alsayf-OS-ARCH[.exe])')
    parser.add_argument('--go', help='Go executable')
    parser.add_argument('--cache-dir', help='retain dependency/build cache in this directory')
    parser.add_argument('--offline', action='store_true', help='require local compiler and cached dependencies')
    parser.add_argument('--timeout', default='30m', help='Go build timeout, e.g. 30m')
    args = parser.parse_args()
    modules = json.loads((ROOT / 'internal' / 'buildkit' / 'modules.json').read_text())
    interactive = args.modules is None
    if interactive:
        print('Select tools (comma-separated names/numbers):\n  0. core only')
        for i, m in enumerate(modules, 1):
            print(f"  {i}. {m['command']}: {m['description']}")
        print('  all. full toolkit')
        args.modules = input('Tools [core]: ').strip() or 'core'
    selected = select_modules(modules, args.modules)
    target_os = args.os or ((input(f'Target OS [{system}]: ').strip() or system) if interactive else system)
    target_arch = args.arch or ((input(f'Target architecture [{arch}]: ').strip() or arch) if interactive else arch)
    if target_os not in ('linux', 'darwin', 'windows') or target_arch not in ('amd64', 'arm64'):
        raise RuntimeError('unsupported target platform')
    default = ROOT / 'bin' / f"alsayf-{target_os}-{target_arch}{'.exe' if target_os == 'windows' else ''}"
    output = args.output or ((input(f'Output [{default}]: ').strip() or str(default)) if interactive else str(default))
    print(f'Building {selected} for {target_os}/{target_arch} -> {output}', flush=True)
    with tempfile.TemporaryDirectory(prefix='alsayf-builder-') as temporary:
        workspace = Path(temporary)
        env = clean_env()
        telemetry = workspace / 'telemetry'
        telemetry.mkdir(mode=0o700)
        (telemetry / 'mode').write_text('off\n')
        env['TEST_TELEMETRY_DIR'] = str(telemetry)
        cache = Path(args.cache_dir).resolve() if args.cache_dir else workspace / 'cache'
        for name in ('mod', 'build'):
            (cache / name).mkdir(parents=True, exist_ok=True)
        env.update(GOCACHE=str(cache / 'build'), GOMODCACHE=str(cache / 'mod'),
                   GOPATH=str(workspace / 'gopath'), GOTMPDIR=str(workspace / 'tmp'))
        (workspace / 'tmp').mkdir()
        if args.offline:
            env.update(GOPROXY='off', GOSUMDB='off')
        go = compiler(args.go, workspace, args.offline, env)
        worker = workspace / ('builder-worker.exe' if os.name == 'nt' else 'builder-worker')
        bootstrap = subprocess.run([go, 'build', '-mod=readonly', '-buildvcs=false', '-o', str(worker), './tools/builder'], cwd=ROOT, env=env)
        if bootstrap.returncode:
            raise RuntimeError(f'builder bootstrap failed: {bootstrap.returncode}')
        command = [str(worker), '--modules', selected,
                   '--source-dir', str(ROOT), '--output', str(Path(output).resolve()),
                   '--os', target_os, '--arch', target_arch, '--go', go, '--timeout', args.timeout]
        # A private cache is shared by bootstrap and worker, then removed here.
        command.extend(['--cache-dir', str(cache)])
        if args.offline:
            command.append('--offline')
        process = subprocess.Popen(command, cwd=ROOT, env=env)
        try:
            status = process.wait()
        except KeyboardInterrupt:
            if process.poll() is None:
                try:
                    process.send_signal(signal.SIGINT)
                except (OSError, ValueError):
                    pass
                try:
                    process.wait(timeout=20)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
            raise
        if status:
            raise RuntimeError(f'builder exited with status {status}')

if __name__ == '__main__':
    try:
        main()
    except (KeyboardInterrupt, EOFError):
        print('\nBuild canceled.', file=sys.stderr)
        sys.exit(130)
    except Exception as exc:
        print(f'Build failed: {exc}', file=sys.stderr)
        sys.exit(1)
