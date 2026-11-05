#!/usr/bin/env python3
"""Experimental response limit, applied only with a Go overlay."""
import json
import pathlib
import sys

root = pathlib.Path(__file__).resolve().parents[2]
output = pathlib.Path(sys.argv[1])
output.mkdir(parents=True, exist_ok=True)
source = root / 'internal/gateway/handler/chat.go'
content = source.read_text()
old = '\t\tbody, readErr := io.ReadAll(upResp.Body)\n\t\tupResp.Body.Close()'
new = '\t\tbody, readErr := io.ReadAll(io.LimitReader(upResp.Body, (1<<20)+1))\n\t\tupResp.Body.Close()\n\t\tif len(body) > 1<<20 {\n\t\t\tlog.Error("upstream response exceeds experimental limit", "request_id", requestID)\n\t\t\tabort(c, http.StatusBadGateway, "upstream response too large")\n\t\t\treturn\n\t\t}'
if content.count(old) != 1:
    raise ValueError('handler overlay anchor changed')
candidate = output / 'chat.go'
candidate.write_text(content.replace(old, new, 1))
(output / 'overlay.json').write_text(json.dumps({'Replace': {str(source): str(candidate)}}))
