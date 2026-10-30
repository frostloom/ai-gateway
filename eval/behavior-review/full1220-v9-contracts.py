"""Strict delegated V9 execution-binding data codec. No execution authority is returned.

Only data shape, fixed candidate declaration references and Go signing bytes are
checked. No signature verification, trusted-key selection, human-intent review,
filesystem evidence, claims, owner, readiness or egress is implemented here.
Even a parsed human_authorized=true object remains untrusted data. Candidate
budget/retry declarations are separate schemas and cannot be parsed as grants.
"""
import json
import re

_BINDING_FIELDS = (
    'schema', 'human_authorized', 'retry_policy_approved', 'human_reply',
    'manifest_sha256', 'source_sha256', 'binary_sha256', 'scope_sha256',
    'execution_sha256', 'budget_declaration_sha256', 'retry_policy_sha256',
    'parent', 'owner_id', 'single_owner', 'no_relaunch',
    'autonomous_authorization', 'global_budget', 'human_reviewed_exact_scope',
)
_BUDGET_SHA = 'd6584f9462b6ffbfe94acede036e0b62de536fe04aacc4bb57394ccd71184664'
_RETRY_SHA = '6a2fb5869eed58f8ed2fee18b6a471ddbb264bffa56f8aabbc6b4eac95598d63'
# unicode.IsSpace / strings.TrimSpace, not Python str.strip's extra U+001C–001F.
_GO_SPACE = '\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000'


def _require(condition, message):
    if not condition:
        raise ValueError(message)


def _pairs(pairs):
    out = {}
    for key, value in pairs:
        _require(key not in out, 'Duplicate JSON key')
        out[key] = value
    return out


def _not_integer(token):
    raise ValueError('Only integer JSON numbers are accepted')


def _scalar_tree(value, depth=0):
    _require(depth <= 8, 'JSON nesting limit')
    if type(value) is str:
        # Deliberately reject invalid Unicode instead of Go's replacement of
        # malformed UTF-8/surrogates, avoiding ambiguous signed input aliases.
        try:
            value.encode('utf-8', 'strict')
        except UnicodeError as exc:
            raise ValueError('Invalid Unicode scalar') from exc
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
        text = raw.decode('utf-8', 'strict')
        value = json.loads(text, object_pairs_hook=_pairs, parse_float=_not_integer,
                           parse_constant=_not_integer)
        _scalar_tree(value)
    except (UnicodeError, RecursionError) as exc:
        raise ValueError('Invalid or overly nested JSON') from exc
    _require(type(value) is dict, 'JSON object required')
    return value


def _fields(value, names):
    _require(type(value) is dict and set(value) == set(names), 'Exact object fields required')
    return {name: value[name] for name in names}


def _digest(value):
    _require(type(value) is str and re.fullmatch('[0-9a-f]{64}', value) is not None,
             'Lowercase SHA256 required')


def _binding(value):
    out = _fields(value, _BINDING_FIELDS)
    _require(out['schema'] == 'full-window-delegated-execution-authorization.v9', 'Execution schema required')
    for key in ('human_authorized', 'retry_policy_approved', 'single_owner', 'no_relaunch'):
        _require(out[key] is True, 'Execution binding flag must be boolean true')
    reply = _fields(out['human_reply'], ('path', 'sha256'))
    _require(type(reply['path']) is str and reply['path'] != '', 'Reply path text required')
    _digest(reply['sha256'])
    out['human_reply'] = reply  # Path existence/canonical identity is NOT checked.
    for key in _BINDING_FIELDS[4:11]:
        _digest(out[key])
    _require(out['budget_declaration_sha256'] == _BUDGET_SHA and
             out['retry_policy_sha256'] == _RETRY_SHA, 'Fixed candidate declaration references required')
    parent = _fields(out['parent'], ('model_micro_cny', 'jev_micro_usd'))
    _require(all(type(v) is int for v in parent.values()) and
             parent == {'model_micro_cny': 180000000, 'jev_micro_usd': 2500000},
             'Exact candidate parent currencies required')
    out['parent'] = parent
    _require(reply['sha256'] == '22a9ec8c97ded148879f4a30abaa2a0e8370371ecd7fecb56a85988f89943a57',
             'Latest global budget reply identity required')
    autonomous = _fields(out['autonomous_authorization'], ('path', 'sha256'))
    _require(type(autonomous['path']) is str and autonomous['path'] != '' and
             autonomous['sha256'] == '8f428b68f0f9f5c2821c03da8c41ee220dbc4e18741ace3982a4cfcb93b52a95',
             'Retained autonomous authorization reference required')
    out['autonomous_authorization'] = autonomous
    global_budget = _fields(out['global_budget'], ('model_micro_cny', 'jev_micro_usd'))
    _require(all(type(v) is int for v in global_budget.values()) and
             global_budget == {'model_micro_cny': 200000000, 'jev_micro_usd': 4000000},
             'Exact global budget currencies required')
    out['global_budget'] = global_budget
    _require(out['human_reviewed_exact_scope'] is False,
             'Delegated technical binding is not exact human scope review')
    owner = out['owner_id']
    _require(type(owner) is str and owner != '' and owner.strip(_GO_SPACE) == owner and
             len(owner.encode('utf-8')) <= 128 and '\r' not in owner and '\n' not in owner,
             'Owner text/UTF-8 byte bound rejected')
    return out


def _go_bytes(binding):
    # Fixed struct order is set by _binding; this is not generic Go map encoding.
    text = json.dumps(binding, ensure_ascii=False, separators=(',', ':'), allow_nan=False)
    for char, escape in (('&', '\\u0026'), ('<', '\\u003c'), ('>', '\\u003e'),
                         ('\u2028', '\\u2028'), ('\u2029', '\\u2029')):
        text = text.replace(char, escape)
    return text.encode('utf-8')


def canonical_execution_binding(raw):
    """Return Go json.Marshal(binding) bytes, excluding any signature field.

    This is strict data normalization, not cryptographic or execution approval.
    """
    return _go_bytes(_binding(_parse(raw)))


def parse_execution_authorization(raw):
    """Return detached, validated-shape data; the hex signature is NOT verified."""
    value = _fields(_parse(raw), (*_BINDING_FIELDS, 'signature'))
    signature = value.pop('signature')
    _require(type(signature) is str and re.fullmatch('[0-9a-fA-F]{128}', signature) is not None,
             '64-byte hex Ed25519 signature required')
    return {**_binding(value), 'signature': signature}


def authorization_signing_bytes(raw):
    """Return the exact binding message from an authorization-shaped document."""
    value = parse_execution_authorization(raw)
    del value['signature']
    return _go_bytes(value)
