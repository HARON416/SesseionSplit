"""Filesystem locks work even when process IDs belong to another namespace."""
from contextlib import contextmanager
import os


@contextmanager
def exclusive_lock(path):
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open('a+b') as handle:
        if os.name == 'nt':
            import msvcrt
            if path.stat().st_size == 0:
                handle.write(b'\0')
                handle.flush()
            handle.seek(0)
            try:
                msvcrt.locking(handle.fileno(), msvcrt.LK_NBLCK, 1)
            except OSError as exc:
                raise RuntimeError(f'Workspace is busy: {path.name}') from exc
            try:
                yield
            finally:
                handle.seek(0)
                msvcrt.locking(handle.fileno(), msvcrt.LK_UNLCK, 1)
        else:
            import fcntl
            try:
                fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError as exc:
                raise RuntimeError(f'Workspace is busy: {path.name}') from exc
            try:
                yield
            finally:
                fcntl.flock(handle, fcntl.LOCK_UN)


def lock_is_held(path):
    try:
        with exclusive_lock(path):
            return False
    except RuntimeError:
        return True
