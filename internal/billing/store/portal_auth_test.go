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

func TestDownstreamAPIKeyShownOnce(t *testing.T) {
	s, close := newTestStore(t)
	defer close()
	ctx := context.Background()
	u, err := s.RegisterPortalUser(ctx, fmt.Sprintf("caller%d", time.Now().UnixNano()), "hash")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.RegisterPortalUser(ctx, fmt.Sprintf("other%d", time.Now().UnixNano()), "hash")
	if err != nil {
		t.Fatal(err)
	}
	plain, row, err := s.CreateDownstreamAPIKey(ctx, u.TenantID, "生产调用")
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) < 16 || plain[:3] != "sk-" || row.KeyPrefix != plain[:11] {
		t.Fatalf("key shape: prefix %s", row.KeyPrefix)
	}
	var stored APIKey
	if err := s.db.First(&stored, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.KeyHash == plain || stored.KeyPrefix == plain {
		t.Fatal("plaintext stored")
	}
	got, err := s.ValidateAPIKey(ctx, portalTokenHash(plain))
	if err != nil || got == nil || got.TenantID != u.TenantID {
		t.Fatal(err)
	}
	listed, err := s.ListDownstreamAPIKeys(ctx, u.TenantID)
	if err != nil || len(listed) != 1 || listed[0].KeyPrefix != row.KeyPrefix {
		t.Fatalf("list: %+v %v", listed, err)
	}
	if err := s.SetDownstreamAPIKeyStatus(ctx, other.TenantID, row.ID, 1); err == nil {
		t.Fatal("other tenant changed the key")
	}
	if err := s.SetDownstreamAPIKeyStatus(ctx, u.TenantID, row.ID, 1); err != nil {
		t.Fatal(err)
	}
	if k, err := s.ValidateAPIKey(ctx, portalTokenHash(plain)); err != nil || k != nil {
		t.Fatal("disabled key still accepted")
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
