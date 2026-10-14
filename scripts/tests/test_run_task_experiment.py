"""Exercise the launcher with local executables only; no model/DB calls."""
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[2]
GO = shutil.which('go') or 'E:/go1.25.12.windows-amd64/go/bin/go.exe'
PS = shutil.which('pwsh') or shutil.which('powershell')


def quote(value):
    return "'" + str(value).replace("'", "''") + "'"


def cleanup_temp(folder):
    # Child processes may release their working-directory handle shortly after
    # exit on Windows. Retry only sharing violations, never suppress cleanup.
    target = Path(folder.name).resolve()
    if target == ROOT or not target.is_relative_to(ROOT):
        raise ValueError('test cleanup must remain inside its workspace')
    for attempt in range(20):
        try:
            folder.cleanup()
            return
        except PermissionError as error:
            if getattr(error, 'winerror', None) != 32 or attempt == 19:
                raise
            time.sleep(0.1)


@unittest.skipUnless(PS and Path(GO).exists(), 'requires local PowerShell and Go')
class LauncherTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.build = tempfile.TemporaryDirectory(dir=ROOT)
        folder = Path(cls.build.name)
        source = folder / 'fake.go'
        source.write_text('''package main
import("encoding/json";"net/http";"os";"strings")
func main(){
 if len(os.Args)==1 { os.WriteFile(os.Getenv("TEST_AGENT_MARKER"),[]byte("started"),0600); http.HandleFunc("/healthz",func(w http.ResponseWriter,r *http.Request){w.Write([]byte("{}"))}); http.ListenAndServe("127.0.0.1:"+os.Getenv("AGENT_PORT"),nil); return }
 args:=os.Args[1:]; out:=""; prep:=false
 for i,a:=range args {if a=="-out" {out=args[i+1]}; if a=="-prepare-only" {prep=true}}
 f,_:=os.OpenFile(os.Getenv("TEST_CALL_LOG"),os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600)
 json.NewEncoder(f).Encode(map[string]any{"args":args,"routing":os.Getenv("AGENT_ROUTING_MODE"),"loaded":os.Getenv("TEST_LOADED")}); f.Close()
 os.WriteFile(out,[]byte("{}"),0600)
 if !prep && strings.TrimSpace(os.Getenv("TEST_FAIL_LIVE"))!="" {os.Exit(2)}
}''', encoding='utf-8')
        cls.exe = folder / 'fake.exe'
        env = os.environ.copy()
        env['GOCACHE'] = str(ROOT / '.gocache')
        subprocess.run([GO, 'build', '-o', str(cls.exe), str(source)], env=env, check=True, capture_output=True)

    @classmethod
    def tearDownClass(cls):
        cleanup_temp(cls.build)

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir=ROOT)
        self.addCleanup(cleanup_temp, self.temp)
        self.root = Path(self.temp.name)
        scripts = self.root / 'scripts'
        scripts.mkdir()
        self.script = scripts / 'run-task-experiment.ps1'
        shutil.copyfile(ROOT / 'scripts/run-task-experiment.ps1', self.script)
        (scripts / 'load-env.ps1').write_text("$env:TEST_LOADED='yes'\n$env:TEST_EXISTING='changed'\n$env:AGENT_LLM_API_KEY='fake'\n$env:JEV_API_KEY='fake'\n", encoding='utf-8')
        self.cases = self.root / 'cases.json'
        self.cases.write_text(json.dumps([{'id': 'one'}]), encoding='utf-8')
        self.log = self.root / 'calls.jsonl'
        self.marker = self.root / 'agent-started'
        self.out = self.root / 'output'
        self.env = os.environ.copy()
        self.env.update(TEST_CALL_LOG=str(self.log), TEST_AGENT_MARKER=str(self.marker), TEST_EXISTING='original')
        self.env.pop('TEST_LOADED', None)
        subprocess.run(['git', 'init', '-q', str(self.root)], check=True, capture_output=True)
        subprocess.run(['git', '-C', str(self.root), '-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '--allow-empty', '-qm', 'fixture'], check=True, capture_output=True)

    def run_launcher(self, extra='', live=False, jev='off'):
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        command = (f"& {quote(self.script)} -JevMode {jev} -Version test -AgentExe {quote(self.exe)} "
                   f"-EvaluatorExe {quote(self.exe)} -SourceRevision HEAD -Out {quote(self.out)} "
                   f"-Model fake-model -CasesFile {quote(self.cases)} {extra}")
        if live:
            command += f' -RunEvaluation -Port {port}'
        state = self.root / 'environment.json'
        wrapper = f"try {{ {command} }} catch {{ Write-Output $_; $failed=$true }} finally {{ @{{existing=$env:TEST_EXISTING;loaded=$env:TEST_LOADED;routing=$env:AGENT_ROUTING_MODE}} | ConvertTo-Json | Set-Content -LiteralPath {quote(state)} }}; if ($failed) {{ exit 1 }}"
        result = subprocess.run([PS, '-NoProfile', '-NonInteractive', '-Command', wrapper], env=self.env, capture_output=True, text=True, encoding='utf-8', errors='replace', timeout=40)
        return result, json.loads(state.read_text(encoding='utf-8-sig'))

    def calls(self):
        return [json.loads(row) for row in self.log.read_text().splitlines()] if self.log.exists() else []

    def test_default_prepares_without_loading_env_or_starting_agent(self):
        result, state = self.run_launcher()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse(self.marker.exists())
        self.assertIsNone(state['loaded'])
        calls = self.calls()
        self.assertEqual(len(calls), 1)
        self.assertIn('-prepare-only', calls[0]['args'])
        self.assertNotIn('-allow-simulated-writes', calls[0]['args'])
        self.assertEqual(calls[0]['args'][calls[0]['args'].index('-max-tasks') + 1], '12')
        self.assertTrue((self.out / 'prepare.json').exists())

    def test_conflicting_routing_rejected_before_any_execution(self):
        result, _ = self.run_launcher('-RoutingMode jev_selective')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), [])
        self.assertFalse(self.marker.exists())

    def test_excess_cases_rejected_before_agent_or_evaluator(self):
        self.cases.write_text(json.dumps([{'id': str(i)} for i in range(13)]))
        result, _ = self.run_launcher(live=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), [])
        self.assertFalse(self.marker.exists())

    def test_live_maps_mode_and_restores_all_environment(self):
        result, state = self.run_launcher(live=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertTrue(self.marker.exists())
        self.assertEqual(state['existing'], 'original')
        self.assertIsNone(state['loaded'])
        self.assertEqual(len(self.calls()), 2)
        live = self.calls()[1]
        self.assertEqual(live['routing'], 'model')
        self.assertIn('-prepared', live['args'])
        self.assertIn('-allow-simulated-writes', live['args'])
        self.assertEqual((self.out / 'exit-code.txt').read_text(), '0')

    def test_live_failure_preserves_exit_and_restores_environment(self):
        self.env['TEST_FAIL_LIVE'] = '1'
        result, state = self.run_launcher(live=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue((self.out / 'exit-code.txt').exists(), result.stdout + result.stderr)
        self.assertEqual((self.out / 'exit-code.txt').read_text(), '2')
        self.assertEqual(state['existing'], 'original')
        self.assertIsNone(state['loaded'])

    def test_existing_output_is_not_overwritten(self):
        self.out.mkdir()
        sentinel = self.out / 'evidence'
        sentinel.write_text('preserve')
        result, _ = self.run_launcher()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(sentinel.read_text(), 'preserve')
        self.assertEqual(self.calls(), [])

    def test_jev_on_maps_to_selective_routing(self):
        result, _ = self.run_launcher(jev='on')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        args = self.calls()[0]['args']
        self.assertEqual(args[args.index('-routing-mode') + 1], 'jev_selective')

    def test_zero_cap_rejected_before_execution(self):
        result, _ = self.run_launcher('-MaxTasks 0')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), [])

    def test_obsolete_fast_path_rejected(self):
        result, _ = self.run_launcher('-FastPath')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), [])


if __name__ == '__main__':
    unittest.main()
