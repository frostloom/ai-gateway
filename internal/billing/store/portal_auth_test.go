package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestPortalAccountLifecycle(t *testing.T) {
	s, close := newTestStore(t)
	defer close()
	ctx := context.Background()
	name := fmt.Sprintf("member%d", time.Now().UnixNano())
	u, err := s.RegisterPortalUser(ctx, name, "bcrypt-test-hash")
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.RegisterPortalUser(ctx, name+"b", "bcrypt-test-hash")
	if err != nil || u.TenantID == v.TenantID {
		t.Fatalf("separate tenants: %v", err)
	}
	var before, after int64
	s.db.Model(&Tenant{}).Count(&before)
	if _, err := s.RegisterPortalUser(ctx, name, "other"); err == nil {
		t.Fatal("duplicate accepted")
	}
	s.db.Model(&Tenant{}).Count(&after)
	if before != after {
		t.Fatal("duplicate registration leaked tenant")
	}
	token := name + "-session"
	if err := s.CreatePortalSession(ctx, u, token, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, key, err := s.PortalIdentity(ctx, token)
	if err != nil || got.ID != u.ID || key.TenantID != u.TenantID {
		t.Fatalf("identity: %v", err)
	}
	if _, _, err := s.PortalIdentity(ctx, "wrong"); err == nil {
		t.Fatal("bad token accepted")
	}
	if err := s.RevokePortalSession(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PortalIdentity(ctx, token); err == nil {
		t.Fatal("logout did not revoke session")
	}
	if k, err := s.ValidateAPIKey(ctx, key.KeyHash); err != nil || k != nil {
		t.Fatal("logout left agent credential active")
	}
	if err := s.CreatePortalSession(ctx, u, token+"expired", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PortalIdentity(ctx, token+"expired"); err == nil {
		t.Fatal("expired accepted")
	}
	if err := s.CreatePortalSession(ctx, u, token+"disabled", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	s.db.Model(&PortalUser{}).Where("id = ?", u.ID).Update("status", 1)
	if _, _, err := s.PortalIdentity(ctx, token+"disabled"); err == nil {
		t.Fatal("disabled user accepted")
	}
	if k, err := s.ValidateAPIKey(ctx, portalTokenHash(token+"disabled")); err != nil || k != nil {
		t.Fatal("disabled user agent credential accepted")
	}
}

func TestChatSessionTenantIsolation(t *testing.T) {
	s, close := newTestStore(t)
	defer close()
	ctx := context.Background()
	if err := s.RecordSession(ctx, "shared-id", 11, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordSession(ctx, "shared-id", 12, 2); err == nil {
		t.Fatal("another tenant can reuse chat session")
	}
	if err := s.RecordSession(ctx, "shared-id", 11, 3); err != nil {
		t.Fatal("same tenant cannot resume after login", err)
	}
}
