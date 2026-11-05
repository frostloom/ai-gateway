#!/usr/bin/env python3
"""Build a bounded-parallel relay overlay from a pinned worktree source."""
import hashlib,json,pathlib
here=pathlib.Path(__file__).resolve().parent;root=here.parents[2]
src=root/'internal/billing/outbox/relay.go'; raw=src.read_text()
assert hashlib.sha256(raw.encode()).hexdigest()=='f9b4e2e60e4e832c57cf8c1c6ccdfa6a69f84c406d9a3664eadf25259718bb9e', 'source changed; re-evaluate overlay'
raw=raw.replace('"sync"\n','"sync"\n\t"sync/atomic"\n',1)
begin=raw.index('\tfor _, row := range rows {',raw.index('func (r *Relay) FlushOnce'))
end=raw.index('\n\tst.LastRunAt = time.Now()',begin)
old=raw[begin:end]
prefix='\tfor _, row := range rows {\n\t\tif err := ctx.Err(); err != nil {\n\t\t\tbreak\n\t\t}\n'
assert old.startswith(prefix) and old.endswith('\n\t}')
body=old[len(prefix):-len('\n\t}')]
for field in ['Failed','DLQed','Sent']:
 assert body.count('st.'+field+'++')==1
 body=body.replace('st.'+field+'++','atomic.AddInt64(&st.'+field+', 1)')
assert body.count('continue')==3
body=body.replace('continue','return')
replacement='''\tvar wg sync.WaitGroup
\tsem := make(chan struct{}, 8)
dispatch:
\tfor _, row := range rows {
\t\tselect {
\t\tcase <-ctx.Done():
\t\t\tbreak dispatch
\t\tcase sem <- struct{}{}:
\t\t}
\t\twg.Add(1)
\t\tgo func(row store.OutboxEvent) {
\t\t\tdefer wg.Done()
\t\t\tdefer func() { <-sem }()
'''+body+'''\n\t\t}(row)
\t}
\twg.Wait()'''
out=here/'relay_parallel_candidate.go.txt';out.write_text(raw[:begin]+replacement+raw[end:])
(here/'parallel-overlay.json').write_text(json.dumps({'Replace':{str(src):str(out)}})+'\n')
print('candidate_sha256='+hashlib.sha256(out.read_bytes()).hexdigest())
