package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"gorm.io/gorm"
)

// Public registration always creates a regular user with a separate tenant.
// Browser credentials are random, HttpOnly, time limited and stored only as hashes.
type PortalUser struct {
	ID           uint64    `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:64;uniqueIndex;not null" json:"username"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	TenantID     uint64    `gorm:"uniqueIndex;not null" json:"tenant_id"`
	Status       int8      `gorm:"not null;default:0" json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type PortalSession struct {
	ID        uint64 `gorm:"primaryKey"`
	UserID    uint64 `gorm:"index;not null"`
	APIKeyID  uint64 `gorm:"uniqueIndex;not null"`
	CreatedAt time.Time
}

const browserKeyName = "__browser_session"

func portalTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Store) RegisterPortalUser(ctx context.Context, username, hash string) (*PortalUser, error) {
	u := &PortalUser{Username: username, PasswordHash: hash}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Tenant names need not match usernames from older seed data.
		tenant := &Tenant{Name: "account:" + username}
		if err := tx.Create(tenant).Error; err != nil {
			return err
		}
		u.TenantID = tenant.ID
		return tx.Create(u).Error
	})
	return u, err
}

func (s *Store) GetPortalUser(ctx context.Context, username string) (*PortalUser, error) {
	var u PortalUser
	err := s.db.WithContext(ctx).Where("username = ?", username).First(&u).Error
	return &u, err
}

func (s *Store) CreatePortalSession(ctx context.Context, u *PortalUser, token string, expires time.Time) error {
	// Reuse the existing expiring credential validator for agent and billing calls.
	// Its plaintext is never returned in JSON or stored in the database.
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		k := &APIKey{TenantID: u.TenantID, KeyHash: portalTokenHash(token), KeyPrefix: "session", Name: browserKeyName, ExpiresAt: &expires}
		if err := tx.Create(k).Error; err != nil {
			return err
		}
		return tx.Create(&PortalSession{UserID: u.ID, APIKeyID: k.ID}).Error
	})
}

func (s *Store) PortalIdentity(ctx context.Context, token string) (*PortalUser, *APIKey, error) {
	if token == "" {
		return nil, nil, ErrSessionExpired
	}
	k, err := s.ValidateAPIKey(ctx, portalTokenHash(token))
	if err != nil {
		return nil, nil, err
	}
	if k == nil || k.Name != browserKeyName {
		return nil, nil, ErrSessionExpired
	}
	var u PortalUser
	err = s.db.WithContext(ctx).Model(&PortalUser{}).
		Joins("JOIN portal_sessions ON portal_sessions.user_id = portal_users.id").
		Where("portal_sessions.api_key_id = ? AND portal_users.status = 0", k.ID).First(&u).Error
	return &u, k, err
}

func (s *Store) RevokePortalSession(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Retain the key row for historic billing/audit references, but revoke it.
		return tx.Model(&APIKey{}).Where("key_hash = ? AND name = ?", portalTokenHash(token), browserKeyName).Update("status", 1).Error
	})
}
