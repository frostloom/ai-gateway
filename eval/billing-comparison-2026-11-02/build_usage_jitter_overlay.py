#!/usr/bin/env python3
"""Add bounded random delay to the test-only usage retry candidate."""
import json,pathlib,subprocess,sys
root=pathlib.Path(__file__).resolve().parents[2];out=pathlib.Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True)
subprocess.run([sys.executable,str(root/'eval/billing-comparison-2026-11-02/build_usage_retry_overlay.py'),str(out)],check=True)
p=out/'chat.go';s=p.read_text();a='"context"';b='time.NewTimer(100 * time.Millisecond)'
if s.count(a)!=1 or s.count(b)!=1:raise ValueError('jitter overlay anchor changed')
s=s.replace(a,a+'\n\t"math/rand/v2"',1).replace(b,'time.NewTimer(time.Duration(50+rand.IntN(101)) * time.Millisecond)',1)
p.write_text(s)
