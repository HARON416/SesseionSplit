#!/usr/bin/env python3
"""Prepare the pinned depot_tools checkout on a provisioned Windows builder."""
import json
import platform
import subprocess

import bootstrap
from workspace_lock import exclusive_lock


def prepare():
    if platform.system() != 'Windows':
        raise RuntimeError('This preparation command requires a Windows builder')
    manifest = json.loads(bootstrap.MANIFEST.read_text())['depot_tools']
    with exclusive_lock(bootstrap.BASE / 'bootstrap.lock'):
        tools = bootstrap.TOOLS
        if tools.exists() and any(tools.iterdir()):
            if bootstrap.git('remote', 'get-url', 'origin', cwd=tools) != manifest['url']:
                raise RuntimeError('Unexpected depot_tools remote; preserving existing files')
            if bootstrap.git('rev-parse', 'HEAD', cwd=tools) != manifest['revision']:
                raise RuntimeError('Existing depot_tools revision differs; use a fresh workspace')
            return
        tools.mkdir(parents=True, exist_ok=True)
        subprocess.run(['git', 'init', str(tools)], check=True)
        subprocess.run(['git', 'config', 'core.autocrlf', 'false'], cwd=tools, check=True)
        subprocess.run(['git', 'config', 'core.longpaths', 'true'], cwd=tools, check=True)
        subprocess.run(['git', 'remote', 'add', 'origin', manifest['url']], cwd=tools, check=True)
        subprocess.run(['git', 'fetch', '--depth=1', 'origin', manifest['revision']], cwd=tools, check=True)
        subprocess.run(['git', 'checkout', '--detach', manifest['revision']], cwd=tools, check=True)


if __name__ == '__main__':
    try:
        prepare()
    except (RuntimeError, subprocess.CalledProcessError) as exc:
        raise SystemExit(str(exc)) from exc
