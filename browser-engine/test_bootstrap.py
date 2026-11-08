"""Offline guardrails for the browser source bootstrap; no git/network/build runs."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import types
import unittest
from unittest import mock
from workspace_lock import exclusive_lock


SPEC = importlib.util.spec_from_file_location(
    'sessionsplit_bootstrap', Path(__file__).with_name('bootstrap.py'))
bootstrap = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(bootstrap)


class BootstrapGuardrailTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='sessionsplit-bootstrap-test-')
        self.addCleanup(self.temp.cleanup)
        root = Path(self.temp.name)
        self.base = root / '.tools' / 'chromium'
        self.workspace = self.base / 'workspace'
        self.source = self.workspace / 'src'
        self.tools = self.base / 'depot_tools'
        self.manifest_path = root / 'upstream.json'
        self.source.joinpath('.git').mkdir(parents=True)
        self.tools.joinpath('.git').mkdir(parents=True)
        self.revision = 'a' * 40
        self.tools_revision = 'b' * 40
        self.manifest = {
            'chromium': {'revision': self.revision, 'version': '157.0.0.0',
                         'url': 'https://chromium.googlesource.com/chromium/src.git'},
            'depot_tools': {'revision': self.tools_revision},
        }
        self.write_manifest()
        for name, value in {'ROOT': root, 'BASE': self.base,
                            'WORKSPACE': self.workspace, 'SOURCE': self.source,
                            'TOOLS': self.tools, 'MANIFEST': self.manifest_path}.items():
            patch = mock.patch.object(bootstrap, name, value)
            patch.start()
            self.addCleanup(patch.stop)
        self.commands = []
        self.git_results = {}
        patches = [
            mock.patch.object(bootstrap, 'git', side_effect=self.fake_git),
            mock.patch.object(bootstrap, 'run', side_effect=self.fake_run),
            mock.patch.object(bootstrap.subprocess, 'run',
                              side_effect=AssertionError('Real subprocess is forbidden in offline tests')),
            mock.patch.object(bootstrap.subprocess, 'check_output',
                              side_effect=AssertionError('Real git is forbidden in offline tests')),
            mock.patch.object(bootstrap.shutil, 'disk_usage',
                              return_value=types.SimpleNamespace(free=150 * 1024 ** 3)),
            mock.patch.object(bootstrap.platform, 'machine', return_value='x86_64'),
        ]
        for patch in patches:
            patch.start()
            self.addCleanup(patch.stop)

    def write_manifest(self):
        self.manifest_path.write_text(json.dumps(self.manifest))

    def fake_git(self, *args, cwd=None):
        key = tuple(args)
        if key in self.git_results:
            return self.git_results[key]
        if key == ('rev-parse', 'HEAD'):
            return self.tools_revision if cwd == self.tools else self.revision
        if key == ('remote', 'get-url', 'origin'):
            return self.manifest['chromium']['url']
        if key == ('status', '--porcelain'):
            return ''
        raise AssertionError(f'Unexpected git query: {args!r}')

    def fake_run(self, command, cwd, env, stage):
        self.commands.append((command, cwd, env, stage))

    def invoke(self, stage, *args):
        with mock.patch.object(bootstrap.sys, 'argv', ['bootstrap.py', stage, *args]):
            with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                bootstrap.main()

    def test_rejects_unpinned_source_revision_before_running_any_command(self):
        self.manifest['chromium']['revision'] = 'main'
        self.write_manifest()
        with self.assertRaisesRegex(RuntimeError, 'exact Chromium commit'):
            self.invoke('sync')
        self.assertEqual(self.commands, [])

    def test_busy_workspace_prevents_mutation_and_preserves_receipt(self):
        receipt = self.base / 'baseline-verification.json'
        receipt.write_text('{"old":"evidence"}')
        with exclusive_lock(self.base / 'bootstrap.lock'):
            with self.assertRaisesRegex(RuntimeError, 'Workspace is busy'):
                self.invoke('verify')
        self.assertTrue(receipt.exists())
        self.assertEqual(self.commands, [])

    def test_status_uses_locks_instead_of_process_visibility(self):
        self.base.joinpath('state.json').write_text(
            '{"status":"running","pid":999999,"stage":"sync"}')
        output = io.StringIO()
        with exclusive_lock(self.base / 'bootstrap.lock'):
            with mock.patch.object(bootstrap.sys, 'argv', ['bootstrap.py', 'status']):
                with contextlib.redirect_stdout(output):
                    bootstrap.main()
        report = json.loads(output.getvalue())
        self.assertTrue(report['stage_active'])
        self.assertFalse(report['pipeline_active'])

    def test_rejects_nonpositive_jobs_before_running_any_command(self):
        with self.assertRaises(SystemExit):
            self.invoke('build', '--jobs', '0')
        self.assertEqual(self.commands, [])

    def test_low_disk_reserve_prevents_stage_start(self):
        with mock.patch.object(bootstrap.shutil, 'disk_usage',
                               return_value=types.SimpleNamespace(free=bootstrap.FLOOR - 1)):
            with self.assertRaisesRegex(RuntimeError, '20 GiB reserve'):
                self.invoke('sync')
        self.assertEqual(self.commands, [])

    def test_unpinned_tools_prevent_dependency_sync(self):
        self.manifest['depot_tools']['revision'] = None
        self.write_manifest()
        with self.assertRaisesRegex(RuntimeError, 'depot_tools checkout'):
            self.invoke('sync')
        self.assertEqual(self.commands, [])

    def test_wrong_source_revision_prevents_hooks(self):
        self.git_results[('rev-parse', 'HEAD')] = 'c' * 40
        # Tools must match first, otherwise this would test the wrong guard.
        self.manifest['depot_tools']['revision'] = 'c' * 40
        self.write_manifest()
        with self.assertRaisesRegex(RuntimeError, 'Chromium checkout'):
            self.invoke('hooks')
        self.assertEqual(self.commands, [])

    def test_unexpected_gclient_configuration_is_preserved(self):
        config = self.workspace / '.gclient'
        existing = 'solutions = [{"name": "a-different-project"}]\n'
        config.write_text(existing)
        with self.assertRaisesRegex(RuntimeError, 'Unexpected .gclient'):
            self.invoke('sync')
        self.assertEqual(config.read_text(), existing)
        self.assertEqual(self.commands, [])

    def test_dirty_source_is_not_checked_out_over(self):
        marker = self.source / 'local-work.cc'
        marker.write_text('preserve this work')
        self.git_results[('status', '--porcelain')] = '?? local-work.cc'
        with self.assertRaisesRegex(RuntimeError, 'Source has changes'):
            self.invoke('fetch')
        self.assertEqual(marker.read_text(), 'preserve this work')
        self.assertNotIn('checkout', [item[3] for item in self.commands])

    def test_unexpected_source_remote_prevents_fetch(self):
        self.git_results[('remote', 'get-url', 'origin')] = 'https://example.invalid/other.git'
        with self.assertRaisesRegex(RuntimeError, 'Unexpected source remote'):
            self.invoke('fetch')
        self.assertEqual(self.commands, [])

    def test_sync_keeps_revision_nohooks_history_and_jobs_explicit(self):
        self.invoke('sync', '--jobs', '2')
        command, cwd, env, stage = self.commands[-1]
        self.assertEqual(stage, 'sync')
        self.assertEqual(cwd, self.workspace)
        self.assertIn('--no-history', command)
        self.assertIn('--nohooks', command)
        self.assertIn('--jobs=2', command)
        self.assertEqual(command[command.index('--revision') + 1], f'src@{self.revision}')
        self.assertEqual(env['DEPOT_TOOLS_UPDATE'], '0')
        self.assertEqual(env['GIT_TERMINAL_PROMPT'], '0')
        self.assertEqual(env['PATH'].split(bootstrap.os.pathsep)[0], str(self.tools))

    def test_sync_and_hooks_use_managed_gclient_launcher(self):
        launcher = 'gclient.bat' if bootstrap.os.name == 'nt' else 'gclient'
        for stage in ('sync', 'hooks'):
            self.invoke(stage)
            command = self.commands[-1][0]
            self.assertEqual(command[0], str(self.tools / launcher))
            self.assertNotIn(str(self.tools / 'gclient.py'), command)

    def test_build_uses_managed_autoninja_with_explicit_small_job_limit(self):
        self.invoke('build', '--jobs', '2')
        command, cwd, env, stage = self.commands[-1]
        launcher = 'autoninja.bat' if bootstrap.os.name == 'nt' else 'autoninja'
        self.assertEqual(command[0], str(self.tools / launcher))
        self.assertEqual(cwd, self.source)
        self.assertIn('-j2', command)
        self.assertEqual(command[-1], 'chrome')
        self.assertEqual(env['DEPOT_TOOLS_UPDATE'], '0')

    def test_dependency_check_uses_pinned_source_without_installing_packages(self):
        with mock.patch.object(bootstrap.sys, 'platform', 'linux'):
            self.invoke('deps-check')
        command, cwd, env, stage = self.commands[-1]
        self.assertEqual(stage, 'deps-check')
        self.assertEqual(cwd, self.source)
        self.assertEqual(command, [bootstrap.sys.executable,
                                  str(self.source / 'build' / 'install-build-deps.py'),
                                  '--quick-check', '--no-arm', '--no-chromeos-fonts'])

    def test_dependency_check_rejects_non_linux_hosts(self):
        with mock.patch.object(bootstrap.sys, 'platform', 'win32'):
            with self.assertRaisesRegex(RuntimeError, 'Linux hosts only'):
                self.invoke('deps-check')
        self.assertEqual(self.commands, [])

    def test_verify_uses_baseline_verifier_after_revision_guards(self):
        self.invoke('verify')
        command, cwd, env, stage = self.commands[-1]
        self.assertEqual(stage, 'verify')
        self.assertEqual(command, [bootstrap.sys.executable,
                                  str(bootstrap.ROOT / 'browser-engine' / 'verify_baseline.py')])
        self.assertEqual(cwd, self.source)

    def test_verify_revision_guard_invalidates_older_passing_receipt(self):
        receipt = self.base / 'baseline-verification.json'
        receipt.write_text('{"old": "evidence"}')
        self.git_results[('rev-parse', 'HEAD')] = 'c' * 40
        self.manifest['depot_tools']['revision'] = 'c' * 40
        self.write_manifest()
        with self.assertRaisesRegex(RuntimeError, 'Chromium checkout'):
            self.invoke('verify')
        self.assertFalse(receipt.exists())
        self.assertEqual(self.commands, [])

    def test_verify_invalid_manifest_pin_cannot_leave_old_passing_receipt(self):
        receipt = self.base / 'baseline-verification.json'
        receipt.write_text('{"old": "evidence"}')
        self.manifest['chromium']['revision'] = 'main'
        self.write_manifest()
        with self.assertRaisesRegex(RuntimeError, 'exact Chromium commit'):
            self.invoke('verify')
        self.assertFalse(receipt.exists())
        self.assertEqual(self.commands, [])

    @unittest.skipIf(bootstrap.os.name == 'nt', 'Linux bootstrap path')
    def test_tools_bootstraps_managed_launchers_with_visible_package_logs(self):
        self.invoke('tools')
        self.assertEqual([c[3] for c in self.commands],
                         ['tools-python', 'tools-packages', 'tools'])
        command = self.commands[-2][0]
        self.assertEqual(command[0], str(self.tools / 'cipd'))
        self.assertIn('ensure', command)
        self.assertEqual(command[command.index('-log-level') + 1], 'info')
        self.assertEqual(command[command.index('-ensure-file') + 1],
                         str(self.tools / 'cipd_manifest.txt'))
        self.assertEqual(self.commands[-1][0],
                         [str(self.tools / 'vpython3'), str(self.tools / 'gsutil.py'), 'version'])


if __name__ == '__main__':
    unittest.main()
