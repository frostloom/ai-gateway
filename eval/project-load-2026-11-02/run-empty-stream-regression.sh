#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
tmp_overlay=$(mktemp)
trap 'rm -f "$tmp_overlay"' EXIT
python3 - "$tmp_overlay" <<'PY'
import json, pathlib, sys
root=pathlib.Path.cwd()
pathlib.Path(sys.argv[1]).write_text(json.dumps({'Replace': {str(root/'internal/gateway/handler/empty_stream_regression_test.go'): str(root/'eval/project-load-2026-11-02/empty_stream_regression_test.go.txt')}}))
PY
SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk GOPROXY=off GOTOOLCHAIN=local go test -race -overlay "$tmp_overlay" ./internal/gateway/handler -count=1
