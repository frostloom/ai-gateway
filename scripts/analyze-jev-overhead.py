"""Read-only decomposition of verified recorded pairs; no network or env access."""
import argparse
import importlib.util
import json
from pathlib import Path

SPEC = importlib.util.spec_from_file_location('jev_efficiency', Path(__file__).with_name('audit-jev-efficiency.py'))
audit = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(audit)


def summarize_turns(journal):
    """Decompose telemetry. Caller must verify source integrity and usage first."""
    turns, groups = [], {}
    for event in journal:
        if event['kind'] != 'finish':
            continue
        for index, observation in enumerate(event['result']['observations']):
            t = observation['turn']['telemetry_raw']
            if isinstance(t, str):
                t = json.loads(t)
            llm, jev = audit.count(t.get('llm_calls')), audit.count(t.get('jev_calls'))
            reason = t.get('jev_fast_path_reason') or 'unknown'
            audit.require(isinstance(reason, str), 'invalid route reason')
            if reason == 'direct_read':
                audit.require(event['arm'] == 'jev_selective' and jev == 1 and llm == 0 and t.get('jev_fast_path') is True, 'inconsistent direct_read')
                route = 'direct_read'
            elif jev and llm:
                route = 'fallback:' + reason
            elif not jev and not llm:
                route = 'no_inference'
            elif llm:
                route = 'model_only'
            else:
                route = 'jev_only:' + reason
            audit.require(not t.get('jev_fast_path') or route == 'direct_read', 'inconsistent direct_read flag')
            metrics = dict(server_latency_ms=t.get('total_latency_ms'))
            for field in ('jev_latency_ms', 'llm_latency_ms', 'tool_latency_ms'):
                metrics[field] = t.get(field)
            for value in metrics.values():
                audit.close(value, value, 'latency')
            phases = sum(metrics[k] for k in ('jev_latency_ms', 'llm_latency_ms', 'tool_latency_ms'))
            audit.require(phases <= metrics['server_latency_ms'] + 1e-6, 'phase latency exceeds server total')
            metrics['other_latency_ms'] = max(0, metrics['server_latency_ms'] - phases)
            audit.require((llm != 0 or metrics['llm_latency_ms'] == 0) and (jev != 0 or metrics['jev_latency_ms'] == 0), 'latency without provider call')
            metrics.update(llm_calls=llm, jev_calls=jev)
            for field in ('llm_input_tokens', 'llm_cache_hit_tokens', 'llm_cache_creation_tokens', 'llm_output_tokens', 'jev_input_tokens', 'jev_output_tokens'):
                metrics[field] = audit.count(t.get(field))
            metrics['llm_billable_input_tokens'] = sum(metrics[k] for k in ('llm_input_tokens', 'llm_cache_hit_tokens', 'llm_cache_creation_tokens'))
            item = dict(key=event['key'], arm=event['arm'], turn_index=index, recorded_action=observation['action'], route=route, reason=reason, **metrics)
            turns.append(item)
            group = groups.setdefault(event['arm'] + '/' + route, dict(turns=0, **{k: 0 for k in metrics}))
            group['turns'] += 1
            for key, value in metrics.items():
                group[key] += value
    return dict(turns=turns, groups=groups)


def analyze_pilot(pilot):
    pilot = Path(pilot)
    verified = audit.audit_pilot(pilot)
    manifest_raw = (pilot / 'manifest.json').read_bytes()
    audit.verify_digest(manifest_raw, verified['source_manifest_sha256'])
    manifest = json.loads(manifest_raw)
    journal_raw = (pilot / 'attempt-01/run.jsonl').read_bytes()
    audit.verify_digest(journal_raw, manifest['files']['attempt-01/run.jsonl'])
    journal = [json.loads(line) for line in journal_raw.decode('utf-8').splitlines()]
    result = summarize_turns(journal)
    result.update(schema='jev-overhead-offline.v1', verified_efficiency=verified,
                  effect_claims_allowed=False, necessary_clarification_success_rate=None,
                  external_model_calls=0, external_jev_calls=0,
                  limitations=[
                      'Four historical task variants only, not 1220-task results or current runtime performance.',
                      'Routes describe execution, not correctness. Recorded unreviewed actions stay unreviewed.',
                      'No-inference means zero observed provider calls, not verified confirmation/cancellation correctness.',
                      'The explicit-scope follow-up is not evidence of necessary missing-parameter clarification success.',
                      'Stage sums are sequential server spans; residual is other server work, not user think time.',
                      'JEV input usage includes provider tokenization; prompt bytes and removable tokens are not recorded.',
                      'Timing differences are observations, not causal effects or measured savings from a proposed optimization.',
                  ])
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('pilot', type=Path)
    args = parser.parse_args()
    print(json.dumps(analyze_pilot(args.pilot), ensure_ascii=False, indent=2, allow_nan=False))
