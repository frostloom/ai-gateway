import json, pathlib, subprocess, sys, tempfile
root=pathlib.Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory(prefix="router-lock-overlay-") as d:
 args=["go","test","-race"]
 if "--candidate" in sys.argv:
  overlay=pathlib.Path(d)/"overlay.json"
  overlay.write_text(json.dumps({"Replace":{str(root/"internal/router/registry.go"):str(root/"eval/project-load-2026-11-02/router_registry_lock_scope_candidate.go.txt")}}))
  args += ["-overlay",str(overlay)]
 args += ["./eval/billing-comparison-2026-11-02","-run","^TestRouter(CachedRouteProgressDuringBlockedReport|ReportSequencePersistsInOrder|DifferentProvidersReportIndependently|ReportWriteFailureStillAcknowledged)$","-count=3","-v"]
 raise SystemExit(subprocess.run(args,cwd=root).returncode)
