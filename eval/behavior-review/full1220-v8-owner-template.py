"""Inactive V8 once-owner template. Explicit commands, no paid window loop.

Render ROOT and PINNED_VERIFIER_SHA256 only after independent technical review.
The signed external authorization is created later from the human's exact reply.
This file does not generate it, infer approval, retry commands or reopen owners.
"""
import sys
sys.dont_write_bytecode=True
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import signal
import stat
import subprocess
import time
import types

ROOT = 'UNFROZEN'
PINNED_VERIFIER_SHA256 = 'UNFROZEN'


def need(ok,message):
    if not ok:raise ValueError(message)


def _json(raw):
    def pairs(items):
        out={}
        for k,v in items:need(k not in out,'duplicate command key');out[k]=v
        return out
    value=json.loads(raw,object_pairs_hook=pairs)
    need(type(value) is dict,'command object');return value


def command(raw,index,prior):
    need(type(raw) is bytes and 0<len(raw)<=4096,'bounded explicit operation')
    value=_json(raw)
    op=value.get('operation')
    if op in ('status','close'):
        need(set(value)=={'operation'},'exact read/close command');return value
    need(op=='run_window' and set(value)=={'operation','expected_index','prior_receipt_sha256'},'explicit exact window command')
    need(type(value['expected_index']) is int and value['expected_index']==index and
         value['prior_receipt_sha256']==prior,'ordered window/prior verified summary required')
    return value


def _load(root):
    need(type(PINNED_VERIFIER_SHA256) is str and re.fullmatch('[0-9a-f]{64}',PINNED_VERIFIER_SHA256),'UNFROZEN verifier pin')
    path=root/'verifier-preparation/verify-window.py'
    for parent in (root,path.parent):need(parent.resolve()==parent and stat.S_ISDIR(parent.lstat().st_mode),'canonical verifier parent')
    before=path.lstat();need(stat.S_ISREG(before.st_mode) and before.st_nlink==1 and before.st_size<256<<10,'verifier regular single-link bound')
    ident=lambda st:(st.st_dev,st.st_ino,st.st_mode,st.st_nlink,st.st_size,st.st_mtime_ns,st.st_ctime_ns)
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    try:
        need(ident(os.fstat(fd))==ident(before),'verifier open changed');raw=b''
        while True:
            chunk=os.read(fd,65536)
            if not chunk:break
            raw+=chunk;need(len(raw)<256<<10,'verifier read bound')
        need(ident(os.fstat(fd))==ident(before)==ident(path.lstat()),'verifier read changed')
    finally:os.close(fd)
    need(hashlib.sha256(raw).hexdigest()==PINNED_VERIFIER_SHA256,'verifier SHA')
    module=types.ModuleType('v8_private_verifier');module.__file__=str(path)
    exec(compile(raw,str(path),'exec'),module.__dict__)
    return module


def _cleanup(child):
    def alive():
        child.poll()
        try:os.killpg(child.pid,0);return True
        except ProcessLookupError:return False
    for sig,seconds in ((signal.SIGTERM,15),(signal.SIGKILL,3)):
        if not alive():break
        try:os.killpg(child.pid,sig)
        except ProcessLookupError:pass
        until=time.monotonic()+seconds
        while alive() and time.monotonic()<until:time.sleep(.05)
    if child.poll() is None:
        try:child.wait(timeout=1)
        except subprocess.TimeoutExpired:pass
    return {'owned_pgid':child.pid,'leader_reaped':child.poll() is not None,'group_empty':not alive()}


def run(binding_sha):
    need(type(ROOT) is str and ROOT.startswith('/') and 'v8' in Path(ROOT).name,'UNFROZEN new V8 root')
    root=Path(ROOT);need(root.resolve()==root and Path(__file__).absolute()==root/'owner-preparation/run-owner.py' and Path(__file__).resolve()==Path(__file__).absolute(),'canonical rendered owner path')
    os.umask(0o077)
    v=_load(root);context=v.load_freeze_binding(root,'runtime-binding.json',binding_sha)
    reader=context.reader;m=context.manifest
    need(context.binding['owner_sha256']==hashlib.sha256(reader.read('owner-preparation/run-owner.py')).hexdigest(),'audited owner bytes')
    # Secret configuration is read only on explicit execution, hashed, never logged.
    settings=root/'final-private-settings.json';st=settings.lstat()
    need(stat.S_IMODE(st.st_mode)==0o600 and st.st_uid==os.getuid(),'owned0600 actual settings')
    reader.read('final-private-settings.json',m['private_settings_sha256'])
    names=['campaign','records','actual-owner-launch-intent.json','actual-owner-process.json','actual-owner-exit.json','actual-owner-group-cleanup.json','actual-owner.stdout.jsonl','actual-owner.stderr','actual-owner-commands.jsonl']
    for name in names:need(not os.path.lexists(root/name),'never relaunch/reclaim existing V8 owner')
    write=lambda name,value:v._exclusive(root/name,json.dumps(value,ensure_ascii=False,sort_keys=True).encode()+b'\n')
    write('actual-owner-launch-intent.json',{'schema':'full1220-v8-once-owner-launch.v1','manifest_sha256':v.FROZEN_MANIFEST_SHA256,
        'binary_sha256':v.FROZEN_BINARY_SHA256,'authorization_sha256':v.sha(context.authority_raw),'binding_sha256':binding_sha,
        'automatic_paid_loop':False,'relaunch_allowed':False,'started_epoch':time.time()})
    argv=[str(root/'full-window-actual-v8'),'-serve-actual','-manifest',str(root/'actual-manifest.json'),
          '-authorization',str(root/'execution-authorization.json'),'-private-settings',str(settings)]
    child=None;awake=None;code=1;failure=None;cleanup=None
    termination_signal=None;launching=False
    class OwnerTermination(RuntimeError):pass
    previous_term=signal.getsignal(signal.SIGTERM)
    def terminate(signum,frame):
        nonlocal termination_signal
        if termination_signal is not None:return
        termination_signal=signum
        # Popen must finish assigning its child before the fail-stop exception,
        # so a TERM delivered during spawn cannot lose the owned process handle.
        if not launching:raise OwnerTermination('supervisor received SIGTERM')
    signal.signal(signal.SIGTERM,terminate)
    try:
        awake=subprocess.Popen(['/usr/bin/caffeinate','-i','-w',str(os.getpid())],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        need(awake.poll() is None,'awake assertion failed')
        with (root/'actual-owner.stdout.jsonl').open('xb',buffering=0) as out,(root/'actual-owner.stderr').open('xb',buffering=0) as err,(root/'actual-owner-commands.jsonl').open('xb',buffering=0) as journal:
            launching=True
            try:child=subprocess.Popen(argv,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=err,start_new_session=True,bufsize=0)
            finally:launching=False
            if termination_signal is not None:raise OwnerTermination('supervisor received SIGTERM during spawn')
            write('actual-owner-process.json',{'pid':child.pid,'owned_pgid':child.pid,'new_session':True,'command':argv,'started_epoch':time.time(),'automatic_paid_loop':False})
            selection=selectors.DefaultSelector();selection.register(child.stdout,selectors.EVENT_READ);buffer=b''
            def receive(limit):
                nonlocal buffer
                until=time.monotonic()+limit
                while b'\n' not in buffer:
                    remaining=until-time.monotonic();need(remaining>0,'owner response timeout; fail-stop')
                    need(selection.select(remaining),'owner response timeout; fail-stop')
                    block=os.read(child.stdout.fileno(),65536);need(block,'owner exited without terminal response')
                    buffer+=block;need(len(buffer)<=16<<20,'owner response byte bound')
                raw,buffer=buffer.split(b'\n',1);raw+=b'\n'
                need(out.write(raw)==len(raw),'owner log short write');os.fsync(out.fileno())
                sys.stdout.buffer.write(raw);sys.stdout.buffer.flush();return v._json(raw)
            def send(value):
                raw=json.dumps(value,separators=(',',':')).encode()+b'\n'
                need(journal.write(raw)==len(raw),'command journal short write');os.fsync(journal.fileno())
                need(child.stdin.write(raw)==len(raw),'owner input short write');child.stdin.flush()
            startup=receive(600)
            need(startup.get('type')=='startup' and startup['status']['next_window_index']==1 and startup['status']['closed'] is False,'unique fresh owner startup')
            ready=startup['readiness']
            need(ready=={'manifest_sha256':v.FROZEN_MANIFEST_SHA256,'scope_sha256':m['scope_sha256'],'execution_sha256':m['execution_sha256'],'pairs':3660,'arms':7320,'defined_turns':11772,'boundaries_excluded':1851},'startup exact preparation')
            index=1;prior=''
            while True:
                raw=sys.stdin.buffer.readline(4097)
                if not raw:
                    send({'operation':'close'});closed=receive(60);need(closed.get('type')=='closed','EOF owner close');break
                operation=command(raw,index,prior);send(operation);reply=receive(600 if operation['operation']=='run_window' else 60)
                if operation['operation']=='close':need(reply.get('type')=='closed','explicit close');break
                if operation['operation']=='status':need(reply.get('type')=='status','status response');continue
                need(reply.get('type')=='window' and reply.get('stopped') is False,'actual window stopped; never retry')
                summary_pin=reply['receipt_sha256'];need(type(summary_pin) is str and re.fullmatch('[0-9a-f]{64}',summary_pin),'raw summary reference')
                output=root/'verifier-preparation'/f'window-{index:06d}-verified.json'
                proofdir=root/'verifier-preparation'/f'window-{index:06d}-source-proof'
                # Verification itself is read-only apart from fresh exclusive proof
                # artifacts. It cannot manufacture another run_window command.
                verified=v.verify_actual(root,binding_sha256=binding_sha,window_index=index,summary_sha256=summary_pin,helper_output_dir=proofdir)
                v._exclusive(output,json.dumps(verified,ensure_ascii=False,sort_keys=True).encode()+b'\n')
                write(f'window-{index:06d}-owner-verification.json',{'summary_sha256':summary_pin,'verified_sha256':v.sha(output.read_bytes()),'next_window_requires_explicit_command':True})
                prior=summary_pin;index+=1
                if index==132:
                    send({'operation':'close'});need(receive(60).get('type')=='closed','final owner close');break
            child.stdin.close();code=child.wait(timeout=30);need(code==0,'owner process nonzero')
    except BaseException as exc:
        failure=type(exc).__name__;raise
    finally:
        # A second TERM must not interrupt group cleanup or evidence writes.
        signal.signal(signal.SIGTERM,signal.SIG_IGN)
        try:
            if child is not None:
                cleanup=_cleanup(child)
                write('actual-owner-group-cleanup.json',cleanup)
                write('actual-owner-exit.json',{'exit_code':child.poll(),'supervisor_exit_code':0 if failure is None and code==0 else 1,
                    'supervision_failure':failure,'supervision_signal':termination_signal,'ended_epoch':time.time(),'relaunch_allowed':False})
        finally:
            try:
                if awake is not None and awake.poll() is None:
                    awake.terminate()
                    try:awake.wait(timeout=5)
                    except subprocess.TimeoutExpired:awake.kill();awake.wait()
            finally:signal.signal(signal.SIGTERM,previous_term)
    need(cleanup is not None and cleanup['leader_reaped'] and cleanup['group_empty'],'owned process group cleanup')
    return code


def main(argv=None):
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--execute',action='store_true');parser.add_argument('--binding-sha256');args=parser.parse_args(argv)
    if not args.execute:
        print('inactive V8 owner: no file reads, process, command, claim or network');return 0
    need(args.binding_sha256 is not None,'exact reviewed binding SHA required')
    return run(args.binding_sha256)

if __name__=='__main__':raise SystemExit(main())
