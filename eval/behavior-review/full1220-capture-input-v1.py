"""Passive supplemental capture factory. No signing, settings or network API.

The fixed operator must select the final binding SHA after review. Capture is
still gated by the unchanged historical issuer and fresh owned process. The
binding is data, never an actual-window or content-projection proof.
"""
import hashlib
import json
from pathlib import Path
import re
import sys
import types

REPO = Path('/Users/a1/项目/ai-gateway')
ORIGINAL_ROOT = REPO / '.tmp/paired-live-private/full1220-actual-v9-freeze-r5-20261031'
ARTIFACT_ROOT = REPO / '.tmp/v9-live-score-horizon-20261031/capture-input-v1'
BINDING_PATH = ARTIFACT_ROOT / 'correction-binding.json'
PREBUILD_BINDING_PATH = ARTIFACT_ROOT / 'prebuild-binding.json'
BASE_PATH = REPO / '.tmp/v9-live-score-horizon-20261031/historical-scores.py'
BASE_SHA = 'f7e45bb0beb499a5e40db6a295fa016d0b94bb3c60ea78f88a7189914788eec2'
ISSUER_SHA = '4d1af5e1b8c8f4e0814774dca3b0e554468b5ca67eb324490609604bd34d8b76'
MANIFEST_SHA = 'aa9a664d767c080648213e3d98c63c340d2207afea9121d104bbb2775fdc7666'
ORIGINAL_BINDING_SHA = '541bb3c79b9d11d96edba5470a6f15dde8c6c4067abfdd1a239cb5ad137c58cb'
SOURCE_MAP_SHA = '47486b91579a39e5304c721a0ddc140797b4109864e79406b5b54b6831328d00'
OLD_CATALOG_SHA = 'b9f281232e5ccfc51f7f14496fca3dc710eeed347051246df1fb397fdd9b71dd'
OLD_HELPER_SHA = '2aa266ad067173e8b6701c7bce793411e29e5fc1e52872cae1df5c8ea3cfac2d'
TEMPLATE_PATH = REPO / 'eval/behavior-review/helpers/full1220_content_projection_input_v1_test.go.txt'
TEMPLATE_SHA = '98a26427ab04797c26c88356e88800d417362e215b5fbf9c39531b2de36c41d9'
PREBUILDER_PATH = REPO / '.tmp/v9-python-adapters-20261030/prebuild-helpers-v9-r5.py'
PREBUILDER_SHA = '181134f29edca0f43ab75d50f61c4d69854b798f3ef7198d33455c4eab196905'
RUNTIME_SHA = '59d0997208f697001dd681d87f3f01ad164c28c27c898c068675b765e93edeb5'
CAPTURE_SPEC = {
    'schema': 'full1220-capture-input-spec.v1', 'base_consumer_sha256': BASE_SHA,
    'historical_issuer_sha256': ISSUER_SHA, 'original_manifest_sha256': MANIFEST_SHA,
    'original_binding_sha256': ORIGINAL_BINDING_SHA, 'original_source_map_sha256': SOURCE_MAP_SHA,
    'original_catalog_sha256': OLD_CATALOG_SHA, 'template_sha256': TEMPLATE_SHA,
    'template_path': str(TEMPLATE_PATH), 'artifact_root': str(ARTIFACT_ROOT),
    'process_root': str(ARTIFACT_ROOT / 'helper-supervision'), 'runtime_module_sha256': RUNTIME_SHA,
}


def _need(ok, reason):
    if not ok:
        raise ValueError(reason)


def _sha(raw):
    return hashlib.sha256(raw).hexdigest()


def _wire(value):
    return json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(',', ':'), allow_nan=False).encode()


def _digest(value):
    return type(value) is str and re.fullmatch('[0-9a-f]{64}', value) is not None


def _replace(raw, old, new):
    old, new = old.encode(), new.encode()
    _need(raw.count(old) == 1, 'exact derivation anchor')
    return raw.replace(old, new)


def derive_consumer_source(base_raw: bytes, correction: dict) -> bytes:
    """Derive fresh code only; no execution, IO, key or proof construction."""
    _need(type(base_raw) is bytes and _sha(base_raw) == BASE_SHA, 'exact base consumer bytes')
    _need(type(correction) is dict and _wire(correction) == _wire(CAPTURE_SPEC), 'fixed capture spec')
    raw = _replace(base_raw, "_PROJECTION_HELPER_SHA256 = '" + OLD_HELPER_SHA + "'",
                   "_PROJECTION_HELPER_SHA256 = '" + TEMPLATE_SHA + "'")
    raw = _replace(raw, "    h=context.helper;p=h['runner']",
                   "    h=_capture_loader(context);p=h['runner']")
    raw = _replace(raw, "artifact_root=context.root,runtime_tree=h['runtime']",
                   "artifact_root=Path(_CAPTURE_SPEC['artifact_root']),runtime_tree=h['runtime']")
    raw = _replace(raw, "p.pin_file(context.root/'helper-build/catalog.json')]",
                   "p.pin_file(Path(_CAPTURE_SPEC['artifact_root'])/'helper-build/catalog.json')] + h['pins']")
    raw = _replace(raw, "state_root=context.root/'helper-supervision',attempt_name=attempt",
                   "state_root=Path(_CAPTURE_SPEC['process_root']),attempt_name=attempt")
    raw = _replace(raw, "actual=context.root/'helper-supervision/attempts'/attempt/process['receipt_file']",
                   "actual=Path(_CAPTURE_SPEC['process_root'])/'attempts'/attempt/process['receipt_file']")
    # No final binding hash is embedded here: final binding pins these bytes,
    # and the loader's closed selector carries its SHA without a hash cycle.
    return raw + b'\n_CAPTURE_SPEC = ' + repr(json.loads(_wire(correction))).encode() + b'\n'


def _prebuilder():
    # Bootstrap this fixed reviewed reader, then use its alias/identity checks
    # for all data and executable-module reads. No caller-supplied module path.
    raw = PREBUILDER_PATH.read_bytes()
    _need(_sha(raw) == PREBUILDER_SHA, 'reviewed prebuilder bytes')
    module = types.ModuleType('capture_input_prebuild_reference')
    module.__file__ = str(PREBUILDER_PATH)
    exec(compile(raw, str(PREBUILDER_PATH), 'exec'), module.__dict__)
    module.read(PREBUILDER_PATH, PREBUILDER_SHA)
    return module


def _read_binding(path, pin, *, build=False):
    _need(_digest(pin), 'UNFROZEN correction binding SHA')
    expected = PREBUILD_BINDING_PATH if build else BINDING_PATH
    _need(type(path) is str and path == str(expected), 'fixed correction binding path')
    b = _prebuilder()
    raw = b.read(expected, pin)
    binding = json.loads(raw, object_pairs_hook=b.pairs)
    fields = {'schema', 'state', 'capture_spec', 'job_sha256'}
    if not build:
        fields |= {'consumer_sha256', 'catalog_sha256', 'catalog_identity', 'artifacts', 'build_evidence_sha256'}
    _need(type(binding) is dict and set(binding) == fields, 'exact correction binding fields')
    _need(binding['schema'] == ('full1220-capture-input-prebuild.v1' if build else 'full1220-capture-input-binding.v1')
          and binding['state'] == ('PREBUILD' if build else 'FROZEN'), 'UNFROZEN correction binding')
    _need(_wire(binding['capture_spec']) == _wire(CAPTURE_SPEC), 'fixed capture spec')
    _need(_digest(binding['job_sha256']), 'prebuild job SHA')
    manifest_raw = b.read(ORIGINAL_ROOT / 'actual-manifest.json', MANIFEST_SHA)
    manifest = json.loads(manifest_raw, object_pairs_hook=b.pairs)
    b.read(ORIGINAL_ROOT / 'runtime-binding.json', ORIGINAL_BINDING_SHA)
    b.read(TEMPLATE_PATH, TEMPLATE_SHA)
    b.read(BASE_PATH, BASE_SHA)
    b.read(BASE_PATH.parent / 'historical-window.py', ISSUER_SHA)
    return binding, b, manifest


def _build_module(b):
    """Minimal deterministic edits to the reviewed two-build pipeline."""
    raw = b.read(PREBUILDER_PATH, PREBUILDER_SHA)
    raw = _replace(raw, 'FREEZE_ROOT=Path('+repr(str(ORIGINAL_ROOT))+')',
                   'FREEZE_ROOT=Path('+repr(str(ARTIFACT_ROOT))+')')
    raw = _replace(raw, "CAPTURE_HELPER_SHA='"+OLD_HELPER_SHA+"'", "CAPTURE_HELPER_SHA='"+TEMPLATE_SHA+"'")
    raw = _replace(raw, " and 'v9' in root.name and 'v8' not in root.name and", ' and')
    raw = _replace(raw, "source==root/'regression/source'", 'source==Path('+repr(str(ORIGINAL_ROOT / 'regression/source'))+')')
    raw = _replace(raw, "j['sources'].get(CAPTURE_HELPER)==CAPTURE_HELPER_SHA",
                   "j['sources'].get(CAPTURE_HELPER)=="+repr(OLD_HELPER_SHA))
    raw = _replace(raw, 'read(source/helper,pin)', 'read(Path('+repr(str(TEMPLATE_PATH))+
                   ") if kind=='capture' else source/helper,pin)")
    module = types.ModuleType('capture_input_prebuild')
    module.__file__ = str(ARTIFACT_ROOT / 'prebuild.py')
    exec(compile(raw, module.__file__, 'exec'), module.__dict__)
    return module, raw


def _job(binding, b, manifest):
    path = ARTIFACT_ROOT / 'prebuild-job.json'
    raw = b.read(path, binding['job_sha256'])
    job = json.loads(raw, object_pairs_hook=b.pairs)
    for key, value in {'freeze_root': str(ARTIFACT_ROOT), 'source_root': manifest['source_root'],
                       'archive_commit': manifest['archive_commit'], 'archive_tree': manifest['archive_tree'],
                       'sources': manifest['sources'], 'source_map_sha256': SOURCE_MAP_SHA,
                       'build_attestation_public_key': manifest['build_attestation_public_key'],
                       'runtime_module_sha256': RUNTIME_SHA}.items():
        _need(_wire(job.get(key)) == _wire(value), 'original manifest prebuild job: '+key)
    return path


def build_capture_components(binding_path: str, binding_sha256: str, *, execute: bool = False) -> dict:
    """Explicit technical builds only; return unsigned message for root signer."""
    if execute is not True:
        return {'state': 'UNFROZEN', 'executed': False, 'proofs_issued': 0,
                'binding_path': str(PREBUILD_BINDING_PATH), 'artifact_root': str(ARTIFACT_ROOT),
                'stages': ['original source -> isolated runtime', 'owned source/capture Go builds',
                           'catalog.message -> external technical signer -> reviewed final binding']}
    binding, b, manifest = _read_binding(binding_path, binding_sha256, build=True)
    job = _job(binding, b, manifest)
    build, raw = _build_module(b)
    b.write(ARTIFACT_ROOT / 'prebuild.py', raw, 0o400)
    return build.prepare(job, binding['job_sha256'])


def _validate_catalog(binding, manifest, b, p, verify_signature):
    """Validate build data before any executable selection or delayed stdin."""
    catalog_raw = b.read(ARTIFACT_ROOT / 'helper-build/catalog.json', binding['catalog_sha256'])
    _need(binding['catalog_sha256'] != OLD_CATALOG_SHA, 'new supplemental catalog required')
    runtime_map = dict(manifest['sources'])
    extras = {spec[1]+'/'+spec[3]: spec[5] for spec in _build_module(b)[0].SPECS}
    _need(not runtime_map.keys() & extras.keys(), 'generated helpers outside original sources')
    runtime_map.update(extras)
    identity = binding['catalog_identity']
    for key, value in {'commit': manifest['archive_commit'], 'tree': manifest['archive_tree'],
                       'source_map_sha256': SOURCE_MAP_SHA, 'runtime_map_sha256': _sha(_wire(runtime_map)),
                       'go_sha256': b.GO_SHA, 'sdk_sha256': b.SDK_SHA, 'platform': 'darwin/arm64'}.items():
        _need(identity.get(key) == value, 'supplemental build identity: '+key)
    catalog = p.load_catalog(catalog_raw, expected_sha256=binding['catalog_sha256'],
                            trusted_public_key=manifest['build_attestation_public_key'],
                            expected_identity=identity, verify_signature=verify_signature)
    _need(catalog['build_evidence_sha256'] == binding['build_evidence_sha256'], 'build evidence binding')
    _need(set(binding['artifacts']) == {'helper-build/source.test', 'helper-build/capture.test'}, 'exact helper binary bindings')
    for kind, cwd, test, filename, _, pin, inschema, outschema in _build_module(b)[0].SPECS:
        entry = next(e for e in catalog['artifacts'] if e['kind'] == kind)
        expected = {'path': 'helper-build/'+kind+'.test', 'test': test, 'cwd': cwd,
                    'input_schema': inschema, 'output_schema': outschema, 'helper_path': cwd+'/'+filename,
                    'helper_sha256': pin, 'sha256': binding['artifacts']['helper-build/'+kind+'.test']}
        _need(all(entry.get(k) == v for k, v in expected.items()), 'supplemental helper API/binary')
    return catalog, runtime_map


def _checked_pin(p, path, expected):
    pin = p.pin_file(path, immutable=False)
    _need(_digest(expected) and pin.sha256 == expected, 'bound fresh file SHA')
    return pin


def _context_runner_pins(context, p, original, tools):
    """Pin with the actual factory's runner; never mix module-local dataclasses.

    A preliminary loader may have imported the same reviewed runner bytes into
    a different module. Only plain expected hashes/paths cross this boundary.
    The closed selector retains no pin objects between capture invocations.
    """
    if context is None:
        return p, original, tools
    selected = context.helper['runner']
    expected = {str(Path(pin.path).relative_to(original.root)): pin.sha256 for pin in original.files}
    tree = selected.pin_tree(Path(original.root), expected)
    new_tools = {name: selected.pin_tool(Path(pin.path)) for name, pin in tools.items()}
    _need(all(new_tools[name].sha256 == pin.sha256 for name, pin in tools.items()), 'context runner tool bytes')
    return selected, tree, new_tools


def _capture_selection(binding_path, binding_sha256, context=None):
    binding, b, manifest = _read_binding(binding_path, binding_sha256)
    job_path = _job(binding, b, manifest)
    build, _ = _build_module(b)
    job, _, p, root, source, original, tools = build.load_job(job_path, binding['job_sha256'])
    _need(root == ARTIFACT_ROOT and source == Path(manifest['source_root']), 'capture/original root separation')
    if context is not None:
        _need(context.root == ORIGINAL_ROOT and _wire(context.manifest) == _wire(manifest), 'original actual capture context')
    p, original, tools = _context_runner_pins(context, p, original, tools)
    verifier = b.module(source / b.VERIFIER_MODULE, job['verifier_module_sha256'], 'capture_input_crypto')
    def crypto(key, message, signature):
        verifier._signature(key, message, signature)
        return True
    catalog, runtime_map = _validate_catalog(binding, manifest, b, p, crypto)
    _need(catalog['identity']['build_env'] == build.build_env(ARTIFACT_ROOT/'helper-build', job['module_cache']), 'fixed offline build environment')
    _need(catalog['identity']['tools'] == {k: {'path': v.path, 'sha256': v.sha256} for k, v in tools.items()}, 'original installed tool selection')
    runtime = p.pin_tree(ARTIFACT_ROOT / 'helper-build/runtime', runtime_map)
    for kind in ('source', 'capture'):
        p.select_helper(catalog, kind, artifact_root=ARTIFACT_ROOT, runtime_tree=runtime, tool_pins=tools)
    p.check_tree(original)
    _need(not (ARTIFACT_ROOT / 'helper-supervision/active.json').exists(), 'unresolved supplemental helper attempt')
    evidence = json.loads(b.read(ARTIFACT_ROOT/'helper-build/build-evidence.json', binding['build_evidence_sha256']))
    _need(evidence['job_sha256'] == binding['job_sha256'] and [r['kind'] for r in evidence['receipts']] == ['source', 'capture'], 'owned build evidence')
    evidence_pins = {ARTIFACT_ROOT/'helper-build/build-evidence.json': binding['build_evidence_sha256']}
    for row in evidence['receipts']:
        path = ARTIFACT_ROOT/'helper-supervision/attempts'/('build-'+row['kind'])/'receipt.json'
        _need(row['path'] == str(path), 'owned build receipt path')
        receipt = json.loads(b.read(path, row['sha256']))
        _need(receipt['accepted'] is True and receipt['cleanup']['confirmed'] is True and receipt['output_validation_supplied'] is True, 'accepted owned build receipt')
        evidence_pins[path] = row['sha256']
    expected_pins = {Path(binding_path): binding_sha256, job_path: binding['job_sha256'], BASE_PATH: BASE_SHA,
                    TEMPLATE_PATH: TEMPLATE_SHA, ORIGINAL_ROOT/'actual-manifest.json': MANIFEST_SHA,
                    ORIGINAL_ROOT/'runtime-binding.json': ORIGINAL_BINDING_SHA,
                    ARTIFACT_ROOT/'helper-build/catalog.json': binding['catalog_sha256'], **evidence_pins}
    pins = [_checked_pin(p, path, expected) for path, expected in expected_pins.items()]
    return {'runner': p, 'catalog': catalog, 'runtime': runtime, 'tools': tools, 'pins': pins}


def load_corrected_consumer(binding_path: str, binding_sha256: str) -> types.ModuleType:
    """Load a new module; never mutate the original consumer or actual context."""
    binding, b, _ = _read_binding(binding_path, binding_sha256)
    raw = derive_consumer_source(b.read(BASE_PATH, BASE_SHA), binding['capture_spec'])
    _need(_digest(binding['consumer_sha256']) and _sha(raw) == binding['consumer_sha256'], 'derived consumer SHA')
    _capture_selection(binding_path, binding_sha256)
    module = types.ModuleType('capture_input_consumer_'+binding['consumer_sha256'])
    module.__file__ = str(ARTIFACT_ROOT / 'consumer.py')
    # Closed loader authority only. No argument accepts a key, catalog, registry
    # root, executable, settings callback or serialized projection proof.
    module.__dict__['_capture_loader'] = lambda context: _capture_selection(binding_path, binding_sha256, context)
    exec(compile(raw, module.__file__, 'exec'), module.__dict__)
    return module
