"""Offline preparation only. No credentials, database, HTTP or execution entry point."""
import argparse
import hashlib
import json
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ARCHIVE = Path('eval/task-definitions/2026-10-12-full-dev-v1')
PROFILE = Path('eval/runtime-profiles/2026-10-13-six-profiles-v1/profiles.json')
OUT = ROOT / 'eval/paired-pilots/2026-10-14-full-preflight-v1'
MANIFEST_SHA = 'afaf5fba49893d2c20e91dc8b0e84c998608b82194d6613a6b6549e289ac4181'
PROFILE_SHA = '83a78d7492c75e0d153cbb9ca4a1a87aa2f15714ad1e855534a084f4848e257d'
PRIORITY = ('answer_complete', 'explicit_scope_followup', 'supplement_then_confirm', 'confirm', 'no_subscription')
# Existing paired runner: <=12 cases; budget: <=256 requests; Chat: <=5
# iterations including reply repair. Evaluation Anthropic never retries Chat.
RUNTIME_FILES = ('internal/agent/chat.go', 'internal/agent/guard.go', 'internal/agent/fast_path.go',
                 'internal/agent/llm.go', 'internal/agent/llm_protocol.go', 'internal/agent/prompt.go',
                 'internal/agent/tools.go', 'internal/agent/policy.go', 'internal/agent/runtime_metadata.go',
                 'internal/agent/reply_boundary.go', 'internal/jev/jev.go', 'internal/jev/failure.go',
                'internal/taskbench/paired_adapter.go', 'internal/taskbench/paired_jev_budget.go',
                'internal/taskbench/paired_budget.go', 'internal/taskbench/paired_runner.go',
                'internal/taskbench/campaign_budget.go', 'internal/taskbench/campaign_runner.go',
                'internal/taskbench/paired_oracle.go', 'internal/taskbench/conversation_comparison.go')


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def encoded(value):
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + '\n').encode('utf-8')


def load_definitions(root):
    manifest_raw = (root / ARCHIVE / 'manifest.json').read_bytes()
    if sha(manifest_raw) != MANIFEST_SHA:
        raise ValueError('Frozen definition manifest changed')
    manifest = json.loads(manifest_raw)
    for name, digest in manifest['files'].items():
        if Path(name).name != name or sha((root / ARCHIVE / name).read_bytes()) != digest:
            raise ValueError('Frozen definition changed: ' + name)
    return json.loads((root / ARCHIVE / 'definitions.json').read_bytes())


def select_primary(rows):
    selected = []
    seen = set()
    for row in rows:
        source = row['source_case_id']
        if source in seen:
            raise ValueError('Duplicate source: ' + source)
        seen.add(source)
        eligible = {s['id']: s for s in row['scenarios'] if s['full_task_metric_eligible'] and s['measurement_scope'] == 'complete_task'}
        scenario = next((eligible[k] for k in PRIORITY if k in eligible), None)
        if scenario is None:
            raise ValueError('No primary complete task: ' + source)
        selected.append((row, scenario))
    return selected


def prepare(root):
    rows = load_definitions(root)
    if len(rows) != 1220 or sha((root / PROFILE).read_bytes()) != PROFILE_SHA:
        raise ValueError('1220 sources and pinned runtime profiles required')
    primary, boundaries, batches = [], [], []
    batch = {'keys': [], 'model_attempt_ceiling': 0, 'jev_attempt_ceiling': 0}
    for row, scenario in select_primary(rows):
        key = row['source_case_id'] + '/' + scenario['id']
        turns = len(scenario['turns'])
        item = {'key': key, 'source_case_id': row['source_case_id'], 'profile': scenario['profile'],
                'partition': row['partition'], 'text_group': row['text_group'], 'turns': turns,
                'measurement_scope': 'complete_task', 'scenario_sha256': sha(encoded(scenario)),
                'answer_assertions_sha256': sha(encoded(row['answer_assertions'])),
                'expected_actions': [t['expected_actions'] for t in scenario['turns']],
                'allowed_tools': [t.get('allowed_tools') for t in scenario['turns']],
                'answer_review_status': 'pending_independent_review'}
        primary.append(item)
        for other in row['scenarios']:
            if other['full_task_metric_eligible'] and other['id'] != scenario['id']:
                boundaries.append(row['source_case_id'] + '/' + other['id'])
        # Both arms may fall back on every turn; do not budget hypothetical JEV savings.
        requests = 2 * 5 * turns
        if batch['keys'] and (len(batch['keys']) == 12 or batch['model_attempt_ceiling'] + requests > 256):
            batches.append(batch)
            batch = {'keys': [], 'model_attempt_ceiling': 0, 'jev_attempt_ceiling': 0}
        batch['keys'].append(key)
        batch['model_attempt_ceiling'] += requests
        batch['jev_attempt_ceiling'] += turns
    if batch['keys']:
        batches.append(batch)
    for i, batch in enumerate(batches):
        batch['id'] = f'batch-{i+1:03d}'
        batch['paired_arms'] = 2 * len(batch['keys'])
        batch['model_reservation_micro_cny'] = batch['model_attempt_ceiling'] * 33792
    return {'schema': 'full-customer-pilot-preflight.v1', 'source_cases': len(primary),
            'paired_arms': 2 * len(primary), 'primary': primary, 'boundary_scenarios': boundaries,
            'selection_priority': list(PRIORITY), 'batches': batches,
            'scenario_counts': dict(Counter(x['key'].split('/')[1] for x in primary)),
            'partition_counts': dict(Counter(x['partition'] for x in primary)),
            'source_bindings': {'manifest_sha256': MANIFEST_SHA, 'profile_sha256': PROFILE_SHA,
                'definition_sha256': sha((root / ARCHIVE / 'definitions.json').read_bytes()),
                'runtime_hash_normalization': 'CRLF to LF; no other normalization',
                'scenario_hash_encoding': 'UTF8 JSON sorted keys, indent 2, final LF',
                'runtime_files_sha256': {p: sha((root / p).read_bytes().replace(b'\r\n', b'\n')) for p in RUNTIME_FILES}},
            'model_attempt_ceiling': sum(b['model_attempt_ceiling'] for b in batches),
            'jev_attempt_ceiling': sum(b['jev_attempt_ceiling'] for b in batches),
            'model_reservation_micro_cny': sum(b['model_reservation_micro_cny'] for b in batches),
            'declared_model_envelope': {'input_token_ceiling': 16384, 'max_request_bytes': 16384,
                'max_output_tokens': 128, 'thinking': 'disabled', 'input_cny_per_million': 2,
                'output_cny_per_million': 8, 'price_evidence': '2026-10-14-four-task-v1/public-price-evidence.json'},
            'provider_proposal': {'model': 'deepseek-v4.1-flash', 'protocol': 'anthropic',
                'base_url': 'https://maas.qianwenaiapi.com/apps/anthropic', 'jev_model': 'jev-1.13.0',
                'credentials_discovered': False, 'live_verified_after_repair': False},
            'ready_for_paid_run': False, 'authorized_micro_cny': None, 'model_calls': 0, 'jev_calls': 0,
            'account_invoice_known': False,
            'campaign_guard': {'implemented': True, 'batch_reservation_permanent': True,
                'currencies_separate': True, 'unfinished_or_failed_resume_stops': True,
                'cli_paid_entry_point': False, 'operator_limits_required': True},
            'blockers': ['Real JEV inference and fixed-budget model pilot not yet verified after repair.',
                'Independent semantic review required; development primary selection is not blind gold.',
                'Fresh channel/account rates, separate JEV USD cap and user budget authorization required.'],
            'metrics': ['complete task structural verdict AND independent answer verdict',
                'necessary clarification counted correct when required', 'preview/confirm/cancel and exact final write state',
                'full-denominator failures, unrun and unknown reviews reported separately',
                'paired server response time and end-to-end wall time separately',
                'actual upstream attempts and provider/cache tokens; local rejection excluded from charged attempts',
                'CNY model and USD JEV priced usage separately; actual account invoice unknown',
                'JEV eligible, confident direct, fallback and failure strata; no causality claim without valid paired coverage']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=('prepare', 'verify'))
    args = parser.parse_args()
    raw = encoded(prepare(ROOT))
    if args.command == 'prepare':
        OUT.mkdir(exist_ok=False)
        (OUT / 'proposal.json').write_bytes(raw)
        (OUT / 'manifest.json').write_bytes(encoded({'schema': 'full-pilot-preparation-manifest.v1',
            'generator_sha256': sha(Path(__file__).read_bytes()), 'files': {'proposal.json': sha(raw)}}))
    manifest = json.loads((OUT / 'manifest.json').read_bytes())
    if manifest['generator_sha256'] != sha(Path(__file__).read_bytes()) or manifest['files'] != {'proposal.json': sha(raw)} or (OUT / 'proposal.json').read_bytes() != raw:
        raise ValueError('Preparation changed; new version/directory required, do not overwrite old archive')
    print('PASS: 1220 primary tasks / 2440 arms; separate 1851 boundary scenarios; offline only, paid readiness false.')


if __name__ == '__main__':
    main()
