"""Cross-process exclusion and stale-state recovery without network access."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from workspace_lock import exclusive_lock, lock_is_held


class WorkspaceLockTests(unittest.TestCase):
    def test_second_process_cannot_enter_locked_workspace(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'workspace.lock'
            command = [sys.executable, '-c',
                       'from pathlib import Path; import sys; '
                       'from workspace_lock import exclusive_lock; '
                       'with_lock = exclusive_lock(Path(sys.argv[1])); '
                       'with_lock.__enter__()', str(path)]
            with exclusive_lock(path):
                self.assertTrue(lock_is_held(path))
                result = subprocess.run(command, cwd=Path(__file__).parent,
                                        capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('Workspace is busy', result.stderr)
            self.assertFalse(lock_is_held(path))
            result = subprocess.run(command, cwd=Path(__file__).parent,
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)

    def test_exception_releases_lock_and_existing_file_does_not_mean_busy(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'workspace.lock'
            path.write_text('stale process metadata')
            with self.assertRaises(ValueError):
                with exclusive_lock(path):
                    raise ValueError('interrupted')
            self.assertFalse(lock_is_held(path))
