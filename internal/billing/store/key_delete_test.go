package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestDownstreamKeyDeletionRevokesAndPreservesHistory(t *testing.T) {
	s, done := newTestStore(t)
	defer done()
	s.EnableAuthCache(time.Minute)
	ctx := context.Background()
	u, err := s.RegisterPortalUser(ctx, fmt.Sprintf("delete%d", time.Now().UnixNano()), "hash")
	if err != nil {
		t.Fatal(err)
	}
	plain, key, err := s.CreateDownstreamAPIKey(ctx, u.TenantID, "delete-test")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.ValidateAPIKey(ctx, portalTokenHash(plain)); err != nil || got == nil {
		t.Fatal("initial key validation failed", err)
	}
	if err := s.DeleteDownstreamAPIKey(ctx, u.TenantID+1, key.ID); err == nil {
		t.Fatal("another tenant deleted key")
	}
	if err := s.CreatePortalSession(ctx, u, u.Username+"-session", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var browser APIKey
	if err := s.db.Where("key_hash = ?", portalTokenHash(u.Username+"-session")).First(&browser).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDownstreamAPIKey(ctx, u.TenantID, browser.ID); err == nil {
		t.Fatal("deleted browser session")
	}
	for i := 0; i < 2; i++ {
		if err := s.DeleteDownstreamAPIKey(ctx, u.TenantID, key.ID); err != nil {
			t.Fatal("delete retry failed", err)
		}
	}
	if got, err := s.ValidateAPIKey(ctx, portalTokenHash(plain)); err != nil || got != nil {
		t.Fatal("deleted cached key still authorized", err)
	}
	listed, err := s.ListDownstreamAPIKeys(ctx, u.TenantID)
	if err != nil || len(listed) != 0 {
		t.Fatal("deleted key still listed", listed, err)
	}
	var retained APIKey
	if err := s.db.First(&retained, key.ID).Error; err != nil || retained.KeyHash != portalTokenHash(plain) {
		t.Fatal("historic key identity lost", err)
	}
	if err := s.SetDownstreamAPIKeyStatus(ctx, u.TenantID, key.ID, 0); err == nil {
		t.Fatal("user revived deleted key")
	}
	if err := s.SetAPIKeyStatus(ctx, key.ID, 0); err == nil {
		t.Fatal("admin revived deleted key")
	}
	// Deleted keys do not consume the visible key quota.
	for i := 0; i < maxDownstreamKeys; i++ {
		if _, _, err := s.CreateDownstreamAPIKey(ctx, u.TenantID, fmt.Sprint("replacement-", i)); err != nil {
			t.Fatal("deleted key consumed quota", err)
		}
	}
}
