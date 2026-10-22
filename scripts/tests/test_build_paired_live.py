import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]


class ArchiveTemporaryDirectoryTests(unittest.TestCase):
    def test_uses_platform_temp_directory_and_cleans_up_on_build_failure(self):
        spec = importlib.util.spec_from_file_location('paired_archive_temp', ROOT / 'scripts/build-paired-live.py')
        module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); repo = root / 'repo'; repo.mkdir()
            system_temp = root / 'system-temp'; system_temp.mkdir()
            (repo / 'go.mod').write_text('module github.com/frostloom/ai-gateway\n\ngo 1.25.12\n')
            def git(*args):
                subprocess.run(['git', '-C', str(repo), *args], check=True, capture_output=True)
            git('init', '-q', '-b', 'main'); git('add', 'go.mod')
            git('-c', 'user.name=test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'fixture')
            original_run = subprocess.run
            build_directories = []
            def run(args, **kwargs):
                if args[:2] == ['go', 'build']:
                    build_directories.append(Path(kwargs['cwd']))
                    raise RuntimeError('intentional compiler stop')
                return original_run(args, **kwargs)
            with patch.dict(os.environ, {'TMPDIR': str(system_temp)}), \
                 patch.object(tempfile, 'tempdir', None), \
                 patch.object(module.subprocess, 'run', side_effect=run), \
                 patch('sys.argv', ['build-paired-live', '--repo', str(repo), '--output', str(root / 'binary')]):
                with self.assertRaisesRegex(RuntimeError, 'intentional compiler stop'):
                    module.main()
            self.assertEqual(len(build_directories), 1)
            self.assertEqual(build_directories[0].parent.parent.resolve(), system_temp.resolve())
            self.assertFalse(build_directories[0].parent.exists())
            self.assertEqual(list(system_temp.glob('paired-live-archive-*')), [])
            self.assertFalse((root / 'binary').exists())
            self.assertFalse((root / 'binary.provenance.json').exists())


if __name__ == '__main__':
    unittest.main()
