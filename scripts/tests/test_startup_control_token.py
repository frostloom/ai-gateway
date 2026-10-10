"""Offline startup regressions; no project .env, services, ports or credentials.

Runs the actual PowerShell entry point in a temporary project. Only OS process
and socket operations are replaced; token setup and service selection stay real.
"""

import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
POWERSHELL = shutil.which("powershell") or shutil.which("pwsh")
GIT = shutil.which("git")
GIT_BASH = Path(GIT).resolve().parents[1] / "bin/bash.exe" if GIT else None


@unittest.skipUnless(POWERSHELL, "PowerShell unavailable")
class StartupControlTokenTests(unittest.TestCase):
    def run_startup(self, *, only="", token=None, dotenv=None):
        scratch = ROOT / "logs"
        scratch.mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(prefix="offline-startup-", dir=scratch) as directory:
            root = Path(directory)
            (root / "scripts").mkdir()
            (root / "bin").mkdir()
            shutil.copyfile(ROOT / "scripts/start-all.ps1", root / "scripts/start-all.ps1")
            for name in ("billing", "router", "mock-provider", "gateway", "reconciler", "event-consumer", "agent"):
                (root / "bin" / (name + ".exe")).touch()
            if dotenv is not None:
                (root / ".env").write_text(dotenv, encoding="utf-8")
            # Fakes record child environment without running binaries or touching sockets.
            harness = root / "harness.ps1"
            harness.write_text(r'''
param([string]$Only = '')
$ErrorActionPreference = 'Stop'
$global:children = @()
$global:stopped = 0
function Start-Process {
    param($FilePath, $WorkingDirectory, $WindowStyle, $RedirectStandardOutput, $RedirectStandardError, $ArgumentList)
    $global:children += @{ Name = [IO.Path]::GetFileName($FilePath); Token = $env:CONTROL_PLANE_TOKEN }
}
function Get-NetTCPConnection { param($LocalPort, $State, $ErrorAction) [pscustomobject]@{OwningProcess=999999} }
function Stop-Process { param($Id, [switch]$Force, $ErrorAction) $global:stopped++ }
function Start-Sleep { param($Seconds, $Milliseconds) }
try {
    & "$PSScriptRoot/scripts/start-all.ps1" -Only $Only -SkipInfra
} finally {
    @{ Children = @($global:children); Stopped = $global:stopped } |
        ConvertTo-Json -Depth 5 | Set-Content "$PSScriptRoot/result.json" -Encoding UTF8
}
''', encoding="utf-8-sig")
            env = os.environ.copy()
            env.pop("CONTROL_PLANE_TOKEN", None)
            if token is not None:
                env["CONTROL_PLANE_TOKEN"] = token
            result = subprocess.run(
                [POWERSHELL, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", str(harness), "-Only", only],
                env=env, capture_output=True, timeout=25,
            )
            observed = json.loads((root / "result.json").read_text(encoding="utf-8-sig"))
            return result, observed

    def test_fresh_shell_partial_restart_rejects_missing_stable_token_before_stopping(self):
        result, observed = self.run_startup(only="agent")
        self.assertNotEqual(result.returncode, 0, "Partial restart silently generated a new token")
        self.assertEqual(observed["Stopped"], 0)
        self.assertEqual(observed["Children"], [])

    def test_partial_restart_passes_explicit_token_to_selected_child(self):
        result, observed = self.run_startup(only="agent", token="offline-stable-token")
        self.assertEqual(result.returncode, 0)
        self.assertEqual(observed["Children"], [{"Name": "agent.exe", "Token": "offline-stable-token"}])
        self.assertNotIn(b"offline-stable-token", result.stdout + result.stderr)

    def test_partial_restart_accepts_quoted_token_from_synthetic_dotenv(self):
        result, observed = self.run_startup(only="router", dotenv='CONTROL_PLANE_TOKEN="offline-dotenv-token"\n')
        self.assertEqual(result.returncode, 0)
        self.assertEqual(observed["Children"], [{"Name": "router.exe", "Token": "offline-dotenv-token"}])

    def test_full_startup_generates_one_shared_token_without_printing_it(self):
        result, observed = self.run_startup()
        self.assertEqual(result.returncode, 0)
        children = observed["Children"]
        self.assertEqual(len(children), 9)
        tokens = {child["Token"] for child in children}
        self.assertEqual(len(tokens), 1)
        token = tokens.pop()
        self.assertEqual(len(token), 32)
        self.assertNotIn(token.encode(), result.stdout + result.stderr)


@unittest.skipUnless(GIT_BASH and GIT_BASH.exists(), "Git Bash unavailable")
class ScenarioControlTokenTests(unittest.TestCase):
    def run_probe(self, token, route_index=0):
        """Execute M5 setup and its actual route pipeline against a fake curl.

        Copy only declarations before stage 1: the destructive restart/scenario
        body is never invoked. The route pipeline is taken from that body so
        removing its authentication makes this behavioral probe fail.
        """
        source = (ROOT / "scripts/m5/scenario.sh").read_text(encoding="utf-8")
        setup = source.split("# ================= 阶段 1", 1)[0]
        routes = [line for line in source.splitlines() if 'pid=$(curl' in line]
        route = routes[route_index]
        scratch = ROOT / "logs"
        scratch.mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(prefix="offline-m5-", dir=scratch) as directory:
            root = Path(directory)
            scripts = root / "scripts/m5"
            scripts.mkdir(parents=True)
            probe = scripts / "probe.sh"
            # curl stands in for the protected route endpoint, with no network.
            # All credential strings here are synthetic test fixture values.
            fake = r'''
curl() {
  local found=0 arg
  for arg in "$@"; do
    [ "$arg" = 'X-Control-Token: offline-m5-token' ] && found=1
  done
  if [ "$found" = 1 ]; then printf '{"provider_id":1}'; else return 22; fi
}
'''
            interpreter = sys.executable.replace("\\", "/")
            probe.write_text(setup + fake + '\npython() { "' + interpreter + '" "$@"; }\n' + route + '\nprintf "%s" "$pid"\n', encoding="utf-8")
            env = os.environ.copy()
            env.pop("CONTROL_PLANE_TOKEN", None)
            if token is not None:
                env["CONTROL_PLANE_TOKEN"] = token
            return subprocess.run([str(GIT_BASH), str(probe)], env=env, capture_output=True, timeout=20)

    def test_route_probe_authenticates_using_configured_control_token(self):
        for route_index in range(3):
            with self.subTest(route_index=route_index):
                result = self.run_probe("offline-m5-token", route_index)
                self.assertEqual(result.returncode, 0)
                self.assertEqual(result.stdout, b"1")
                self.assertNotIn(b"offline-m5-token", result.stdout + result.stderr)

    def test_missing_control_token_fails_before_scenario_requests(self):
        result = self.run_probe(None)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b"CONTROL_PLANE_TOKEN", result.stderr)


if __name__ == "__main__":
    unittest.main()
