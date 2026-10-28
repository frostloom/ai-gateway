"""Pure, explicitly SHA-anchored full1220 window score plans.

Task1 is pure. Task2 accepts explicit frozen paths, runs only a pinned offline
helper in an isolated source copy, and validates traces through a supplied
read-only client. There is no credential loader, network client or publication
entry point. Neither a plan nor current content validation authorizes scores.

Records are original Go encoding/json snapshots. Context reconstruction takes
nested JSON fragments from those externally anchored bytes, preserving Go's
struct order, sorted maps, float lexemes, nulls and omission rules. It assembles
the *actual* capitalized Go context struct and compares every recomputed digest;
it never treats review verification flags or recorded context digests as proof.
"""
import hashlib
import json
import math
import re
import os
import stat
import subprocess
from pathlib import Path


NAMESPACE = 'full1220.v1.'
_ACTION = "Independently verify observed action label {action}, allowed actions and reply facts against this turn's definition and authoritative state; no fabricated claims."
_FINAL = 'Final reply accurately describes the task outcome, amounts, identities, scope and limitations using the verified final state; no false success claim.'
_HEX64 = re.compile(r'[0-9a-f]{64}\Z')
_HEX40 = re.compile(r'[0-9a-f]{40}\Z')
_TRACE = re.compile(r'[0-9a-f]{32}\Z')
_JSON_SPACE = re.compile(r'("(?:\\.|[^"\\])*")|\s+')
_CAMPAIGN = {'pairs': 3660, 'arms': 7320, 'defined_turns': 11772, 'boundaries_excluded': 1851}


def _sha(raw):
    return hashlib.sha256(raw).hexdigest()


def _require(condition, message):
    if not condition:
        raise ValueError(message)


def _equal(actual, expected, message):
    # JSON equality is type-sensitive: bool is not an integer counter.
    _require(json.dumps(actual, sort_keys=True, ensure_ascii=False, allow_nan=False)
             == json.dumps(expected, sort_keys=True, ensure_ascii=False, allow_nan=False), message)


def _string(value):
    _require(type(value) is str, 'Go context string required')
    return (json.dumps(value, ensure_ascii=False, separators=(',', ':'))
            .replace('<', '\\u003c').replace('>', '\\u003e').replace('&', '\\u0026')
            .replace('\u2028', '\\u2028').replace('\u2029', '\\u2029'))


def _hex(value, expression, message):
    _require(type(value) is str and expression.fullmatch(value) is not None, message)
    return value


def _integer(value, minimum, message):
    _require(type(value) is int and value >= minimum, message)
    return value


class _Document:
    """Strict JSON document plus token spans, without lossy reserialization."""
    def __init__(self, raw, limit=16 << 20):
        _require(type(raw) is bytes and 0 < len(raw) <= limit, 'Bounded original JSON bytes required')
        self.text = raw.decode('utf-8', errors='strict')
        self.spans = {}
        self.decoder = json.JSONDecoder(parse_constant=self._invalid_constant, parse_float=self._float)
        self.value, end = self._parse(0, ())
        _require(not self.text[end:].strip(), 'Trailing JSON data')
        _require(type(self.value) is dict, 'JSON object required')

    @staticmethod
    def _invalid_constant(value):
        raise ValueError('Nonfinite JSON number: ' + value)

    @staticmethod
    def _float(value):
        result = float(value)
        _require(math.isfinite(result), 'Nonfinite JSON number')
        return result

    def _ws(self, index):
        while index < len(self.text) and self.text[index] in ' \t\r\n':
            index += 1
        return index

    def _parse(self, index, path):
        index = self._ws(index)
        _require(index < len(self.text) and len(path) <= 80, 'Invalid or excessively nested JSON')
        start, char = index, self.text[index]
        if char == '{':
            value = {}; index = self._ws(index + 1)
            while index < len(self.text) and self.text[index] != '}':
                key, index = self.decoder.raw_decode(self.text, index)
                _require(type(key) is str and key not in value, 'Duplicate or invalid JSON key')
                index = self._ws(index)
                _require(index < len(self.text) and self.text[index] == ':', 'Missing JSON colon')
                value[key], index = self._parse(index + 1, path + (key,))
                index = self._ws(index)
                if index >= len(self.text) or self.text[index] != ',':
                    break
                index = self._ws(index + 1)
                _require(index < len(self.text) and self.text[index] != '}', 'Trailing JSON comma')
            _require(index < len(self.text) and self.text[index] == '}', 'Missing JSON object end')
            index += 1
        elif char == '[':
            value = []; index = self._ws(index + 1)
            while index < len(self.text) and self.text[index] != ']':
                child, index = self._parse(index, path + (len(value),)); value.append(child)
                index = self._ws(index)
                if index >= len(self.text) or self.text[index] != ',':
                    break
                index = self._ws(index + 1)
                _require(index < len(self.text) and self.text[index] != ']', 'Trailing JSON comma')
            _require(index < len(self.text) and self.text[index] == ']', 'Missing JSON array end')
            index += 1
        else:
            value, index = self.decoder.raw_decode(self.text, index)
        self.spans[path] = (start, index)
        return value, index

    def fragment(self, *path):
        start, end = self.spans[path]
        return _JSON_SPACE.sub(lambda match: match[1] or '', self.text[start:end])


def _index(rows, key, description):
    _require(type(rows) is list, description + ' list required')
    result = {}
    for row in rows:
        _require(type(row) is dict, description + ' object required')
        identity = key(row)
        _require(identity not in result, 'Duplicate ' + description)
        result[identity] = row
    return result


def _identity(row):
    _integer(row['ordinal'], 1, 'Integer ordinal identity required')
    _integer(row['pass'], 1, 'Integer pass identity required')
    _require(type(row['case_key']) is str and row['case_key']
             and row['arm'] in ('model', 'jev_selective'), 'Case and arm identity required')
    return (row['ordinal'], row['pass'], row['case_key'], row['arm'])


def _requirements(document):
    """Re-derive the complete Go requirement list from source and observations."""
    result = document.value['result']; case = result['case']; key = case['source_case_id'] + '/' + case['scenario']['id']
    observations = result['observations'] or []
    turns = case['scenario']['turns']
    _require(type(turns) is list and type(observations) is list and len(observations) <= len(turns),
             'Observed turn/source definition mismatch')
    derived = []
    for index, observation in enumerate(observations):
        turn = observation['turn']; response = turn['response']; base = ('result', 'observations', index)
        assertions = [{'id': f'{key}/turn-{index+1}/action-reply',
                       'requirement': _ACTION.format(action=observation['action'])}]
        if index == 0:
            assertions.extend(case['answer_assertions'] or [])
        if turns[index].get('answer'):
            assertions.append({'id': f'{key}/turn-{index+1}/answer', 'requirement': document.fragment(
                'result', 'case', 'scenario', 'turns', index, 'answer')})
        if index == len(turns)-1 and case['scenario']['full_task_metric_eligible']:
            assertions.append({'id': key + '/final-reply', 'requirement': _FINAL})
        for assertion in assertions:
            fields = [('Case', document.fragment('result', 'case')),
                      ('Fixture', document.fragment('result', 'fixture')),
                      ('Initial', document.fragment('result', 'initial')),
                      ('TurnIndex', str(index)), ('State', document.fragment(*base, 'state')),
                      ('Trace', document.fragment(*base, 'turn', 'response', 'trace')),
                      ('Pending', document.fragment(*base, 'pending') if 'pending' in observation else 'null'),
                      ('Message', _string(turn['message'])), ('Requirement', _string(assertion['requirement']))]
            context = '{' + ','.join(_string(k) + ':' + v for k, v in fields) + '}'
            derived.append({'id': assertion['id'], 'turn_index': index, 'requirement': assertion['requirement'],
                            'requirement_sha256': _sha(context.encode('utf-8')),
                            'reply_sha256': _sha(response['reply'].encode('utf-8'))})
    _index(derived, lambda row: (row['id'], row['turn_index']), 'derived requirement')
    return derived


def _aggregate(verdicts):
    if 'failed' in verdicts:
        return 'failed'
    if not verdicts or 'not_assessable' in verdicts:
        return 'not_assessable'
    return 'passed'


def _validate_observed_evidence(review, result):
    """Optional legacy evidence projections, when supplied, must match originals."""
    if 'terminal_evidence' in review:
        terminal = review['terminal_evidence']
        _require(type(terminal) is dict, 'Terminal evidence object required')
        expected = {'initial_balance_cent': result['initial']['own']['balance_cent'],
                    'final_balance_cent': result['final']['own']['balance_cent'],
                    'final_ledger_balance': result['final']['own']['ledger_balance'],
                    'final_consistent': result['final']['own']['consistent'],
                    'controls_unchanged': result['initial']['controls'] == result['final']['controls']}
        for key, value in expected.items():
            if key in terminal:
                _equal(terminal[key], value, 'Review terminal evidence mismatch: ' + key)
    if 'observed_evidence' not in review:
        return
    observed = result['observations'] or []
    evidence = _index(review['observed_evidence'], lambda item: item['turn_index'], 'observed evidence')
    _equal(sorted(evidence), list(range(len(observed))), 'Incomplete observed evidence')
    for index, observation in enumerate(observed):
        item = evidence[index]; turn = observation['turn']; response = turn['response']
        expected = {'message': turn['message'], 'reply': response['reply'],
                    'reply_sha256': _sha(response['reply'].encode()),
                    'expected_actions': result['case']['scenario']['turns'][index]['expected_actions'],
                    'allowed_tools': result['case']['scenario']['turns'][index]['allowed_tools'],
                    'observed_action_label': observation['action'], 'tool_trace': response['trace'],
                    'pending_confirm': response['pending_confirm'], 'pending_tool': response['pending_tool'],
                    'pending_preview': response['pending_preview'],
                    'oracle_balance_cent': observation['state']['own']['balance_cent'],
                    'oracle_ledger_balance': observation['state']['own']['ledger_balance'],
                    'oracle_consistent': observation['state']['own']['consistent']}
        for key, value in expected.items():
            if key in item:
                if key == 'tool_trace':
                    _require(item[key] is None or type(item[key]) is list, 'Review tool trace must be null or list')
                    _equal(item[key] or [], value or [], 'Review observed evidence mismatch: ' + key)
                else:
                    _equal(item[key], value, 'Review observed evidence mismatch: ' + key)


def _planned_rows(manifest, receipt):
    scope = manifest['scope']; slots = scope['slots']; windows = scope['windows']
    _require(type(slots) is list and len(slots) == _CAMPAIGN['pairs'], 'Full original slot denominator required')
    _equal([slot['ordinal'] for slot in slots], list(range(1, 3661)), 'Full original ordinal order required')
    _equal(sum(_integer(slot['user_turns'], 1, 'Positive defined turn count required') * 2 for slot in slots),
           _CAMPAIGN['defined_turns'], 'Full original turn denominator required')
    window_index = _integer(receipt['window_index'], 1, 'Positive window index required')
    _require(type(windows) is list and window_index <= len(windows), 'Unknown window index')
    window = windows[window_index-1]
    _equal(receipt['window_id'], window['id'], 'Wrong window identity')
    selected = [slot for slot in slots if slot['window_id'] == window['id']]
    _require(selected and selected[0]['ordinal'] == window['first_ordinal']
             and selected[-1]['ordinal'] == window['last_ordinal'], 'Window scope range mismatch')
    identities = []
    for slot in selected:
        _integer(slot['pass'], 1, 'Positive pass required')
        _require(slot['pass'] <= 3 and type(slot['case_key']) is str and slot['case_key'], 'Invalid pass/case identity')
        _require(slot['arm_order'] in (['model', 'jev_selective'], ['jev_selective', 'model']), 'Exact dual arm order required')
        for arm in slot['arm_order']:
            identities.append({'ordinal': slot['ordinal'], 'pass': slot['pass'], 'case_key': slot['case_key'],
                               'arm': arm, 'defined_turns': slot['user_turns']})
    _require(type(receipt['rows']) is list and len(receipt['rows']) == len(identities), 'Complete planned window rows required')
    for row, expected in zip(receipt['rows'], identities):
        for key, value in expected.items():
            _equal(row[key], value, 'Receipt/scope identity mismatch: ' + key)
        for key in ('attempted', 'finished', 'cleanup', 'observer', 'structural_pass', 'success'):
            _require(type(row[key]) is bool, 'Boolean receipt state required: ' + key)
        _integer(row['observed_turns'], 0, 'Observed turn count required')
        _require(row['observed_turns'] <= row['defined_turns'] and (not row['finished'] or row['attempted']),
                 'Impossible attempted/finished/observed state')
    for key, expected in [('planned_campaign_pairs', 3660), ('planned_campaign_arms', 7320),
                          ('planned_campaign_turns', 11772)]:
        _equal(receipt[key], expected, 'Campaign receipt denominator mismatch: ' + key)
    quality_rows = []
    for row in receipt['rows']:
        quality = row.get('quality_end')
        if quality is not None:
            _require(type(quality) is dict and row['attempted'] and row['finished']
                     and row['disposition'] == 'halt' and row['status'] == 'failed'
                     and row.get('exclude_complete_task_comparison') is True,
                     'Quality-end original failure and pair exclusion required')
            _equal(quality['observed_turns'], row['observed_turns'], 'Quality observed turn mismatch')
            _equal(quality['unrun_turns'], row['defined_turns']-row['observed_turns'], 'Quality suffix turn mismatch')
            for key in ('policy_sha256', 'source_sha256', 'rule_sha256', 'candidate_sha256', 'cleanup_sha256', 'envelope_sha256'):
                _hex(quality[key], _HEX64, 'Quality evidence digest required: ' + key)
            for key in ('candidate_file', 'cleanup_file'):
                _require(type(quality[key]) is str and re.fullmatch(r'[A-Za-z0-9_.-]+', quality[key]) is not None
                         and quality[key] not in ('.', '..'), 'Safe quality evidence filename required')
            _require(quality['candidate_file'] != quality['cleanup_file'], 'Distinct quality evidence files required')
            quality_rows.append(row)
        elif row['finished']:
            _equal(row['observed_turns'], row['defined_turns'], 'Finished arm requires full observations or explicit quality suffix')
    quality_suffix = sum(row['quality_end']['unrun_turns'] for row in quality_rows)
    for key, expected in [('quality_end_arms', len(quality_rows)), ('quality_end_unrun_turns', quality_suffix),
                          ('global_unrun_turns', sum(row['defined_turns']-row['observed_turns'] for row in receipt['rows'])-quality_suffix)]:
        if key in receipt:
            _equal(receipt[key], expected, 'Explicit window suffix denominator mismatch: ' + key)
        elif quality_rows:
            _require(False, 'Quality-end requires explicit window suffix denominator: ' + key)
    for key, expected in [('planned_current_pairs', len(selected)), ('planned_current_arms', len(identities)),
                          ('planned_current_turns', sum(slot['user_turns']*2 for slot in selected)),
                          ('attempted_arms', sum(row['attempted'] for row in receipt['rows'])),
                          ('finished_arms', sum(row['finished'] for row in receipt['rows'])),
                          ('unrun_arms', sum(not row['attempted'] for row in receipt['rows'])),
                          ('observed_turns', sum(row['observed_turns'] for row in receipt['rows']))]:
        _equal(receipt[key], expected, 'Receipt denominator mismatch: ' + key)
    return identities


def _validate_summary(summary, receipt, receipt_sha, manifest):
    _equal(summary['evidence_kind'], 'recorded_run', 'Recorded summary required')
    for key, expected in [('planned_pairs', 3660), ('planned_arms', 7320), ('defined_turns', 11772),
                          ('boundaries_excluded', 1851), ('window_index', receipt['window_index']),
                          ('report_sha256', receipt_sha), ('report_status', receipt['status']),
                          ('ledger_sha256', receipt['ledger_sha256']),
                          ('payload_manifest_sha256', receipt['payload_manifest_sha256'])]:
        _equal(summary[key], expected, 'Summary binding mismatch: ' + key)
    attempted = _integer(summary['attempted_arms'], 0, 'Cumulative attempted arms required')
    finished = _integer(summary['finished_arms'], 0, 'Cumulative finished arms required')
    _require(receipt['attempted_arms'] <= attempted <= 7320 and receipt['finished_arms'] <= finished <= attempted,
             'Invalid cumulative acquisition counts')
    _equal(summary['unattempted_arms'], 7320-attempted, 'Cumulative unattempted denominator mismatch')
    _equal(summary['failed_attempted_arms'], attempted-finished, 'Cumulative failed attempted mismatch')
    prior_slots = [slot for slot in manifest['scope']['slots'] if slot['ordinal'] < receipt['rows'][0]['ordinal']]
    _equal(attempted, len(prior_slots)*2+receipt['attempted_arms'], 'Cumulative attempts contradict completed prefix and current window')
    _equal(finished, len(prior_slots)*2+receipt['finished_arms'], 'Cumulative finished arms contradict completed prefix and current window')
    observed = _integer(summary['observed_turns'], 0, 'Cumulative observed turns required')
    quality = _integer(summary['quality_end_unrun_turns'], 0, 'Quality-end suffix count required')
    unrun = _integer(summary['global_unrun_turns'], 0, 'Global unrun turns required')
    _require(observed >= receipt['observed_turns'], 'Cumulative observed turns below window')
    _equal(observed+quality+unrun, 11772, 'Defined/observed/quality-end/global suffix partition mismatch')
    current_quality = sum((row.get('quality_end') or {}).get('unrun_turns', 0) for row in receipt['rows'])
    _equal(observed+quality, sum(slot['user_turns']*2 for slot in prior_slots)+receipt['observed_turns']+current_quality,
           'Cumulative turns contradict completed prefix and current observed/quality suffix')
    _require(quality >= current_quality, 'Cumulative quality suffix below current window')
    if not prior_slots:
        _equal(observed, receipt['observed_turns'], 'First-window cumulative observed turns must equal current observations')
        _equal(quality, current_quality, 'First-window cumulative quality suffix must equal current suffix')
    # An exact current receipt cannot establish the observed/quality split of
    # earlier windows. Preserve the claimed components and require replay of
    # their original receipts before any consumer treats the prefix as proved.
    return {'status': 'prior_receipt_chain_pending' if prior_slots else 'exact_current_window',
            'current_window_partition_verified': True,
            'prior_partition_verified': not prior_slots,
            'current_window_observed_turns': receipt['observed_turns'],
            'current_window_quality_end_unrun_turns': current_quality,
            'claimed_prior_observed_turns': observed-receipt['observed_turns'],
            'claimed_prior_quality_end_unrun_turns': quality-current_quality}


def _build(manifest_raw, summary_raw, receipt_raw, records, review_raw, expected_hashes):
    _require(type(expected_hashes) is dict and type(records) is dict, 'Explicit artifact and record SHA anchors required')
    raw_inputs = {'manifest': manifest_raw, 'summary': summary_raw, 'receipt': receipt_raw, 'review': review_raw}
    documents = {}
    for name, raw in raw_inputs.items():
        expected = _hex(expected_hashes[name+'_sha256'], _HEX64, 'Explicit SHA anchor required: ' + name)
        _require(type(raw) is bytes and _sha(raw) == expected, 'Artifact SHA mismatch: ' + name)
        documents[name] = _Document(raw)
    manifest, summary, receipt, review = (documents[name].value for name in ('manifest', 'summary', 'receipt', 'review'))
    _require(manifest['schema'] in ('full-window-actual-ready.v2', 'full-window-actual-ready.v3')
             and manifest['evidence_kind'] == 'actual', 'Frozen actual v2/v3 manifest required')
    epoch = manifest['schema'].rsplit('.', 1)[1]
    _equal(manifest['scope']['schema'], 'full-window-scope.'+epoch, 'Manifest/scope epoch mismatch')
    _hex(manifest['archive_commit'], _HEX40, 'Archive commit required'); _hex(manifest['archive_tree'], _HEX40, 'Archive tree required')
    _equal(manifest['scope']['bindings']['executor_revision'], manifest['archive_commit'], 'Source executor revision mismatch')
    _require(type(manifest['sources']) is dict and manifest['sources'], 'Frozen source inventory required')
    for name, digest in manifest['sources'].items():
        _require(type(name) is str and name and not name.startswith('/') and '..' not in name.split('/'), 'Invalid frozen source path')
        _hex(digest, _HEX64, 'Frozen source SHA required')
    _require('internal/taskbench/conversation_runner.go' in manifest['sources'], 'Frozen review-context producer required')
    scope_sha = _hex(manifest['scope_sha256'], _HEX64, 'Scope SHA required')
    _equal(_sha(documents['manifest'].fragment('scope').encode()), scope_sha, 'Scope SHA recomputation mismatch')
    execution_sha = _hex(manifest['execution_sha256'], _HEX64, 'Execution SHA required')
    _equal(manifest['knowledge_model'], 'off', 'Original model knowledge configuration required')
    _equal(manifest['knowledge_jev'], 'off', 'Original JEV knowledge configuration required')
    _equal(receipt['schema'], 'full-window-acquisition-receipt.v1', 'Original acquisition receipt required')
    _equal(receipt['evidence_kind'], 'recorded_run', 'Recorded receipt required')
    _equal(receipt['scope_sha256'], scope_sha, 'Receipt scope mismatch')
    _equal(receipt['execution_sha256'], execution_sha, 'Receipt execution mismatch')
    _require(receipt['status'] in ('collected_window', 'stopped'), 'Unknown acquisition receipt status')
    _planned_rows(manifest, receipt)
    cumulative_validation = _validate_summary(summary, receipt, expected_hashes['receipt_sha256'], manifest)
    _require(review['schema'] == 'full1220-window-independent-review.v1'
             and review['method'] == 'independent_agent_evidence_review'
             and type(review['reviewer']) is str and review['reviewer'].strip()
             and review.get('evidence_kind', receipt['evidence_kind']) == 'recorded_run', 'Full-window independent review required')
    for key, expected in [('manifest_sha256', expected_hashes['manifest_sha256']), ('scope_sha256', scope_sha),
                          ('execution_sha256', execution_sha), ('receipt_sha256', expected_hashes['receipt_sha256']),
                          ('archive_commit', manifest['archive_commit']), ('archive_tree', manifest['archive_tree'])]:
        _equal(review['bindings'][key], expected, 'Review provenance mismatch: ' + key)
    review_rows = _index(review['rows'], _identity, 'review identity')
    _require(set(review_rows) == set(_identity(row) for row in receipt['rows']), 'Complete exact independent review identity set required')
    required_files = [row['recording_file'] for row in receipt['rows'] if row['attempted']]
    _require(len(set(required_files)) == len(required_files), 'Duplicate recording association')
    _require(type(expected_hashes['records']) is dict and set(records) == set(required_files)
             == set(expected_hashes['records']), 'Exact explicit record inventory required')
    plan_rows, scores, traces = [], [], {}
    counts = {'passed': 0, 'failed': 0, 'not_assessable': 0, 'not_started': 0}
    strict_counts = dict(counts); total_requirements = 0
    quality_suffix = 0
    provenance = {key: expected_hashes[key] for key in ('manifest_sha256', 'summary_sha256', 'receipt_sha256', 'review_sha256')}
    provenance.update({'scope_sha256': scope_sha, 'execution_sha256': execution_sha,
                       'archive_commit': manifest['archive_commit'], 'archive_tree': manifest['archive_tree'],
                       'context_producer_sha256': manifest['sources']['internal/taskbench/conversation_runner.go'],
                       'reviewer': review['reviewer'], 'review_method': review['method'],
                       'review_evidence_kind_source': 'review' if 'evidence_kind' in review else 'bound_recorded_run_receipt'})
    for row in receipt['rows']:
        independent = review_rows[_identity(row)]
        for key in ('defined_turns', 'observed_turns', 'attempted', 'finished'):
            if key in independent:
                _equal(independent[key], row[key], 'Review acquisition state mismatch: ' + key)
        identity = {key: row[key] for key in ('ordinal', 'pass', 'case_key', 'arm')}
        identity.update({'window_id': receipt['window_id'], 'window_index': receipt['window_index']})
        if not row['attempted']:
            _require(row['status'] == 'not_started' and not row['finished'] and not row['observed_turns']
                     and not row['trace_id'] and not row['recording_file'] and not row['recording_sha256'],
                     'Unattempted row cannot have a trace, recording or observations')
            for key in ('trace_id', 'recording_file', 'recording_sha256'):
                _require(not independent.get(key), 'Unattempted review cannot invent evidence')
            _equal(independent['original_status'], 'not_started', 'Unattempted original status mismatch')
            _equal(independent['original_success'], False, 'Unattempted original success must remain false')
            _equal(independent['pass_score_publish_eligible'], False, 'Unattempted identity cannot authorize pass scores')
            _require(not row['structural_pass'] and not row['success'] and not row['cleanup'] and not row['observer'],
                     'Unattempted identity cannot claim completed evidence')
            _equal(independent['task_verdict'], 'not_started', 'Unattempted strict verdict mismatch')
            _equal(independent.get('semantic_task_verdict', 'not_started'), 'not_started', 'Unattempted semantic verdict mismatch')
            _equal(independent['requirements'], [], 'Unattempted review cannot invent requirements')
            output = {**identity, 'attempted': False, 'finished': False, 'defined_turns': row['defined_turns'],
                      'observed_turns': 0, 'unrun_turns': row['defined_turns'], 'trace_id': None,
                      'original_status': 'not_started', 'semantic_task_verdict': 'not_started',
                      'strict_task_verdict': 'not_started', 'requirements': [], 'scores_planned': 0}
            plan_rows.append(output); counts['not_started'] += 1; strict_counts['not_started'] += 1
            continue
        name = row['recording_file']
        _equal(name, f"arm-{row['ordinal']:06d}-{row['arm']}.json", 'Canonical recording filename required')
        record_sha = _hex(expected_hashes['records'][name], _HEX64, 'Explicit record SHA required')
        _require(type(records[name]) is bytes and _sha(records[name]) == record_sha, 'Record SHA mismatch')
        _equal(row['recording_sha256'], record_sha, 'Receipt record SHA mismatch')
        _equal(independent['recording_file'], name, 'Review recording filename mismatch')
        _equal(independent['recording_sha256'], record_sha, 'Review recording SHA mismatch')
        document = _Document(records[name]); record = document.value; result = record['result']
        _equal(record['policy_id'], 'full-customer-recording-policy.v1', 'Frozen recording policy required')
        _hex(record['policy_sha256'], _HEX64, 'Recording policy SHA required')
        _equal(record['result_sha256'], _sha(document.fragment('result').encode()), 'Original result digest mismatch')
        case_key = result['case']['source_case_id'] + '/' + result['case']['scenario']['id']
        _equal(row['case_key'], case_key, 'Source case identity mismatch')
        _equal(len(result['case']['scenario']['turns']), row['defined_turns'], 'Source defined turns mismatch')
        _equal(independent['source_case'], result['case'], 'Independent source/rubric mismatch')
        _equal(record['disposition'], row['disposition'], 'Original disposition mismatch')
        _require(record['disposition'] in ('continue_recorded', 'quality_end', 'halt'), 'Unknown collection disposition')
        for original, actual in [('original_status', 'status'), ('original_structural_pass', 'structural_pass'),
                                 ('original_success', 'success'), ('original_errors', 'errors')]:
            if actual == 'errors':
                _require(independent[original] is None or type(independent[original]) is list, 'Review original errors must be null or list')
                _equal(independent[original] or [], result[actual] or [], 'Original deterministic review state mismatch: ' + actual)
            else:
                _equal(independent[original], result[actual], 'Original deterministic review state mismatch: ' + actual)
            if actual in row:
                _equal(row[actual], result[actual], 'Original deterministic receipt state mismatch: ' + actual)
        _require(result['status'] in ('passed', 'needs_review', 'failed', 'setup_failed'), 'Unknown original result status')
        observations = result['observations'] or []
        _equal(len(observations), row['observed_turns'], 'Observed turn count mismatch')
        trace = row['trace_id']
        _equal(independent['trace_id'], trace, 'Independent trace identity mismatch')
        if trace:
            _require(_TRACE.fullmatch(trace) is not None and trace != '0'*32 and trace not in traces, 'Invalid or duplicate trace')
        else:
            _require(not observations and not row['finished'], 'Observed/finished arm requires actual trace')
        for observation in observations:
            telemetry = observation['turn']['response'].get('telemetry') or {}
            _equal(telemetry.get('langfuse_trace_id'), trace, 'Actual record trace mismatch')
            if 'runtime' in telemetry:
                _equal(telemetry['runtime']['routing_mode'], row['arm'], 'Actual record arm mismatch')
        derived = _requirements(document)
        _equal(result['review_requirements'] or [], derived, 'Recomputed source/context/reply requirement mismatch')
        requirement_reviews = _index(independent['requirements'], lambda item: (item['id'], item['turn_index']), 'requirement review')
        _require(set(requirement_reviews) == set((item['id'], item['turn_index']) for item in derived), 'Exact original requirement coverage required')
        requirements = []
        for expected in derived:
            item = requirement_reviews[(expected['id'], expected['turn_index'])]
            for key, value in expected.items():
                _equal(item[key], value, 'Independent requirement binding mismatch: ' + key)
            _require(item['verdict'] in ('passed', 'failed', 'not_assessable')
                     and type(item['reason']) is str and item['reason'].strip(), 'Reasoned independent requirement verdict required')
            if 'evidence_turn_indexes' in item:
                indexes = item['evidence_turn_indexes']
                _require(type(indexes) is list and indexes and len(set(indexes)) == len(indexes)
                         and all(type(i) is int and 0 <= i < len(observations) for i in indexes)
                         and expected['turn_index'] in indexes, 'Bound observed evidence turn indexes required')
            requirements.append({**expected, 'verdict': item['verdict'], 'reason': item['reason']})
        _validate_observed_evidence(independent, result)
        semantic = _aggregate([item['verdict'] for item in requirements])
        for key in ('semantic_task_verdict', 'semantic_requirements_verdict'):
            if key in independent:
                _equal(independent[key], semantic, 'Semantic aggregate contradicts requirement verdicts')
        negative = (result['status'] in ('failed', 'setup_failed') or result['structural_pass'] is not True
                    or not row['finished'] or record['disposition'] == 'halt' or bool(result['errors']))
        strict = 'failed' if negative or semantic == 'failed' else semantic
        _equal(independent['task_verdict'], strict, 'Strict aggregate contradicts original negatives or semantic verdicts')
        if 'strict_task_verdict' in independent:
            _equal(independent['strict_task_verdict'], strict, 'Strict verdict mismatch')
        eligibility = independent['pass_score_publish_eligible']
        _require(type(eligibility) is bool and (not eligibility or (not negative and semantic == 'passed')),
                 'Original negative/unknown cannot authorize a passing score')
        output = {**identity, 'attempted': True, 'finished': row['finished'], 'defined_turns': row['defined_turns'],
                  'observed_turns': row['observed_turns'], 'unrun_turns': row['defined_turns']-row['observed_turns'],
                  'trace_id': trace or None, 'recording_file': name, 'recording_sha256': record_sha,
                  'original_status': result['status'], 'original_structural_pass': result['structural_pass'],
                  'original_success': result['success'], 'original_errors': result['errors'],
                  'disposition': record['disposition'], 'original_issues': record['issues'],
                  'quality_end': row.get('quality_end'),
                  'exclude_complete_task_comparison': row.get('exclude_complete_task_comparison', False),
                  'semantic_task_verdict': semantic, 'strict_task_verdict': strict,
                  'deterministic_semantic_disagreement': negative and semantic != 'failed',
                  'independent_disagreement_evidence': independent.get('deterministic_assessment_disagreement',
                      independent.get('deterministic_semantic_disagreement')),
                  'positive_score_suppressed': semantic == 'passed' and (negative or not eligibility),
                  'requirements': requirements}
        metadata = {**identity, **provenance, 'trace_id': trace or None, 'recording_sha256': record_sha,
                    'original_status': result['status'], 'original_structural_pass': result['structural_pass'],
                    'original_success': result['success'], 'original_errors': result['errors'],
                    'semantic_task_verdict': semantic, 'strict_task_verdict': strict,
                    'deterministic_semantic_disagreement': output['deterministic_semantic_disagreement'],
                    'positive_score_suppressed': output['positive_score_suppressed']}
        candidates = [('original_status', result['status']), ('semantic_task', semantic), ('strict_task', strict)]
        output['scores_planned'] = 0
        if trace:
            traces[trace] = {**metadata, 'expected_observed_turns': len(observations),
                             'expected_defined_turns': row['defined_turns']}
            for metric, verdict in candidates:
                if verdict == 'passed' and (negative or not eligibility):
                    continue
                logical = {'scope_sha256': scope_sha, 'window_id': receipt['window_id'], 'pass': row['pass'],
                           'ordinal': row['ordinal'], 'arm': row['arm'], 'trace_id': trace, 'metric': NAMESPACE+metric}
                logical_raw = json.dumps(logical, sort_keys=True, separators=(',', ':'), ensure_ascii=False)
                scores.append({'id': _sha(logical_raw.encode()), 'logical_identity': logical_raw,
                               'traceId': trace, 'name': NAMESPACE+metric, 'value': verdict,
                               'dataType': 'CATEGORICAL', 'environment': 'evaluation', 'metadata': dict(metadata),
                               'comment': 'Independent nonblind evidence review; original deterministic results retained. Pure plan, pending content/trace validation.'})
                output['scores_planned'] += 1
        quality_suffix += (row.get('quality_end') or {}).get('unrun_turns', 0)
        total_requirements += len(requirements); counts[semantic] += 1; strict_counts[strict] += 1
        plan_rows.append(output)
    return {'schema': 'full1220-window-score-plan.v1', 'namespace': NAMESPACE, 'publish_ready': False,
            'pending_gates': ['frozen_capture_projection', 'persisted_trace_content_validation', 'single_publication_protocol']
                + ([] if cumulative_validation['prior_partition_verified'] else ['prior_receipt_turn_partition_chain']),
            'cumulative_turn_validation': cumulative_validation,
            'model_calls': 0, 'jev_calls': 0, 'claims_created': 0, 'scores_published': 0,
            'provenance': provenance, 'window_id': receipt['window_id'], 'window_index': receipt['window_index'],
            'denominators': {'campaign': dict(_CAMPAIGN), 'acquisition_cumulative': {
                key: summary[key] for key in ('attempted_arms', 'finished_arms', 'failed_attempted_arms',
                    'unattempted_arms', 'observed_turns', 'quality_end_unrun_turns', 'global_unrun_turns')},
                'window': {'pairs': receipt['planned_current_pairs'], 'arms': receipt['planned_current_arms'],
                           'defined_turns': receipt['planned_current_turns'], 'attempted_arms': receipt['attempted_arms'],
                           'finished_arms': receipt['finished_arms'], 'unattempted_arms': receipt['unrun_arms'],
                           'observed_turns': receipt['observed_turns'], 'requirements': total_requirements,
                           'quality_end_unrun_turns': quality_suffix,
                           'global_unrun_turns': receipt['planned_current_turns']-receipt['observed_turns']-quality_suffix,
                           'semantic_task_verdicts': counts, 'strict_task_verdicts': strict_counts}},
            'rows': plan_rows, 'trace_bindings': traces, 'scores': scores,
            'limitations': ['Independent agent judgments are preserved, not independently proved by hash validation.',
                            'Source snapshots are externally SHA-anchored; this pure function does not verify an Ed25519 signature or source checkout.',
                            'Partial windows do not establish completion or a representative score for the full campaign.',
                            'For later windows, cumulative observed/quality components before the current window remain claims pending original prior-receipt chain replay.',
                            'No captured-content or persisted Langfuse trace validation has been performed by this function.']}


def build_window_score_plan(manifest_raw, summary_raw, receipt_raw, records, review_raw, expected_hashes):
    """Validate a complete explicit window and return a non-publishing plan.

    All raw artifacts must be bytes. ``records`` maps canonical receipt filenames
    to bytes; ``expected_hashes`` requires manifest/summary/receipt/review_sha256
    plus a ``records`` map of exactly the same filenames to external SHA256 pins.
    Pins must come from a trusted frozen inventory, not from untrusted artifacts.
    Review schema evolution is explicit: original task_verdict is the strict
    axis; semantic_task_verdict/semantic_requirements_verdict, when present, must
    match the aggregate of the complete bound requirements. Verification flags
    are intentionally ignored. Any mismatch rejects the entire window.
    """
    try:
        return _build(manifest_raw, summary_raw, receipt_raw, records, review_raw, expected_hashes)
    except (KeyError, IndexError, TypeError, UnicodeError, OverflowError, RecursionError) as error:
        raise ValueError('Incomplete or invalid full-window evidence: ' + type(error).__name__) from error


_PROJECTION_SDKROOT = '/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk'
_PROJECTION_HELPER_SHA256 = '57847bf1ba02864c918261813ea45f68dc57f4dbd5d06349cc0dd661e225cdfc'
_CONTENT_SOURCE_SHA256 = '1c2884a41724bcc116764eac61f807d7c7845bae92285009406580575ace5a73'
_PROJECTION_HELPER_RELATIVE = 'internal/observability/full1220_content_projection_test.go'
_PROJECTION_SEAL = object()


def _json_bytes(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), ensure_ascii=False, allow_nan=False).encode('utf-8')


def _regular_bytes(path, limit=64 << 20):
    """Bounded O_RDONLY/no-follow read with opened-file identity checks."""
    path = Path(path)
    before = path.lstat()
    _require(stat.S_ISREG(before.st_mode) and before.st_size <= limit, 'Bounded regular file required')
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, 'rb') as stream:
        opened = os.fstat(stream.fileno())
        _require(os.path.samestat(before, opened), 'File identity changed before read')
        raw = stream.read(limit+1)
        after = os.fstat(stream.fileno())
    final = path.lstat()
    _require(len(raw) <= limit and len(raw) == before.st_size and os.path.samestat(opened, after)
             and os.path.samestat(after, final) and before.st_size == after.st_size == final.st_size
             and before.st_mtime_ns == after.st_mtime_ns == final.st_mtime_ns, 'File changed during read')
    return raw


def _exclusive_write(path, raw):
    descriptor = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, 'wb') as stream:
        stream.write(raw); stream.flush(); os.fsync(stream.fileno())


def _verify_frozen_sources(root, sources, extras=None, *, allow_root_git_pointer=False):
    _require(root.is_dir() and not root.is_symlink(), 'Explicit regular frozen source directory required')
    expected = {**sources, **(extras or {})}; actual = set(); git_pointer_sha = None
    for directory, subdirs, files in os.walk(root, followlinks=False):
        for name in subdirs:
            _require(name != '.git', 'Git metadata directories are not frozen source')
            _require(not (Path(directory)/name).is_symlink(), 'Source symlink directory forbidden')
        for name in files:
            path = Path(directory)/name
            relative = path.relative_to(root).as_posix()
            if relative == '.git':
                _require(allow_root_git_pointer and relative not in expected, 'Only original-root Git pointer metadata is allowed')
                pointer = _regular_bytes(path, 4096)
                match = re.fullmatch(rb'gitdir: ([^\r\n\x00]+)\n', pointer)
                _require(match is not None, 'Canonical bounded gitdir pointer required')
                target = match[1].decode('utf-8', errors='strict')
                _require(Path(target).is_absolute() and str(Path(target)) == target and target == target.strip()
                         and '..' not in Path(target).parts and all(ord(char) >= 32 and ord(char) != 127 for char in target),
                         'Absolute clean gitdir path required')
                git_pointer_sha = _sha(pointer)
                continue
            _require(relative in expected, 'Unexpected source inventory file')
            _equal(_sha(_regular_bytes(path)), expected[relative], 'Frozen source SHA mismatch: '+relative)
            actual.add(relative)
    _require(actual == set(expected), 'Incomplete frozen source inventory')
    return git_pointer_sha


class _ContentProjection:
    """Opaque local provenance, deliberately not reconstructible from JSON."""
    __slots__ = ('_raw', '_raw_sha', '_plan_sha', '_job', '_seal')

    def __init__(self, seal, raw, plan, job):
        _require(seal is _PROJECTION_SEAL, 'Fixed helper execution proof required')
        for name, value in (('_raw', raw), ('_raw_sha', _sha(raw)), ('_plan_sha', _sha(_json_bytes(plan))),
                            ('_job', _json_bytes(job)), ('_seal', seal)):
            object.__setattr__(self, name, value)

    def __setattr__(self, name, value):
        raise AttributeError('Frozen helper proof is immutable')

    def as_dict(self):
        return _Document(self._raw).value


def _projection_job(plan, manifest):
    rows = []
    for row in plan['rows']:
        if not row['attempted']:
            continue
        _require(row['trace_id'], 'Attempted arm without actual trace cannot establish trace content')
        slot = manifest['scope']['slots'][row['ordinal']-1]
        metadata = {'run_id': plan['provenance']['scope_sha256'], 'case_key': row['case_key'], 'arm': row['arm'],
                    'evidence_kind': 'recorded_run', 'pair_id': plan['provenance']['scope_sha256']+'/'+plan['window_id']
                        + f"/ordinal-{row['ordinal']:06d}/pass-{row['pass']}",
                    'block_id': plan['window_id'], 'arm_order': ','.join(slot['arm_order']),
                    'attributes.langfuse.release': plan['provenance']['archive_commit']}
        rows.append({'trace_id': row['trace_id'], 'recording_file': row['recording_file'],
                     'recording_sha256': row['recording_sha256'], 'observed_turns': row['observed_turns'], 'metadata': metadata})
    return {'schema': 'full1220-content-projection-input.v1',
            **{key: plan['provenance'][key] for key in ('manifest_sha256', 'receipt_sha256', 'scope_sha256',
                                                       'execution_sha256', 'archive_commit', 'archive_tree')},
            'content_source_sha256': _CONTENT_SOURCE_SHA256, 'helper_sha256': _PROJECTION_HELPER_SHA256,
            'window_id': plan['window_id'], 'rows': rows}


def _check_projected_output(payload, job, input_raw, records):
    _equal(payload['schema'], 'full1220-content-projection.v1', 'Fixed helper output schema required')
    for key in ('manifest_sha256', 'receipt_sha256', 'scope_sha256', 'execution_sha256', 'archive_commit',
                'archive_tree', 'content_source_sha256', 'helper_sha256', 'window_id'):
        _equal(payload[key], job[key], 'Projection provenance mismatch: '+key)
    _equal(payload['input_sha256'], _sha(input_raw), 'Projection input digest mismatch')
    _equal(payload['new_external_requests'], 0, 'Offline projection required')
    _equal([row['trace_id'] for row in payload['rows']], [row['trace_id'] for row in job['rows']], 'Exact ordered projection trace set required')
    for projected, source in zip(payload['rows'], job['rows']):
        _equal(projected['recording_sha256'], source['recording_sha256'], 'Projected recording digest mismatch')
        observations = _Document(records[source['recording_file']]).value['result']['observations'] or []
        _equal([turn['turn_index'] for turn in projected['turns']], list(range(1, len(observations)+1)), 'Exact projection turn coverage required')
        for turn, observation in zip(projected['turns'], observations):
            _equal(turn['original_input_sha256'], _sha(observation['turn']['message'].encode()), 'Helper original input digest mismatch')
            _equal(turn['original_reply_sha256'], _sha(observation['turn']['response']['reply'].encode()), 'Helper original reply digest mismatch')
            for field in ('input', 'output'):
                value = turn[field]
                _require(type(value) in (str, dict), 'Frozen string capture or bounded wrapper required')
                if type(value) is dict:
                    _require(set(value) == {'truncated', 'preview'} and value['truncated'] is True
                             and type(value['preview']) is str and len(value['preview'].encode()) <= 1500,
                             'Original content wrapper shape required')
                _require(len(_json_bytes(value)) <= 4096, 'Projected content exceeds frozen capture bound')


def project_window_content(manifest_raw, summary_raw, receipt_raw, records, review_raw, expected_hashes,
                           *, source_root, output_dir, runtime, execute=False):
    """Run the fixed no-network Go helper against an isolated frozen archive.

    Requires explicit execute=True (schedule this outside measurement windows),
    an absent output_dir whose parent exists, and runtime={go_executable,
    go_sha256,module_cache}. The executable is an externally trusted SHA pin,
    never PATH lookup; arguments, helper, SDK26.5 and environment are fixed.
    All manifest source files are rehashed before/after an isolated full copy;
    originals and staged records are revalidated after the command. No settings
    or credentials are read. Return an opaque proof; JSON alone cannot reload it.
    """
    try:
        _require(execute is True, 'Explicit fixed offline helper execution required')
        plan = build_window_score_plan(manifest_raw, summary_raw, receipt_raw, records, review_raw, expected_hashes)
        manifest = _Document(manifest_raw).value
        _equal(expected_hashes['projection_helper_sha256'], _PROJECTION_HELPER_SHA256, 'Externally pinned fixed helper required')
        helper_path = Path(__file__).resolve().parent/'helpers/full1220_content_projection_test.go.txt'
        helper = _regular_bytes(helper_path, 1 << 20)
        _equal(_sha(helper), _PROJECTION_HELPER_SHA256, 'Fixed helper source changed')
        _equal(manifest['sources']['internal/observability/content.go'], _CONTENT_SOURCE_SHA256, 'Unsupported frozen content source version')
        observer = manifest['observer']
        for key, expected in [('enabled', True), ('capture_content', True), ('sample_rate', 1),
                              ('environment', 'evaluation'), ('release', manifest['archive_commit'])]:
            _equal(observer[key], expected, 'Frozen capture configuration mismatch: '+key)
        root = Path(source_root).absolute()
        _equal(str(root.resolve()), str(Path(manifest['source_root']).resolve()), 'Explicit source root must match pinned manifest')
        root_git_pointer = _verify_frozen_sources(root, manifest['sources'], allow_root_git_pointer=True)
        sdk = Path(_PROJECTION_SDKROOT)
        _require(sdk.is_dir() and sdk.name == 'MacOSX26.5.sdk', 'Fixed MacOSX26.5 SDK required')
        executable = Path(runtime['go_executable']).resolve()
        _equal(_sha(_regular_bytes(executable)), _hex(runtime['go_sha256'], _HEX64, 'Pinned Go executable SHA required'), 'Go executable SHA mismatch')
        _require(os.access(executable, os.X_OK), 'Pinned executable must be executable')
        module_cache = Path(runtime['module_cache']).resolve()
        _require(module_cache.is_dir(), 'Explicit existing offline module cache required')
        output = Path(output_dir).absolute()
        _require(output.parent.is_dir() and not output.exists() and not output.is_symlink(), 'Absent isolated projection output directory required')
        canonical_root, canonical_output = root.resolve(), output.resolve()
        _require(canonical_root != canonical_output and canonical_root not in canonical_output.parents
                 and canonical_output not in canonical_root.parents, 'Projection directory must be separate from frozen source')
        output.mkdir(mode=0o700)
        staged = output/'source'; staged.mkdir(mode=0o700)
        for name in manifest['sources']:
            target = staged/name; target.parent.mkdir(parents=True, exist_ok=True)
            raw = _regular_bytes(root/name)
            _equal(_sha(raw), manifest['sources'][name], 'Source changed while staging')
            _exclusive_write(target, raw)
        _require(_PROJECTION_HELPER_RELATIVE not in manifest['sources'], 'Frozen archive already contains helper destination')
        _exclusive_write(staged/_PROJECTION_HELPER_RELATIVE, helper)
        extra = {_PROJECTION_HELPER_RELATIVE: _PROJECTION_HELPER_SHA256}
        _verify_frozen_sources(staged, manifest['sources'], extra)
        staged_records = output/'records'; staged_records.mkdir(mode=0o700)
        for name, raw in records.items():
            _exclusive_write(staged_records/name, raw)
        job = _projection_job(plan, manifest); input_raw = _json_bytes(job)
        _exclusive_write(output/'input.json', input_raw)
        for name in ('cache', 'tmp'):
            (output/name).mkdir(mode=0o700)
        env = {'PATH': '/usr/bin:/bin:/usr/sbin:/sbin', 'GOENV': 'off', 'GOWORK': 'off', 'GOFLAGS': '',
               'GOTOOLCHAIN': 'local', 'GOPROXY': 'off', 'GOSUMDB': 'off', 'GO111MODULE': 'on',
               'GOCACHE': str(output/'cache'), 'GOTMPDIR': str(output/'tmp'), 'GOMODCACHE': str(module_cache),
               'SDKROOT': str(sdk), 'CGO_ENABLED': '1', 'CC': '/usr/bin/clang', 'CXX': '/usr/bin/clang++',
               'CGO_CFLAGS': '-isysroot '+str(sdk), 'CGO_LDFLAGS': '-isysroot '+str(sdk),
               'FULL1220_PROJECTION_INPUT': str(output/'input.json'),
               'FULL1220_PROJECTION_OUTPUT': str(output/'projection.json'),
               'FULL1220_PROJECTION_INPUT_SHA256': _sha(input_raw)}
        command = [str(executable), 'test', '-mod=readonly', '-buildvcs=false', '-v', './internal/observability',
                   '-run', '^TestFull1220FrozenContentProjection$', '-count=1', '-timeout=2m']
        result = subprocess.run(command, cwd=str(staged), env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=150, check=False)
        _require(len(result.stdout) <= 8 << 20 and len(result.stderr) <= 8 << 20, 'Helper output bound exceeded')
        _exclusive_write(output/'helper.stdout', result.stdout); _exclusive_write(output/'helper.stderr', result.stderr)
        _require(result.returncode == 0, 'Fixed helper failed; projection not verified')
        _equal(_verify_frozen_sources(root, manifest['sources'], allow_root_git_pointer=True), root_git_pointer, 'Original Git pointer changed during helper execution')
        _verify_frozen_sources(staged, manifest['sources'], extra)
        _equal(_sha(_regular_bytes(helper_path, 1 << 20)), _PROJECTION_HELPER_SHA256, 'Helper changed during execution')
        _equal(_sha(_regular_bytes(executable)), runtime['go_sha256'], 'Go executable changed during execution')
        _require(_regular_bytes(output/'input.json') == input_raw, 'Helper input changed during execution')
        for name, raw in records.items():
            _require(_regular_bytes(staged_records/name, 16 << 20) == raw, 'Staged record changed during helper execution')
        projected_raw = _regular_bytes(output/'projection.json', 16 << 20)
        output_receipts = re.findall(rb'(?m)^\s+full1220_content_projection_test\.go:[0-9]+: FULL1220_PROJECTION_OUTPUT_SHA256=([0-9a-f]{64})\r?$', result.stdout)
        _require(len(output_receipts) == 1 and output_receipts[0].decode() == _sha(projected_raw),
                 'Projection bytes do not match the fixed helper process receipt')
        payload = _Document(projected_raw).value
        _check_projected_output(payload, job, input_raw, records)
        return _ContentProjection(_PROJECTION_SEAL, projected_raw, plan, job)
    except (KeyError, IndexError, TypeError, OSError, UnicodeError, subprocess.SubprocessError) as error:
        raise ValueError('Frozen offline projection unavailable: '+type(error).__name__) from error


def _content_matches(actual, expected):
    if _json_bytes(actual) == _json_bytes(expected):
        return True
    if type(actual) is str:
        try:
            return _json_bytes(json.loads(actual)) == _json_bytes(expected)
        except ValueError:
            return False
    return False


def validate_window_traces(plan, projection, client):
    """Read each original trace once using client.observations(trace), never POST.

    Missing/delayed observations or IO remain unverified; a provenance, identity,
    parent, turn or delivered-content contradiction rejects the whole result.
    Only the opaque fixed-helper proof is accepted. Native LLM payload capture
    and actual use of included content are never inferred from agent.turn IO.
    This does not consume or clear the prior receipt partition chain gate.
    """
    try:
        _require(type(projection) is _ContentProjection and projection._seal is _PROJECTION_SEAL,
                 'Live fixed helper projection proof required; plain JSON/SHA is insufficient')
        _equal(_sha(projection._raw), projection._raw_sha, 'Projection proof mutated')
        _equal(_sha(_json_bytes(plan)), projection._plan_sha, 'Projection belongs to another or modified plan')
        payload = projection.as_dict(); job = _Document(projection._job).value
        _require(callable(getattr(client, 'observations', None)), 'Read-only observations client required')
        projected = {row['trace_id']: row for row in payload['rows']}
        bound = {row['trace_id']: row for row in job['rows']}
        _require(set(projected) == set(plan['trace_bindings']) == set(bound), 'Exact projection/plan trace set required')
        summaries, pending = [], []
        for trace, binding in plan['trace_bindings'].items():
            try:
                observations = client.observations(trace)
            except (ValueError, OSError, TimeoutError):
                pending.append({'trace_id': trace, 'reason': 'read_unavailable'}); continue
            _require(type(observations) is list and len(observations) <= 1000, 'Bounded original observation list required')
            by_id = _index(observations, lambda row: row['id'], 'observation ID')
            _require(all(type(key) is str and key for key in by_id), 'Observation ID required')
            for observation in observations:
                _equal(observation['traceId'], trace, 'Foreign observation trace')
                _equal(observation['environment'], 'evaluation', 'Observation environment mismatch')
                for key, value in bound[trace]['metadata'].items():
                    _equal(observation['metadata'][key], value, 'Observation frozen identity mismatch: '+key)
            roots = [row for row in observations if row['name'] == 'evaluation.task']
            turns = [row for row in observations if row['name'] == 'agent.turn']
            _require(len(roots) <= 1 and len(turns) <= binding['expected_observed_turns'], 'Extra root or turn observation')
            if not roots or len(turns) < binding['expected_observed_turns']:
                pending.append({'trace_id': trace, 'reason': 'missing_root_or_turn'}); continue
            root = roots[0]
            _require(root.get('parentObservationId') in (None, ''), 'Evaluation root has foreign parent')
            expected_success = ('true' if binding['original_status'] == 'passed' else 'false'
                                if binding['original_status'] in ('failed', 'setup_failed') else 'unknown')
            for key, value in [('status', binding['original_status']),
                               ('structural_pass', str(binding['original_structural_pass']).lower()),
                               ('task_success', expected_success)]:
                actual = root['metadata'][key]
                if key in ('structural_pass', 'task_success') and value in ('true', 'false'):
                    _require((type(actual) is bool and actual is (value == 'true'))
                             or (type(actual) is str and actual == value),
                             'Original evaluation boolean metadata mismatch: '+key)
                else:
                    _equal(actual, value, 'Original evaluation status mismatch: '+key)
            indexed_turns = {}
            for turn in turns:
                index = turn['metadata']['turn_index']
                if type(index) is str and re.fullmatch(r'[1-9][0-9]*', index):
                    index = int(index)
                _integer(index, 1, 'Canonical observed turn index required')
                _require(index not in indexed_turns, 'Duplicate observed turn index')
                indexed_turns[index] = turn
                _equal(turn['parentObservationId'], root['id'], 'Agent turn belongs to another task root')
            _equal(sorted(indexed_turns), list(range(1, binding['expected_observed_turns']+1)), 'Exact observed turn index set required')
            for observation in observations:
                chain = set(); current = observation
                while current['id'] != root['id']:
                    parent = current.get('parentObservationId')
                    _require(parent in by_id and parent not in chain, 'Foreign or cyclic observation parent chain')
                    chain.add(parent); current = by_id[parent]
            missing_io = False
            for expected in projected[trace]['turns']:
                actual = indexed_turns[expected['turn_index']]
                for field in ('input', 'output'):
                    if field not in actual or actual[field] is None:
                        missing_io = True
                    else:
                        _require(_content_matches(actual[field], expected[field]), 'Captured agent turn content mismatch: '+field)
            if missing_io:
                pending.append({'trace_id': trace, 'reason': 'missing_captured_content'}); continue
            summaries.append({'trace_id': trace, 'observations': len(observations), 'turns': len(turns),
                              'observation_body_sha256': _sha(_json_bytes(observations)),
                              'llm_attempts_without_payload_io': sum(row['name'] == 'llm.attempt' and row.get('input') is None
                                  and row.get('output') is None for row in observations)})
        all_pass = not pending and len(summaries) == len(plan['trace_bindings'])
        gates = [gate for gate in plan['pending_gates'] if gate != 'frozen_capture_projection'
                 and not (all_pass and gate == 'persisted_trace_content_validation')]
        return {'schema': 'full1220-window-content-validation.v1', 'all_pass': all_pass, 'publish_ready': False,
                'verified_traces': len(summaries), 'expected_traces': len(plan['trace_bindings']),
                'verified_observations': sum(row['observations'] for row in summaries),
                'verified_turns': sum(row['turns'] for row in summaries), 'summaries': summaries, 'unverified': pending,
                'pending_gates': gates, 'projection_sha256': projection._raw_sha,
                'helper_sha256': _PROJECTION_HELPER_SHA256, 'content_source_sha256': _CONTENT_SOURCE_SHA256,
                'plan_sha256': projection._plan_sha, 'new_external_requests_from_projection': 0,
                'new_model_calls': 0, 'new_jev_calls': 0, 'claims_created': 0, 'scores_published': 0,
                'native_llm_payload_io_claimed': False, 'included_does_not_prove_used': True}
    except (KeyError, IndexError, TypeError, UnicodeError) as error:
        raise ValueError('Invalid captured trace evidence: '+type(error).__name__) from error
