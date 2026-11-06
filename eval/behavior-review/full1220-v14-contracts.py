"""Strict V14 binding codecs returning untrusted DATA only.

No signature verification, key selection, human-intent review, file reads,
preparation, claims, owner, credentials or egress exists here. A well-shaped
signature (including all zeroes) is not approval. Runtime must independently
verify the new human timing reply and both signed bindings under its trusted key.
"""
import hashlib
import json
import posixpath
import re

_BINDING_FIELDS = (
    'schema', 'human_authorized', 'retry_policy_approved', 'human_reply',
    'manifest_sha256', 'source_sha256', 'binary_sha256', 'scope_sha256',
    'execution_sha256', 'budget_declaration_sha256', 'retry_policy_sha256',
    'parent', 'owner_id', 'single_owner', 'no_relaunch',
    'autonomous_authorization', 'global_budget', 'human_reviewed_exact_scope',
    'private_settings_sha256', 'spool_root', 'observer',
    'collection_policy_sha256', 'completion_sha256', 'executor_equivalence_sha256',
    'operation_policy_sha256', 'operation_policy_approval',
)
_APPROVAL_FIELDS = (
    'schema', 'human_policy_approved', 'human_reply', 'manifest_sha256',
    'source_sha256', 'binary_sha256', 'scope_sha256', 'execution_sha256',
    'operation_policy_sha256', 'human_reviewed_exact_scope',
)
_IDENTITY_DIGESTS = ('manifest_sha256', 'source_sha256', 'binary_sha256',
                     'scope_sha256', 'execution_sha256', 'operation_policy_sha256')
_OBSERVER_FIELDS = (
    'observation_receipt_boundary', 'spool_root_sha256', 'spool_run_id',
    'spool_max_bytes', 'enabled', 'capture_content', 'sample_rate', 'environment',
    'release', 'export_timeout_ms', 'queue_size', 'endpoint_sha256', 'project_sha256',
)
_BUDGET_REPLY_SHA = '22a9ec8c97ded148879f4a30abaa2a0e8370371ecd7fecb56a85988f89943a57'
_AUTONOMY_SHA = '8f428b68f0f9f5c2821c03da8c41ee220dbc4e18741ace3982a4cfcb93b52a95'
_RETRY_SHA = '26fef0e90b466ed651b2e8bc11790d336d12d75554f14e7fd06a8d18009cf088'
_COLLECTION_POLICY_SHA = '3fd68fc05f8ebc4b3a5d87cbeb905caa46abbcedc5b34de836d910a5425f3213'
_BUDGET_SHA = '58f7bea973d9c401c6ae8231652a91fa30ccf26de90a8adcc4ad66b0d4539e7d'
_COMPLETION_SHA = '9a2d01ba483bde017baf06f7ae66c151d99d7f4fd0cc0ed911a8e18c905a8042'
_POLICY_BYTES = b'{"schema":"full-window-operation-policy.v14","window_seconds":600,"model_seconds":90,"jev_seconds":10}'
_POLICY_SHA = hashlib.sha256(_POLICY_BYTES).hexdigest()
# strings.TrimSpace / unicode.IsSpace differs from Python strip at U+001C..1F.
_GO_SPACE = '\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000'


def _require(condition, message):
    if not condition:
        raise ValueError(message)


def _pairs(pairs):
    result = {}
    for key, value in pairs:
        _require(key not in result, 'Duplicate JSON key')
        result[key] = value
    return result


def _not_integer(token):
    raise ValueError('Only integer JSON numbers are accepted')


def _scalar_tree(value, depth=0):
    _require(depth <= 8, 'JSON nesting limit')
    if type(value) is str:
        value.encode('utf-8', 'strict')
    elif type(value) is int:
        _require(-(1 << 63) <= value < (1 << 63), 'Integer outside int64')
    elif type(value) is dict:
        for key, item in value.items():
            _scalar_tree(key, depth + 1)
            _scalar_tree(item, depth + 1)
    elif type(value) is list:
        for item in value:
            _scalar_tree(item, depth + 1)
    else:
        _require(value is None or type(value) is bool, 'Unsupported JSON value')


def _parse(raw):
    _require(type(raw) is bytes and 0 < len(raw) <= 65536, 'Bounded UTF-8 bytes required')
    try:
        value = json.loads(raw.decode('utf-8', 'strict'), object_pairs_hook=_pairs,
                           parse_float=_not_integer, parse_constant=_not_integer)
        _scalar_tree(value)
    except (UnicodeError, RecursionError) as exc:
        raise ValueError('Invalid Unicode scalar or overly nested JSON') from exc
    _require(type(value) is dict, 'JSON object required')
    return value


def _fields(value, names):
    _require(type(value) is dict and set(value) == set(names), 'Exact object fields required')
    return {name: value[name] for name in names}


def _digest(value):
    _require(type(value) is str and re.fullmatch('[0-9a-f]{64}', value) is not None,
             'Lowercase SHA256 required')


def _ref(value):
    out = _fields(value, ('path', 'sha256'))
    _require(type(out['path']) is str and out['path'] != '', 'Reference path text required')
    _digest(out['sha256'])
    # Existence, canonical namespace and physical identity are runtime checks.
    return out


def _amounts(value, expected):
    out = _fields(value, ('model_micro_cny', 'jev_micro_usd'))
    _require(all(type(item) is int for item in out.values()) and out == expected,
             'Exact candidate currencies required')
    return out


def _go_bytes(binding):
    # Fixed struct order from validators, never generic Go map serialization.
    text = json.dumps(binding, ensure_ascii=False, separators=(',', ':'), allow_nan=False)
    for char, escaped in (('&', '\\u0026'), ('<', '\\u003c'), ('>', '\\u003e'),
                          ('\u2028', '\\u2028'), ('\u2029', '\\u2029')):
        text = text.replace(char, escaped)
    return text.encode('utf-8')


def canonical_operation_policy(raw):
    """Return exact candidate declaration bytes; no execution permission."""
    out = _fields(_parse(raw), ('schema', 'window_seconds', 'model_seconds', 'jev_seconds'))
    _require(out['schema'] == 'full-window-operation-policy.v14' and
             all(type(out[key]) is int for key in ('window_seconds', 'model_seconds', 'jev_seconds')) and
             (out['window_seconds'], out['model_seconds'], out['jev_seconds']) == (600, 90, 10),
             'Exact V14 operation policy required')
    return _go_bytes(out)


def observer_identity(value):
    out = _fields(value, _OBSERVER_FIELDS)
    _require(out['observation_receipt_boundary'] == 'durable_local' and
             out['enabled'] is True and out['capture_content'] is True and
             type(out['sample_rate']) is int and out['sample_rate'] == 1 and
             out['environment'] == 'evaluation', 'Exact V14 observer boundary required')
    for key in ('spool_root_sha256', 'spool_run_id', 'endpoint_sha256', 'project_sha256'):
        _digest(out[key])
    _require(type(out['release']) is str and re.fullmatch('[0-9a-f]{40}', out['release']) is not None,
             'Observer release required')
    _require(type(out['spool_max_bytes']) is int and out['spool_max_bytes'] == 4 << 30 and
             type(out['export_timeout_ms']) is int and out['export_timeout_ms'] == 10000 and
             type(out['queue_size']) is int and 0 < out['queue_size'] <= 4096,
             'Exact observer capacity/timeout required')
    return out


def _binding(value):
    out = _fields(value, _BINDING_FIELDS)
    _require(out['schema'] == 'full-window-delegated-execution-authorization.v14',
             'V14 execution schema required')
    for key in ('human_authorized', 'retry_policy_approved', 'single_owner', 'no_relaunch'):
        _require(out[key] is True, 'Execution flag must be boolean true')
    _require(out['human_reviewed_exact_scope'] is False, 'Delegated scope is not exact human review')
    for key in (*_IDENTITY_DIGESTS, 'budget_declaration_sha256', 'retry_policy_sha256',
                'private_settings_sha256', 'collection_policy_sha256', 'completion_sha256',
                'executor_equivalence_sha256'):
        _digest(out[key])
    _require(out['budget_declaration_sha256'] == _BUDGET_SHA and
             out['completion_sha256'] == _COMPLETION_SHA and
             out['retry_policy_sha256'] == _RETRY_SHA and
             out['collection_policy_sha256'] == _COLLECTION_POLICY_SHA and
             out['operation_policy_sha256'] == _POLICY_SHA,
             'Fixed candidate policy references required')
    out['human_reply'] = _ref(out['human_reply'])
    _require(out['human_reply']['sha256'] == _BUDGET_REPLY_SHA, 'Retained global budget reply required')
    out['autonomous_authorization'] = _ref(out['autonomous_authorization'])
    _require(out['autonomous_authorization']['sha256'] == _AUTONOMY_SHA, 'Retained autonomous reference required')
    out['operation_policy_approval'] = _ref(out['operation_policy_approval'])
    out['parent'] = _amounts(out['parent'], {'model_micro_cny': 200000000, 'jev_micro_usd': 3000000})
    out['global_budget'] = _amounts(out['global_budget'], {'model_micro_cny': 200000000, 'jev_micro_usd': 4000000})
    owner = out['owner_id']
    _require(type(owner) is str and owner != '' and owner.strip(_GO_SPACE) == owner and
             len(owner.encode('utf-8')) <= 128 and '\r' not in owner and '\n' not in owner,
             'Owner text/UTF-8 byte bound rejected')
    root = out['spool_root']
    _require(type(root) is str and root.startswith('/') and not root.startswith('//') and
             posixpath.normpath(root) == root, 'Canonical absolute spool root required')
    out['observer'] = observer_identity(out['observer'])
    _require(out['observer']['spool_root_sha256'] == hashlib.sha256(root.encode('utf-8')).hexdigest(),
             'Observer root binding required')
    return out


def _approval(value):
    out = _fields(value, _APPROVAL_FIELDS)
    _require(out['schema'] == 'full-window-operation-policy-human-approval.v14',
             'V14 human timing-policy approval schema required')
    _require(out['human_policy_approved'] is True and out['human_reviewed_exact_scope'] is False,
             'Human policy approval flag and delegated exact-scope distinction required')
    out['human_reply'] = _ref(out['human_reply'])
    path = out['human_reply']['path']
    _require(path.startswith('/') and not path.startswith('//') and
             posixpath.normpath(path) == path, 'Canonical absolute timing-policy reply path required')
    _require(out['human_reply']['sha256'] not in (_BUDGET_REPLY_SHA, _AUTONOMY_SHA),
             'New timing-policy human reply required; retained budget/autonomy insufficient')
    for key in _IDENTITY_DIGESTS:
        _digest(out[key])
    _require(out['operation_policy_sha256'] == _POLICY_SHA, 'Exact V14 operation policy required')
    return out


def _signed(raw, names, validate):
    value = _fields(_parse(raw), (*names, 'signature'))
    signature = value.pop('signature')
    _require(type(signature) is str and re.fullmatch('[0-9a-fA-F]{128}', signature) is not None,
             '64-byte hex Ed25519 signature shape required; not verified')
    return {**validate(value), 'signature': signature}


def canonical_execution_binding(raw):
    """Normalize execution binding DATA to Go json.Marshal order, unsigned."""
    return _go_bytes(_binding(_parse(raw)))


def parse_execution_authorization(raw):
    """Return detached execution-shaped DATA; no signature/approval verification."""
    return _signed(raw, _BINDING_FIELDS, _binding)


def authorization_signing_bytes(raw):
    """Return unsigned canonical message bytes from execution-shaped DATA."""
    value = parse_execution_authorization(raw)
    del value['signature']
    return _go_bytes(value)


def canonical_operation_policy_approval(raw):
    """Normalize approval binding DATA; never establishes human consent."""
    return _go_bytes(_approval(_parse(raw)))


def parse_operation_policy_approval(raw):
    """Return detached approval-shaped DATA; signature and human reply unchecked."""
    return _signed(raw, _APPROVAL_FIELDS, _approval)


def policy_approval_signing_bytes(raw):
    """Return unsigned canonical message bytes from approval-shaped DATA."""
    value = parse_operation_policy_approval(raw)
    del value['signature']
    return _go_bytes(value)
