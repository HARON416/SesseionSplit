"""No passing Windows package receipt without current browser verification."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import windows_package as package


class WindowsPackageTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.source = self.base / 'src'
        self.out = self.source / 'out' / 'SessionSplitBaseline'
        self.out.mkdir(parents=True)
        self.receipt = self.base / 'windows-package.json'
        self.receipt.write_text('{"old":"evidence"}')
        for patch in [mock.patch.object(package.bootstrap, 'BASE', self.base),
                      mock.patch.object(package.bootstrap, 'SOURCE', self.source),
                      mock.patch.object(package.platform, 'system', return_value='Windows')]:
            patch.start()
            self.addCleanup(patch.stop)

    def test_failed_runtime_verification_invalidates_old_package_evidence(self):
        with mock.patch.object(package.verify_baseline, 'verify', side_effect=RuntimeError('smoke failed')):
            with self.assertRaisesRegex(RuntimeError, 'smoke failed'):
                package.record_package()
        self.assertFalse(self.receipt.exists())

    def test_missing_installer_cannot_publish_package_evidence(self):
        with mock.patch.object(package.verify_baseline, 'verify'):
            with self.assertRaisesRegex(RuntimeError, 'installer is missing'):
                package.record_package()
        self.assertFalse(self.receipt.exists())

    def test_receipt_binds_installer_hash_and_current_baseline_receipt(self):
        (self.out / 'mini_installer.exe').write_bytes(b'installer fixture')
        baseline = self.base / 'baseline-verification.json'

        def verify():
            baseline.write_text('{"scope":"baseline smoke"}')

        with mock.patch.object(package.verify_baseline, 'verify', side_effect=verify):
            package.record_package()
        record = json.loads(self.receipt.read_text())
        self.assertEqual(record['installer_sha256'], package.verify_baseline.sha256(self.out / 'mini_installer.exe'))
        self.assertEqual(record['baseline_receipt_sha256'], package.verify_baseline.sha256(baseline))
        self.assertIn('not the custom', record['scope'])
