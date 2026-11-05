#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/../.."
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT
python3 - "$tmp_dir/overlay.json" <<'PY'
import json, pathlib, sys
root=pathlib.Path.cwd()
pathlib.Path(sys.argv[1]).write_text(json.dumps({'Replace': {str(root/'internal/gateway/client/router_report_duration_test.go'):str(root/'eval/project-load-2026-11-02/router_report_duration_test.go.txt')}}))
PY
go test -race -overlay "$tmp_dir/overlay.json" ./internal/gateway/client -run '^TestRouterReport(DurationIncludesFailure|RecordsTransportAndHTTPOutcome|RecordsMalformedRequestAndBodyFailure)$' -count=3
