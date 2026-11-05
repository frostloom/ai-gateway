#!/usr/bin/env python3
"""Test-only Redis dirty marker protocol, not a production-ready overlay."""
import hashlib,json,pathlib
root=pathlib.Path(__file__).resolve().parents[2]; out=root/'eval/billing-comparison-2026-11-02'
cash_path=root/'internal/billing/store/cash.go'; mysql_path=root/'internal/billing/store/mysql.go'
cash=cash_path.read_text();mysql=mysql_path.read_text()
assert hashlib.sha256(cash.encode()).hexdigest()=='0e17b053e88753a8f8bcb76c6f82ed2dc345a57127c9d824f4528b805c3c9584'
assert hashlib.sha256(mysql.encode()).hexdigest()=='e4a53b0eaca810e385a7336c0ace3b05723313b362342b6094b822bee0d8f98c'
cash=cash.replace('"fmt"\n','"fmt"\n\t"sync"\n',1)
old='''\tsqlctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
\trelease := func() { unlock(); cancel() }
\tlocked := *s'''
new='''\tsqlctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
\tlocked := *s
\tverified := false
\tvar once sync.Once
\trelease := func() { once.Do(func() {
\t\tif verified {
\t\t\t// A failed rebuild must leave dirty set; a stale owner must not clear it.
\t\t\tif _, err := locked.rebuildCash(sqlctx, tenant, "", ""); err == nil {
\t\t\t\t_ = s.rdb.Eval(sqlctx, `if redis.call('GET',KEYS[1]) == ARGV[1] and redis.call('GET',KEYS[2]) == ARGV[1] then return redis.call('DEL',KEYS[2]) end return 0`, []string{cashFenceKey(tenant), cashDirtyKey(tenant)}, locked.cash.token).Err()
\t\t\t}
\t\t}
\t\tunlock(); cancel()
\t}) }'''
assert cash.count(old)==1;cash=cash.replace(old,new,1)
old='if err := s.rdb.Set(ctx, cashFenceKey(tenant), locked.cash.token, 0).Err(); err != nil {'
new='if err := s.rdb.Eval(ctx, `redis.call("SET",KEYS[1],ARGV[1]); redis.call("SET",KEYS[2],ARGV[1]); return 1`, []string{cashFenceKey(tenant), cashDirtyKey(tenant)}, locked.cash.token).Err(); err != nil {'
assert cash.count(old)==1;cash=cash.replace(old,new,1)
old='\treturn &locked, release, nil\n}'
assert cash.count(old)==1;cash=cash.replace(old,'\tverified = true\n'+old,1)
anchor='func cashFenceKey(tenant uint64) string { return fmt.Sprintf("billing:cash-owner:%d", tenant) }'
assert cash.count(anchor)==1;cash=cash.replace(anchor,anchor+'\nfunc cashDirtyKey(tenant uint64) string { return fmt.Sprintf("billing:cash-dirty:%d", tenant) }',1)
old='func (s *Store) GetBalance(ctx context.Context, tenantID uint64) (int64, error) {\n\treturn s.balanceOrEnsure(ctx, tenantID)\n}'
new='''func (s *Store) GetBalance(ctx context.Context, tenantID uint64) (int64, error) {
\tresult, err := s.rdb.Eval(ctx, `if redis.call('EXISTS',KEYS[2]) == 1 then return {1,''} end return {0,redis.call('GET',KEYS[1]) or ''}`, []string{BalanceKey(tenantID), cashDirtyKey(tenantID)}).Slice()
\tif err != nil { return 0, err }
\tdirty, ok := result[0].(int64); if !ok { return 0, fmt.Errorf("invalid dirty marker result") }
\tif dirty != 0 {
\t\tlocked, release, err := s.lockCash(ctx, tenantID); if err != nil { return 0, err }; defer release()
\t\tlocked.cash.allowDisabledTerminal = true
\t\treturn locked.rebuildCash(locked.cashSQLContext(), tenantID, "", "")
\t}
\tvalue, ok := result[1].(string); if !ok { return 0, fmt.Errorf("invalid balance result") }
\tif value == "" { return s.balanceOrEnsure(ctx, tenantID) }
\treturn strconv.ParseInt(value, 10, 64)
}'''
assert mysql.count(old)==1;mysql=mysql.replace(old,new,1)
cp=out/'dirty_cash_candidate.go.txt';mp=out/'dirty_mysql_candidate.go.txt';cp.write_text(cash);mp.write_text(mysql)
(out/'dirty-balance-overlay.json').write_text(json.dumps({'Replace':{str(cash_path):str(cp),str(mysql_path):str(mp)}})+'\n')
for p in (cp,mp):print(p.name+'='+hashlib.sha256(p.read_bytes()).hexdigest())
