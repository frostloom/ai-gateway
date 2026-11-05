#!/usr/bin/env python3
"""Build test-only billing auth pool candidate; never changes workspace Go files."""
import json,pathlib,sys
root=pathlib.Path(__file__).resolve().parents[2];out=pathlib.Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True)
def change(src, old, new):
 if src.count(old)!=1:raise ValueError('overlay anchor mismatch: '+old[:100])
 return src.replace(old,new,1)
svcpath=root/'internal/billing/server/service.go';svc=svcpath.read_text()
svc=change(svc,'\tstore            *store.Store\n','\tstore            *store.Store\n\tauthStore        *store.Store\n')
svc=change(svc,'type Option func(*Service)\n','type Option func(*Service)\n\n// WithTestAuthStore is isolated evaluation code, not a production API.\nfunc WithTestAuthStore(st *store.Store) Option { return func(s *Service) { s.authStore = st } }\n')
svc=change(svc,'s := &Service{store: st, log: log}','s := &Service{store: st, authStore: st, log: log}')
svc=change(svc,'k, err := s.store.ValidateAPIKey(ctx, req.ApiKeyHash)','k, err := s.authStore.ValidateAPIKey(ctx, req.ApiKeyHash)')
svc_out=out/'service.go';svc_out.write_text(svc)
mainpath=root/'cmd/billing/main.go';main=mainpath.read_text()
main=change(main,'sqlDB.SetMaxOpenConns(100)','sqlDB.SetMaxOpenConns(84)')
main=change(main,'sqlDB.SetMaxIdleConns(100)','sqlDB.SetMaxIdleConns(84)')
anchor='\tvar serviceOptions []server.Option\n'
new='''\tauthDB, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Error)})
	if err != nil { log.Error("auth pool connect", "err", err); os.Exit(1) }
	authSQL, err := authDB.DB()
	if err != nil { log.Error("auth pool handle", "err", err); os.Exit(1) }
	authSQL.SetMaxOpenConns(16)
	authSQL.SetMaxIdleConns(16)
	authSQL.SetConnMaxLifetime(3 * time.Minute)
	defer authSQL.Close()
	var serviceOptions []server.Option
	serviceOptions = append(serviceOptions, server.WithTestAuthStore(store.NewStore(authDB, rdb)))
'''
main=change(main,anchor,new);main_out=out/'main.go';main_out.write_text(main)
testpath=root/'eval/billing-comparison-2026-11-02/auth_pool_isolation_test.go';test=testpath.read_text()
test=change(test,'return server.NewService(primary, slog.New(slog.DiscardHandler))','return server.NewService(primary, slog.New(slog.DiscardHandler), server.WithTestAuthStore(auth))')
test_out=out/'auth_pool_isolation_test.go';test_out.write_text(test)
mapping={str(svcpath):str(svc_out),str(mainpath):str(main_out)}
(out/'overlay.json').write_text(json.dumps({'Replace':mapping}))
mapping[str(testpath)]=str(test_out)
(out/'test-overlay.json').write_text(json.dumps({'Replace':mapping}))
