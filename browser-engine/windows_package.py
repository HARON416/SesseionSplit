#!/usr/bin/env python3
"""Record a Windows baseline installer only after current runtime verification."""
import json
import platform

import bootstrap
import verify_baseline


def record_package():
    receipt = bootstrap.BASE / 'windows-package.json'
    receipt.unlink(missing_ok=True)
    if platform.system() != 'Windows':
        raise RuntimeError('Windows packaging requires a Windows builder')
    verify_baseline.verify()
    baseline = bootstrap.BASE / 'baseline-verification.json'
    installer = bootstrap.SOURCE / 'out' / 'SessionSplitBaseline' / 'mini_installer.exe'
    if not installer.is_file():
        raise RuntimeError('The Windows baseline installer is missing')
    bootstrap.record_state(receipt, {
        'schema_version': 1,
        'installer': installer.name,
        'installer_sha256': verify_baseline.sha256(installer),
        'baseline_receipt_sha256': verify_baseline.sha256(baseline),
        'baseline': json.loads(baseline.read_text()),
        'scope': 'Unmodified Chromium baseline; not the custom SessionSplit fingerprint engine',
        'signed': False,
    })


if __name__ == '__main__':
    try:
        record_package()
    except (OSError, RuntimeError) as exc:
        raise SystemExit(str(exc)) from exc
