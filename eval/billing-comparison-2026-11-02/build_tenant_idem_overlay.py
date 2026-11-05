#!/usr/bin/env python3
"""Test-only new namespace; deliberately excludes legacy migration."""
import json,pathlib,sys
root=pathlib.Path(__file__).resolve().parents[2];out=pathlib.Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True);p=root/'internal/gateway/middleware/idempotency.go';s=p.read_text()
def change(a,b):
 global s
 if s.count(a)!=1:raise ValueError('idempotency overlay anchor changed: '+a)
 s=s.replace(a,b,1)
change('"context"','"context"\n\t"crypto/sha256"\n\t"encoding/base64"\n\t"fmt"')
change('''		owner := rid
		if key != "" {
			ok, err := rdb.SetNX(c.Request.Context(), "idem:"+key, rid, 5*time.Minute).Result()''','''		owner := rid
		redisKey := ""
		if key != "" {
			tenant := c.GetInt64("tenant_id")
			if tenant <= 0 {
				abort(c, http.StatusUnauthorized, "invalid tenant")
				return
			}
			digest := sha256.Sum256([]byte(fmt.Sprintf("%d\\x00%s", tenant, key)))
			redisKey = "i2:" + base64.RawURLEncoding.EncodeToString(digest[:])
			ok, err := rdb.SetNX(c.Request.Context(), redisKey, rid, 5*time.Minute).Result()''')
change('rid = "idem:" + key','rid = redisKey')
change('[]string{"idem:" + key}', '[]string{redisKey}')
q=out/'idempotency.go';q.write_text(s);(out/'overlay.json').write_text(json.dumps({'Replace':{str(p):str(q)}}))
