"""Offline V9 build-identity and process primitives; never constructs a proof.

Trusted factories select catalog/key, argv/cwd/env, shared state root and pins.
This module has no CLI, scope loader, credentials loader, Go build or network API.
"""
from dataclasses import dataclass, asdict
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import threading
import time


def need(ok, reason):
    if not ok:
        raise ValueError(reason)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def canonical(value):
    return json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(',', ':'), allow_nan=False).encode()


def _object(pairs):
    result = {}
    for key, value in pairs:
        need(key not in result, 'duplicate JSON key')
        result[key] = value
    return result


def _hex(value, length=64):
    return type(value) is str and re.fullmatch('[0-9a-f]{'+str(length)+'}', value) is not None


def _relative(value):
    return (type(value) is str and value and not value.startswith('/') and
            all(x not in ('', '.', '..') for x in value.split('/')) and '\\' not in value)


def catalog_signing_bytes(catalog):
    """Typed signing message for the EXISTING technical signer, not a new key."""
    return b'full-window-helper-build-catalog.v1\n' + canonical({k: v for k, v in catalog.items() if k != 'signature'})


def load_catalog(raw, *, expected_sha256, trusted_public_key, expected_identity, verify_signature):
    """verify_signature is the trusted factory's crypto-only Ed25519 verifier.

    It must return True after verification (wrap an existing raise-on-failure
    verifier explicitly). No key or executable is selected from untrusted jobs.
    Returned JSON is build identity DATA, never source/window/capture proof.
    """
    need(type(raw) is bytes and _hex(expected_sha256) and sha(raw) == expected_sha256, 'catalog SHA')
    need(_hex(trusted_public_key), 'trusted public key')
    obj = json.loads(raw, object_pairs_hook=_object,
                     parse_constant=lambda _: (_ for _ in ()).throw(ValueError('nonfinite JSON')))
    need(type(obj) is dict and set(obj) == {'schema', 'identity', 'artifacts', 'build_evidence_sha256', 'signature'}, 'catalog fields')
    need(obj['schema'] == 'full-window-helper-build-catalog.v1', 'catalog schema')
    identity = obj['identity']
    need(type(identity) is dict and set(identity) == {'commit', 'tree', 'source_map_sha256', 'runtime_map_sha256',
         'go_sha256', 'sdk_sha256', 'platform', 'build_env', 'tools'}, 'identity fields')
    need(identity == expected_identity, 'frozen build identity')
    need(_hex(identity['commit'], 40) and _hex(identity['tree'], 40), 'source commit/tree')
    need(all(_hex(identity[k]) for k in ('source_map_sha256', 'runtime_map_sha256', 'go_sha256', 'sdk_sha256')), 'source/tool hashes')
    need(type(identity['platform']) is str and re.fullmatch('[a-z0-9]+/[a-z0-9]+', identity['platform']), 'platform')
    need(type(identity['build_env']) is dict and all(type(k) is str and type(v) is str for k, v in identity['build_env'].items()), 'build env')
    tools = identity['tools']
    need(type(tools) is dict and tools, 'build tools')
    for name, tool in tools.items():
        need(type(name) is str and type(tool) is dict and set(tool) == {'path', 'sha256'} and
             type(tool['path']) is str and tool['path'].startswith('/') and _hex(tool['sha256']), 'build tool pin')
    entries = obj['artifacts']
    need(type(entries) is list and len(entries) == 2, 'two helper artifacts')
    need([e.get('kind') if type(e) is dict else None for e in entries] == ['source', 'capture'], 'helper kinds/order')
    for entry in entries:
        need(set(entry) == {'kind', 'path', 'sha256', 'size', 'cwd', 'test', 'input_schema', 'output_schema', 'helper_path', 'helper_sha256'}, 'artifact fields')
        cwd = 'internal/taskbench' if entry['kind'] == 'source' else 'internal/observability'
        need(entry['cwd'] == cwd and _relative(entry['path']) and _relative(entry['helper_path']) and
             entry['helper_path'].startswith(cwd+'/') and entry['helper_path'].endswith('_test.go'), 'artifact paths/cwd')
        need(_hex(entry['sha256']) and _hex(entry['helper_sha256']) and type(entry['size']) is int and entry['size'] > 0, 'artifact bytes')
        need(type(entry['test']) is str and re.fullmatch('Test[A-Za-z0-9_]+', entry['test']), 'exact test')
        need(all(type(entry[k]) is str and entry[k] for k in ('input_schema', 'output_schema')), 'helper schemas')
    need(len({e['path'] for e in entries}) == 2, 'distinct binaries')
    need(_hex(obj['build_evidence_sha256']) and _hex(obj['signature'], 128), 'catalog evidence/signature')
    need(verify_signature(trusted_public_key, catalog_signing_bytes(obj), obj['signature']) is True, 'catalog signature rejected')
    return obj


def _no_alias(path):
    path = Path(path)
    need(path.is_absolute(), 'absolute path required')
    for part in reversed([path, *path.parents]):
        need(not part.is_symlink(), 'symlink path')
    return path


@dataclass(frozen=True)
class FilePin:
    path: str
    sha256: str
    size: int
    device: int
    inode: int
    mode: int
    uid: int
    immutable: bool


def pin_file(path, *, immutable=True):
    path = _no_alias(path)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        before = os.fstat(fd)
        need(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_uid == os.getuid(), 'regular single-link owned file')
        need(not immutable or stat.S_IMODE(before.st_mode) in (0o400, 0o500), 'private immutable file mode')
        chunks = []
        while True:
            block = os.read(fd, 1024*1024)
            if not block:
                break
            chunks.append(block)
        after = os.fstat(fd)
        current = os.stat(path, follow_symlinks=False)
        fields = lambda s: (s.st_dev, s.st_ino, s.st_mode, s.st_nlink, s.st_uid, s.st_size, s.st_mtime_ns, s.st_ctime_ns)
        need(fields(before) == fields(after) == fields(current), 'file changed while reading')
        return FilePin(str(path), sha(b''.join(chunks)), before.st_size, before.st_dev, before.st_ino,
                       stat.S_IMODE(before.st_mode), before.st_uid, immutable)
    finally:
        os.close(fd)


def check_file(pin):
    if type(pin) is SystemToolPin:
        check_tool(pin)
        return
    need(type(pin) is FilePin and pin_file(pin.path, immutable=pin.immutable) == pin, 'file identity/bytes mismatch')


@dataclass(frozen=True)
class SystemToolPin:
    """Only catalog-selected installed tools, never source/settings/artifacts.

    OS-owned tool hardlinks are legitimate; their ACTUAL link count and complete
    identity are fixed. This is specified-tool measurement, not hermetic SDK.
    """
    path: str
    sha256: str
    size: int
    device: int
    inode: int
    mode: int
    file_type: int
    uid: int
    gid: int
    nlink: int
    mtime_ns: int
    ctime_ns: int

    @property
    def immutable(self):return False


def pin_tool(path):
    """Trusted caller must compare path/SHA to its signed catalog selection."""
    path=_no_alias(path)
    need(path.resolve()==path,'canonical installed tool path')
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW)
    try:
        before=os.fstat(fd)
        need(stat.S_ISREG(before.st_mode) and before.st_uid in (0,os.getuid()) and before.st_nlink>=1,'regular root/current-user tool')
        need(before.st_mode & 0o022==0,'tool group/world writable')
        digest=hashlib.sha256()
        while True:
            block=os.read(fd,1024*1024)
            if not block:break
            digest.update(block)
        after=os.fstat(fd);current=os.stat(path,follow_symlinks=False)
        fields=lambda s:(s.st_dev,s.st_ino,s.st_mode,s.st_nlink,s.st_uid,s.st_gid,s.st_size,s.st_mtime_ns,s.st_ctime_ns)
        need(fields(before)==fields(after)==fields(current),'tool identity changed during read')
        return SystemToolPin(str(path),digest.hexdigest(),before.st_size,before.st_dev,before.st_ino,
            stat.S_IMODE(before.st_mode),stat.S_IFMT(before.st_mode),before.st_uid,before.st_gid,before.st_nlink,before.st_mtime_ns,before.st_ctime_ns)
    finally:os.close(fd)


def check_tool(pin):
    need(type(pin) is SystemToolPin and pin_tool(pin.path)==pin,'catalog tool identity/bytes mismatch')


@dataclass(frozen=True)
class TreePin:
    root: str
    directories: tuple
    files: tuple


def pin_tree(root, expected_files):
    """Capture an EXACT expected file-map, including separately declared extras.

    Factory must compare that map to its selected signed catalog/source seals.
    No allowed-set is rebuilt after execution. Directory identities are retained.
    """
    root = _no_alias(root)
    need(root.is_dir(), 'existing runtime tree root')
    need(type(expected_files) is dict and all(_relative(k) and _hex(v) for k, v in expected_files.items()), 'expected tree map')
    directories, found = [], {}
    for current, dirs, files in os.walk(root, followlinks=False):
        current = _no_alias(Path(current))
        s = current.stat(follow_symlinks=False)
        need(stat.S_ISDIR(s.st_mode) and s.st_uid == os.getuid(), 'owned tree directory')
        directories.append((str(current.relative_to(root)), s.st_dev, s.st_ino, stat.S_IMODE(s.st_mode)))
        for name in dirs:
            _no_alias(current/name)
        for name in files:
            p = pin_file(current/name)
            found[str((current/name).relative_to(root))] = p
    need(set(found) == set(expected_files), 'tree exact file set')
    need(all(found[k].sha256 == v for k, v in expected_files.items()), 'tree bytes')
    return TreePin(str(root), tuple(sorted(directories)), tuple(found[k] for k in sorted(found)))


def check_tree(pin):
    current = pin_tree(pin.root, {str(Path(p.path).relative_to(pin.root)): p.sha256 for p in pin.files})
    need(current == pin, 'tree identity changed')


@dataclass(frozen=True)
class HelperSelection:
    """Selected DATA only. Not a proof and not execution authorization."""
    binary: FilePin
    cwd: str
    argv: tuple
    input_schema: str
    output_schema: str


def select_helper(catalog, kind, *, artifact_root, runtime_tree, tool_pins):
    """Called only on load_catalog output by the trusted new factory.

    Its exact catalog SHA/signature were already selected externally. Freshly
    checks every supplied tool, complete runtime bytes/set, helper, and binary.
    Factory also supplies original archive/job/settings pins to run_owned.
    """
    need(kind in ('source', 'capture'), 'fixed helper kind')
    root = _no_alias(Path(artifact_root))
    check_tree(runtime_tree)
    runtime_map = {str(Path(p.path).relative_to(runtime_tree.root)): p.sha256 for p in runtime_tree.files}
    need(sha(canonical(runtime_map)) == catalog['identity']['runtime_map_sha256'], 'catalog runtime map')
    need(set(tool_pins) == set(catalog['identity']['tools']), 'complete tool pin set')
    for name, pin in tool_pins.items():
        check_tool(pin)
        expected = catalog['identity']['tools'][name]
        need(pin.path == expected['path'] and pin.sha256 == expected['sha256'], 'catalog tool bytes/path')
    entries = [e for e in catalog['artifacts'] if e['kind'] == kind]
    need(len(entries) == 1, 'one selected helper')
    entry = entries[0]
    need(_relative(entry['path']) and _relative(entry['helper_path']), 'artifact paths')
    need(runtime_map.get(entry['helper_path']) == entry['helper_sha256'], 'helper source bytes')
    binary = pin_file(root/entry['path'])
    need(binary.sha256 == entry['sha256'] and binary.size == entry['size'] and binary.mode & 0o111, 'catalog executable bytes')
    cwd = _no_alias(Path(runtime_tree.root)/entry['cwd'])
    need(cwd.is_dir(), 'selected package cwd')
    argv = (binary.path, '-test.run=^'+entry['test']+'$', '-test.v', '-test.count=1', '-test.timeout=2m')
    return HelperSelection(binary, str(cwd), argv, entry['input_schema'], entry['output_schema'])


class Blocked(RuntimeError):
    pass


class Interrupted(RuntimeError):
    pass


def _fsync_dir(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def _write(path, raw):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as out:
        out.write(raw)
        out.flush()
        os.fsync(out.fileno())
    _fsync_dir(path.parent)


def _cleanup(child):
    result = {'leader_reaped': False, 'group_empty': False, 'confirmed': False, 'actions': [], 'error_type': None, 'errors': []}
    def alive():
        child.poll()
        try:
            os.killpg(child.pid, 0)
            return True
        except ProcessLookupError:
            return False
        except OSError as exc:
            # A denied probe is UNKNOWN, never absence. Keep attempting bounded
            # cleanup/reap; only a later genuine ESRCH can establish group-empty.
            result['error_type'] = type(exc).__name__
            if not result['errors'] or result['errors'][-1] != ['probe', type(exc).__name__]:
                result['errors'].append(['probe', type(exc).__name__])
            return True
    try:
        for sig, seconds in ((signal.SIGTERM, 15), (signal.SIGKILL, 3)):
            if not alive():
                break
            try:
                os.killpg(child.pid, sig)
                result['actions'].append(sig.name)
            except ProcessLookupError:
                pass
            except OSError as exc:
                result['error_type'] = type(exc).__name__
                result['errors'].append([sig.name, type(exc).__name__])
            end = time.monotonic()+seconds
            while alive() and time.monotonic() < end:
                time.sleep(.02)
        if child.poll() is None:
            try:
                child.wait(timeout=1)
            except subprocess.TimeoutExpired:
                pass
        result['leader_reaped'] = child.poll() is not None
        result['group_empty'] = not alive()
        result['confirmed'] = result['leader_reaped'] and result['group_empty']
    except BaseException as exc:
        result['error_type'] = type(exc).__name__
    return result


def run_owned(*, state_root, attempt_name, argv, cwd, env, executable_pin, inputs,
              timeout, trees=(), stdin_factory=None, validate_output=None, phase='helper-run'):
    """One trusted-factory invocation, no retry, no helper output/proof parsing.

    state_root must be the SAME fixed scope/preparation supervisor root across
    every window/build, never selected by a job. Any failed or incomplete attempt
    leaves active.json; this primitive has no reset/unblock API.
    validate_output must perform the factory's ORIGINAL marker/schema/semantic
    byte validation and raise on failure. It must NOT construct a proof or seal
    an artifact before this function persists its accepted process receipt.
    Without that callback accepted means process+pins only, never helper-valid.
    """
    need(threading.current_thread() is threading.main_thread(), 'signals require main thread')
    need(type(attempt_name) is str and re.fullmatch('[a-zA-Z0-9][a-zA-Z0-9_-]{0,95}', attempt_name), 'attempt name')
    need(type(argv) is list and argv and all(type(x) is str for x in argv) and argv[0] == executable_pin.path, 'fixed argv')
    need(type(timeout) in (int, float) and 0 < timeout <= 3600, 'bounded timeout')
    need(phase in ('helper-run', 'build-source', 'build-capture', 'source', 'capture'), 'fixed phase')
    need(phase not in ('build-source', 'build-capture', 'source') or stdin_factory is None, 'only capture stdin')
    need(type(env) is dict and all(type(k) is str and type(v) is str for k, v in env.items()), 'explicit environment')
    need(not any(re.search('KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL', k, re.I) for k in env), 'no credential env')
    state_root = _no_alias(Path(state_root))
    if not state_root.exists():
        state_root.mkdir(mode=0o700)
        _fsync_dir(state_root.parent)
    s = state_root.stat()
    need(s.st_uid == os.getuid() and stat.S_IMODE(s.st_mode) == 0o700, 'private shared state root')
    lockfd = os.open(state_root/'lock', os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        lockstat = os.fstat(lockfd)
        need(stat.S_ISREG(lockstat.st_mode) and lockstat.st_uid == os.getuid() and lockstat.st_nlink == 1 and stat.S_IMODE(lockstat.st_mode) == 0o600, 'state lock')
        try:
            fcntl.flock(lockfd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise Blocked('owned invocation already running') from None
        if (state_root/'active.json').exists() or (state_root/'active.json').is_symlink():
            raise Blocked('prior failed/incomplete intent; no automatic recovery')
        attempts = state_root/'attempts'
        if not attempts.exists():
            attempts.mkdir(mode=0o700)
        _no_alias(attempts)
        outdir = attempts/attempt_name
        outdir.mkdir(mode=0o700)
        _fsync_dir(attempts)
        _write(state_root/'active.json', canonical({'schema': 'owned-helper-intent.v1', 'attempt': attempt_name,
               'argv': argv, 'cwd': str(cwd), 'env': env, 'timeout_seconds': timeout,
               'phase': phase, 'executable_sha256': executable_pin.sha256, 'started_epoch': time.time()}))
        return _execute(outdir, state_root, argv, cwd, env, executable_pin, tuple(inputs), tuple(trees), timeout, stdin_factory, validate_output, phase)
    finally:
        os.close(lockfd)


def _execute(outdir, state_root, argv, cwd, env, executable_pin, inputs, trees, timeout, stdin_factory, validate_output, phase):
    child = None
    launching = cleaning = False
    signals = []
    failure = None
    failure_stage = None
    stage = 'preflight'
    cleanup = None
    receipt = None
    late_writing = False
    original_intent = (state_root/'active.json').read_bytes()
    previous = {s: signal.getsignal(s) for s in (signal.SIGINT, signal.SIGTERM)}
    def record_late_interruption():
        nonlocal late_writing
        if receipt is None:
            return
        receipt.update(accepted=False, failure_type='Interrupted', failure_stage='terminal-commit')
        if late_writing:
            return
        late_writing = True
        try:
            # active may already have been unlinked, but the scope lock is still
            # held. Restore the original intent before recording terminal abort.
            if not os.path.lexists(state_root/'active.json'):
                _write(state_root/'active.json', original_intent)
            while True:
                count = len(signals)
                receipt['signals'] = list(signals)
                name = f'interrupted-receipt-{count:06d}.json'
                receipt['receipt_file'] = name
                if not (outdir/name).exists():
                    _write(outdir/name, canonical(receipt))
                if len(signals) == count:
                    break
        finally:
            late_writing = False
    def interrupted(signum, frame):
        signals.append(signum)
        if cleaning and receipt is not None:
            record_late_interruption()
        if not launching and not cleaning:
            raise Interrupted('supervisor signal')
    for s in previous:
        signal.signal(s, interrupted)
    secret = view = None
    stdout = stderr = None
    try:
        need(phase in ('build-source', 'build-capture') or executable_pin.immutable, 'selected helper executable must be immutable')
        check_file(executable_pin)
        need(os.access(executable_pin.path, os.X_OK), 'executable permission')
        cwd = _no_alias(Path(cwd))
        need(cwd.is_dir(), 'package cwd')
        for p in inputs:
            check_file(p)
        for t in trees:
            check_tree(t)
        if stdin_factory is not None:
            stage = 'stdin-read'
            secret = stdin_factory()
            need(type(secret) is bytes, 'stdin bytes')
        stdout = os.fdopen(os.open(outdir/'stdout', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb', buffering=0)
        stderr = os.fdopen(os.open(outdir/'stderr', os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb', buffering=0)
        deadline = time.monotonic()+timeout
        stage = 'spawn'
        launching = True
        try:
            child = subprocess.Popen(argv, cwd=cwd, env=env, stdin=subprocess.DEVNULL if secret is None else subprocess.PIPE,
                                     stdout=stdout, stderr=stderr, start_new_session=True, close_fds=True)
        finally:
            launching = False
        if signals:
            raise Interrupted('signal during spawn')
        _write(outdir/'process.json', canonical({'owned_pgid': child.pid, 'new_session': True, 'argv': argv, 'cwd': str(cwd)}))
        if secret is not None:
            os.set_blocking(child.stdin.fileno(), False)
            view = memoryview(secret)
        stage = 'stdin-write-or-wait'
        while True:
            if time.monotonic() >= deadline:
                raise TimeoutError('helper deadline')
            if view is not None:
                if not view:
                    child.stdin.close()
                    view.release()
                    view = None
                    secret = None
                else:
                    try:
                        sent = os.write(child.stdin.fileno(), view)
                        view = view[sent:]
                    except BlockingIOError:
                        pass
            status = child.poll()
            if status is not None:
                need(view is None, 'helper exited before stdin complete')
                need(status == 0, 'helper nonzero exit')
                break
            time.sleep(.01)
    except BaseException as exc:
        failure = type(exc).__name__
        failure_stage = stage
    finally:
        cleaning = True
        try:
            if view is not None:
                view.release()
            view = secret = None
            if child is not None:
                try:
                    if child.stdin is not None and not child.stdin.closed:
                        child.stdin.close()
                except BaseException as exc:
                    failure = failure or type(exc).__name__
                    failure_stage = failure_stage or 'stdin-close'
                finally:
                    cleanup = _cleanup(child)
            for stream in (stdout, stderr):
                if stream is not None:
                    os.fsync(stream.fileno())
                    stream.close()
            try:
                check_file(executable_pin)
                for p in inputs:
                    check_file(p)
                for t in trees:
                    check_tree(t)
            except BaseException as exc:
                failure = failure or type(exc).__name__
                failure_stage = failure_stage or 'postflight'
            if failure is None and not signals and cleanup is not None and cleanup['confirmed'] and validate_output is not None:
                try:
                    validate_output(outdir)
                except BaseException as exc:
                    failure = type(exc).__name__
                    failure_stage = 'output-validation'
            accepted = failure is None and not signals and cleanup is not None and cleanup['confirmed']
            receipt = {'schema': 'owned-helper-process.v1', 'accepted': accepted,
                       'phase': phase, 'failure_type': failure, 'failure_stage': failure_stage, 'signals': list(signals),
                       'receipt_file': 'receipt.json',
                       'output_validation_supplied': validate_output is not None,
                       'owned_pgid': None if child is None else child.pid,
                       'exit_code': None if child is None else child.poll(), 'cleanup': cleanup,
                       'executable': asdict(executable_pin), 'inputs': [asdict(p) for p in inputs],
                       'stdout_sha256': sha((outdir/'stdout').read_bytes()) if stdout is not None else None,
                       'stderr_sha256': sha((outdir/'stderr').read_bytes()) if stderr is not None else None,
                       'ended_epoch': time.time(), 'proof_issued': False}
            _write(outdir/'receipt.json', canonical(receipt))
            if receipt['accepted']:
                (state_root/'active.json').unlink()
                _fsync_dir(state_root)
            # Also covers a signal between evaluating the branch and unlink:
            # the handler's earlier restored barrier may have been removed by
            # that already selected branch. Never use the cached accepted bool.
            if signals and accepted:
                record_late_interruption()
        finally:
            # Completion horizon begins only AFTER terminal persistence/fsync:
            # atomically block both signals for handoff to the caller's original
            # policy. Drain already pending signals conservatively as interrupted
            # attempts, so restoring the two handlers has no half-installed gap.
            # New signals after this horizon are delivered under caller policy
            # on unmask; they are not swallowed by our cleanup latch.
            old_mask = signal.pthread_sigmask(signal.SIG_BLOCK, set(previous))
            try:
                for s in signal.sigpending() & set(previous):
                    signal.sigwait({s})
                    signals.append(s)
                    record_late_interruption()
            finally:
                try:
                    for s, handler in previous.items():
                        signal.signal(s, handler)
                finally:
                    signal.pthread_sigmask(signal.SIG_SETMASK, old_mask)
    return receipt
