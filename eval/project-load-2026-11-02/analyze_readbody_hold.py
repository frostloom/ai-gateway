#!/usr/bin/env python3
"""Correlate synthetic load IDs with local mock's body-read log."""
import collections, csv, json, pathlib, sys
p = pathlib.Path(sys.argv[1])
reads = collections.Counter(line.split(' ', 1)[1] for line in (p / 'mock.log').read_text().splitlines() if line.startswith('TRACE_BODY_READ '))
summary = collections.Counter()
for path in sorted(p.glob('trace-64-*.csv')):
    with path.open(newline='') as f:
        for row in csv.DictReader(f):
            summary[(row['status'], reads[row['request_id']])] += 1
result = {'http_by_body_reads': [{'http': k[0], 'body_reads': k[1], 'requests': n} for k,n in sorted(summary.items())], 'unique_body_ids': len(reads), 'body_ids_repeated': sum(n > 1 for n in reads.values())}
print(json.dumps(result, indent=2))
