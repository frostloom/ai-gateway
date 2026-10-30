package taskbench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFullWindowActualOpenRejectsBeforeSettings(t *testing.T) {
	for _, token := range []*FullWindowActualReady{nil, {}} {
		s, err := OpenFullWindowActual(context.Background(), token, "/nonexistent/private-settings")
		if err == nil || s != nil || !strings.Contains(err.Error(), "v9 delegated authority required before settings") {
			t.Fatalf("ready must reject before settings: %v", err)
		}
	}
}
func TestFullWindowActualPrivateSettings(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	p := filepath.Join(root, "settings")
	for _, raw := range []string{`{}`, `null`, `{"model_key":null}`, `{"model_key":"x","model_key":"y"}`, `{"unknown":1}`} {
		if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readWindowActualSettings(p); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	raw := actualTestSettings("http://127.0.0.1:3000/api/public/otel/v1/traces")
	if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readWindowActualSettings(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readWindowActualSettings(p); err == nil {
		t.Fatal("public credentials accepted")
	}
	if err := os.Chmod(p, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readWindowActualSettings(link); err == nil {
		t.Fatal("aliased credentials accepted")
	}
	for _, endpoint := range []string{"http://localhost:3000/api/public/otel/v1/traces", "https://127.0.0.1/api/public/otel/v1/traces", "http://192.0.2.1/api/public/otel/v1/traces", "http://127.0.0.1/other", "http://127.0.0.1/api/public/otel/v1/traces?x"} {
		if err := os.WriteFile(p, []byte(actualTestSettings(endpoint)), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readWindowActualSettings(p); err == nil {
			t.Fatalf("endpoint accepted %s", endpoint)
		}
	}
}
func actualTestSettings(endpoint string) string {
	return `{"model_key":"synthetic-model","jev_key":"synthetic-jev","jev_https_proxy":"","loopback_admin_dsn":"fixture@tcp(127.0.0.1:3306)/","allow_fixture_creation":true,"observer":{"enabled":true,"capture_content":true,"endpoint":"` + endpoint + `","public_key":"pk-test","secret_key":"sk-test","hash_key":"hhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhh","release":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","environment":"evaluation","sample_rate":1,"queue_size":256,"export_timeout_ms":2000}}`
}
func TestFullWindowActualHealthIsBoundedAuthenticatedGet(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method != "GET" {
			t.Error("non GET")
		}
		if r.URL.Path == "/api/public/projects" {
			u, p, ok := r.BasicAuth()
			if !ok || u != "pk-test" || p != "sk-test" {
				t.Error("missing project auth")
			}
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	if err := checkWindowActualHealth(context.Background(), windowActualObserverSettings{Endpoint: server.URL + "/api/public/otel/v1/traces", PublicKey: "pk-test", SecretKey: "sk-test"}); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "/api/public/health" || paths[1] != "/api/public/projects" {
		t.Fatalf("unexpected health operations %v", paths)
	}
}

// Synthetic protocol/archive fixtures below cannot create production readiness,
// actual claims or a Handler fixture. They never read historical private data.
func actualTestSession(t *testing.T) *FullWindowActualSession {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	info, _ := os.Lstat(root)
	ctx, cancel := context.WithCancel(context.Background())
	return &FullWindowActualSession{ready: &FullWindowActualReady{manifest: windowActualManifest{SessionRoot: root}}, rootInfo: info, lifetime: ctx, cancel: cancel, files: map[string]string{}, reportRoots: map[string]bool{}, reportDirs: map[string]os.FileInfo{}, status: FullWindowActualStatus{EvidenceKind: "recorded_run", NextWindowIndex: 1}}
}
func TestFullWindowActualReportRejectsSubstitutedReceipt(t *testing.T) {
	s := actualTestSession(t)
	defer s.Close()
	p := filepath.Join(s.ready.manifest.SessionRoot, "window-000001")
	if e := os.Mkdir(p, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(p, "receipt.json"), []byte(`{"status":"forged"}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e := s.rememberReport("window-000001", windowAcquisitionReceipt{}); e == nil {
		t.Fatal("substituted collector receipt accepted")
	}
}
func TestFullWindowActualSessionInvalidTransitionCloses(t *testing.T) {
	for _, test := range []struct {
		index int
		prior string
	}{{2, ""}, {1, strings.Repeat("a", 64)}} {
		s := actualTestSession(t)
		if _, e := s.RunWindow(context.Background(), test.index, test.prior); e == nil {
			t.Fatal("invalid operation accepted")
		}
		if !s.Status().Closed || s.lifetime.Err() == nil {
			t.Fatal("owner retained after invalid operation")
		}
		if e := s.Close(); e != nil {
			t.Fatal(e)
		}
		if _, e := s.RunWindow(context.Background(), 1, ""); e == nil {
			t.Fatal("closed operation accepted")
		}
	}
}
func TestFullWindowActualArchiveExactInventory(t *testing.T) {
	s := actualTestSession(t)
	defer s.Close()
	sha, e := s.persist("startup.json", s.Status())
	if e != nil || len(sha) != 64 {
		t.Fatal(e)
	}
	if _, e = s.persist("startup.json", s.Status()); e == nil {
		t.Fatal("receipt overwritten")
	}
	if e = s.verifyReports(); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(s.ready.manifest.SessionRoot, "orphan")
	if e = os.WriteFile(p, []byte("orphan"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.verifyReports(); e == nil {
		t.Fatal("orphan accepted")
	}
	if e = os.Remove(p); e != nil {
		t.Fatal(e)
	}
	p = filepath.Join(s.ready.manifest.SessionRoot, "startup.json")
	if e = os.WriteFile(p, []byte("tampered"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.verifyReports(); e == nil {
		t.Fatal("modified receipt accepted")
	}
}
func TestFullWindowActualProviderRedirectDoesNotReplay(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Location", "/")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	transport := newWindowActualTransport()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: &windowActualNoRedirectTransport{transport}}
	response, err := client.Post(server.URL, "application/json", strings.NewReader(`{}`))
	if response != nil {
		response.Body.Close()
	}
	if err == nil || calls != 1 {
		t.Fatalf("provider redirect replayed: calls %d err %v", calls, err)
	}
}
func TestFullWindowActualHealthRejectsRedirectAndAuthenticationFailure(t *testing.T) {
	for _, code := range []int{301, 401, 500} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Location", "/api/public/health")
			w.WriteHeader(code)
		}))
		err := checkWindowActualHealth(context.Background(), windowActualObserverSettings{Endpoint: server.URL + "/api/public/otel/v1/traces"})
		server.Close()
		if err == nil || calls != 1 {
			t.Fatalf("preflight continued after failure %d calls %d", code, calls)
		}
	}
}
func TestFullWindowActualQualityEndArchivePreservesSuffixEvidence(t *testing.T) {
	s := actualTestSession(t)
	defer s.Close()
	name := "window-000001"
	p := filepath.Join(s.ready.manifest.SessionRoot, name)
	if e := os.Mkdir(p, 0700); e != nil {
		t.Fatal(e)
	}
	candidate, cleanup := []byte(`{"synthetic":"candidate"}`), []byte(`{"synthetic":"cleanup"}`)
	report := windowAcquisitionReceipt{Rows: []windowAcquisitionRow{{QualityEnd: &windowQualityEndEvidence{CandidateFile: "candidate.json", CandidateSHA256: conversationHash(candidate), CleanupFile: "cleanup.json", CleanupSHA256: conversationHash(cleanup), UnrunTurns: 2}}}}
	raw, _ := json.Marshal(report)
	for file, data := range map[string][]byte{"receipt.json": raw, "candidate.json": candidate, "cleanup.json": cleanup} {
		if e := os.WriteFile(filepath.Join(p, file), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.rememberReport(name, report); e != nil {
		t.Fatalf("quality-end evidence rejected: %v", e)
	}
	s.reportRoots[name] = true
	if e := s.verifyReports(); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(p, "cleanup.json"), []byte(`{}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e := s.verifyReports(); e == nil {
		t.Fatal("quality-end cleanup tampering accepted")
	}
}
func TestFullWindowActualArchiveReadBoundedPrivateRegular(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	p := filepath.Join(root, "evidence")
	if e := os.WriteFile(p, []byte("synthetic"), 0600); e != nil {
		t.Fatal(e)
	}
	if raw, e := readWindowActualPrivateBytes(p, 32); e != nil || string(raw) != "synthetic" {
		t.Fatalf("private evidence %q %v", raw, e)
	}
	if _, e := readWindowActualPrivateBytes(p, 2); e == nil {
		t.Fatal("oversized accepted")
	}
	if e := os.Chmod(p, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := readWindowActualPrivateBytes(p, 32); e == nil {
		t.Fatal("nonprivate accepted")
	}
	if e := os.Chmod(p, 0600); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(root, "link")
	if e := os.Symlink(p, link); e != nil {
		t.Fatal(e)
	}
	if _, e := readWindowActualPrivateBytes(link, 32); e == nil {
		t.Fatal("symlink accepted")
	}
	if _, e := readWindowActualPrivateBytes(root, 32); e == nil {
		t.Fatal("directory accepted")
	}
}
func TestFullWindowActualSettingsRejectsFixtureAndObserverOverrides(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	p := filepath.Join(root, "settings")
	base := actualTestSettings("http://127.0.0.1:3000/api/public/otel/v1/traces")
	for _, change := range [][2]string{
		{"fixture@tcp(127.0.0.1:3306)/", "fixture@tcp(localhost:3306)/"},
		{"fixture@tcp(127.0.0.1:3306)/", "fixture@tcp(127.0.0.1:3306)/existing"},
		{"fixture@tcp(127.0.0.1:3306)/", "fixture@unix(/tmp/fixture.sock)/"},
		{`"allow_fixture_creation":true`, `"allow_fixture_creation":false`},
		{`"capture_content":true`, `"capture_content":false`},
		{`"sample_rate":1`, `"sample_rate":0.5`},
		{`"export_timeout_ms":2000`, `"export_timeout_ms":3000`},
		{`"queue_size":256`, `"queue_size":4097`},
		{`"environment":"evaluation"`, `"environment":"production"`},
		{`"model_key":"synthetic-model"`, `"model_key":""`},
		{`"jev_key":"synthetic-jev"`, `"jev_key":""`},
	} {
		raw := strings.Replace(base, change[0], change[1], 1)
		if e := os.WriteFile(p, []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := readWindowActualSettings(p); e == nil {
			t.Fatalf("unsafe override accepted: %s", change[1])
		}
	}
}

func TestFullWindowActualSettingsRejectsInvalidDatabasePort(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	p := filepath.Join(root, "settings")
	base := actualTestSettings("http://127.0.0.1:3000/api/public/otel/v1/traces")
	for _, port := range []string{"0", "65536", "mysql", "+3306", "-1"} {
		raw := strings.Replace(base, "127.0.0.1:3306", "127.0.0.1:"+port, 1)
		if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readWindowActualSettings(p); err == nil {
			t.Errorf("invalid database port accepted: %s", port)
		}
	}
}
func TestFullWindowActualSettingsFingerprintExactBytes(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	p := filepath.Join(root, "settings")
	base := actualTestSettings("http://127.0.0.1:3000/api/public/otel/v1/traces")
	for _, raw := range []string{base, base + "\n"} {
		if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		settings, err := readWindowActualSettings(p)
		if err != nil {
			t.Fatal(err)
		}
		if settings.rawSHA256 != conversationHash([]byte(raw)) {
			t.Fatal("settings fingerprint does not bind raw bytes")
		}
		encoded, err := json.Marshal(settings)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "rawSHA256") || strings.Contains(string(encoded), settings.rawSHA256) {
			t.Fatal("private fingerprint serialized")
		}
	}
}
func TestFullWindowActualDatabasePreflightRejectsBeforeConnection(t *testing.T) {
	for _, dsn := range []string{"fixture@tcp(localhost:3306)/", "fixture@tcp(192.0.2.1:3306)/", "fixture@tcp(127.0.0.1:0)/", "fixture@tcp(127.0.0.1:65536)/", "fixture@tcp(127.0.0.1:mysql)/", "fixture@tcp(127.0.0.1:3306)/existing"} {
		if err := checkWindowActualDatabase(context.Background(), dsn); err == nil {
			t.Fatalf("invalid DSN accepted: %s", dsn)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := checkWindowActualDatabase(ctx, "fixture@tcp(127.0.0.1:3306)/"); err == nil {
		t.Fatal("canceled database preflight accepted")
	}
	if err := checkWindowActualDatabase(nil, "fixture@tcp(127.0.0.1:3306)/"); err == nil {
		t.Fatal("nil database preflight accepted")
	}
}
func TestFullWindowActualSettingsDigestMismatchRejected(t *testing.T) {
	settings := windowActualSettings{rawSHA256: strings.Repeat("a", 64)}
	if err := checkWindowActualSettingsDigest(settings, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	for _, digest := range []string{strings.Repeat("b", 64), ""} {
		if err := checkWindowActualSettingsDigest(settings, digest); err == nil {
			t.Fatal("changed or missing frozen settings digest accepted")
		}
	}
}
func TestFullWindowActualSettingsRejectsDatabaseInitializationQueries(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	p := filepath.Join(root, "settings")
	base := actualTestSettings("http://127.0.0.1:3000/api/public/otel/v1/traces")
	for _, query := range []string{"time_zone=%27%2B00%3A00%27", "charset=utf8mb4"} {
		raw := strings.Replace(base, "127.0.0.1:3306)/", "127.0.0.1:3306)/?"+query, 1)
		if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readWindowActualSettings(p); err == nil {
			t.Errorf("DSN initialization SQL accepted: %s", query)
		}
	}
	raw := strings.Replace(base, "127.0.0.1:3306)/", "127.0.0.1:3306)/?parseTime=true&loc=UTC&readTimeout=1s", 1)
	if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readWindowActualSettings(p); err != nil {
		t.Fatalf("client-only DSN settings rejected: %v", err)
	}
}

func TestFullWindowActualJEVProxySettings(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	path := filepath.Join(root, "settings")
	baseline := actualTestSettings("http://127.0.0.1:3000/api/public/otel/v1/traces")
	for _, proxy := range []string{"", "http://127.0.0.1:7892", "http://[::1]:7892"} {
		var obj map[string]any
		_ = json.Unmarshal([]byte(baseline), &obj)
		obj["jev_https_proxy"] = proxy
		raw, _ := json.Marshal(obj)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readWindowActualSettings(path); err != nil {
			t.Errorf("explicit local JEV route rejected: %q: %v", proxy, err)
		}
	}
	for _, proxy := range []any{nil, 3, "http://localhost:7892", "http://192.0.2.1:7892", "https://127.0.0.1:7892", "socks5://127.0.0.1:7892", "http://user:pass@127.0.0.1:7892", "http://127.0.0.1:0", "http://127.0.0.1:65536", "http://127.0.0.1:http", "http://127.0.0.1:7892/", "http://127.0.0.1:7892?", "http://127.0.0.1:7892#x"} {
		var obj map[string]any
		_ = json.Unmarshal([]byte(baseline), &obj)
		obj["jev_https_proxy"] = proxy
		raw, _ := json.Marshal(obj)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readWindowActualSettings(path); err == nil {
			t.Errorf("invalid JEV route accepted: %#v", proxy)
		}
	}
}

func TestFullWindowActualJEVTransportOnlyExplicitConnect(t *testing.T) {
	calls := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "CONNECT" || r.Host != "api.typesafe.ai:443" || r.Header.Get("Authorization") != "" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("credential or target leaked to CONNECT proxy")
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("HTTP_PROXY", proxy.URL)
	direct := newWindowActualTransport()
	defer direct.CloseIdleConnections()
	if direct.Proxy != nil {
		t.Fatal("model/health inherited environment proxy")
	}
	tr, e := newWindowActualJEVTransport(proxy.URL)
	if e != nil {
		t.Fatal(e)
	}
	defer tr.CloseIdleConnections()
	if tr.Proxy == nil {
		t.Fatal("explicit JEV route ignored")
	}
	if tr.TLSClientConfig != nil && tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS verification disabled")
	}
	req, _ := http.NewRequest("GET", "https://api.typesafe.ai/v1/models", nil)
	req.Header.Set("Authorization", "Bearer synthetic-test-key")
	if u, e := tr.Proxy(req); e != nil || u.String() != proxy.URL {
		t.Fatal("JEV proxy differs from exact frozen route")
	}
	cli := &http.Client{Transport: tr, Timeout: time.Second}
	resp, e := cli.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if e == nil || calls != 1 {
		t.Fatalf("CONNECT count=%d error=%v", calls, e)
	}
	noProxy, e := newWindowActualJEVTransport("")
	if e != nil || noProxy.Proxy != nil {
		t.Fatal("empty explicit route inherited environment")
	}
	noProxy.CloseIdleConnections()
}
