#!/usr/bin/env python3
"""Verify a local unmodified baseline; emit evidence only after a sandboxed run."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import platform
import re
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
from urllib.parse import quote


ROOT = Path(__file__).resolve().parents[1]
BASE = ROOT / '.tools' / 'chromium'
SOURCE = BASE / 'workspace' / 'src'
TOOLS = BASE / 'depot_tools'
MARKER = 'sessionsplit-baseline-script-executed'


def capture(command, cwd, timeout=60):
    try:
        result = subprocess.run(command, cwd=cwd, capture_output=True, text=True,
                                timeout=timeout, check=True)
    except subprocess.CalledProcessError as exc:
        if exc.stderr:
            print(exc.stderr, flush=True)
        raise
    if result.stderr:
        print(result.stderr, flush=True)
    return result.stdout.strip()


def sha256(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def windows_smoke(binary, out, profile, timeout=60):
    """Windows GUI executables need DevTools rather than console --version output."""
    page = 'data:text/html,' + quote(
        f'<script>document.title="{MARKER}"</script>')
    command = [str(binary), '--headless', '--no-first-run',
               '--disable-background-networking', '--disable-component-update',
               '--disable-sync', '--remote-debugging-port=0',
               '--remote-debugging-address=127.0.0.1',
               f'--user-data-dir={profile}', page]
    active_port = Path(profile) / 'DevToolsActivePort'
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with (BASE / 'logs' / 'windows-smoke.log').open('w', encoding='utf-8') as log:
        child = subprocess.Popen(command, cwd=out, stdout=log, stderr=log)
        try:
            deadline = time.monotonic() + timeout
            while time.monotonic() < deadline:
                if child.poll() is not None:
                    raise RuntimeError('Windows baseline browser exited before smoke verification')
                try:
                    port = int(active_port.read_text().splitlines()[0])
                    if not 0 < port < 65536:
                        raise ValueError('Invalid DevTools port')
                    address = f'http://127.0.0.1:{port}'
                    with opener.open(address + '/json/list', timeout=2) as response:
                        targets = json.load(response)
                    if any(target.get('type') == 'page' and target.get('title') == MARKER
                           for target in targets):
                        with opener.open(address + '/json/version', timeout=2) as response:
                            return json.load(response)['Browser']
                except (OSError, ValueError, KeyError, IndexError):
                    pass  # The browser may not have finished startup yet.
                time.sleep(0.2)
            raise RuntimeError('Windows baseline did not execute the smoke page before timeout')
        finally:
            subprocess.run(['taskkill', '/PID', str(child.pid), '/T', '/F'],
                           capture_output=True, check=False)
            child.wait(timeout=15)


def verify():
    receipt = BASE / 'baseline-verification.json'
    # Invalidate old evidence before a new attempt, including a failed attempt.
    receipt.unlink(missing_ok=True)
    host = platform.system()
    if host not in ('Linux', 'Windows'):
        raise RuntimeError('Baseline runtime verification supports Linux and Windows only')
    manifest = json.loads((ROOT / 'browser-engine' / 'upstream.json').read_text())
    for name, directory in [('chromium', SOURCE), ('depot_tools', TOOLS)]:
        revision = capture(['git', 'rev-parse', 'HEAD'], directory)
        if revision != manifest[name]['revision']:
            raise RuntimeError(f'{name} revision does not match manifest')
        if capture(['git', 'status', '--porcelain', '--untracked-files=no'], directory):
            raise RuntimeError(f'{name} has tracked changes; baseline must be unmodified')
    out = SOURCE / 'out' / 'SessionSplitBaseline'
    binary = out / ('chrome.exe' if host == 'Windows' else 'chrome')
    gn_args = out / 'args.gn'
    expected_args = ROOT / 'browser-engine' / (
        'args.windows.gn' if host == 'Windows' else 'args.linux.gn')
    if gn_args.read_bytes() != expected_args.read_bytes():
        raise RuntimeError('Build arguments differ from the baseline template')
    expected_version = manifest['chromium']['version']
    # Start with a fresh isolated profile. Do not disable Chromium's sandbox.
    with tempfile.TemporaryDirectory(prefix='smoke-profile-', dir=BASE) as profile:
        if host == 'Windows':
            BASE.joinpath('logs').mkdir(exist_ok=True)
            version = windows_smoke(binary, out, profile)
        else:
            version = capture([str(binary), '--version'], out, timeout=15)
            page = ('data:text/html,<body><script>document.body.textContent='
                    f'"{MARKER}"</script>')
            command = [str(binary), '--headless', '--no-first-run',
                       '--disable-background-networking', '--disable-component-update',
                       '--disable-sync', f'--user-data-dir={profile}', '--dump-dom', page]
            dom = capture(command, out)
            if f'<body>{MARKER}</body>' not in dom:
                raise RuntimeError('Headless browser did not execute the baseline smoke page')
        if not re.search(r'(?<![\d.])' + re.escape(expected_version) + r'(?![\d.])', version):
            raise RuntimeError(f'Unexpected browser version: {version}')
    record = {
        'schema_version': 1,
        'verified_at': datetime.now(timezone.utc).isoformat(),
        'host': {'os': platform.system(), 'machine': platform.machine()},
        'upstream': manifest,
        'browser_version': version,
        'binary': str(binary),
        'binary_sha256': sha256(binary),
        'gn_args': gn_args.read_text(),
        'gn_args_sha256': sha256(gn_args),
        'smoke': {'javascript_executed': True, 'sandbox_disabled': False},
        'scope': 'Unmodified baseline smoke test; no fingerprint capability verified',
    }
    temporary = receipt.with_suffix('.json.tmp')
    temporary.write_text(json.dumps(record, indent=2) + '\n')
    temporary.replace(receipt)
    print(f'Baseline verification passed; evidence: {receipt}', flush=True)


if __name__ == '__main__':
    argparse.ArgumentParser(description=__doc__).parse_args()
    try:
        verify()
    except (OSError, RuntimeError, subprocess.SubprocessError) as exc:
        raise SystemExit(str(exc)) from exc
