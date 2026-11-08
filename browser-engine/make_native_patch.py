#!/usr/bin/env python3
"""Generate the draft parser patch without modifying the baseline checkout."""
import difflib
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / '.tools' / 'chromium' / 'workspace' / 'src'
DESTINATION = ROOT / 'browser-engine' / 'patches' / '0001-profile-config-parser.patch'
TARGET = 'components/embedder_support/'


def main():
    manifest = json.loads((ROOT / 'browser-engine' / 'upstream.json').read_text())
    head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=SOURCE,
                                   text=True).strip()
    if head != manifest['chromium']['revision']:
        raise SystemExit('Refusing to generate a patch for an unpinned checkout')
    relative = TARGET + 'BUILD.gn'
    original = subprocess.check_output(['git', 'show', f'HEAD:{relative}'],
                                       cwd=SOURCE, text=True)
    if (SOURCE / relative).read_text() != original:
        raise SystemExit('Baseline BUILD.gn has changes; refusing to overwrite their context')
    imported = 'import("//device/vr/buildflags/buildflags.gni")\n'
    if original.count(imported) != 1:
        raise SystemExit('Unexpected BUILD.gn imports')
    updated = original.replace(imported, imported + 'import("//testing/test.gni")\n', 1)
    anchor = 'static_library("content_settings") {'
    if updated.count(anchor) != 1:
        raise SystemExit('Unexpected BUILD.gn targets')
    targets = '''component("sessionsplit_profile_config") {
  sources = [
    "sessionsplit_profile_config.cc",
    "sessionsplit_profile_config.h",
  ]
  defines = [ "IS_SESSIONSPLIT_PROFILE_CONFIG_IMPL" ]
  public_deps = [ "//base" ]
  deps = [
    "//base/i18n:language_tag",
    "//third_party/icu",
  ]
}

test("sessionsplit_profile_config_unittests") {
  sources = [ "sessionsplit_profile_config_unittest.cc" ]
  deps = [
    ":sessionsplit_profile_config",
    "//base/test:run_all_unittests",
    "//base/test:test_support",
    "//testing/gtest",
  ]
}

'''
    updated = updated.replace(anchor, targets + anchor, 1)
    patch = [f'diff --git a/{relative} b/{relative}\n']
    patch.extend(difflib.unified_diff(original.splitlines(keepends=True),
                                     updated.splitlines(keepends=True),
                                     fromfile='a/' + relative, tofile='b/' + relative))
    for path in sorted((ROOT / 'browser-engine' / 'native-draft').glob('sessionsplit_profile_config*')):
        relative = TARGET + path.name
        patch.extend([f'diff --git a/{relative} b/{relative}\n', 'new file mode 100644\n'])
        patch.extend(difflib.unified_diff([], path.read_text().splitlines(keepends=True),
                                         fromfile='/dev/null', tofile='b/' + relative))
    DESTINATION.parent.mkdir(parents=True, exist_ok=True)
    DESTINATION.write_text(''.join(patch))
    subprocess.run(['git', 'apply', '--check', str(DESTINATION)], cwd=SOURCE, check=True)
    print(f'Generated and checked applicability: {DESTINATION}')


if __name__ == '__main__':
    main()
