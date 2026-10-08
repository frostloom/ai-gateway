"""Append byte-exact JEV experiment evidence; never replace an existing archive."""
import argparse
import datetime
import gzip
import hashlib
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def write(path, raw):
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open('xb') as stream:
        stream.write(raw)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', required=True, type=Path)
    parser.add_argument('--out', required=True, type=Path)
    args = parser.parse_args()
    source, output = args.source.resolve(), args.out.resolve()
    if not source.is_relative_to(ROOT / 'eval/reports') or not output.is_relative_to(ROOT / 'eval/experiments'):
        raise ValueError('Use a report source and a new experiment archive inside this repository.')
    if output.exists():
        raise ValueError('Archive already exists; all previous bytes preserved.')
    for arm in ('nojev', 'hint', 'fast'):
        if len(list((source / ('revised-full-' + arm)).glob('intent-*.json'))) != 1:
            raise ValueError('Require one completed full-suite result per arm.')
        if not (source / ('revised-tasks-' + arm) / 'tasks.json').is_file():
            raise ValueError('Require completed task results per arm.')
    for name in ('README.md', 'comparison-offpeak/comparison.json', 'comparison-peak/comparison.json', 'revised-task-comparison/comparison.json'):
        if not (source / name).is_file():
            raise ValueError('Missing reviewed supporting report: ' + name)
    # Read credentials only for a no-export check. Never print matching values.
    credentials = []
    for line in (ROOT / '.env').read_text(encoding='utf-8-sig').splitlines():
        name, sep, value = line.partition('=')
        if sep and name.strip().endswith('API_KEY'):
            value = value.strip().strip('\"\'').encode()
            if value:
                credentials.append(value)
    files = []
    for path in sorted(source.rglob('*')):
        if path.is_file() and path.suffix in ('.json', '.md', '.log', '.patch', '.txt', '.html'):
            raw = path.read_bytes()
            if any(value in raw for value in credentials) or re.search(rb'sk-[a-fA-F0-9]{32}(?![a-fA-F0-9])', raw):
                raise ValueError('Credential check failed; no archive created.')
            files.append((path, raw))
    entries = []
    for path, raw in files:
        relative = path.relative_to(source)
        archive = Path('raw') / (relative.as_posix() + '.gz')
        compressed = gzip.compress(raw, compresslevel=9, mtime=0)
        write(output / archive, compressed)
        entries.append({'source': path.relative_to(ROOT).as_posix(), 'archive': archive.as_posix(),
                        'source_bytes': len(raw), 'archive_bytes': len(compressed),
                        'source_sha256': digest(raw), 'archive_sha256': digest(compressed)})
    write(output / 'README.md', (source / 'README.md').read_bytes())
    write(output / 'verify.py', (ROOT / 'eval/experiments/2026-10-08-deepseek-flash/verify.py').read_bytes())
    for name, destination in [('comparison-offpeak/comparison.json', 'comparison-offpeak.json'),
                               ('comparison-peak/comparison.json', 'comparison-peak.json'),
                               ('revised-task-comparison/comparison.json', 'task-comparison.json'),
                               ('clarification-review.json', 'clarification-review.json'),
                               ('provenance.json', 'provenance.json')]:
        write(output / destination, (source / name).read_bytes())
    write(output / 'revised-provenance.json', (source / 'revised-provenance.json').read_bytes())
    write(output / 'revised-clarification-review.json', (source / 'revised-clarification-review.json').read_bytes())
    write(output / 'matched-fast-observation.json', (source / 'matched-fast-observation.json').read_bytes())
    write(output / 'recovery-comparison.json', (source / 'recovery-comparison/comparison.json').read_bytes())
    supporting = [{'path': p.name, 'sha256': digest(p.read_bytes())} for p in sorted(output.iterdir()) if p.is_file()]
    manifest = {'schema_version': 1, 'archived_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
                'credential_pattern_matches': 0, 'credential_review_scope': 'explicit experiment root; actual configured API keys and DeepSeek key pattern',
                'compression': {'level': 9, 'mtime': 0, 'filename': ''}, 'files': entries, 'supporting_files': supporting}
    write(output / 'manifest.json', (json.dumps(manifest, ensure_ascii=False, indent=2) + '\n').encode())
    print(f'Frozen {len(entries)} byte-exact artifacts; prior evidence unchanged; no API credentials exported.')


if __name__ == '__main__':
    main()
