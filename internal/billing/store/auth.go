// 管理员登录（newapi 式）：管理员账号 + 会话。
// 账号密码 bcrypt 哈希入库；会话是随机 token（HttpOnly cookie 载体）。
// 首次无管理员时走「初始化」，之后走「登录」。生产应加登录失败限流/AES 加密等，演示从简。
package store

import (
	"context"
	"errors"
	"time"
)

// ErrSessionExpired 会话过期/无效（admin auth me 返回 401）。
var ErrSessionExpired = errors.New("session expired")

// AdminUser 管理员账号。PasswordHash 存 bcrypt 哈希，绝不回显。
type AdminUser struct {
	ID           uint64    `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:64;uniqueIndex:uk_admin_username;not null" json:"username"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	Status       int8      `gorm:"not null;default:0" json:"status"` // 0=active 1=disabled
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AdminSession 管理员会话。Token 为随机串，存明文（演示）；生产应存哈希并设短过期。
type AdminSession struct {
	ID        uint64    `gorm:"primaryKey"`
	Token     string    `gorm:"size:64;uniqueIndex:uk_admin_token;not null"`
	Username  string    `gorm:"size:64;not null"`
	ExpiresAt time.Time `gorm:"not null"`
	CreatedAt time.Time
}

// CountAdmins 管理员总数（0 = 未初始化，可走 setup）。
func (s *Store) CountAdmins(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.WithContext(ctx).Model(&AdminUser{}).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CreateAdmin 新建管理员（setup 仅首次调用；靠 uk_admin_username 兜底并发重名）。
func (s *Store) CreateAdmin(ctx context.Context, u *AdminUser) error {
	return s.db.WithContext(ctx).Create(u).Error
}

// GetAdminByUsername 按用户名取管理员（登录校验用）。
func (s *Store) GetAdminByUsername(ctx context.Context, name string) (*AdminUser, error) {
	var u AdminUser
	if err := s.db.WithContext(ctx).Where("username = ?", name).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// CreateSession 落一条会话（login/setup 后签发）。
func (s *Store) CreateSession(ctx context.Context, se *AdminSession) error {
	return s.db.WithContext(ctx).Create(se).Error
}

// GetSessionByToken 按 token 取会话（/admin/auth/me 校验）。过期视为无效。
func (s *Store) GetSessionByToken(ctx context.Context, token string) (*AdminSession, error) {
	var se AdminSession
	if err := s.db.WithContext(ctx).Where("token = ?", token).First(&se).Error; err != nil {
		return nil, err
	}
	if time.Now().After(se.ExpiresAt) {
		return nil, ErrSessionExpired
	}
	return &se, nil
}

// DeleteSession 删会话（logout）。
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	return s.db.WithContext(ctx).Where("token = ?", token).Delete(&AdminSession{}).Error
}
