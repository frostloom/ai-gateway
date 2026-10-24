package knowledgepilot

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
)

type ClaimDependency struct {
	Path, SHA256                       string
	ReservedCNYMicro, ReservedUSDMicro int64
}

// Claim capabilities are private. This build can create loopback-only claims;
// no caller can manufacture a paid execution capability via a JSON boolean.
type Claim struct {
	endpoint string
	scope    Scope
	local    bool
}

func AcquireClaim(_ string, _ Scope, _ []ClaimDependency) (*Claim, error) {
	return nil, fmt.Errorf("%s: no paid claim can be issued", BudgetBlocker)
}
func literalLoopback(endpoint string) bool {
	u, e := url.Parse(endpoint)
	return e == nil && u.Scheme == "http" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Opaque == "" && net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback() && (u.Path == "" || u.Path == "/v1")
}
func OpenLocalClaim(path, endpoint string, s Scope) (*Claim, error) {
	if !literalLoopback(endpoint) {
		return nil, fmt.Errorf("local claim requires literal HTTP loopback")
	}
	b, _ := json.Marshal(struct{ Kind, Endpoint, ScopeSHA string }{"local_stub", endpoint, func() string { b, _ := json.Marshal(s); return SHA(b) }()})
	if e := writeExclusive(path, append(b, '\n')); e != nil {
		return nil, e
	}
	return &Claim{endpoint: endpoint, scope: s, local: true}, nil
}
func writeExclusive(path string, b []byte) error {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return fmt.Errorf("exclusive artifact unavailable")
	}
	defer f.Close()
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
