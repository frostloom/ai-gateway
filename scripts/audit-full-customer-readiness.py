"""Offline full-set coverage and declared budget audit; never authorizes execution."""
import argparse
import hashlib
import importlib.util
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CLAIM_SHA = 'f09de5bb9ab4147f140f54db7a25c5f419b7a4e48137d63c1782ea7c38d768b3'
PRICING = Path('eval/paired-pilots/2026-10-27-receiver-repeated-entry/pricing.json')
PILOT_KEYS = frozenset([
    'balance-001/answer_complete', 'plans-001/answer_complete',
    'bills-001/answer_complete', 'consumption-001/answer_complete',
    'subscription-001/answer_complete', 'balance-020/explicit_scope_followup',
    'recharge-001/supplement_then_confirm', 'recharge-002/confirm',
    'recharge-002/cancel', 'recharge-002/duplicate_confirm',
    'recharge-002/cancel_then_confirm', 'refund-001/balance_cap'])


def require(ok, message):
    if not ok:
        raise ValueError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def audit(root, probe, reservation):
    root = Path(root)
    spec = importlib.util.spec_from_file_location('full_source', root / 'scripts/prepare-full-customer-pilot.py')
    prepare = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(prepare)
    definitions = prepare.load_definitions(root)
    selected = prepare.select_primary(definitions)
    require(len(selected) == 1220, 'Exactly 1220 frozen sources required')
    require(probe.get('schema') == 'full-customer-offline-preflight.v1'
            and probe.get('allow_paid') is False and probe.get('ready_for_paid_run') is False
            and type(probe.get('model_calls')) is int and probe['model_calls'] == 0
            and type(probe.get('jev_calls')) is int and probe['jev_calls'] == 0,
            'Offline preparation without paid or inference claims required')
    require(re.fullmatch('[a-f0-9]{40}', probe.get('source_revision', '')), 'Source revision required')
    for key, expected in [('source_cases', 1220), ('paired_arms', 2440),
                          ('planned_user_turns', 3924), ('boundary_count', 1851)]:
        require(type(probe.get(key)) is int and probe[key] == expected, 'Full denominator mismatch: ' + key)
    for key, expected in [('definition_sha256', sha((root / prepare.ARCHIVE / 'definitions.json').read_bytes())),
                          ('manifest_sha256', prepare.MANIFEST_SHA), ('profile_sha256', prepare.PROFILE_SHA)]:
        require(probe.get(key) == expected, 'Source binding mismatch: ' + key)
    require(sha((root / prepare.PROFILE).read_bytes()) == prepare.PROFILE_SHA, 'Profile bytes changed')
    runtime = probe.get('prepared_runtime')
    require(isinstance(runtime, dict), 'Prepared runtime contract required')
    for key, expected in [('model', 'deepseek-flash'), ('provider_protocol', 'openai'),
                          ('routing_mode', 'model'), ('evidence_kind', 'recorded_run'),
                          ('output_budget', 128), ('thinking_mode', 'disabled'),
                          ('definition_sha256', probe['definition_sha256']),
                          ('manifest_sha256', probe['manifest_sha256']),
                          ('execution_revision', probe['source_revision']), ('profile_sha256', None)]:
        require(type(runtime.get(key)) is type(expected) and runtime.get(key) == expected,
                'Prepared runtime differs from budget/comparison contract: ' + key)
    for key in ['prompt_sha256', 'tool_schema_sha256']:
        require(re.fullmatch('[a-f0-9]{64}', runtime.get(key, '')), 'Prepared runtime digest required: ' + key)
    rows = probe.get('rows')
    require(isinstance(rows, list) and len(rows) == 1220, 'Full per-case coverage required')
    cases, covered = [], 0
    for observed, (source, scenario) in zip(rows, selected):
        key = source['source_case_id'] + '/' + scenario['id']
        expected = {'key': key, 'source_case_id': source['source_case_id'],
                    'profile': scenario['profile'], 'scenario': scenario['id'],
                    'turns': len(scenario['turns'])}
        require(all(observed.get(k) == v and type(observed.get(k)) is type(v) for k, v in expected.items()),
                'Changed, missing, duplicate or reordered primary task: ' + key)
        member = key in PILOT_KEYS
        require(observed.get('configuration_prepared') is True
                and observed.get('pilot_whitelist_member') is member
                and re.fullmatch('[a-f0-9]{64}', observed.get('pair_run_contract', '')),
                'Configuration and pilot-whitelist coverage must be distinguished')
        covered += int(member)
        cases.append({**expected, 'pair_run_contract': observed['pair_run_contract'],
                      'configuration_prepared': True, 'pilot_whitelist_member': member,
                      'full_case_handler_fixture_verified': False,
                      'full_case_independent_semantic_review': 'not_performed',
                      'source_definition_status': source['definition_status'],
                      'source_annotation_status': source['annotation_status']})
    require(type(probe.get('pilot_whitelist_primary_count')) is int
            and probe['pilot_whitelist_primary_count'] == covered, 'Pilot coverage total mismatch')
    # This audit is intentionally bound to the latest consumed claim. A later
    # budget chain requires a new explicit snapshot, not a lower usage balance.
    for key, expected in [('schema', 'paired-live-receiver-repeated-claim.v1'),
                          ('campaign_id', 'v5-repeated-receiver-20261027'),
                          ('status', 'consumed_before_execution'),
                          ('cumulative_reserved_model_micro_cny', 29061120),
                          ('cumulative_reserved_jev_micro_usd', 223104)]:
        require(type(reservation.get(key)) is type(expected) and reservation[key] == expected,
                'Consumed reservation snapshot differs: ' + key)
    price_raw = (root / PRICING).read_bytes()
    price_manifest = json.loads((root / PRICING.parent / 'manifest.json').read_bytes())
    require(price_manifest['files'].get(PRICING.as_posix()) == sha(price_raw), 'Archived pricing binding differs')
    price = json.loads(price_raw)
    model, jev = price['deepseek'], price['typesafe']
    model_per_attempt = (model['max_input'] * model['peak_input_micro_cny_per_million']
                         + model['max_output'] * model['peak_output_micro_cny_per_million'] + 999999) // 1000000
    jev_per_attempt = (jev['max_request_input'] * jev['input_micro_usd_per_million'] + 999999) // 1000000
    require(model_per_attempt == model['per_attempt_reserved_micro_cny'] == 33792
            and jev_per_attempt == jev['per_attempt_reserved_micro_usd'] == 2688,
            'Dated declared reservation envelope differs')
    turns = sum(c['turns'] for c in cases)
    remaining_cny = 100000000 - reservation['cumulative_reserved_model_micro_cny']
    remaining_usd = 1000000 - reservation['cumulative_reserved_jev_micro_usd']

    def ceiling(passes):
        model_calls, jev_calls = turns * 2 * 5 * passes, turns * passes
        cny, usd = model_calls * model_per_attempt, jev_calls * jev_per_attempt
        return {'passes': passes, 'model_attempts': model_calls, 'jev_attempts': jev_calls,
                'model_micro_cny': cny, 'jev_micro_usd': usd,
                'fits_existing_remaining_ceiling': cny <= remaining_cny and usd <= remaining_usd}

    return {'schema': 'full-customer-readiness-audit.v1', 'source_cases': 1220,
            'paired_arms_per_pass': 2440, 'planned_turns_per_pass': 2 * turns,
            'boundary_scenarios_separate': 1851, 'source_revision_declared': probe['source_revision'],
            'prepared_runtime': dict(runtime),
            'pilot_whitelist_primary_count': covered, 'not_in_legacy_pilot_whitelist': 1220 - covered,
            'cases': cases, 'allow_paid': False, 'ready_for_paid_run': False,
            'full1220_evaluation_complete': False, 'model_calls': 0, 'jev_calls': 0,
            'budget': {'pricing_evidence_date': price['checked_client_date'],
                       'fresh_tariff_check': False, 'pricing_sha256': sha(price_raw),
                       'reservation_snapshot_sha256': CLAIM_SHA,
                       'remaining_micro_cny': remaining_cny, 'remaining_micro_usd': remaining_usd,
                       'one_pass': ceiling(1), 'minimum_three_passes': ceiling(3),
                       'actual_invoice_or_expected_spend': False,
                       'full_request_envelope_verified': False,
                       'permanent_reservations_released': False},
            'blockers': ['Full-set execution is not accepted by the current pilot whitelist.',
                         'Per-case real Handler/fixture/scoring acceptance across the full set is unverified.',
                         'Full-set semantic standards and recorded-answer independent review remain required.',
                         'Full-set request envelope and new scope authorization/claims are not frozen.',
                         'Current conservative one- and three-pass envelopes exceed the remaining approved budget.',
                         'Full execution schedule, 300-second windows, and quality-failure versus infrastructure/safety-stop policy need explicit binding.'],
            'limitations': ['Configuration preparation is not execution, task success, independent gold or provider validation.',
                            'Dated conservative envelopes are neither invoices nor a forecast of actual spend.',
                            'Three interleaved passes are required by the evaluation contract, not proved by repeated six-case samples.',
                            'No new paid scope, claim, credential lookup, database or network operation.']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--probe', type=Path, required=True)
    parser.add_argument('--reservation', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    reservation_raw = args.reservation.read_bytes()
    require(sha(reservation_raw) == CLAIM_SHA, 'Explicit latest original consumed claim required')
    probe_raw = args.probe.read_bytes()
    result = audit(ROOT, json.loads(probe_raw), json.loads(reservation_raw))
    result['probe_sha256'] = sha(probe_raw)
    args.out.mkdir(exist_ok=False)
    (args.out / 'audit.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
    print(f"Offline: 1220 prepared configurations; {result['pilot_whitelist_primary_count']} primary tasks in legacy pilot whitelist; full evaluation/paid readiness false.")


if __name__ == '__main__':
    main()
