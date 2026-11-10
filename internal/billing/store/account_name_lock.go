package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

var ErrAccountNameTaken = errors.New("account name already in use")

// Account creation takes one MySQL advisory lock on a pinned connection so
// admin setup and public registration cannot claim the same name concurrently.
func (s *Store) withAccountName(ctx context.Context, name string, fn func(*gorm.DB) error) error {
	return s.db.WithContext(ctx).Connection(func(conn *gorm.DB) (result error) {
		var acquired int
		if err := conn.Raw("SELECT GET_LOCK(?, ?)", "ai_gateway_account_names_v1", 5).Scan(&acquired).Error; err != nil {
			return err
		}
		if acquired != 1 {
			return errors.New("account name lock unavailable")
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var released sql.NullInt64
			err := conn.WithContext(cleanup).Raw("SELECT RELEASE_LOCK(?)", "ai_gateway_account_names_v1").Scan(&released).Error
			if err != nil || !released.Valid || released.Int64 != 1 {
				// Never return a connection that may still own a named lock to the pool.
				if c, ok := conn.Statement.ConnPool.(*sql.Conn); ok {
					_ = c.Raw(func(any) error { return driver.ErrBadConn })
				}
				if result == nil {
					result = errors.New("account name lock release failed; check whether account was created")
				}
			}
		}()
		return conn.Transaction(func(tx *gorm.DB) error {
			normalized := strings.ToLower(strings.TrimSpace(name))
			var a, u int64
			if err := tx.Model(&AdminUser{}).Where("LOWER(username) = ?", normalized).Count(&a).Error; err != nil {
				return err
			}
			if err := tx.Model(&PortalUser{}).Where("LOWER(username) = ?", normalized).Count(&u).Error; err != nil {
				return err
			}
			if a != 0 || u != 0 {
				return ErrAccountNameTaken
			}
			return fn(tx)
		})
	})
}
