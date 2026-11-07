"""Pure, explicitly SHA-anchored full1220 window score plans.

Task1 is pure. Task2 runs a pinned offline capture helper in an isolated
source copy. Task3 requires the original receipt chain, an offline Ed25519
attestation proof, complete trace/content validation, and explicit --execute
before once-only local score publication. Default plan and verify are read-only;
no inference, actual-budget claims, private signing keys or consumed binary
execution are supported. The publication registry is fixed to this local project.

Records are original Go encoding/json snapshots. Context reconstruction takes
nested JSON fragments from those externally anchored bytes, preserving Go's
struct order, sorted maps, float lexemes, nulls and omission rules. It assembles
the *actual* capitalized Go context struct and compares every recomputed digest;
it never treats review verification flags or recorded context digests as proof.
"""
import sys
sys.dont_write_bytecode = True
import hashlib
import json
import math
import re
import os
import base64
import argparse
import importlib.util
import stat
import subprocess
from pathlib import Path


# O_NOFOLLOW / O_DIRECTORY / flock are Unix. Windows CI still executes this module;
# missing flags stay zero and directory or lock operations use the portable path.
_O_NOFOLLOW = getattr(os, 'O_NOFOLLOW', 0)
_O_DIRECTORY = getattr(os, 'O_DIRECTORY', 0)


def _owned_private(info, mode):
    """Unix owner/mode is the private-file boundary. Windows ACLs do not round-trip chmod."""
    if os.name == 'nt':
        return True
    return info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == mode


def _open_directory(path):
    if os.name != 'nt':
        return os.open(path, os.O_RDONLY | _O_DIRECTORY | _O_NOFOLLOW)
    import ctypes
    import msvcrt
    kernel = ctypes.WinDLL('kernel32', use_last_error=True)
    kernel.CreateFileW.argtypes = [ctypes.c_wchar_p, ctypes.c_uint32, ctypes.c_uint32, ctypes.c_void_p, ctypes.c_uint32, ctypes.c_uint32, ctypes.c_void_p]
    kernel.CreateFileW.restype = ctypes.c_void_p
    share = 1 | 2 | 4
    handle = kernel.CreateFileW(os.fspath(path), 0x80000000, share, None, 3, 0x02000000, None)
    if handle is None or handle == ctypes.c_void_p(-1).value:
        raise OSError(ctypes.get_last_error(), 'cannot open directory')
    return msvcrt.open_osfhandle(handle, os.O_RDONLY)


def _open_at(dir_fd, relative, path, flags, mode=None):
    """dir_fd is Unix-only. Windows opens the already-resolved path."""
    flags |= _O_NOFOLLOW
    if os.name != 'nt' and dir_fd is not None:
        if mode is None:
            return os.open(relative, flags, dir_fd=dir_fd)
        return os.open(relative, flags, mode, dir_fd=dir_fd)
    if mode is None:
        return os.open(path, flags)
    return os.open(path, flags, mode)


def _lock_exclusive_nonblocking(fd):
    if os.name == 'nt':
        import msvcrt
        os.lseek(fd, 0, os.SEEK_SET)
        try:
            # One byte is enough. Locking past EOF is valid and keeps an empty lock file empty.
            msvcrt.locking(fd, msvcrt.LK_NBLCK, 1)
        except OSError as error:
            raise BlockingIOError(11, 'registry lock busy') from error
        return
    import fcntl
    fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)


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
            # This pinned Go producer omits zero-valued receipt suffix ints.
            # Only an exactly rederived zero may be absent; unknown producers
            # and every nonzero component still require explicit evidence.
            _require(expected == 0 and manifest['sources'].get('internal/taskbench/window_collector.go')
                     == '9131a3bc61c7cef89c1bda02dabffffe5d85bbebd48e9834afb542553cd200d7',
                     'Quality-end requires explicit window suffix denominator: ' + key)
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


_COLLECTION_QUALITY_CODES = frozenset(('unexpected_action', 'wrong_read_tool', 'missing_read',
    'missing_preview', 'missing_execution', 'terminal_goal', 'semantic_negative', 'fact_mismatch'))
_COLLECTION_POLICY_SHA256 = '604c68b1c192c17594ba422b85367579cabd60c31623af6a9632278e401ae970'


def validate_completed_record_state(record_raw):
    """Check bound source-policy state only; does not verify native or semantic evidence."""
    try:
        document = _Document(record_raw); record = document.value; result = record['result']
        _equal(record['policy_id'], 'full-customer-recording-policy.v1', 'Recording policy required')
        _equal(record['policy_sha256'], _COLLECTION_POLICY_SHA256, 'Source policy digest mismatch')
        _equal(record['result_sha256'], _sha(document.fragment('result').encode()), 'Result digest mismatch')
        _equal(record['disposition'], 'continue_recorded', 'Unverified halt cannot be completed')
        _require(result['errors'] is None or type(result['errors']) is list, 'Error list required')
        _require(type(record['issues']) is list, 'Issue list required')
        errors = result['errors'] or []
        issues = record['issues']
        _require(type(errors) is list and all(type(e) is str and e for e in errors), 'Errors required')
        _require(type(issues) is list and len(issues) == len(errors), 'Issue/error coverage required')
        for index, issue in enumerate(issues):
            _require(type(issue) is dict and set(issue) == {'code','turn','error_index','disposition'}, 'Typed issue required')
            _require(issue['code'] in _COLLECTION_QUALITY_CODES and issue['disposition'] == 'continue_recorded', 'Unknown issue origin')
            _equal(issue['error_index'], index, 'Issue/error index mismatch')
            _require(type(issue['turn']) is int and 1 <= issue['turn'] <= len(result['observations'] or []), 'Issue turn mismatch')
        _require(type(result['structural_pass']) is bool and type(result['success']) is bool and result['setup_error'] is False, 'Invalid result flags')
        if errors:
            _equal(result['status'], 'failed', 'Negative status required')
            _equal(result['success'], False, 'Negative success forbidden')
            pre_review = any(i['code'] not in ('semantic_negative', 'fact_mismatch') for i in issues)
            _equal(result['structural_pass'], not pre_review, 'Structural state mismatch')
            return 'recorded_negative'
        _equal(result['structural_pass'], True, 'Structural pass required')
        if result['review_requirements']:
            _equal(result['status'], 'needs_review', 'Review status required')
            _equal(result['success'], False, 'Unreviewed success forbidden')
        else:
            _equal(result['status'], 'passed', 'Pass status required')
            _equal(result['success'], True, 'Pass success required')
        return 'recorded_nonnegative'
    except (KeyError, TypeError, IndexError) as error:
        raise ValueError('Invalid completed recording: '+type(error).__name__) from error


def _score_cache_accounting(manifest, epoch):
    limits = manifest.get('provider_limits', {})
    if epoch not in ('v4', 'v5', 'v6', 'v7'):
        for provider in ('model', 'jev'):
            _equal(limits.get(provider, {}).get('cache_hit_input_micro_per_million', 0), 0,
                   'Legacy score epoch cannot enable cache accounting')
        return None
    scope = manifest['scope']
    if epoch == 'v4':
        label = 'Epoch4'
        baseline = '12a9d584ea9508a9fc7701532ad9214b0c743a8e13d17829432fac0d6b48a20c'
        claims_sha = 'c444e717aeff328eb2b82fc0910901de22610541a39551c103e55fb5f4cef354'
        claims_label = 'twenty-five'
        caps_sha = '0c020b80cf65c9876025d72ef8ad68bcfdea6c77b429b0390f2aabde895ca025'
        model_cap, jev_cap = 64000000, 690000
        caps_label = '64m/690k'
    elif epoch == 'v5':
        label = 'Epoch5'
        baseline = '2f8c83546a2918b85801160f521465c2f53e040073b3590e1ecde53f278ef340'
        claims_sha = '9b06ad5fd58440494b49fb44437160a870d123007467f798b2f6d0e613783335'
        claims_label = 'twenty-eight'
        caps_sha = '7fe19887167feaa4ce85520f2717cf82d0143cd5e2eef6577b3b07afcfe04862'
        model_cap, jev_cap = 62000000, 676000
        caps_label = '62m/676k'
    elif epoch == 'v6':
        label = 'Epoch6'
        baseline = 'b363ba194e641374a8a63bfacc7a1312a5b080718f637036ab4040fc075a3fc1'
        claims_sha = '7f6a967d739a1e338b69153413a5e9f06dcc3fb7d7bae05b64e2988686b5dc1b'
        claims_label = 'thirty-two'
        caps_sha = '30c8151031f40cb05562b255b789b9fb021a93467288e677c4eb39d87806113d'
        model_cap, jev_cap = 85000000, 600000
        caps_label = '85m/600k'
    else:
        label = 'Epoch7'
        baseline = 'ccf528106789daf05a61c8d8f4e5f72d3a29eea65d78a6f81afb85f4e1ea45ad'
        claims_sha = '2b67c23b0b3b6c48e676fff5dfbc3587ab5f2432e96acb3824f931130fd75ede'
        claims_label = 'thirty-three'
        caps_sha = '7b4078f89969bb46797217ea677a01cfec9adf51257437f603455a684368029d'
        model_cap, jev_cap = 84500000, 600000
        caps_label = '84.5m/600k'
        _equal(manifest['observer']['export_timeout_ms'], 10000, 'Epoch7 fixed observer ACK wait required')
    _equal(scope['baseline_evidence_sha256'], baseline, label+' stopped baseline mismatch')
    _equal(manifest['baseline_evidence']['sha256'], baseline, label+' manifest baseline mismatch')
    approval = ('ef6322fe60807a7a608a2707edf016501717f85e9d649e28d0311de2b6ed4bce' if epoch in ('v6', 'v7')
                else 'd5062e6f39f9ae7c9a8461121f9ba907c798100d7a5428af72da459595d2d554')
    _equal(scope['budget_approval_sha256'], approval, label+' fixed approval mismatch')
    # Fixed expectedStoppedV3/V4 maps; v5 also retains all three P4 claims,
    # including the reserved third claim with no ledger or provider request.
    _equal(_sha(_json_bytes(manifest['historical_claims'])), claims_sha,
           label+' exact '+claims_label+' historical claims required')
    expected = {
        'model': {'model': 'deepseek-flash', 'max_request_bytes': 32768, 'max_input_tokens': 36864,
                  'max_output_tokens': 128, 'input_micro_per_million': 2000000,
                  'output_micro_per_million': 8000000, 'cache_hit_input_micro_per_million': 40000},
        'jev': {'model': 'jev-1.13.0', 'max_request_bytes': 4096, 'max_input_tokens': 64000,
                'max_output_tokens': 1000000, 'input_micro_per_million': 42000, 'output_micro_per_million': 0}}
    normalized = {key: dict(value) for key, value in limits.items()}
    if 'cache_hit_input_micro_per_million' in normalized.get('jev', {}):
        _equal(normalized['jev'].pop('cache_hit_input_micro_per_million'), 0, 'JEV cache accounting forbidden')
    _equal(normalized, expected, label+' exact provider rates and envelopes required')
    _equal(scope['width'], 28, label+' original width required')
    _equal(len(scope['windows']), 131, label+' original window count required')
    caps = [{key: _integer(window[key], 0, 'Integer '+label.lower()+' cap required')
             for key in ('model_cap_micro_cny', 'jev_cap_micro_usd')} for window in scope['windows']]
    # Go's fullWindowCaps struct order is model then JEV, independent of map ordering.
    _equal(_sha(json.dumps(caps, separators=(',', ':')).encode()), caps_sha,
           label+' fixed '+caps_label+' cap vector required')
    return {'schema': 'full-window-accounting.v2', 'model_cache_hit_input_micro_per_million': 40000,
            'model_input_micro_per_million': 2000000, 'model_output_micro_per_million': 8000000,
            'jev_input_micro_per_million': 42000, 'jev_output_micro_per_million': 0,
            'model_total_cap_micro_cny': model_cap, 'jev_total_cap_micro_usd': jev_cap,
            'basis': 'frozen_configuration_not_invoice_or_observed_cache_split',
            'cache_absent_settlement': 'all_input_at_miss_rate_without_observed_cache_split',
            'proves_cache_usage': False, 'proves_knowledge_usage': False}


_QUALITY_V6_HELPER_SHA256 = 'c8aae02c9bf7f27d6aa7091bb94286453c4e5c2f604e78bd896c1c0f2ebba458'
_QUALITY_V6_HELPER_RELATIVE = 'internal/taskbench/full1220_quality_v6_test.go'
_QUALITY_V7_HELPER_SHA256 = '281d78af5cdce131cf587c4648f0632da6184585ceb2de23f27306c12c48a67b'
_QUALITY_V7_HELPER_RELATIVE = 'internal/taskbench/full1220_quality_v7_test.go'
_QUALITY_HALT_CODES = frozenset(('preview_arguments', 'pending_identity', 'unexpected_pending',
                               'unauthorized_tool', 'pending_not_cleared'))


def _quality_v6_record(row, raw, sidecars, policy):
    """Pure linkage only. Source-owned replay is deliberately a separate gate."""
    doc = _Document(raw); record = doc.value; result = record['result']; q = row['quality_end']
    _equal(record['policy_id'], 'full-customer-recording-policy.v1', 'Recording policy required')
    _equal(record['policy_sha256'], _COLLECTION_POLICY_SHA256, 'Recording policy mismatch')
    _equal(record['result_sha256'], _sha(doc.fragment('result').encode()), 'Quality result digest mismatch')
    _require(record['disposition'] == row['disposition'] == 'halt' and result['status'] == row['status'] == 'failed'
             and result['structural_pass'] is False and result['success'] is False and result['setup_error'] is False
             and row['structural_pass'] is False and row['success'] is False and row['cleanup'] is True
             and row['observer'] is True, 'Original owned-quality failure/cleanup/observer required')
    turns = result['case']['scenario']['turns']; observed = row['observed_turns']
    _require(0 < observed == len(result['observations']) <= len(turns) == row['defined_turns'], 'Quality turn counts mismatch')
    errors, issues = result['errors'], record['issues']
    _require(type(errors) is list and errors and all(type(e) is str and e for e in errors)
             and type(issues) is list and len(issues) == len(errors), 'Quality issue/error coverage required')
    origin = False
    for index, issue in enumerate(issues):
        _require(type(issue) is dict and set(issue) == {'code','turn','error_index','disposition'}, 'Typed quality issue required')
        _equal(issue['error_index'], index, 'Quality error index mismatch')
        _require(type(issue['turn']) is int and 1 <= issue['turn'] <= observed, 'Quality issue turn mismatch')
        if issue['code'] in _COLLECTION_QUALITY_CODES:
            _equal(issue['disposition'], 'continue_recorded', 'Quality continue disposition mismatch')
        else:
            _require(issue['code'] in _QUALITY_HALT_CODES | {'incomplete'} and issue['disposition'] == 'halt'
                     and issue['turn'] == observed, 'Unknown technical halt or earlier halt rejected')
            origin |= issue['code'] in _QUALITY_HALT_CODES
    _require(origin, 'Source-owned preview origin required')
    _equal(q['policy_sha256'], policy, 'Quality policy mismatch')
    for kind in ('candidate','cleanup'):
        _equal(q[kind+'_file'], f"quality-{row['ordinal']:06d}-{row['arm']}-{kind}.json", 'Canonical quality filename required')
        _equal(_sha(sidecars[q[kind+'_file']]), q[kind+'_sha256'], 'Quality raw sidecar SHA mismatch')
    candidate = _Document(sidecars[q['candidate_file']]).value
    cleanup = _Document(sidecars[q['cleanup_file']]).value
    _require(set(candidate) == {'policy_id','policy_sha256','source_sha256','rule_sha256','disposition',
                           'completion_proof','audit_only','evidence','unrun'}, 'Quality candidate fields mismatch')
    _equal(candidate['policy_id'], 'full-customer-owned-preview-candidate.v2', 'Quality candidate policy mismatch')
    _hex(candidate['policy_sha256'], _HEX64, 'Quality candidate policy digest required')
    _equal(candidate['disposition'], 'candidate_precleanup', 'Candidate disposition mismatch')
    _equal(candidate['completion_proof'], 'unavailable_from_recording', 'Candidate cannot self-certify completion')
    _equal(candidate['audit_only'], True, 'Candidate must remain audit-only data')
    _equal(candidate['source_sha256'], q['source_sha256'], 'Quality source digest linkage mismatch')
    _equal(candidate['rule_sha256'], q['rule_sha256'], 'Quality rule digest linkage mismatch')
    _equal(candidate['evidence']['record'], record, 'Candidate record mismatch')
    _equal(candidate['evidence']['turn'], observed, 'Candidate observed turn mismatch')
    _equal(len(candidate['unrun']), q['unrun_turns'], 'Candidate suffix mismatch')
    for index, item in enumerate(candidate['unrun'], observed):
        _require(set(item) == {'turn','rule','rule_sha256','reason'}, 'Candidate suffix fields mismatch')
        _equal(item['turn'], index+1, 'Candidate suffix turn mismatch')
        _equal(item['rule'], turns[index], 'Candidate source suffix rule mismatch')
        _hex(item['rule_sha256'], _HEX64, 'Suffix rule SHA required')
        _equal(item['reason'], 'quality_end_candidate_unrun', 'Candidate suffix reason mismatch')
    _require(set(cleanup) == {'post_cleanup','deleted','session_absent'}, 'Cleanup fields mismatch')
    _require(type(cleanup['deleted']) is int and cleanup['deleted'] == 1 and cleanup['session_absent'] is True,
             'Exactly one owned session cleanup required')
    _equal(cleanup['post_cleanup'], candidate['evidence']['after'], 'Cleanup capture changed')


def _quality_inputs(manifest_raw, receipt_raw, records, expected_hashes, evidence, epoch):
    """Authenticate raw checkpoint inputs without semantic review or summary."""
    for label, raw in (('manifest',manifest_raw), ('receipt',receipt_raw)):
        _require(type(raw) is bytes, 'Original raw quality input required')
        _equal(_sha(raw), _hex(expected_hashes[label+'_sha256'], _HEX64, 'Quality input pin required'), 'Quality input digest mismatch')
    document = _Document(manifest_raw); manifest = document.value; receipt = _Document(receipt_raw).value
    _equal(manifest['schema'], 'full-window-actual-ready.'+epoch, 'Matching quality manifest required')
    _equal(manifest['evidence_kind'], 'actual', 'Actual quality manifest required')
    scope = manifest['scope']
    _equal(scope['schema'], 'full-window-scope.'+epoch, 'Matching quality scope required')
    _score_cache_accounting(manifest, epoch)
    _equal(scope['bindings']['customer_revision'], '50e3c8dab14b4b6cc25322fd340a274e6970200b', 'Original customer revision required')
    _equal(scope['bindings']['policy_sha256'], '333c69bbb7506bd0c9237891cce8ddca720174974bf3f85753dd62339f368f15', 'Original owned quality policy required')
    _hex(manifest['archive_commit'], _HEX40, 'Archive commit required'); _hex(manifest['archive_tree'], _HEX40, 'Archive tree required')
    _equal(scope['bindings']['executor_revision'], manifest['archive_commit'], 'Quality executor revision mismatch')
    _equal(_sha(document.fragment('scope').encode()), manifest['scope_sha256'], 'Quality scope digest mismatch')
    _equal(manifest['knowledge_model'], 'off', 'Model knowledge must remain off')
    _equal(manifest['knowledge_jev'], 'off', 'JEV knowledge must remain off')
    _require(type(manifest['sources']) is dict and manifest['sources'], 'Quality frozen source inventory required')
    for name, digest in manifest['sources'].items():
        _require(type(name) is str and name and not name.startswith('/') and '..' not in name.split('/'), 'Invalid quality source path')
        _hex(digest, _HEX64, 'Quality source SHA required')
    _equal(receipt['schema'], 'full-window-acquisition-receipt.v1', 'Quality receipt schema mismatch')
    _equal(receipt['evidence_kind'], 'recorded_run', 'Recorded quality receipt required')
    _require(receipt['status'] in ('collected_window','stopped'), 'Quality receipt status mismatch')
    for key in ('scope_sha256','execution_sha256'):
        _equal(receipt[key], manifest[key], 'Quality receipt binding mismatch')
    _planned_rows(manifest, receipt)
    _require(type(evidence) is dict and set(evidence) == {'envelope_raw','sidecars'}, 'Explicit '+epoch+' evidence required')
    _equal(scope['bindings']['request_envelope_sha256'], manifest['envelope']['sha256'], 'Scope envelope binding mismatch')
    envelope = _Document(evidence['envelope_raw'], 8 << 20).value
    _equal(_sha(evidence['envelope_raw']), manifest['envelope']['sha256'], 'Envelope digest mismatch')
    basis = envelope['basis']; paths, digests = set(), set()
    _require(type(basis) is list and basis, 'Envelope basis required')
    for entry in basis:
        _require(type(entry) is dict and set(entry) == {'path','sha256'}, 'Typed envelope basis required')
        _require(type(entry['path']) is str and entry['path'] and entry['path'] not in paths, 'Duplicate envelope basis path')
        _hex(entry['sha256'], _HEX64, 'Envelope basis SHA required')
        _require(entry['sha256'] not in digests, 'Duplicate envelope basis digest')
        paths.add(entry['path']); digests.add(entry['sha256'])
    _require(scope['budget_approval_sha256'] in digests, 'Current budget approval missing from envelope')
    names = [q[k] for row in receipt['rows'] if (q := row.get('quality_end')) is not None for k in ('candidate_file','cleanup_file')]
    _require(type(evidence['sidecars']) is dict and len(names) == len(set(names)) and set(names) == set(evidence['sidecars']), 'Exact sidecar inventory required')
    required = [row['recording_file'] for row in receipt['rows'] if row['attempted']]
    _require(type(records) is dict and type(expected_hashes['records']) is dict and len(required) == len(set(required))
             and set(required) == set(records) == set(expected_hashes['records']), 'Exact quality record inventory required')
    quality_rows = []
    for row in receipt['rows']:
        if not row['attempted']:
            continue
        name = row['recording_file']
        _equal(name, f"arm-{row['ordinal']:06d}-{row['arm']}.json", 'Canonical quality recording filename required')
        _equal(_sha(records[name]), row['recording_sha256'], 'Quality receipt record digest mismatch')
        _equal(row['recording_sha256'], expected_hashes['records'][name], 'Quality external record digest mismatch')
        record = _Document(records[name]).value; result = record['result']
        _equal(row['case_key'], result['case']['source_case_id']+'/'+result['case']['scenario']['id'], 'Quality case key mismatch')
        _equal(len(result['case']['scenario']['turns']), row['defined_turns'], 'Quality defined turns mismatch')
        _equal(len(result['observations'] or []), row['observed_turns'], 'Quality observed turns mismatch')
        for field in ('status','structural_pass','success'):
            _equal(row[field], result[field], 'Quality receipt result state mismatch')
        _equal(row['disposition'], record['disposition'], 'Quality receipt disposition mismatch')
        for obs in result['observations'] or []:
            tm = obs['turn']['response']['telemetry']
            _equal(tm['langfuse_trace_id'], row['trace_id'], 'Quality trace mismatch')
            if 'runtime' in tm:
                _equal(tm['runtime']['routing_mode'], row['arm'], 'Quality runtime arm mismatch')
        if row.get('quality_end') is None:
            validate_completed_record_state(records[name])
        else:
            _quality_v6_record(row, records[name], evidence['sidecars'], scope['bindings']['policy_sha256'])
            quality_rows.append(row)
    binding = {'manifest_sha256': _sha(manifest_raw), 'receipt_sha256': _sha(receipt_raw),
               'source_inventory_sha256': _sha(_json_bytes(manifest['sources'])),
               'archive_commit': manifest['archive_commit'], 'archive_tree': manifest['archive_tree'],
               'helper_sha256': _QUALITY_V6_HELPER_SHA256 if epoch == 'v6' else _QUALITY_V7_HELPER_SHA256, 'envelope_sha256': _sha(evidence['envelope_raw']),
               'records': {name:_sha(raw) for name,raw in records.items()},
               'rows': [{key: row[key] for key in ('ordinal','pass','case_key','arm','recording_file','recording_sha256','quality_end')} for row in quality_rows]}
    return manifest, receipt, quality_rows, binding


def _quality_v6_inputs(manifest_raw, receipt_raw, records, expected_hashes, evidence):
    return _quality_inputs(manifest_raw, receipt_raw, records, expected_hashes, evidence, 'v6')


def _quality_v7_inputs(manifest_raw, receipt_raw, records, expected_hashes, evidence):
    return _quality_inputs(manifest_raw, receipt_raw, records, expected_hashes, evidence, 'v7')


def _build(manifest_raw, summary_raw, receipt_raw, records, review_raw, expected_hashes, epoch6_evidence=None, epoch7_evidence=None):
    _require(type(expected_hashes) is dict and type(records) is dict, 'Explicit artifact and record SHA anchors required')
    raw_inputs = {'manifest': manifest_raw, 'summary': summary_raw, 'receipt': receipt_raw, 'review': review_raw}
    documents = {}
    for name, raw in raw_inputs.items():
        expected = _hex(expected_hashes[name+'_sha256'], _HEX64, 'Explicit SHA anchor required: ' + name)
        _require(type(raw) is bytes and _sha(raw) == expected, 'Artifact SHA mismatch: ' + name)
        documents[name] = _Document(raw)
    manifest, summary, receipt, review = (documents[name].value for name in ('manifest', 'summary', 'receipt', 'review'))
    _require(manifest['schema'] in ('full-window-actual-ready.v2', 'full-window-actual-ready.v3', 'full-window-actual-ready.v4', 'full-window-actual-ready.v5', 'full-window-actual-ready.v6', 'full-window-actual-ready.v7')
             and manifest['evidence_kind'] == 'actual', 'Frozen actual v2/v3/v4/v5/v6/v7 manifest required')
    epoch = manifest['schema'].rsplit('.', 1)[1]
    _equal(manifest['scope']['schema'], 'full-window-scope.'+epoch, 'Manifest/scope epoch mismatch')
    cache_accounting = _score_cache_accounting(manifest, epoch)
    quality_binding = None
    _require(epoch7_evidence is None or epoch == 'v7', 'Epoch7 evidence forbidden for other epochs')
    if epoch == 'v7':
        _require(epoch6_evidence is None, 'Epoch6 evidence forbidden for epoch7')
        _, _, quality_rows, quality_binding = _quality_v7_inputs(manifest_raw, receipt_raw, records, expected_hashes, epoch7_evidence)
    elif epoch == 'v6':
        _, _, quality_rows, quality_binding = _quality_v6_inputs(manifest_raw, receipt_raw, records, expected_hashes, epoch6_evidence)
    else:
        _require(epoch6_evidence is None, 'Epoch6 evidence forbidden for legacy plans')
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
    if cache_accounting is not None:
        provenance['configured_cache_accounting'] = cache_accounting
    if epoch in ('v6', 'v7'):
        provenance.update({'epoch': epoch, 'budget_approval_sha256': manifest['scope']['budget_approval_sha256'],
                           'baseline_evidence_sha256': manifest['scope']['baseline_evidence_sha256'],
                           'historical_claims_sha256': _sha(_json_bytes(manifest['historical_claims'])),
                           'envelope_sha256': manifest['envelope']['sha256'],
                           'parent_budget_micro_cny': 130000000, 'parent_budget_micro_usd': 1000000})
        if quality_rows:
            provenance.update({'quality_'+epoch+'_source_replay': 'pending', 'quality_'+epoch+'_inputs_sha256': _sha(_json_bytes(quality_binding))})
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
        if epoch in ('v6', 'v7') and record['disposition'] == 'continue_recorded':
            validate_completed_record_state(records[name])
        elif epoch in ('v6', 'v7'):
            _require(row.get('quality_end') is not None, 'Owned quality evidence required')
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


def build_window_score_plan(manifest_raw, summary_raw, receipt_raw, records, review_raw, expected_hashes, *, epoch6_evidence=None, epoch7_evidence=None):
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
        return _build(manifest_raw, summary_raw, receipt_raw, records, review_raw, expected_hashes, epoch6_evidence, epoch7_evidence)
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
    descriptor = os.open(path, os.O_RDONLY | _O_NOFOLLOW)
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
    descriptor = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY | _O_NOFOLLOW, 0o600)
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


_CLOSED_P4_ROOT = Path('/Users/a1/项目/ai-gateway/.tmp/paired-live-private/full1220-actual-v4-freeze-20261029')
_CLOSED_ARCHIVE_EXTRA_POLICY = {
    'schema': 'full1220-closed-archive-extra-witness.v1', 'phase': 'post_stop_content_projection',
    'not_actual_ready': True,
    'manifest_sha256': 'fb48dd1465a452ffc2dfdb2029a107adfeb351f4c176892a071d07e18b95c0ea',
    'source_map_sha256': '5e7de1c0ed75706390e3625287270376d16d976cb6e3f310a9c0b033f1b472cf',
    'source_root': str(_CLOSED_P4_ROOT/'regression/source'), 'tracked_file_count': 2108,
    'extra_files': [{'path': 'scripts/__pycache__/full1220-window-scores.cpython-312.pyc',
                     'sha256': 'c16f5c7917e7ec91f154c828d596569af2ff24d7f2de24ad5ee23380abda49c4', 'size': 129686}],
    'extra_directories': ['scripts/__pycache__'],
    'owner_exit': {'path': str(_CLOSED_P4_ROOT/'actual-owner-exit.json'),
                   'sha256': '0015af89d829c625c3fef4b21219dd3a40682c54308f9426c7e1bc279f268c66'},
    'terminal': {'path': str(_CLOSED_P4_ROOT/'records/terminal.json'),
                 'sha256': '13520b9e02f814b7c7e3c9cb2ca8f40793714d2649c14dd9f2acfdc9886bbe29'},
    'independent_stop_audit': {'path': str(_CLOSED_P4_ROOT/'stopped-preflight-independent-audit.json'),
                              'sha256': '2818476f6f36d0cc86cef7e15ef3d16fe0cefce86eb4c17a0f7387a299fa3aac'}}


def _source_directories(names):
    return {parent.as_posix() for name in names for parent in Path(name).parents if parent != Path('.')}


def _verify_closed_archive_inventory(root, sources, extras):
    """Exact file AND directory inventory; never imports or decodes an extra."""
    _require(root.resolve() == root.absolute(), 'Closed archive root alias forbidden')
    extra_map = {}; identities = {}
    for item in extras:
        name = item['path']; relative = Path(name)
        _require(not relative.is_absolute() and relative.as_posix() == name and '..' not in relative.parts
                 and name != '.git' and name not in sources and name not in extra_map, 'Exact separate extra path required')
        _hex(item['sha256'], _HEX64, 'Pinned extra SHA required')
        _integer(item['size'], 1, 'Pinned positive extra size required')
        info = (root/name).lstat()
        _require(stat.S_ISREG(info.st_mode) and info.st_size == item['size'], 'Pinned regular extra size mismatch')
        identities[name] = [info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns, info.st_mode]
        extra_map[name] = item['sha256']
    git_pointer = _verify_frozen_sources(root, sources, extra_map, allow_root_git_pointer=True)
    directories = {str((Path(directory)/name).relative_to(root).as_posix())
                   for directory, names, _ in os.walk(root, followlinks=False) for name in names}
    _equal(sorted(directories), sorted(_source_directories([*sources, *extra_map])), 'Unexpected closed archive directory inventory')
    for name, expected in identities.items():
        info = (root/name).lstat()
        _equal([info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns, info.st_mode], expected,
               'Extra changed during inventory read')
    info = root.lstat()
    return {'git_pointer_sha256': git_pointer, 'source_directory_identity': [info.st_dev, info.st_ino],
            'extra_file_identities': identities}


def _validate_closed_archive_extra_witness(spec, manifest_raw, root):
    """One pinned P4 post-stop exception, solely for content staging; no actual capability."""
    _require(type(spec) is dict and set(spec) == {'path', 'sha256'}, 'Explicit witness path and SHA required')
    path = Path(spec['path']).absolute()
    _require(path.resolve() == path, 'Witness path alias forbidden')
    witness_raw = _regular_bytes(path, 16384)
    _equal(_sha(witness_raw), _hex(spec['sha256'], _HEX64, 'External witness SHA required'), 'Closed archive witness SHA mismatch')
    witness = _Document(witness_raw, 16384).value
    _equal(witness, _CLOSED_ARCHIVE_EXTRA_POLICY, 'Only the pinned stopped P4 archive witness is supported')
    manifest = _Document(manifest_raw).value
    _equal(_sha(manifest_raw), witness['manifest_sha256'], 'Closed witness manifest mismatch')
    _equal(_sha(_json_bytes(manifest['sources'])), witness['source_map_sha256'], 'Closed witness source map mismatch')
    _equal(len(manifest['sources']), witness['tracked_file_count'], 'Closed witness tracked count mismatch')
    _equal(str(root), witness['source_root'], 'Closed witness requires original declared root')
    _equal(manifest['source_root'], witness['source_root'], 'Manifest original source root mismatch')
    _equal(sorted(_source_directories([*manifest['sources'], *[x['path'] for x in witness['extra_files']]])
                  - _source_directories(manifest['sources'])), witness['extra_directories'], 'Closed extra directory policy mismatch')
    documents = {}
    for name in ('owner_exit', 'terminal', 'independent_stop_audit'):
        entry = witness[name]; evidence_path = Path(entry['path'])
        _require(evidence_path.resolve() == evidence_path, 'Stopped evidence path alias forbidden')
        raw = _regular_bytes(evidence_path, 16 << 20)
        _equal(_sha(raw), entry['sha256'], 'Pinned stopped evidence changed: '+name)
        documents[name] = _Document(raw).value
    _equal(documents['owner_exit']['exit_code'], 1, 'Closed witness requires exit1')
    _equal(documents['owner_exit']['relaunch_allowed'], False, 'Closed witness forbids relaunch')
    _equal(documents['terminal']['closed'], True, 'Closed terminal required')
    _equal(documents['terminal']['acquisition_done'], False, 'Stopped incomplete scope required')
    audit = documents['independent_stop_audit']
    _equal(audit['schema'], 'full1220-v4-stopped-preflight-independent-audit.v1', 'Independent stopped audit required')
    for key, expected in [('owner_exit_code', 1), ('closed', True), ('resume_relaunch_allowed', False)]:
        _equal(audit['stop'][key], expected, 'Independent stopped state mismatch')
    _equal(audit['source_inventory']['tracked_files'], 2108, 'Independent original inventory count mismatch')
    _equal(audit['source_inventory']['changed_files'], [], 'Independent original sources changed')
    _equal(audit['source_inventory']['extra_files'], [x['path'] for x in witness['extra_files']], 'Independent extra inventory mismatch')
    _equal(audit['pyc']['sha256'], witness['extra_files'][0]['sha256'], 'Independent extra SHA mismatch')
    _equal(audit['pyc']['size'], witness['extra_files'][0]['size'], 'Independent extra size mismatch')
    inventory = _verify_closed_archive_inventory(root, manifest['sources'], witness['extra_files'])
    return {'schema': 'full1220-closed-archive-projection-source.v1', 'phase': witness['phase'], 'not_actual_ready': True,
            'witness_path': str(path), 'witness_sha256': spec['sha256'], 'manifest_sha256': witness['manifest_sha256'],
            'source_map_sha256': witness['source_map_sha256'], 'source_root': str(root), 'tracked_file_count': 2108,
            'extra_files': witness['extra_files'], 'extra_directories': witness['extra_directories'],
            'owner_exit_sha256': witness['owner_exit']['sha256'], 'terminal_sha256': witness['terminal']['sha256'],
            'independent_stop_audit_sha256': witness['independent_stop_audit']['sha256'], 'inventory': inventory,
            'extras_copied_or_executed': False}


class _ContentProjection:
    """Opaque local provenance, deliberately not reconstructible from JSON."""
    __slots__ = ('_raw', '_raw_sha', '_plan_sha', '_job', '_seal', '_archive_provenance', '_archive_provenance_sha')

    def __init__(self, seal, raw, plan, job, archive_provenance=None):
        _require(seal is _PROJECTION_SEAL, 'Fixed helper execution proof required')
        provenance = _json_bytes(archive_provenance) if archive_provenance is not None else None
        for name, value in (('_raw', raw), ('_raw_sha', _sha(raw)), ('_plan_sha', _sha(_json_bytes(plan))),
                            ('_job', _json_bytes(job)), ('_seal', seal), ('_archive_provenance', provenance),
                            ('_archive_provenance_sha', _sha(provenance) if provenance is not None else None)):
            object.__setattr__(self, name, value)

    def __setattr__(self, name, value):
        raise AttributeError('Frozen helper proof is immutable')

    def as_dict(self):
        return _Document(self._raw).value

    def archive_source_provenance(self):
        if self._archive_provenance is None:
            return None
        _equal(_sha(self._archive_provenance), self._archive_provenance_sha, 'Closed archive proof provenance mutated')
        return _Document(self._archive_provenance).value


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
                           *, source_root, output_dir, runtime, execute=False, closed_archive_extra_witness=None, epoch6_evidence=None, epoch7_evidence=None):
    """Run the fixed no-network Go helper against an isolated frozen archive.

    Requires explicit execute=True (schedule this outside measurement windows),
    an absent output_dir whose parent exists, and runtime={go_executable,
    go_sha256,module_cache}. The executable is an externally trusted SHA pin,
    never PATH lookup; arguments, helper, SDK26.5 and environment are fixed.
    All manifest source files are rehashed before/after an isolated full copy;
    originals and staged records are revalidated after the command. No settings
    or credentials are read. Return an opaque proof; JSON alone cannot reload it.
    The optional explicit path/SHA witness supports only pinned stopped P4;
    its extra bytecode is retained outside staging and never executed. This
    source evidence does not establish actual readiness. Default inventory is strict.
    """
    try:
        _require(execute is True, 'Explicit fixed offline helper execution required')
        plan = build_window_score_plan(manifest_raw, summary_raw, receipt_raw, records, review_raw, expected_hashes, epoch6_evidence=epoch6_evidence, epoch7_evidence=epoch7_evidence)
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
        archive_provenance = None
        if closed_archive_extra_witness is not None:
            archive_provenance = _validate_closed_archive_extra_witness(closed_archive_extra_witness, manifest_raw, root)
        else:
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
        if closed_archive_extra_witness is not None:
            _equal(_validate_closed_archive_extra_witness(closed_archive_extra_witness, manifest_raw, root), archive_provenance,
                   'Closed archive witness/source/extra changed during helper execution')
        else:
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
        if archive_provenance is not None:
            archive_provenance['checked_before_and_after'] = True
            _exclusive_write(output/'archive-source-provenance.json', _json_bytes(archive_provenance))
        return _ContentProjection(_PROJECTION_SEAL, projected_raw, plan, job, archive_provenance)
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


def _validate_projection_plan_binding(plan, projection):
    """Authenticate the immutable plan seal without client or filesystem I/O."""
    _require(type(projection) is _ContentProjection and projection._seal is _PROJECTION_SEAL,
             'Live fixed helper projection proof required; plain JSON/SHA is insufficient')
    _equal(_sha(projection._raw), projection._raw_sha, 'Projection proof mutated')
    _equal(_sha(_json_bytes(plan)), projection._plan_sha, 'Projection belongs to another or modified plan')


def validate_window_traces(plan, projection, client):
    """Read each original trace once using client.observations(trace), never POST.

    Missing/delayed observations or IO remain unverified; a provenance, identity,
    parent, turn or delivered-content contradiction rejects the whole result.
    Only the opaque fixed-helper proof is accepted. Native LLM payload capture
    and actual use of included content are never inferred from agent.turn IO.
    This does not consume or clear the prior receipt partition chain gate.
    """
    try:
        _validate_projection_plan_binding(plan, projection)
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
        result = {'schema': 'full1220-window-content-validation.v1', 'all_pass': all_pass, 'publish_ready': False,
                'verified_traces': len(summaries), 'expected_traces': len(plan['trace_bindings']),
                'verified_observations': sum(row['observations'] for row in summaries),
                'verified_turns': sum(row['turns'] for row in summaries), 'summaries': summaries, 'unverified': pending,
                'pending_gates': gates, 'projection_sha256': projection._raw_sha,
                'helper_sha256': _PROJECTION_HELPER_SHA256, 'content_source_sha256': _CONTENT_SOURCE_SHA256,
                'plan_sha256': projection._plan_sha, 'new_external_requests_from_projection': 0,
                'new_model_calls': 0, 'new_jev_calls': 0, 'claims_created': 0, 'scores_published': 0,
                'native_llm_payload_io_claimed': False, 'included_does_not_prove_used': True}
        archive_provenance = projection.archive_source_provenance()
        if archive_provenance is not None:
            _equal(archive_provenance['manifest_sha256'], plan['provenance']['manifest_sha256'], 'Closed source proof manifest mismatch')
            _require(archive_provenance['not_actual_ready'] is True and archive_provenance['checked_before_and_after'] is True,
                     'Complete post-stop source proof required')
            result['archive_source_provenance'] = archive_provenance
        return result
    except (KeyError, IndexError, TypeError, UnicodeError) as error:
        raise ValueError('Invalid captured trace evidence: '+type(error).__name__) from error


# This registry is intentionally specific to this local project. No command,
# review, output directory, copied script, or manifest may override its root.
# The design cannot resist an owner maliciously deleting all disk evidence.
_PUBLICATION_REGISTRY_ROOT = Path('/Users/a1/项目/ai-gateway/.tmp/full1220-score-publications')
_PROJECT_ROOT = Path('/Users/a1/项目/ai-gateway')
_ATTESTATION_HELPER_SHA256 = 'cadae5ad17b7233b97ba9d6d24029365e10dc280223b4991987d6ca31ea58b63'
_ATTESTATION_V6_HELPER_SHA256 = 'c4c9b998d7b549b2cebe598e6ef87de0aa6534ea510a9d7c4a766db40ba36b92'
_ATTESTATION_V7_HELPER_SHA256 = '2bb44f9fed73daf666348b05750c239142664756bb3de5a950930371ebaa49c4'
_PUBLICATION_SEAL = object()


class _PublicationProof:
    __slots__ = ('_kind', '_plan_sha', '_raw', '_seal')

    def __init__(self, seal, kind, data, plan=None):
        _require(seal is _PUBLICATION_SEAL, 'Verified gate proof required')
        for key, value in (('_kind', kind), ('_plan_sha', _sha(_json_bytes(plan)) if plan is not None else None),
                           ('_raw', _json_bytes(data)), ('_seal', seal)):
            object.__setattr__(self, key, value)

    def __setattr__(self, name, value):
        raise AttributeError('Immutable verified gate proof')

    def as_dict(self):
        return _Document(self._raw).value


def _quality_v6_source_identity(root, manifest):
    pointer_sha = _verify_frozen_sources(root, manifest['sources'], allow_root_git_pointer=True)
    _require(pointer_sha is not None, 'Quality source requires original Git identity pointer')
    pointer = _regular_bytes(root/'.git', 4096).decode()
    gitdir = pointer[len('gitdir: '):-1]
    environment = {'PATH':'/usr/bin:/bin', 'TMPDIR':'/private/tmp', 'GIT_CONFIG_NOSYSTEM':'1', 'GIT_CONFIG_GLOBAL':'/dev/null',
                   'GIT_NO_REPLACE_OBJECTS':'1', 'GIT_OPTIONAL_LOCKS':'0'}
    def git(*args):
        result = subprocess.run(['/usr/bin/git','--git-dir='+gitdir,*args], env=environment,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30, check=False)
        _require(result.returncode == 0 and len(result.stdout) <= 8 << 20 and not result.stderr, 'Quality Git identity unavailable')
        return result.stdout
    _equal(git('rev-parse',manifest['archive_commit']+'^{tree}').decode().strip(), manifest['archive_tree'], 'Quality archive tree mismatch')
    entries = git('ls-tree','-r','-z','--full-tree',manifest['archive_commit'])
    names = set()
    for entry in entries.split(b'\0'):
        if not entry: continue
        header, raw_name = entry.split(b'\t',1); mode, kind, sha = header.split(b' ')
        name = raw_name.decode('utf-8'); names.add(name)
        _require(mode in (b'100644',b'100755') and kind == b'blob' and name in manifest['sources'], 'Quality Git inventory mismatch')
        raw = _regular_bytes(root/name)
        _equal(hashlib.sha1(b'blob '+str(len(raw)).encode()+b'\0'+raw).hexdigest(), sha.decode(), 'Quality source differs from Git blob')
    _require(names == set(manifest['sources']), 'Quality Git/source inventories differ')
    _equal(sorted(_source_directories(manifest['sources'])), sorted(str(Path(directory).relative_to(root)) for directory,_,_ in os.walk(root) if Path(directory) != root), 'Quality source directory inventory mismatch')
    return {'git_pointer_sha256':pointer_sha, 'tree_listing_sha256':_sha(entries)}


def _verify_window_quality(manifest_raw, receipt_raw, records, expected_hashes, *, evidence, epoch,
                           source_root, output_dir, runtime, execute=False):
    """Replay source-owned quality offline, before semantic review/checkpoint.

    Raw pins cover manifest, receipt, records and quality_helper_sha256. Runtime
    pins Go and the fixed SDK's SDKSettings.json metadata (not the whole SDK).
    The fixed helper runs only in a new authenticated source copy. No-quality
    windows return None without a helper, output directory or runtime access.
    This proves evidence replay, not actual execution, native IO or semantics.
    """
    try:
        _require(execute is True, 'Explicit fixed offline quality helper execution required')
        inputs = _quality_v6_inputs if epoch == 'v6' else _quality_v7_inputs
        manifest, receipt, rows, binding = inputs(manifest_raw, receipt_raw, records, expected_hashes, evidence)
        if not rows:
            return None
        helper_sha = _QUALITY_V6_HELPER_SHA256 if epoch == 'v6' else _QUALITY_V7_HELPER_SHA256
        helper_relative = _QUALITY_V6_HELPER_RELATIVE if epoch == 'v6' else _QUALITY_V7_HELPER_RELATIVE
        proof_kind = 'quality_'+epoch
        _equal(expected_hashes['quality_helper_sha256'], helper_sha, 'Externally pinned quality helper required')
        helper_path = Path(__file__).resolve().parent/('helpers/full1220_quality_'+epoch+'_test.go.txt')
        helper = _regular_bytes(helper_path, 1 << 20)
        _equal(_sha(helper), helper_sha, 'Fixed quality helper source changed')
        root = Path(source_root).absolute()
        _equal(str(root.resolve()), str(Path(manifest['source_root']).resolve()), 'Quality source root mismatch')
        source_identity = _quality_v6_source_identity(root, manifest)
        executable = Path(runtime['go_executable']).resolve()
        _equal(_sha(_regular_bytes(executable)), _hex(runtime['go_sha256'], _HEX64, 'Pinned quality Go SHA required'), 'Quality Go executable SHA mismatch')
        _require(os.access(executable, os.X_OK), 'Quality Go executable unavailable')
        sdk = Path(_PROJECTION_SDKROOT)
        _require(sdk.is_dir() and sdk.name == 'MacOSX26.5.sdk', 'Fixed quality SDK required')
        sdk_sha = _hex(runtime['sdk_settings_sha256'], _HEX64, 'Pinned SDKSettings metadata SHA required')
        _equal(_sha(_regular_bytes(sdk/'SDKSettings.json',1 << 20)), sdk_sha, 'SDKSettings metadata SHA mismatch')
        module_cache = Path(runtime['module_cache']).resolve()
        _require(module_cache.is_dir(), 'Explicit existing offline module cache required')
        output = Path(output_dir).absolute()
        _require(output.parent.is_dir() and not output.exists() and not output.is_symlink(), 'Absent isolated quality output required')
        _require(str(output) == str(output.resolve()), 'Unaliased quality output required')
        _require(root.resolve() not in output.parents and output not in root.resolve().parents and output != root.resolve(), 'Quality output must be separate from frozen source')
        output.mkdir(mode=0o700); staged = output/'source'; staged.mkdir(mode=0o700)
        for name, digest in manifest['sources'].items():
            raw = _regular_bytes(root/name); _equal(_sha(raw), digest, 'Quality source changed while staging')
            target = staged/name; target.parent.mkdir(parents=True,exist_ok=True); _exclusive_write(target,raw)
        _require(helper_relative not in manifest['sources'], 'Quality helper destination already present')
        _exclusive_write(staged/helper_relative,helper)
        extra = {helper_relative:helper_sha}
        _verify_frozen_sources(staged,manifest['sources'],extra)
        staged_records = output/'records'; staged_records.mkdir(mode=0o700)
        all_raw = {**records,**evidence['sidecars']}
        _require(len(all_raw) == len(records)+len(evidence['sidecars']), 'Quality sidecar/record alias')
        for name, raw in all_raw.items(): _exclusive_write(staged_records/name,raw)
        input_raw = _json_bytes({'scope':manifest['scope'],'rows':rows,'binding':binding})
        _require(len(input_raw) <= 16 << 20, 'Quality helper input bound exceeded')
        _exclusive_write(output/'input.json',input_raw)
        for name in ('cache','tmp'): (output/name).mkdir(mode=0o700)
        environment = {'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','TMPDIR':str(output/'tmp'),'GOENV':'off','GOWORK':'off','GOFLAGS':'',
            'GOTOOLCHAIN':'local','GOPROXY':'off','GOSUMDB':'off','GO111MODULE':'on',
            'GOCACHE':str(output/'cache'),'GOTMPDIR':str(output/'tmp'),'GOMODCACHE':str(module_cache),
            'SDKROOT':str(sdk),'CGO_ENABLED':'1','CC':'/usr/bin/clang','CXX':'/usr/bin/clang++',
            'CGO_CFLAGS':'-isysroot '+str(sdk),'CGO_LDFLAGS':'-isysroot '+str(sdk),
            'FULL1220_QUALITY_INPUT':str(output/'input.json'), 'FULL1220_QUALITY_OUTPUT':str(output/'quality.json'),
            'FULL1220_QUALITY_INPUT_SHA256':_sha(input_raw)}
        command = [str(executable),'test','-mod=readonly','-buildvcs=false','-v','./internal/taskbench',
                   '-run','^TestFull1220QualityV'+epoch[1:]+'$','-count=1','-timeout=2m']
        process = subprocess.run(command,cwd=str(staged),env=environment,stdout=subprocess.PIPE,
                                 stderr=subprocess.PIPE,timeout=150,check=False)
        _require(len(process.stdout) <= 8 << 20 and len(process.stderr) <= 8 << 20, 'Quality helper output bound exceeded')
        _exclusive_write(output/'helper.stdout',process.stdout); _exclusive_write(output/'helper.stderr',process.stderr)
        _require(process.returncode == 0, 'Fixed quality helper failed; no proof')
        _equal(_quality_v6_source_identity(root,manifest),source_identity,'Quality source/Git identity changed')
        _verify_frozen_sources(staged,manifest['sources'],extra)
        _equal(_sha(_regular_bytes(helper_path,1 << 20)),helper_sha,'Quality helper changed')
        _equal(_sha(_regular_bytes(executable)),runtime['go_sha256'],'Quality executable changed')
        _equal(_sha(_regular_bytes(sdk/'SDKSettings.json',1 << 20)),sdk_sha,'SDKSettings metadata changed')
        _require(_regular_bytes(output/'input.json') == input_raw,'Quality helper input changed')
        for name, raw in all_raw.items():
            _require(_regular_bytes(staged_records/name,16 << 20) == raw,'Quality evidence changed in helper')
        _require(set(path.name for path in staged_records.iterdir()) == set(all_raw),'Quality evidence inventory changed')
        output_raw = _regular_bytes(output/'quality.json',16 << 20)
        receipts = re.findall(rb'(?m)^\s+'+re.escape(Path(helper_relative).name.encode())+rb':[0-9]+: FULL1220_QUALITY_OUTPUT_SHA256=([0-9a-f]{64})\r?$',process.stdout)
        _require(len(receipts) == 1 and receipts[0].decode() == _sha(output_raw),'Quality output lacks exact process receipt')
        _equal(_Document(output_raw).value,{'schema':'full1220-quality-'+epoch+'-replay.v1','input_sha256':_sha(input_raw),'binding':binding},'Quality helper output binding mismatch')
        return _PublicationProof(_PUBLICATION_SEAL,proof_kind,{
            **binding,proof_kind+'_inputs_sha256':_sha(_json_bytes(binding)), 'source_identity':source_identity,
            'output_sha256':_sha(output_raw),'go_sha256':runtime['go_sha256'],'sdk_settings_sha256':sdk_sha})
    except (KeyError, IndexError, TypeError, OSError, UnicodeError, subprocess.SubprocessError) as error:
        raise ValueError('Offline quality proof unavailable: '+type(error).__name__) from error


def verify_window_quality_v6(manifest_raw, receipt_raw, records, expected_hashes, *, epoch6_evidence,
                             source_root, output_dir, runtime, execute=False):
    """Original v6 API and proof encoding; no-quality windows touch no runtime/files."""
    return _verify_window_quality(manifest_raw, receipt_raw, records, expected_hashes,
        evidence=epoch6_evidence, epoch='v6', source_root=source_root, output_dir=output_dir,
        runtime=runtime, execute=execute)


def verify_window_quality_v7(manifest_raw, receipt_raw, records, expected_hashes, *, epoch7_evidence,
                             source_root, output_dir, runtime, execute=False):
    """Independent v7 proof from exact offline Go replay; JSON cannot restore it."""
    return _verify_window_quality(manifest_raw, receipt_raw, records, expected_hashes,
        evidence=epoch7_evidence, epoch='v7', source_root=source_root, output_dir=output_dir,
        runtime=runtime, execute=execute)


def verify_window_receipt_chain(plan, manifest_raw, entries, expected_hashes):
    """Recompute the complete raw prior-summary → summary → receipt chain.

    entries and SHA pins explicitly contain windows1..current, including the
    current partial/stopped window. Only prior windows must have completed.
    No records, directories, historical claims or original files are modified.
    """
    try:
        manifest = _Document(manifest_raw).value
        _equal(_sha(manifest_raw), plan['provenance']['manifest_sha256'], 'Chain manifest mismatch')
        _require(type(entries) is list and type(expected_hashes) is list
                 and len(entries) == len(expected_hashes) == plan['window_index'], 'Complete original prefix chain required')
        totals = {'attempted_arms': 0, 'finished_arms': 0, 'observed_turns': 0, 'quality_end_unrun_turns': 0}
        previous = ''; inventory = []
        for index, (entry, pins) in enumerate(zip(entries, expected_hashes), 1):
            raw_summary, raw_receipt = entry['summary_raw'], entry['receipt_raw']
            _require(type(raw_summary) is bytes and type(raw_receipt) is bytes, 'Original chain bytes required')
            for name, raw in [('summary', raw_summary), ('receipt', raw_receipt)]:
                _equal(_sha(raw), _hex(pins[name+'_sha256'], _HEX64, 'External chain SHA pin required'), 'Original chain SHA mismatch')
            summary, receipt = _Document(raw_summary).value, _Document(raw_receipt).value
            _equal(receipt['schema'], 'full-window-acquisition-receipt.v1', 'Original receipt schema required')
            _equal(receipt['evidence_kind'], 'recorded_run', 'Original recorded receipt required')
            _equal(receipt['window_index'], index, 'Contiguous prefix windows required')
            for key in ('scope_sha256', 'execution_sha256'):
                _equal(receipt[key], manifest[key], 'Prefix scope/execution mismatch')
            _planned_rows(manifest, receipt)
            _validate_summary(summary, receipt, pins['receipt_sha256'], manifest)
            _equal(summary['last_receipt_sha256'], previous, 'Original prior-summary SHA chain mismatch')
            if index < len(entries):
                _require(receipt['status'] == 'collected_window' and receipt['finished_arms'] == receipt['planned_current_arms']
                         and receipt['attempted_arms'] == receipt['planned_current_arms']
                         and all(receipt[key] is True for key in ('cleanup', 'environment_cleanup', 'observer', 'advanced', 'payload_verified'))
                         and summary['closed'] is False, 'Only completed prior windows may form a prefix')
            for key in ('attempted_arms', 'finished_arms', 'observed_turns'):
                totals[key] += receipt[key]
            totals['quality_end_unrun_turns'] += sum((row.get('quality_end') or {}).get('unrun_turns', 0) for row in receipt['rows'])
            exact = {**totals, 'failed_attempted_arms': totals['attempted_arms']-totals['finished_arms'],
                     'unattempted_arms': 7320-totals['attempted_arms'],
                     'global_unrun_turns': 11772-totals['observed_turns']-totals['quality_end_unrun_turns']}
            for key, value in exact.items():
                _equal(summary[key], value, 'Original cumulative component mismatch: '+key)
            previous = pins['summary_sha256']; inventory.append(dict(pins))
        _equal(previous, plan['provenance']['summary_sha256'], 'Chain must end at exact planned summary')
        _equal(expected_hashes[-1]['receipt_sha256'], plan['provenance']['receipt_sha256'], 'Chain current receipt mismatch')
        return _PublicationProof(_PUBLICATION_SEAL, 'receipt_chain', {'manifest_sha256': _sha(manifest_raw),
            'inventory': inventory, 'chain_sha256': _sha(_json_bytes(inventory)), 'cumulative': exact}, plan)
    except (KeyError, IndexError, TypeError, UnicodeError) as error:
        raise ValueError('Invalid original receipt chain: '+type(error).__name__) from error


def verify_window_attestation(manifest_raw, attestation_raw, attestation_sha256, binary_path, *, runtime, output_dir, execute=False):
    """Fixed stdlib Ed25519 verifier; never invoke the consumed actual binary."""
    try:
        _require(execute is True, 'Explicit fixed offline signature helper execution required')
        manifest, attestation = _Document(manifest_raw).value, _Document(attestation_raw, 65536).value
        _equal(_sha(attestation_raw), _hex(attestation_sha256, _HEX64, 'Pinned attestation SHA required'), 'Attestation SHA mismatch')
        binding = {key: attestation[key] for key in ('schema', 'archive_commit', 'archive_tree', 'ready_manifest_sha256', 'binary_sha256')}
        _equal(binding['schema'], 'full-window-actual-binary-attestation.v1', 'Original binary attestation schema required')
        for key in ('archive_commit', 'archive_tree'):
            _equal(binding[key], manifest[key], 'Attested archive mismatch')
        _equal(binding['ready_manifest_sha256'], _sha(manifest_raw), 'Attested manifest mismatch')
        _hex(manifest['build_attestation_public_key'], _HEX64, 'Frozen public key required')
        _require(type(attestation['signature']) is str and re.fullmatch(r'[0-9a-f]{128}', attestation['signature']), 'Original Ed25519 signature required')
        binary = Path(binary_path).absolute()
        _require(str(binary.resolve()) == str(binary), 'Unaliased actual binary path required')
        _equal(_sha(_regular_bytes(binary, 1 << 30)), _hex(binding['binary_sha256'], _HEX64, 'Actual binary SHA required'), 'Actual binary SHA mismatch')
        if manifest['schema'] == 'full-window-actual-ready.v7':
            _equal(manifest['scope']['schema'], 'full-window-scope.v7', 'Manifest/scope epoch mismatch')
            helper_name, helper_sha = 'full1220_attestation_v7.go.txt', _ATTESTATION_V7_HELPER_SHA256
        elif manifest['schema'] == 'full-window-actual-ready.v6':
            _equal(manifest['scope']['schema'], 'full-window-scope.v6', 'Manifest/scope epoch mismatch')
            helper_name, helper_sha = 'full1220_attestation_v6.go.txt', _ATTESTATION_V6_HELPER_SHA256
        else:
            helper_name, helper_sha = 'full1220_attestation.go.txt', _ATTESTATION_HELPER_SHA256
        helper = _regular_bytes(Path(__file__).resolve().parent/'helpers'/helper_name, 1 << 20)
        _equal(_sha(helper), helper_sha, 'Fixed Ed25519 helper changed')
        executable = Path(runtime['go_executable']).resolve()
        _equal(_sha(_regular_bytes(executable)), _hex(runtime['go_sha256'], _HEX64, 'Pinned Go executable required'), 'Go executable SHA mismatch')
        sdk = Path(_PROJECTION_SDKROOT)
        _require(sdk.is_dir() and sdk.name == 'MacOSX26.5.sdk', 'Fixed SDK26.5 required')
        output = Path(output_dir).absolute()
        _require(output.parent.is_dir() and not output.exists() and not output.is_symlink(), 'New isolated signature output required')
        output.mkdir(mode=0o700)
        _exclusive_write(output/'full1220_attestation.go', helper); (output/'cache').mkdir(mode=0o700); (output/'tmp').mkdir(mode=0o700)
        job = {'manifest_raw': base64.b64encode(manifest_raw).decode(), 'attestation_raw': base64.b64encode(attestation_raw).decode(),
               'manifest_sha256': _sha(manifest_raw), 'attestation_sha256': attestation_sha256, 'binary_path': str(binary)}
        env = {'PATH': '/usr/bin:/bin:/usr/sbin:/sbin', 'GOENV': 'off', 'GOWORK': 'off', 'GOFLAGS': '',
               'GOTOOLCHAIN': 'local', 'GOPROXY': 'off', 'GOSUMDB': 'off', 'GO111MODULE': 'off',
               'GOCACHE': str(output/'cache'), 'GOTMPDIR': str(output/'tmp'), 'SDKROOT': str(sdk), 'CGO_ENABLED': '0'}
        result = subprocess.run([str(executable), 'run', str(output/'full1220_attestation.go')], input=_json_bytes(job),
                                cwd=str(output), env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=150, check=False)
        _exclusive_write(output/'helper.stdout', result.stdout); _exclusive_write(output/'helper.stderr', result.stderr)
        _require(result.returncode == 0, 'Original Ed25519 verification rejected')
        data = _Document(result.stdout, 65536).value
        expected = {'schema': 'full1220-attestation-verification.v1', 'manifest_sha256': _sha(manifest_raw),
                    'attestation_sha256': attestation_sha256, 'binary_sha256': binding['binary_sha256'],
                    'canonical_binding_sha256': _sha(('{'+','.join(_string(key)+':'+_string(value) for key, value in binding.items())+'}').encode()),
                    'signature_verified': True}
        _equal(data, expected, 'Signature helper binding mismatch')
        _equal(_regular_bytes(output/'full1220_attestation.go', 1 << 20).decode(), helper.decode(), 'Signature helper changed')
        _equal(_sha(_regular_bytes(executable)), runtime['go_sha256'], 'Go executable changed')
        _equal(_sha(_regular_bytes(binary, 1 << 30)), binding['binary_sha256'], 'Actual binary changed during verification')
        return _PublicationProof(_PUBLICATION_SEAL, 'attestation', {**expected, 'binary_path': str(binary),
                                'helper_sha256': helper_sha})
    except (KeyError, IndexError, TypeError, OSError, UnicodeError, subprocess.SubprocessError) as error:
        raise ValueError('Offline attestation unavailable: '+type(error).__name__) from error


def _old_langfuse_client_class(manifest):
    path = _PROJECT_ROOT/'scripts/langfuse-eval.py'
    _equal(_sha(_regular_bytes(path, 1 << 20)), manifest['sources']['scripts/langfuse-eval.py'], 'Frozen restricted client changed')
    spec = importlib.util.spec_from_file_location('full1220_frozen_local_client', path)
    module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
    return module.Client


class _WindowScoreClient:
    __slots__ = ('_inner', '_manifest_sha', '_seal')
    def __init__(self, seal, inner, manifest_sha):
        _require(seal is _PUBLICATION_SEAL, 'Pinned local evaluation client required')
        self._inner = inner; self._manifest_sha = manifest_sha; self._seal = seal
    def observations(self, trace):
        return self._inner.observations(trace)
    def post_score(self, score):
        return self._inner.request('/api/public/scores', score)
    def verify_scores(self, scores):
        return self._inner.verify_scores(scores)


def load_window_score_client(manifest_raw, settings_path):
    """Only explicit owner0600 evaluation-only JSON; never load model settings."""
    try:
        info = Path(settings_path).lstat()
        _require(stat.S_ISREG(info.st_mode) and _owned_private(info, 0o600), 'Private owner0600 evaluation settings required')
        settings = _Document(_regular_bytes(settings_path, 16384)).value
        _require(set(settings) == {'OTEL_EXPORTER_OTLP_TRACES_ENDPOINT', 'AGENT_LANGFUSE_ENVIRONMENT', 'LANGFUSE_PUBLIC_KEY', 'LANGFUSE_SECRET_KEY'},
                 'Evaluation-only client configuration required')
        manifest = _Document(manifest_raw).value
        _equal(_sha(settings['OTEL_EXPORTER_OTLP_TRACES_ENDPOINT'].encode()), manifest['observer']['endpoint_sha256'], 'Frozen observer endpoint mismatch')
        _equal(_sha(settings['LANGFUSE_PUBLIC_KEY'].encode()), manifest['observer']['project_sha256'], 'Frozen observer project mismatch')
        _equal(settings['AGENT_LANGFUSE_ENVIRONMENT'], 'evaluation', 'Evaluation client required')
        return _WindowScoreClient(_PUBLICATION_SEAL, _old_langfuse_client_class(manifest)(settings), _sha(manifest_raw))
    except (KeyError, TypeError, OSError, UnicodeError) as error:
        raise ValueError('Invalid evaluation client settings: '+type(error).__name__) from error


def _private_directory(path):
    info = path.lstat()
    _require(stat.S_ISDIR(info.st_mode) and _owned_private(info, 0o700),
             'Private owner0700 registry directory required')
    _require(path.resolve() == path.absolute(), 'Registry path must have no symlink aliases')
    return info


def _sync_directory(path):
    descriptor = _open_directory(path)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


class _RegistryHandle:
    def __init__(self, root, scope, root_fd, scope_fd, lock_fd, anchor):
        self.root = root; self.scope = scope; self.root_fd = root_fd; self.scope_fd = scope_fd
        self.lock_fd = lock_fd; self.anchor = anchor
    def check(self):
        root_info, scope_info = _private_directory(self.root), _private_directory(self.scope)
        _require(os.path.samestat(root_info, os.fstat(self.root_fd)) and os.path.samestat(scope_info, os.fstat(self.scope_fd)), 'Publication registry directory moved or replaced')
        _equal([scope_info.st_dev, scope_info.st_ino], self.anchor['directory_identity'], 'Fixed scope directory identity changed')
        _equal(_Document(_regular_bytes(self.root/(self.scope.name+'.anchor.json'), 16384)).value, self.anchor, 'Fixed scope anchor changed')
        _equal(_Document(_regular_bytes(self.scope/'scope.json', 16384)).value['nonce'], self.anchor['nonce'], 'Fixed scope nonce changed')
    def close(self):
        for descriptor in (self.scope_fd, self.lock_fd, self.root_fd):
            os.close(descriptor)


def _open_publication_registry(scope, *, create=False):
    _hex(scope, _HEX64, 'Scope registry identity required')
    root = Path(_PUBLICATION_REGISTRY_ROOT).absolute()
    if not root.exists():
        _require(not root.is_symlink(), 'Registry alias forbidden')
        if not create:
            return None
        root.mkdir(mode=0o700); _sync_directory(root.parent)
    _private_directory(root)
    root_fd = _open_directory(root)
    lock_fd = scope_fd = None
    try:
        flags = os.O_RDWR | (os.O_CREAT if create else 0)
        lock_fd = _open_at(root_fd, '.lock', root/'.lock', flags, 0o600)
        lock_info = os.fstat(lock_fd)
        _require(stat.S_ISREG(lock_info.st_mode) and _owned_private(lock_info, 0o600), 'Private stable registry lock required')
        _lock_exclusive_nonblocking(lock_fd)
        directory = root/scope; anchor_path = root/(scope+'.anchor.json')
        if not anchor_path.exists():
            if not create:
                _require(not directory.exists(), 'Orphan publication directory')
                os.close(lock_fd); os.close(root_fd); return None
            _require(not directory.exists() and not directory.is_symlink(), 'Unanchored publication directory cannot be adopted')
            directory.mkdir(mode=0o700)
            info = _private_directory(directory)
            anchor = {'schema': 'full1220-fixed-scope-registry.v1', 'scope_sha256': scope,
                      'directory_identity': [info.st_dev, info.st_ino], 'nonce': os.urandom(16).hex()}
            _exclusive_write(directory/'scope.json', _json_bytes({'nonce': anchor['nonce']})); _sync_directory(directory)
            _exclusive_write(anchor_path, _json_bytes(anchor)); _sync_directory(root)
        anchor = _Document(_regular_bytes(anchor_path, 16384)).value
        _equal(anchor['schema'], 'full1220-fixed-scope-registry.v1', 'Scope anchor schema mismatch')
        _equal(anchor['scope_sha256'], scope, 'Scope anchor mismatch')
        scope_fd = _open_directory(directory)
        handle = _RegistryHandle(root, directory, root_fd, scope_fd, lock_fd, anchor); handle.check()
        return handle
    except BaseException:
        for descriptor in (scope_fd, lock_fd, root_fd):
            if descriptor is not None:
                os.close(descriptor)
        raise


def _publication_event(handle, name, event):
    handle.check()
    _require(re.fullmatch(r'[a-z0-9.-]+', name) is not None, 'Fixed event filename required')
    descriptor = _open_at(handle.scope_fd, name, handle.scope/name, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(descriptor, 'wb') as stream:
        stream.write(_json_bytes(event)); stream.flush(); os.fsync(stream.fileno())
    os.fsync(handle.scope_fd); handle.check()


def _publication_scores(plan):
    _equal(plan['schema'], 'full1220-window-score-plan.v1', 'Original full-window plan required')
    result = []; logicals = set(); ids = set()
    for score in plan['scores']:
        metadata = score['metadata']; trace = score['traceId']
        _require(trace in plan['trace_bindings'] and score['name'] in [NAMESPACE+name for name in ('original_status', 'semantic_task', 'strict_task')], 'Unknown score namespace or trace')
        logical = {'scope_sha256': plan['provenance']['scope_sha256'], 'window_id': plan['window_id'], 'pass': metadata['pass'],
                   'ordinal': metadata['ordinal'], 'arm': metadata['arm'], 'trace_id': trace, 'metric': score['name']}
        raw = _json_bytes(logical).decode()
        _equal(score['logical_identity'], raw, 'Stable logical identity mismatch')
        _equal(score['id'], _sha(raw.encode()), 'Stable score ID cannot be replaced')
        _require(raw not in logicals and score['id'] not in ids, 'Duplicate logical score or score ID')
        _require(not (score['value'] == 'passed' and (metadata['original_status'] in ('failed', 'setup_failed')
                     or metadata['original_structural_pass'] is not True)), 'Original negative cannot publish a passing score')
        logicals.add(raw); ids.add(score['id']); result.append(score)
    return result


def _gate_data(proof, kind, plan):
    _require(type(proof) is _PublicationProof and proof._seal is _PUBLICATION_SEAL and proof._kind == kind, 'Complete verified '+kind+' gate required')
    if proof._plan_sha is not None:
        _equal(proof._plan_sha, _sha(_json_bytes(plan)), 'Gate proof belongs to a different plan')
    data = proof.as_dict()
    _equal(data['manifest_sha256'], plan['provenance']['manifest_sha256'], 'Gate manifest mismatch')
    return data


def _batch_source_identity(plan):
    return {trace: {key: binding[key] for key in ('ordinal', 'pass', 'case_key', 'arm', 'recording_sha256',
                                                  'expected_defined_turns', 'expected_observed_turns')}
            for trace, binding in plan['trace_bindings'].items()}


def publish_window_scores(plan, projection, receipt_chain, attestation, client, *, execute=False, quality_proof=None):
    """One POST per durable intent, fail closed; uncertainty permits verify only.

    Stable logical intent is fsynced before score-ID intent. All scores are
    reserved before any POST; all traces/gates pass first. No inference, claim,
    automatic retry, mutable receipt overwrite, or caller publication-root option.
    """
    handle = None
    try:
        _require(execute is True, 'Explicit --execute required for score publication')
        quality = None
        epoch = plan['provenance'].get('epoch')
        if epoch in ('v6', 'v7') and any(row.get('quality_end') is not None for row in plan['rows']):
            proof_kind = 'quality_'+epoch
            _require(type(quality_proof) is _PublicationProof, 'Opaque '+proof_kind+' proof required')
            quality = _gate_data(quality_proof, proof_kind, plan)
            _equal(quality['receipt_sha256'], plan['provenance']['receipt_sha256'], 'Quality proof receipt mismatch')
            _equal(quality[proof_kind+'_inputs_sha256'], plan['provenance'][proof_kind+'_inputs_sha256'], 'Quality proof input/source mismatch')
            expected_rows = [{key:row[key] for key in ('ordinal','pass','case_key','arm','recording_file','recording_sha256','quality_end')}
                             for row in plan['rows'] if row.get('quality_end') is not None]
            _equal(quality['rows'], expected_rows, 'Quality proof row/sidecar mismatch')
            _equal(quality['records'], {row['recording_file']:row['recording_sha256'] for row in plan['rows'] if row['attempted']}, 'Quality proof record inventory mismatch')
            for key in ('archive_commit','archive_tree','envelope_sha256'):
                _equal(quality[key], plan['provenance'][key], 'Quality proof source/envelope mismatch')
        # A fresh chain can seal a caller-modified plan. Removed quality markers
        # must still fail the original projection seal before registry or reads.
        _validate_projection_plan_binding(plan, projection)
        chain = _gate_data(receipt_chain, 'receipt_chain', plan); signed = _gate_data(attestation, 'attestation', plan)
        _require(type(client) is _WindowScoreClient and client._seal is _PUBLICATION_SEAL, 'Pinned local evaluation client required')
        _equal(client._manifest_sha, plan['provenance']['manifest_sha256'], 'Client manifest mismatch')
        _equal(_sha(_regular_bytes(signed['binary_path'], 1 << 30)), signed['binary_sha256'], 'Attested actual binary changed')
        scores = _publication_scores(plan)
        handle = _open_publication_registry(plan['provenance']['scope_sha256'], create=True)
        # The batch fixes the complete original inventory independently of later
        # reviews, caller outputs, or missing individual intent/link files.
        batch_name = 'window-%06d.batch.json' % plan['window_index']
        _require(not (handle.scope/batch_name).exists(), 'Window publication batch exists; verify original batch only')
        # Refuse the entire operation before reads if any identity was claimed.
        for score in scores:
            _require(not any((handle.scope/name).exists() for name in (
                'logical-'+score['id']+'.intent.json', 'score-'+score['id']+'.intent.json',
                score['id']+'.sent.json', score['id']+'.accepted.json')),
                'Publication evidence exists; verify only, never resend')
        validation = validate_window_traces(plan, projection, client)
        _require(validation['all_pass'] is True, 'All captured trace content must verify before any score intent')
        handle.check(); intents = []
        batch = {'schema': 'full1220-score-batch.v1', 'scope_sha256': plan['provenance']['scope_sha256'],
                 'manifest_sha256': plan['provenance']['manifest_sha256'], 'window_id': plan['window_id'],
                 'window_index': plan['window_index'], 'plan_sha256': _sha(_json_bytes(plan)),
                 'trace_source_identity': _batch_source_identity(plan),
                 'scores': [{key: score[key] for key in ('id', 'logical_identity', 'traceId', 'name')} for score in scores]}
        if quality is not None:
            batch[proof_kind+'_proof'] = quality
        batch['batch_sha256'] = _sha(_json_bytes(batch))
        _publication_event(handle, batch_name, batch)
        for score in scores:
            payload = json.loads(_json_bytes(score))
            payload.pop('logical_identity')  # Local lock authority, never an API score field.
            payload['metadata'].update(plan_sha256=_sha(_json_bytes(plan)), receipt_chain_sha256=chain['chain_sha256'],
                                       binary_attestation_sha256=signed['attestation_sha256'], actual_binary_sha256=signed['binary_sha256'])
            event = {'schema': 'full1220-score-intent.v1', 'logical_identity': score['logical_identity'], 'score': payload,
                     'scope_sha256': plan['provenance']['scope_sha256'], 'window_id': plan['window_id'], 'batch_sha256': batch['batch_sha256']}
            event['intent_sha256'] = _sha(_json_bytes(event))
            _publication_event(handle, 'logical-'+score['id']+'.intent.json', event)
            _publication_event(handle, 'score-'+score['id']+'.intent.json', {'logical_sha256': score['id'], 'intent_sha256': event['intent_sha256']})
            intents.append(event)
        for event in intents:
            score = event['score']; identifier = score['id']
            handle.check()
            _publication_event(handle, identifier+'.sent.json', {'intent_sha256': event['intent_sha256'], 'may_have_been_sent': True})
            handle.check()
            try:
                ack = client.post_score(score)
            except Exception:
                raise ValueError('Score POST outcome uncertain; intent retained, verify only') from None
            _require(type(ack) is dict and ack.get('id') == identifier, 'Score ACK unknown; intent retained, verify only')
            _publication_event(handle, identifier+'.accepted.json', {'intent_sha256': event['intent_sha256'], 'ack_id': identifier})
        handle.check()
        try:
            client.verify_scores([event['score'] for event in intents]); verified = True
        except (ValueError, OSError, TimeoutError):
            verified = False
        return {'schema': 'full1220-score-publication.v1', 'all_verified': verified, 'intents': len(intents),
                'acknowledged': len(intents), 'scores_published_once': len(intents), 'model_calls': 0, 'jev_calls': 0,
                'actual_claims_created': 0, 'needs_readback': not verified}
    except (KeyError, IndexError, TypeError, OSError) as error:
        raise ValueError('Score publication stopped; retained intents permit verify only: '+type(error).__name__) from error
    finally:
        if handle is not None:
            handle.close()


def verify_window_scores(plan, client):
    """Read original intent payloads and existing score IDs; never create files or POST."""
    handle = None
    try:
        _require(type(client) is _WindowScoreClient and client._seal is _PUBLICATION_SEAL, 'Pinned local evaluation client required')
        _equal(client._manifest_sha, plan['provenance']['manifest_sha256'], 'Client manifest mismatch')
        handle = _open_publication_registry(plan['provenance']['scope_sha256'], create=False)
        if handle is None:
            return {'all_verified': False, 'reason': 'not_published', 'scores_checked': 0, 'scores_published': 0}
        batch_path = handle.scope/('window-%06d.batch.json' % plan['window_index'])
        if not batch_path.exists():
            return {'all_verified': False, 'reason': 'complete_batch_manifest_missing', 'scores_checked': 0, 'scores_published': 0}
        batch = _Document(_regular_bytes(batch_path, 1 << 20)).value
        integrity = dict(batch); batch_sha = integrity.pop('batch_sha256')
        _equal(_sha(_json_bytes(integrity)), batch_sha, 'Original complete batch manifest tampered')
        for key, expected in [('schema', 'full1220-score-batch.v1'), ('scope_sha256', plan['provenance']['scope_sha256']),
                              ('manifest_sha256', plan['provenance']['manifest_sha256']), ('window_id', plan['window_id']),
                              ('window_index', plan['window_index']), ('trace_source_identity', _batch_source_identity(plan))]:
            _equal(batch[key], expected, 'Original batch source identity mismatch: '+key)
        catalog = _index(batch['scores'], lambda item: item['id'], 'original batch score ID')
        events = {}
        for path in sorted(handle.scope.glob('logical-*.intent.json')):
            event = _Document(_regular_bytes(path, 1 << 20)).value
            integrity = dict(event); claimed = integrity.pop('intent_sha256')
            _equal(_sha(_json_bytes(integrity)), claimed, 'Original publication intent tampered')
            if event['window_id'] != plan['window_id']:
                continue
            score = event['score']; logical = json.loads(event['logical_identity'])
            _equal(score['id'], _sha(_json_bytes(logical)), 'Original score identity tampered')
            _equal(path.name, 'logical-'+score['id']+'.intent.json', 'Original logical filename tampered')
            _equal(event['scope_sha256'], plan['provenance']['scope_sha256'], 'Publication scope mismatch')
            _equal(event['batch_sha256'], batch_sha, 'Original intent belongs to a different batch')
            _equal(score['metadata']['plan_sha256'], batch['plan_sha256'], 'Original intent plan provenance mismatch')
            _require(score['id'] not in events, 'Duplicate original score intent')
            events[score['id']] = event
        if set(events) != set(catalog):
            return {'all_verified': False, 'reason': 'incomplete_original_intent_inventory', 'scores_checked': 0,
                    'expected_scores': len(catalog), 'present_intents': len(events), 'scores_published': 0}
        intents = []
        for identifier, expected in catalog.items():
            event = events[identifier]; score = event['score']
            _equal({key: score[key] for key in ('id', 'traceId', 'name')},
                   {key: expected[key] for key in ('id', 'traceId', 'name')}, 'Original batch score binding mismatch')
            _equal(event['logical_identity'], expected['logical_identity'], 'Original batch logical identity mismatch')
            _require(score['traceId'] in plan['trace_bindings'], 'Original publication trace not in current window')
            link = _Document(_regular_bytes(handle.scope/('score-'+identifier+'.intent.json'), 16384)).value
            _equal(link, {'logical_sha256': identifier, 'intent_sha256': event['intent_sha256']}, 'Original score-ID link tampered')
            intents.append(score)
        handle.check()
        if not intents:
            return {'all_verified': False, 'reason': 'not_published', 'scores_checked': 0, 'scores_published': 0}
        try:
            client.verify_scores(intents); verified = True
        except (ValueError, OSError, TimeoutError):
            verified = False
        return {'schema': 'full1220-score-verification.v1', 'all_verified': verified, 'scores_checked': len(intents),
                'scores_published': 0, 'uses_original_intent_metadata': True}
    except (KeyError, IndexError, TypeError, OSError) as error:
        raise ValueError('Original score verification unavailable: '+type(error).__name__) from error
    finally:
        if handle is not None:
            handle.close()


def _read_score_job(path):
    job = _Document(_regular_bytes(path, 1 << 20)).value
    _equal(job['schema'], 'full1220-window-score-job.v1', 'Explicit full-window score job required')
    raw = [_regular_bytes(job[key+'_path'], 16 << 20) for key in ('manifest', 'summary', 'receipt')]
    review = _regular_bytes(job['review_path'], 16 << 20)
    records = {name: _regular_bytes(Path(job['records_dir'])/name, 16 << 20) for name in job['expected_hashes']['records']}
    args = (*raw, records, review, job['expected_hashes'])
    manifest = _Document(raw[0]).value
    evidence = None
    evidence_args = {}
    if manifest['schema'] in ('full-window-actual-ready.v6', 'full-window-actual-ready.v7'):
        evidence_key = 'epoch'+manifest['schema'][-1]+'_evidence'
        receipt = _Document(raw[2]).value
        names = [q[key] for row in receipt['rows'] if (q := row.get('quality_end')) is not None
                 for key in ('candidate_file', 'cleanup_file')]
        _require(all(type(name) is str and re.fullmatch(r'[A-Za-z0-9_.-]+', name) is not None
                     and name not in ('.', '..') for name in names), 'Safe sidecar filenames required')
        evidence = {'envelope_raw': _regular_bytes(manifest['envelope']['path'], 8 << 20),
                    'sidecars': {name: _regular_bytes(Path(job['records_dir'])/name, 16 << 20) for name in names}}
        job['_'+evidence_key] = evidence
        evidence_args[evidence_key] = evidence
    return job, args, build_window_score_plan(*args, **evidence_args)


def main(argv=None):
    parser = argparse.ArgumentParser(description='Full1220 offline plans, explicit once-only local score publication, or read-only verification.')
    parser.add_argument('mode', nargs='?', choices=('plan', 'verify', 'publish'), default='plan')
    parser.add_argument('--job', help='Explicit paths and hashes; optional closed_archive_extra_witness={path,sha256} for pinned post-stop projection only')
    parser.add_argument('--out', help='Optional new result JSON; never a publication lock authority')
    parser.add_argument('--execute', action='store_true')
    args = parser.parse_args(argv)
    if not args.job:
        parser.print_help(); return 0
    if args.mode == 'publish' and not args.execute:
        print('Explicit --execute required for score publication', file=sys.stderr); return 2
    try:
        job, inputs, plan = _read_score_job(args.job)
        if args.mode == 'plan':
            result = plan
        elif args.mode == 'verify':
            result = verify_window_scores(plan, load_window_score_client(inputs[0], job['client_settings_path']))
        else:
            entries = [{'summary_raw': _regular_bytes(entry['summary_path'], 16 << 20),
                        'receipt_raw': _regular_bytes(entry['receipt_path'], 16 << 20)} for entry in job['chain']]
            pins = [{key: entry[key] for key in ('summary_sha256', 'receipt_sha256')} for entry in job['chain']]
            chain = verify_window_receipt_chain(plan, inputs[0], entries, pins)
            attestation = verify_window_attestation(inputs[0], _regular_bytes(job['attestation_path'], 65536),
                job['attestation_sha256'], job['binary_path'], runtime=job['runtime'], output_dir=job['attestation_output'], execute=True)
            projection_args = {'source_root': _Document(inputs[0]).value['source_root'],
                'output_dir': job['projection_output'], 'runtime': job['runtime'], 'execute': True,
                'closed_archive_extra_witness': job.get('closed_archive_extra_witness')}
            epoch = _Document(inputs[0]).value['schema'].rsplit('.', 1)[1]
            if epoch in ('v6', 'v7'):
                evidence_key = 'epoch'+epoch[1:]+'_evidence'
                projection_args[evidence_key] = job['_'+evidence_key]
            quality_proof = None
            if epoch in ('v6', 'v7') and any(row.get('quality_end') is not None for row in plan['rows']):
                quality_verifier = verify_window_quality_v6 if epoch == 'v6' else verify_window_quality_v7
                quality_proof = quality_verifier(inputs[0], inputs[2], inputs[3], inputs[5],
                    **{evidence_key: job['_'+evidence_key]}, source_root=projection_args['source_root'],
                    output_dir=job['quality_output'], runtime=job['runtime'], execute=True)
            projection = project_window_content(*inputs, **projection_args)
            result = publish_window_scores(plan, projection, chain, attestation,
                load_window_score_client(inputs[0], job['client_settings_path']), execute=True, quality_proof=quality_proof)
        if args.out:
            _exclusive_write(Path(args.out), _json_bytes(result)+b'\n')
        else:
            print(_json_bytes(result).decode())
        return 0 if args.mode == 'plan' or result.get('all_verified') else 2
    except (ValueError, OSError) as error:
        print(str(error), file=sys.stderr); return 2


if __name__ == '__main__':
    raise SystemExit(main())
