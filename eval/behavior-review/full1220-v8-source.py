"""V8 source-state rules, exact extracted definitions from reviewed source 776f8670.
No imports/exec of the historical scorer. No semantic verdict or owned proof.
"""
import hashlib
import json
import math
import re

_JSON_SPACE = re.compile(r'("(?:\\.|[^"\\])*")|\s+')

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
