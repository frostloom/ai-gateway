"""Verify frozen task definitions against their archived runtime code, not current runtime correctness."""
import argparse
import importlib.util
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ARCHIVE = ROOT / 'eval/task-definitions/2026-10-12-full-dev-v1'
SNAPSHOT = ROOT / 'eval/code-snapshots/2026-10-12-full-dev-v1'
# Pinned from the frozen manifest at c028aa85; never derive this from runtime input.
FROZEN_MANIFEST_SHA256 = 'afaf5fba49893d2c20e91dc8b0e84c998608b82194d6613a6b6549e289ac4181'
ARCHIVED_BINDINGS = ('internal/agent/tools.go', 'internal/billing/store/plan.go')


def verify(archive=ARCHIVE, snapshot=SNAPSHOT):
    spec = importlib.util.spec_from_file_location('frozen_task_generator', ROOT / 'scripts/define-customer-tasks.py')
    generator = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(generator)
    archive, snapshot = Path(archive), Path(snapshot)
    frozen_raw = (archive / 'manifest.json').read_bytes()
    if hashlib.sha256(frozen_raw).hexdigest() != FROZEN_MANIFEST_SHA256:
        raise ValueError('Frozen manifest hash changed')
    frozen = json.loads(frozen_raw)
    expected = generator.artifacts()
    manifest = json.loads(expected['manifest.json'])
    for name in ARCHIVED_BINDINGS:
        digest = generator.sha((snapshot / (name + '.snapshot')).read_bytes().replace(b'\r\n', b'\n'))
        if digest != frozen['code_canonical_sha256'][name]:
            raise ValueError('Snapshot hash changed: ' + name)
        manifest['code_canonical_sha256'][name] = digest
    expected['manifest.json'] = generator.encode(manifest)
    if {p.name for p in archive.iterdir()} != set(expected):
        raise ValueError('Artifact set changed')
    for name, raw in expected.items():
        if (archive / name).read_bytes() != raw:
            raise ValueError('Artifact changed: ' + name)
    return json.loads(expected['summary.json'])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['verify'])
    parser.parse_args()
    print(json.dumps(verify(), ensure_ascii=False))


if __name__ == '__main__':
    main()
