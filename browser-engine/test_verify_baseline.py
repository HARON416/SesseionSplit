"""Baseline evidence must not survive failed provenance or runtime checks."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location(
    'verify_baseline', Path(__file__).with_name('verify_baseline.py'))
verify = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(verify)


class BaselineVerificationTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory(prefix='baseline-verification-')
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        self.base = self.root / '.tools' / 'chromium'
        self.source = self.base / 'workspace' / 'src'
        self.tools = self.base / 'depot_tools'
        self.out = self.source / 'out' / 'SessionSplitBaseline'
        self.out.mkdir(parents=True)
        self.tools.mkdir()
        config = self.root / 'browser-engine'
        config.mkdir()
        self.manifest = {'chromium': {'revision': 'a' * 40, 'version': '157.0.8103.38'},
                         'depot_tools': {'revision': 'b' * 40}}
        (config / 'upstream.json').write_text(json.dumps(self.manifest))
        (config / 'args.linux.gn').write_text('is_debug = false\n')
        (self.out / 'args.gn').write_text('is_debug = false\n')
        (self.out / 'chrome').write_bytes(b'baseline-binary-fixture')
        self.receipt = self.base / 'baseline-verification.json'
        self.receipt.write_text('{"old": "evidence"}')
        self.commands = []
        self.dirty = False
        self.version = 'Chromium 157.0.8103.38'
        self.dom = f'<html><head></head><body>{verify.MARKER}</body></html>'
        self.runtime_error = None
        for name, value in {'ROOT': self.root, 'BASE': self.base,
                            'SOURCE': self.source, 'TOOLS': self.tools}.items():
            patch = mock.patch.object(verify, name, value)
            patch.start()
            self.addCleanup(patch.stop)
        for patch in [mock.patch.object(verify.platform, 'system', return_value='Linux'),
                      mock.patch.object(verify, 'capture', side_effect=self.capture)]:
            patch.start()
            self.addCleanup(patch.stop)

    def capture(self, command, cwd, timeout=60):
        self.commands.append(command)
        if command[:3] == ['git', 'rev-parse', 'HEAD']:
            return 'a' * 40 if cwd == self.source else 'b' * 40
        if command[:2] == ['git', 'status']:
            return ' M source.cc' if self.dirty else ''
        if '--version' in command:
            return self.version
        if '--dump-dom' in command:
            if self.runtime_error:
                raise self.runtime_error
            return self.dom
        raise AssertionError(f'Unexpected command: {command}')

    def invoke(self):
        with contextlib.redirect_stdout(io.StringIO()):
            verify.verify()

    def test_success_records_pins_args_binary_hash_and_real_smoke_scope(self):
        self.invoke()
        record = json.loads(self.receipt.read_text())
        self.assertEqual(record['upstream'], self.manifest)
        self.assertEqual(record['binary_sha256'], verify.sha256(self.out / 'chrome'))
        self.assertEqual(record['gn_args'], 'is_debug = false\n')
        self.assertTrue(record['smoke']['javascript_executed'])
        command = next(c for c in self.commands if '--dump-dom' in c)
        self.assertNotIn('--no-sandbox', command)
        self.assertTrue(any(c.startswith('--user-data-dir=') for c in command))
        self.assertFalse(list(self.base.glob('smoke-profile-*')))

    def test_dirty_source_invalidates_old_evidence_without_starting_browser(self):
        self.dirty = True
        with self.assertRaisesRegex(RuntimeError, 'tracked changes'):
            self.invoke()
        self.assertFalse(self.receipt.exists())
        self.assertFalse(any('--version' in c for c in self.commands))

    def test_wrong_revision_invalidates_old_evidence(self):
        manifest_path = self.root / 'browser-engine' / 'upstream.json'
        self.manifest['chromium']['revision'] = 'c' * 40
        manifest_path.write_text(json.dumps(self.manifest))
        with self.assertRaisesRegex(RuntimeError, 'revision does not match'):
            self.invoke()
        self.assertFalse(self.receipt.exists())

    def test_changed_build_arguments_prevent_receipt(self):
        (self.out / 'args.gn').write_text('is_debug = true\n')
        with self.assertRaisesRegex(RuntimeError, 'Build arguments differ'):
            self.invoke()
        self.assertFalse(self.receipt.exists())

    def test_version_prefix_match_is_not_accepted(self):
        self.version = 'Chromium 157.0.8103.380'
        with self.assertRaisesRegex(RuntimeError, 'Unexpected browser version'):
            self.invoke()
        self.assertFalse(self.receipt.exists())

    def test_echoed_page_source_does_not_prove_javascript_execution(self):
        self.dom = f'<body><script>document.body.textContent="{verify.MARKER}"</script></body>'
        with self.assertRaisesRegex(RuntimeError, 'did not execute'):
            self.invoke()
        self.assertFalse(self.receipt.exists())

    def test_browser_timeout_and_sandbox_failure_never_leave_passing_evidence(self):
        for error in [subprocess.TimeoutExpired(['chrome'], 60),
                      subprocess.CalledProcessError(1, ['chrome'], stderr='No usable sandbox')]:
            with self.subTest(error=type(error).__name__):
                self.receipt.write_text('{"old": "evidence"}')
                self.runtime_error = error
                with self.assertRaises(subprocess.SubprocessError):
                    self.invoke()
                self.assertFalse(self.receipt.exists())
                self.assertFalse(list(self.base.glob('smoke-profile-*')))

    def test_windows_uses_windows_arguments_executable_and_runtime_probe(self):
        (self.root / 'browser-engine' / 'args.windows.gn').write_text('is_debug = false\n')
        (self.out / 'chrome.exe').write_bytes(b'windows-baseline-fixture')
        with mock.patch.object(verify.platform, 'system', return_value='Windows'):
            with mock.patch.object(verify, 'windows_smoke', return_value='Chrome/157.0.8103.38') as probe:
                self.invoke()
        self.assertEqual(probe.call_args.args[:2], (self.out / 'chrome.exe', self.out))
        record = json.loads(self.receipt.read_text())
        self.assertEqual(record['host']['os'], 'Windows')
        self.assertEqual(record['binary_sha256'], verify.sha256(self.out / 'chrome.exe'))
        self.assertFalse(any('--version' in c for c in self.commands))

    def test_windows_smoke_failure_or_wrong_version_invalidates_receipt(self):
        (self.root / 'browser-engine' / 'args.windows.gn').write_text('is_debug = false\n')
        (self.out / 'chrome.exe').write_bytes(b'windows-baseline-fixture')
        with mock.patch.object(verify.platform, 'system', return_value='Windows'):
            for outcome in [RuntimeError('smoke failed'), 'Chrome/157.0.8103.380']:
                self.receipt.write_text('{"old":"evidence"}')
                options = {'side_effect': outcome} if isinstance(outcome, Exception) else {'return_value': outcome}
                with mock.patch.object(verify, 'windows_smoke', **options):
                    with self.assertRaises(RuntimeError):
                        self.invoke()
                self.assertFalse(self.receipt.exists())

    def test_windows_probe_checks_executed_title_and_cleans_up_process_tree(self):
        profile = self.base / 'probe-profile'
        profile.mkdir()
        (profile / 'DevToolsActivePort').write_text('12345\n/devtools/browser/test\n')
        self.base.joinpath('logs').mkdir()
        opener = mock.Mock()
        opener.open.side_effect = [io.StringIO(json.dumps([
            {'type': 'page', 'title': verify.MARKER}])),
            io.StringIO('{"Browser":"Chrome/157.0.8103.38"}')]
        child = mock.Mock(pid=123)
        child.poll.return_value = None
        with mock.patch.object(verify.urllib.request, 'build_opener', return_value=opener):
            with mock.patch.object(verify.subprocess, 'Popen', return_value=child) as start:
                with mock.patch.object(verify.subprocess, 'run') as stop:
                    result = verify.windows_smoke(self.out / 'chrome.exe', self.out, profile)
        self.assertEqual(result, 'Chrome/157.0.8103.38')
        command = start.call_args.args[0]
        self.assertNotIn('--no-sandbox', command)
        self.assertIn('--remote-debugging-address=127.0.0.1', command)
        stop.assert_called_once_with(['taskkill', '/PID', '123', '/T', '/F'],
                                     capture_output=True, check=False)
        child.wait.assert_called_once_with(timeout=15)

    def test_windows_probe_timeout_cannot_leave_browser_running(self):
        profile = self.base / 'probe-profile'
        profile.mkdir()
        self.base.joinpath('logs').mkdir()
        child = mock.Mock(pid=456)
        child.poll.return_value = None
        with mock.patch.object(verify.subprocess, 'Popen', return_value=child):
            with mock.patch.object(verify.subprocess, 'run') as stop:
                with mock.patch.object(verify.time, 'monotonic', side_effect=[0, 61]):
                    with self.assertRaisesRegex(RuntimeError, 'before timeout'):
                        verify.windows_smoke(self.out / 'chrome.exe', self.out, profile)
        stop.assert_called_once()
        child.wait.assert_called_once()


if __name__ == '__main__':
    unittest.main()
