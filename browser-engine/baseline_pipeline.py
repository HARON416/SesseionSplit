#!/usr/bin/env python3
"""Run baseline stages sequentially, stopping at the first failed stage."""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import signal
import subprocess
import sys

import bootstrap
from workspace_lock import exclusive_lock, lock_is_held

STAGES = ('deps-check', 'sync', 'hooks', 'configure', 'build', 'verify')


def record(status, stage, **details):
    bootstrap.record_state(bootstrap.BASE / 'baseline-pipeline.json', {
        'status': status, 'stage': stage, 'pid': os.getpid(),
        'updated_at': datetime.now(timezone.utc).isoformat(), **details,
    })
    print(f'{stage}: {status}', flush=True)


def run_pipeline(start, jobs):
    with exclusive_lock(bootstrap.BASE / 'baseline-pipeline.lock'):
        if lock_is_held(bootstrap.BASE / 'bootstrap.lock'):
            raise RuntimeError('A bootstrap stage is active; refusing concurrent work')
        stage = start
        child = None
        previous = signal.getsignal(signal.SIGTERM)

        def interrupted(signum, frame):
            raise InterruptedError(f'Pipeline interrupted by signal {signum}')

        signal.signal(signal.SIGTERM, interrupted)
        try:
            for stage in STAGES[STAGES.index(start):]:
                record('running', stage)
                child = subprocess.Popen([
                    sys.executable, str(Path(bootstrap.__file__)), stage,
                    '--jobs', str(jobs),
                ], cwd=bootstrap.ROOT)
                code = child.wait()
                if code:
                    raise RuntimeError(f'{stage} failed ({code}); inspect logs/{stage}.log')
            record('passed', 'verify', receipt=str(
                bootstrap.BASE / 'baseline-verification.json'))
        except BaseException as exc:
            if child is not None and child.poll() is None:
                child.terminate()
                # bootstrap handles SIGTERM and stops its full child process group.
                child.wait()
            record('interrupted' if isinstance(exc, (InterruptedError, KeyboardInterrupt))
                   else 'failed', stage, error=str(exc))
            raise
        finally:
            signal.signal(signal.SIGTERM, previous)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--from-stage', choices=STAGES, default=STAGES[0])
    parser.add_argument('--jobs', type=int, default=2)
    args = parser.parse_args()
    if args.jobs < 1:
        parser.error('--jobs must be positive')
    run_pipeline(args.from_stage, args.jobs)


if __name__ == '__main__':
    try:
        main()
    except (RuntimeError, InterruptedError, KeyboardInterrupt) as exc:
        print(str(exc), file=sys.stderr)
        sys.exit(1)
