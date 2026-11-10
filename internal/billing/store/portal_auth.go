package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	err := s.withAccountName(ctx, username, func(tx *gorm.DB) error {
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
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Retain the key row for historic billing/audit references, but revoke it.
		return tx.Model(&APIKey{}).Where("key_hash = ? AND name = ?", portalTokenHash(token), browserKeyName).Update("status", 1).Error
	})
	if err != nil {
		return err
	}
	return s.ForgetAuthCache(ctx, portalTokenHash(token))
}

// SetAPIKeyStatus 停用或重新启用一把密钥，并立刻清掉它的缓存。
func (s *Store) SetAPIKeyStatus(ctx context.Context, id uint64, next int8) error {
	if id == 0 || (next != 0 && next != 1) {
		return gorm.ErrInvalidValue
	}
	var row APIKey
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, id).Error; err != nil {
			return err
		}
		if row.Status == deletedAPIKeyStatus {
			return errors.New("密钥已删除")
		}
		return tx.Model(&row).Update("status", next).Error
	})
	if err != nil {
		return err
	}
	return s.ForgetAuthCache(ctx, row.KeyHash)
}

const maxDownstreamKeys = 20
const deletedAPIKeyStatus int8 = 2

// DownstreamKey 是给外部程序用的钥匙，只含可反复查看的字段。明文只在创建时返回一次。
type DownstreamKey struct {
	ID        uint64    `json:"id"`
	Name      string    `json:"name"`
	KeyPrefix string    `json:"key_prefix"`
	Status    int8      `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateDownstreamAPIKey(ctx context.Context, tenantID uint64, name string) (string, DownstreamKey, error) {
	name = strings.TrimSpace(name)
	if tenantID == 0 || name == "" || name == browserKeyName || utf8.RuneCountInString(name) > 64 {
		return "", DownstreamKey{}, errors.New("密钥名称无效")
	}
	var tenant Tenant
	if err := s.db.WithContext(ctx).First(&tenant, tenantID).Error; err != nil {
		return "", DownstreamKey{}, err
	}
	if tenant.Status != 0 {
		return "", DownstreamKey{}, errors.New("账户已停用")
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&APIKey{}).Where("tenant_id = ? AND name <> ? AND status <> ?", tenantID, browserKeyName, deletedAPIKeyStatus).Count(&n).Error; err != nil {
		return "", DownstreamKey{}, err
	}
	if n >= maxDownstreamKeys {
		return "", DownstreamKey{}, errors.New("密钥数量已达上限")
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", DownstreamKey{}, err
	}
	plain := "sk-" + hex.EncodeToString(buf)
	row := APIKey{TenantID: tenantID, KeyHash: portalTokenHash(plain), KeyPrefix: plain[:11], Name: name}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return "", DownstreamKey{}, err
	}
	return plain, DownstreamKey{ID: row.ID, Name: row.Name, KeyPrefix: row.KeyPrefix, Status: row.Status, CreatedAt: row.CreatedAt}, nil
}

func (s *Store) ListDownstreamAPIKeys(ctx context.Context, tenantID uint64) ([]DownstreamKey, error) {
	var rows []APIKey
	err := s.db.WithContext(ctx).Where("tenant_id = ? AND name <> ? AND status <> ?", tenantID, browserKeyName, deletedAPIKeyStatus).Order("id desc").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]DownstreamKey, 0, len(rows))
	for _, row := range rows {
		out = append(out, DownstreamKey{ID: row.ID, Name: row.Name, KeyPrefix: row.KeyPrefix, Status: row.Status, CreatedAt: row.CreatedAt})
	}
	return out, nil
}

func (s *Store) SetDownstreamAPIKeyStatus(ctx context.Context, tenantID, id uint64, next int8) error {
	if tenantID == 0 {
		return errors.New("缺少账户")
	}
	var row APIKey
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return err
	}
	if row.TenantID != tenantID || row.Name == browserKeyName {
		return errors.New("不能修改这把密钥")
	}
	return s.SetAPIKeyStatus(ctx, id, next)
}

// DeleteDownstreamAPIKey retains billing references while permanently revoking
// the credential. Repeating a deletion also retries cache invalidation.
func (s *Store) DeleteDownstreamAPIKey(ctx context.Context, tenantID, id uint64) error {
	if tenantID == 0 || id == 0 {
		return errors.New("缺少账户或密钥")
	}
	var row APIKey
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ? AND name <> ?", id, tenantID, browserKeyName).First(&row).Error; err != nil {
			return errors.New("密钥不存在或不能删除")
		}
		return tx.Model(&row).Update("status", deletedAPIKeyStatus).Error
	})
	if err != nil {
		return err
	}
	return s.ForgetAuthCache(ctx, row.KeyHash)
}
