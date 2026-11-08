#!/usr/bin/env python3
"""Reproducible Chromium baseline stages. No fingerprint patches are implied."""
import argparse
import json
import os
from pathlib import Path
import platform
import re
import shutil
import signal
import subprocess
import sys
import time

from workspace_lock import exclusive_lock, lock_is_held

ROOT = Path(__file__).resolve().parents[1]
BASE = ROOT / '.tools' / 'chromium'
WORKSPACE = BASE / 'workspace'
SOURCE = WORKSPACE / 'src'
TOOLS = BASE / 'depot_tools'
MANIFEST = ROOT / 'browser-engine' / 'upstream.json'
FLOOR = 20 * 1024 ** 3


def git(*args, cwd=SOURCE):
    return subprocess.check_output(['git', *args], cwd=cwd, text=True).strip()


def stop_child(child):
    if child.poll() is not None:
        return
    try:
        if os.name != 'nt':
            os.killpg(child.pid, signal.SIGTERM)
        else:
            subprocess.run(['taskkill', '/PID', str(child.pid), '/T', '/F'],
                           check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        child.wait(timeout=10)
    except subprocess.TimeoutExpired:
        if os.name != 'nt':
            os.killpg(child.pid, signal.SIGKILL)
        else:
            child.kill()
        child.wait(timeout=10)
    except ProcessLookupError:
        child.wait(timeout=10)


def record_state(path, state):
    temporary = path.with_name(f'{path.name}.{os.getpid()}.tmp')
    temporary.write_text(json.dumps(state, indent=2) + '\n')
    temporary.replace(path)


def run(command, cwd, env, stage):
    BASE.joinpath('logs').mkdir(parents=True, exist_ok=True)
    log = BASE / 'logs' / f'{stage}.log'
    print(f'{stage}: running; log: {log}', flush=True)
    with log.open('a', encoding='utf-8') as output:
        output.write(f'\nStage {stage} at {time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}\n')
        output.flush()
        child = subprocess.Popen(command, cwd=cwd, env=env, stdout=output,
                                 stderr=subprocess.STDOUT, start_new_session=os.name != 'nt')
        state = {'stage': stage, 'pid': child.pid, 'status': 'running', 'log': str(log)}
        state_path = BASE / 'state.json'
        record_state(state_path, state)
        previous_handler = signal.getsignal(signal.SIGTERM)
        def interrupted(signum, frame):
            raise InterruptedError(f'{stage} interrupted by signal {signum}')
        signal.signal(signal.SIGTERM, interrupted)
        try:
            while child.poll() is None:
                if shutil.disk_usage(BASE).free < FLOOR:
                    raise RuntimeError('Stopped before disk space fell below the 20 GiB reserve')
                time.sleep(2)
        except BaseException:
            stop_child(child)
            state['status'] = 'interrupted'
            record_state(state_path, state)
            raise
        finally:
            signal.signal(signal.SIGTERM, previous_handler)
        state['status'] = 'passed' if child.returncode == 0 else 'failed'
        state['exit_code'] = child.returncode
        record_state(state_path, state)
    if child.returncode:
        raise RuntimeError(f'{stage} failed ({child.returncode}); inspect {log}')
    print(f'{stage}: passed', flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('stage', choices=['status', 'tools', 'fetch', 'sync', 'deps-check', 'hooks', 'configure', 'build', 'verify', 'package-windows'])
    parser.add_argument('--jobs', type=int, default=2)
    args = parser.parse_args()
    if args.jobs < 1:
        parser.error('--jobs must be positive')
    if args.stage == 'status':
        execute(args)
    else:
        with exclusive_lock(BASE / 'bootstrap.lock'):
            execute(args)


def execute(args):
    BASE.mkdir(parents=True, exist_ok=True)
    if args.stage == 'verify':
        # Revision/resource guards below must not leave evidence from an older run.
        BASE.joinpath('baseline-verification.json').unlink(missing_ok=True)
    if args.stage == 'package-windows':
        BASE.joinpath('windows-package.json').unlink(missing_ok=True)
        if os.name != 'nt':
            raise RuntimeError('Windows packaging requires a native Windows builder')
    manifest = json.loads(MANIFEST.read_text())
    revision = manifest['chromium']['revision']
    if not re.fullmatch(r'[0-9a-f]{40}', revision):
        raise RuntimeError('An exact Chromium commit must be pinned')
    if args.stage == 'status':
        result = {'version': manifest['chromium']['version'], 'revision': revision,
                  'free_gib': round(shutil.disk_usage(BASE).free / 1024 ** 3, 1),
                  'source': str(SOURCE), 'host': platform.system(), 'fingerprint_patches': False}
        if BASE.joinpath('state.json').exists():
            result['last_stage'] = json.loads(BASE.joinpath('state.json').read_text())
        result['stage_active'] = lock_is_held(BASE / 'bootstrap.lock')
        result['pipeline_active'] = lock_is_held(BASE / 'baseline-pipeline.lock')
        if SOURCE.joinpath('.git').exists():
            try:
                result['checked_out_revision'] = git('rev-parse', 'HEAD')
            except subprocess.CalledProcessError:
                result['checked_out_revision'] = None
        print(json.dumps(result, indent=2))
        return
    if platform.machine().lower() not in ('x86_64', 'amd64'):
        raise RuntimeError('The initial baseline targets x64 hosts')
    if ' ' in str(WORKSPACE):
        raise RuntimeError('Chromium build paths must not contain spaces')
    if shutil.disk_usage(BASE).free < FLOOR:
        raise RuntimeError('Insufficient free disk space for the 20 GiB reserve')
    env = os.environ.copy()
    env['PATH'] = str(TOOLS) + os.pathsep + env.get('PATH', '')
    env['DEPOT_TOOLS_UPDATE'] = '0'
    env['GIT_TERMINAL_PROMPT'] = '0'
    env['CIPD_CACHE_DIR'] = str(BASE / 'cache' / 'cipd')
    env['VPYTHON_VIRTUALENV_ROOT'] = str(BASE / 'cache' / 'vpython')
    if os.name == 'nt':
        env['DEPOT_TOOLS_WIN_TOOLCHAIN'] = '0'
    WORKSPACE.mkdir(parents=True, exist_ok=True)
    if args.stage == 'fetch':
        if not SOURCE.joinpath('.git').exists():
            if shutil.disk_usage(BASE).free < 100 * 1024 ** 3:
                raise RuntimeError('A fresh Chromium checkout requires at least 100 GiB free')
            SOURCE.mkdir(parents=True, exist_ok=True)
            subprocess.run(['git', 'init'], cwd=SOURCE, check=True)
            subprocess.run(['git', 'remote', 'add', 'origin', manifest['chromium']['url']], cwd=SOURCE, check=True)
            subprocess.run(['git', 'config', 'core.autocrlf', 'false'], cwd=SOURCE, check=True)
            if os.name == 'nt':
                subprocess.run(['git', 'config', 'core.longpaths', 'true'], cwd=SOURCE, check=True)
        if git('remote', 'get-url', 'origin') != manifest['chromium']['url']:
            raise RuntimeError('Unexpected source remote; refusing to change this checkout')
        # A shallow fetch pins the baseline without downloading the entire history.
        run(['git', '-c', 'pack.threads=2', 'fetch', '--progress', '--depth=1', 'origin', revision], SOURCE, env, 'fetch')
        if git('status', '--porcelain'):
            raise RuntimeError('Source has changes; refusing to overwrite them')
        run(['git', 'checkout', '--detach', revision], SOURCE, env, 'checkout')
        return
    pinned_tools = manifest['depot_tools']['revision']
    if not pinned_tools or git('rev-parse', 'HEAD', cwd=TOOLS) != pinned_tools:
        raise RuntimeError('depot_tools checkout does not match the pinned revision')
    if args.stage == 'tools':
        if os.name == 'nt':
            run(['cmd.exe', '/d', '/c', str(TOOLS / 'bootstrap' / 'win_tools.bat')], TOOLS, env, 'tools')
        else:
            # Bootstrap the packages pinned by this checkout without self-updating it.
            run(['bash', '-c', 'source ./bootstrap_python3 && bootstrap_python3'], TOOLS, env, 'tools-python')
            # vpython3 otherwise hides this bootstrap's output, making gclient
            # sync appear idle while CIPD initializes its managed launchers.
            run([str(TOOLS / 'cipd'), 'ensure', '-log-level', 'info',
                 '-ensure-file', str(TOOLS / 'cipd_manifest.txt'),
                 '-root', str(TOOLS / '.cipd_bin')], TOOLS, env, 'tools-packages')
            # GCS dependencies can start concurrently during sync. Initialize
            # gsutil first so slow first-time setup cannot exceed its lock wait.
            run([str(TOOLS / 'vpython3'), str(TOOLS / 'gsutil.py'), 'version'],
                TOOLS, env, 'tools')
        return
    if git('rev-parse', 'HEAD') != revision:
        raise RuntimeError('Chromium checkout does not match the pinned revision')
    if args.stage == 'sync':
        config = WORKSPACE / '.gclient'
        expected = 'solutions = ' + repr([{'name': 'src', 'url': manifest['chromium']['url'], 'managed': False, 'custom_deps': {}, 'custom_vars': {}}]) + '\n'
        if config.exists() and config.read_text() != expected:
            raise RuntimeError('Unexpected .gclient configuration; refusing to replace it')
        config.write_text(expected)
        run([str(TOOLS / ('gclient.bat' if os.name == 'nt' else 'gclient')), 'sync', '--no-history', '--nohooks', f'--jobs={args.jobs}', '--revision', f'src@{revision}'], WORKSPACE, env, 'sync')
    elif args.stage == 'deps-check':
        if sys.platform != 'linux':
            raise RuntimeError('deps-check currently supports Linux hosts only')
        # Upstream's quick check exits before apt updates or package installation.
        run([sys.executable, str(SOURCE / 'build' / 'install-build-deps.py'),
             '--quick-check', '--no-arm', '--no-chromeos-fonts'],
            SOURCE, env, 'deps-check')
    elif args.stage == 'hooks':
        run([str(TOOLS / ('gclient.bat' if os.name == 'nt' else 'gclient')), 'runhooks'], WORKSPACE, env, 'hooks')
    elif args.stage == 'configure':
        if os.name == 'nt':
            template = 'args.windows.gn'
        elif sys.platform == 'linux':
            template = 'args.linux.gn'
        else:
            raise RuntimeError('Initial build templates support Linux and Windows')
        out = SOURCE / 'out' / 'SessionSplitBaseline'
        out.mkdir(parents=True, exist_ok=True)
        desired = (ROOT / 'browser-engine' / template).read_text()
        arguments = out / 'args.gn'
        if arguments.exists() and arguments.read_text() != desired:
            raise RuntimeError('Existing args.gn differs from the baseline; refusing to replace it')
        arguments.write_text(desired)
        # The pinned wrapper handles both old and new CIPD binary layouts.
        run([str(TOOLS / ('gn.bat' if os.name == 'nt' else 'gn')), 'gen', 'out/SessionSplitBaseline'], SOURCE, env, 'configure')
    elif args.stage == 'verify':
        run([sys.executable, str(ROOT / 'browser-engine' / 'verify_baseline.py')],
            SOURCE, env, 'verify')
    elif args.stage == 'package-windows':
        run([str(TOOLS / 'autoninja.bat'), '-C', 'out/SessionSplitBaseline',
             f'-j{args.jobs}', 'mini_installer'], SOURCE, env, 'package-windows')
        run([sys.executable, str(ROOT / 'browser-engine' / 'windows_package.py')],
            SOURCE, env, 'verify-package')
    else:
        # Chromium's own launcher selects the checkout's supported local build tool.
        run([str(TOOLS / ('autoninja.bat' if os.name == 'nt' else 'autoninja')), '-C', 'out/SessionSplitBaseline', f'-j{args.jobs}', 'chrome'], SOURCE, env, 'build')


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, InterruptedError, subprocess.CalledProcessError) as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(1)
