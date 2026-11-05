#!/usr/bin/env python3
"""Test-only refresh outside router state lock with invalidation generation."""
import json, os, pathlib, sys
if os.environ.get('TEST_ROUTER_AUTHORITY_METADATA') == '1' and os.environ.get('TEST_ROUTER_AUTHORITY_CHECK') != '1': raise ValueError('metadata check requires authority check')
root=pathlib.Path(__file__).resolve().parents[2]
out=pathlib.Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True)
path=root/'internal/router/registry.go';source=path.read_text()
anchor='\tmu            sync.Mutex\n'
if source.count(anchor)!=1:raise ValueError('registry lock anchor changed')
source=source.replace(anchor,anchor+'\trefreshPermit chan struct{}\n\tcacheGeneration uint64\n',1)
new_anchor='\t\tdb:            db,\n'
if source.count(new_anchor)!=1:raise ValueError('registry initialization anchor changed')
source=source.replace(new_anchor,new_anchor+'\t\trefreshPermit: make(chan struct{}, 1),\n',1)
anchor='func (r *Registry) clearCache() {\n\tr.cache = make(map[string]*cachedRoute)\n}'
if source.count(anchor)!=1:raise ValueError('cache invalidation anchor changed')
source=source.replace(anchor,'func (r *Registry) clearCache() {\n\tr.cacheGeneration++\n\tr.cache = make(map[string]*cachedRoute)\n}',1)
a=source.index('func (r *Registry) snapshot(');b=source.index('\nfunc contains(',a)
replacement='''func (r *Registry) snapshot(ctx context.Context, model string) ([]Provider, error) {
    r.mu.Lock()
    if err := ctx.Err(); err != nil { r.mu.Unlock(); return nil, err }
    if c, ok := r.cache[model]; ok && time.Now().Before(c.expireAt) { ps := c.providers; r.mu.Unlock(); return ps, nil }
    r.mu.Unlock()
    select {
    case r.refreshPermit <- struct{}{}: defer func() { <-r.refreshPermit }()
    case <-ctx.Done(): return nil, ctx.Err()
    }
    for {
        r.mu.Lock()
        if err := ctx.Err(); err != nil { r.mu.Unlock(); return nil, err }
        if c, ok := r.cache[model]; ok && time.Now().Before(c.expireAt) { ps := c.providers; r.mu.Unlock(); return ps, nil }
        gen := r.cacheGeneration
        r.mu.Unlock()
        type row struct { ID uint64; Name string; BaseURL string; UpstreamKey string; Weight int; Models string }
        var rows []row
        if err := r.db.WithContext(ctx).Table("providers").Where("status = ?", 0).Find(&rows).Error; err != nil { return nil, err }
        ps := make([]Provider, 0, len(rows))
        for _, rr := range rows {
            var models []string
            _ = json.Unmarshal([]byte(rr.Models), &models)
            if contains(models, "*") || contains(models, model) { ps = append(ps, Provider{ID: rr.ID, Name: rr.Name, BaseURL: rr.BaseURL, UpstreamKey: rr.UpstreamKey, Weight: rr.Weight}) }
        }
        r.mu.Lock()
        if err := ctx.Err(); err != nil { r.mu.Unlock(); return nil, err }
        if gen != r.cacheGeneration { r.mu.Unlock(); continue }
        r.cache[model] = &cachedRoute{providers: ps, expireAt: time.Now().Add(r.ttl)}
        r.mu.Unlock()
        return ps, nil
    }
}
'''
if os.environ.get('TEST_ROUTER_REFRESH_UNFENCED') == '1':
    replacement = replacement.replace('if gen != r.cacheGeneration { r.mu.Unlock(); continue }', '_ = gen')
full = source[:a]+replacement+source[b:]
if os.environ.get('TEST_ROUTER_REFRESH_DEADLINE') == '1':
    if os.environ.get('TEST_ROUTER_REFRESH_UNFENCED') == '1':
        raise ValueError('unfenced and deadline modes are mutually exclusive')
    start = full.index('func (r *Registry) snapshot(')
    end = full.index('\nfunc contains(',start)
    section = full[start:end]
    if section.count('r.mu.Lock()') != 3:
        raise ValueError('snapshot lock count changed')
    section = section.replace('r.mu.Lock()', 'if err := r.lockRouteContext(ctx); err != nil { return nil, err }')
    full = full[:start] + section + full[end:]
    route_anchor = '\t// probeInFlight=false，HALF-OPEN 会放出多个探测。\n\tr.mu.Lock()'
    if full.count(route_anchor) != 1: raise ValueError('route lock anchor changed')
    full = full.replace(route_anchor, '\t// probeInFlight=false，HALF-OPEN 会放出多个探测。\n\tif err := r.lockRouteContext(ctx); err != nil { return nil, err }',1)
    marker = '// ---------- snapshot（M2 原逻辑） ----------'
    helper = """// Test-only cancellation polling; performance and fairness are not established.
func (r *Registry) lockRouteContext(ctx context.Context) error {
    for {
        if err := ctx.Err(); err != nil { return err }
        if r.mu.TryLock() { return nil }
        timer := time.NewTimer(time.Millisecond)
        select { case <-ctx.Done(): timer.Stop(); return ctx.Err(); case <-timer.C: }
    }
}

"""
    if full.count(marker) != 1: raise ValueError('helper anchor changed')
    full = full.replace(marker,helper+marker,1)
if os.environ.get('TEST_ROUTER_AUTHORITY_CHECK') == '1':
    import_anchor = '\t\"context\"\n'
    if full.count(import_anchor) != 1: raise ValueError('authority import anchor changed')
    full = full.replace(import_anchor, import_anchor + '\t\"errors\"\n', 1)
    start = full.index('func (r *Registry) snapshot(')
    end = full.index('\nfunc contains(',start)
    section = full[start:end]
    header = 'func (r *Registry) snapshot(ctx context.Context, model string) ([]Provider, error) {'
    if section.count(header) != 1: raise ValueError('authority snapshot anchor changed')
    section = section.replace(header, header + '\n    if r.db == nil { return nil, errors.New(\"router authority database unavailable\") }', 1)
    if section.count('return ps, nil') != 3: raise ValueError('snapshot return count changed')
    section = section.replace('return ps, nil', 'return r.activeProviders(ctx, ps)')
    full = full[:start] + section + full[end:]
    marker = '// ---------- snapshot（M2 原逻辑） ----------'
    helper = """// Test-only authority check; no cross-system snapshot or send fencing.
func (r *Registry) activeProviders(ctx context.Context, ps []Provider) ([]Provider, error) {
    if r.db == nil { return nil, errors.New("router authority database unavailable") }
    if len(ps) == 0 { return ps, nil }
    ids := make([]uint64, 0, len(ps))
    for _, p := range ps { ids = append(ids, p.ID) }
    var active []uint64
    if err := r.db.WithContext(ctx).Table("providers").Where("id IN ? AND status = ?", ids, 0).Pluck("id", &active).Error; err != nil { return nil, err }
    allowed := make(map[uint64]bool, len(active))
    for _, id := range active { allowed[id] = true }
    filtered := make([]Provider, 0, len(ps))
    for _, p := range ps { if allowed[p.ID] { filtered = append(filtered,p) } }
    return filtered, nil
}

"""
    if os.environ.get('TEST_ROUTER_AUTHORITY_METADATA') == '1':
        helper = """// Test-only authority metadata read; still a point-in-time check.
func (r *Registry) activeProviders(ctx context.Context, model string, ps []Provider) ([]Provider, error) {
    if r.db == nil { return nil, errors.New("router authority database unavailable") }
    if len(ps) == 0 { return ps, nil }
    ids := make([]uint64, 0, len(ps))
    for _, p := range ps { ids = append(ids, p.ID) }
    type row struct { ID uint64; Name string; BaseURL string; UpstreamKey string; Weight int; Models string }
    var rows []row
    if err := r.db.WithContext(ctx).Table("providers").Select("id,name,base_url,upstream_key,weight,models").Where("id IN ? AND status = ?", ids, 0).Find(&rows).Error; err != nil { return nil, err }
    current := make(map[uint64]Provider,len(rows))
    for _, rr := range rows {
        var models []string
        _ = json.Unmarshal([]byte(rr.Models), &models)
        if contains(models,"*") || contains(models,model) { current[rr.ID]=Provider{ID:rr.ID,Name:rr.Name,BaseURL:rr.BaseURL,UpstreamKey:rr.UpstreamKey,Weight:rr.Weight} }
    }
    filtered := make([]Provider,0,len(ps))
    for _, old := range ps { if p,ok:=current[old.ID];ok { filtered=append(filtered,p) } }
    return filtered,nil
}

"""
        if full.count('r.activeProviders(ctx, ps)') != 3: raise ValueError('metadata call anchor changed')
        full = full.replace('r.activeProviders(ctx, ps)', 'r.activeProviders(ctx, model, ps)')
    if full.count(marker) != 1: raise ValueError('authority helper anchor changed')
    full = full.replace(marker,helper+marker,1)
result=out/'registry.go';result.write_text(full);(out/'overlay.json').write_text(json.dumps({'Replace':{str(path):str(result)}}))
