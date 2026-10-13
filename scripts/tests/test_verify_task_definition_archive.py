import importlib.util
import json
import hashlib
from pathlib import Path
import tempfile
import shutil
import unittest

ROOT = Path(__file__).resolve().parents[2]

class ArchiveVerificationTests(unittest.TestCase):
    def load(self):
        spec = importlib.util.spec_from_file_location('archive_verifier', ROOT / 'scripts/verify-task-definition-archive.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def test_frozen_archive_survives_runtime_changes(self):
        self.assertEqual(self.load().verify()['source_cases'], 1220)

    def test_snapshot_tampering_is_rejected(self):
        module = self.load()
        with tempfile.TemporaryDirectory() as tmp:
            snapshot = Path(tmp) / 'snapshot'
            shutil.copytree(module.SNAPSHOT, snapshot)
            (snapshot / 'internal/agent/tools.go.snapshot').write_bytes(b'changed')
            with self.assertRaisesRegex(ValueError, 'Snapshot hash'):
                module.verify(snapshot=snapshot)

    def test_archive_tampering_is_rejected(self):
        module = self.load()
        with tempfile.TemporaryDirectory() as tmp:
            archive = Path(tmp) / 'archive'
            shutil.copytree(module.ARCHIVE, archive)
            (archive / 'summary.json').write_bytes(b'{}')
            with self.assertRaisesRegex(ValueError, 'Artifact changed'):
                module.verify(archive=archive)

    def test_coordinated_snapshot_and_manifest_change_is_rejected(self):
        module = self.load()
        with tempfile.TemporaryDirectory() as tmp:
            archive, snapshot = Path(tmp) / 'archive', Path(tmp) / 'snapshot'
            shutil.copytree(module.ARCHIVE, archive)
            shutil.copytree(module.SNAPSHOT, snapshot)
            name = 'internal/agent/tools.go'
            changed = b'changed runtime code'
            (snapshot / (name + '.snapshot')).write_bytes(changed)
            manifest = json.loads((archive / 'manifest.json').read_bytes())
            manifest['code_canonical_sha256'][name] = hashlib.sha256(changed).hexdigest()
            (archive / 'manifest.json').write_bytes((json.dumps(manifest, ensure_ascii=False, indent=2, sort_keys=True) + '\n').encode('utf-8'))
            with self.assertRaisesRegex(ValueError, 'Frozen manifest hash'):
                module.verify(archive=archive, snapshot=snapshot)
