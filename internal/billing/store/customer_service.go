package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/frostloom/ai-gateway/internal/supportconfig"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrCustomerServiceConflict = errors.New("客服配置已被其他管理员更新，请重新加载")
var ErrCustomerServiceInvalid = errors.New("客服配置无效")

// CustomerServiceSettings is a singleton. Only ciphertext is persisted for API
// keys, and secret columns never appear in its JSON representation.
type CustomerServiceSettings struct {
	ID                 uint64    `gorm:"primaryKey" json:"id"`
	Revision           uint64    `gorm:"not null;default:0" json:"revision"`
	LLMBaseURL         string    `gorm:"column:llm_base_url;size:512;not null" json:"llm_base_url"`
	LLMModel           string    `gorm:"column:llm_model;size:128;not null" json:"llm_model"`
	LLMProtocol        string    `gorm:"column:llm_protocol;size:16;not null" json:"llm_protocol"`
	LLMMaxOutputTokens int       `gorm:"column:llm_max_output_tokens;not null" json:"llm_max_output_tokens"`
	LLMKeyCiphertext   string    `gorm:"column:llm_key_ciphertext;type:text;not null" json:"-"`
	JEVEnabled         bool      `gorm:"column:jev_enabled;not null;default:false" json:"jev_enabled"`
	JEVEndpoint        string    `gorm:"column:jev_endpoint;size:512;not null" json:"jev_endpoint"`
	JEVModel           string    `gorm:"column:jev_model;size:128;not null" json:"jev_model"`
	JEVKeyCiphertext   string    `gorm:"column:jev_key_ciphertext;type:text;not null" json:"-"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func decodeCustomerService(row CustomerServiceSettings, cipher *supportconfig.Cipher) (*supportconfig.Config, error) {
	llmKey, err := cipher.Open(row.LLMKeyCiphertext, "llm")
	if err != nil {
		return nil, err
	}
	jevKey, err := cipher.Open(row.JEVKeyCiphertext, "jev")
	if err != nil {
		return nil, err
	}
	cfg := &supportconfig.Config{Revision: row.Revision, UpdatedAt: row.UpdatedAt, LLM: supportconfig.LLMConfig{BaseURL: row.LLMBaseURL, Model: row.LLMModel, Protocol: row.LLMProtocol, MaxOutputTokens: row.LLMMaxOutputTokens, APIKey: llmKey}, JEV: supportconfig.JEVConfig{Enabled: row.JEVEnabled, Endpoint: row.JEVEndpoint, Model: row.JEVModel, APIKey: jevKey}}
	if supportconfig.ValidateConfig(*cfg) != nil {
		return nil, ErrCustomerServiceInvalid
	}
	return cfg, nil
}

func (s *Store) LoadCustomerServiceConfig(ctx context.Context, cipher *supportconfig.Cipher) (*supportconfig.Config, error) {
	var row CustomerServiceSettings
	err := s.db.WithContext(ctx).First(&row, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeCustomerService(row, cipher)
}

// SaveCustomerServiceConfig atomically compares the admin's revision and replaces
// one immutable config. The optional fallback supplies environment credentials on
// the first save; after that only encrypted database credentials can be retained.
func (s *Store) SaveCustomerServiceConfig(ctx context.Context, cipher *supportconfig.Cipher, in supportconfig.Update, fallback ...supportconfig.Config) (*supportconfig.Config, error) {
	// Check availability even when a disabled JEV has no key.
	if _, err := cipher.Seal("", "llm"); err != nil {
		return nil, err
	}
	var saved *supportconfig.Config
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&CustomerServiceSettings{ID: 1}).Error; err != nil {
			return err
		}
		var row CustomerServiceSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, 1).Error; err != nil {
			return err
		}
		if row.Revision != in.Revision || row.Revision == ^uint64(0) {
			return ErrCustomerServiceConflict
		}
		var previous supportconfig.Config
		if row.Revision > 0 {
			decoded, err := decodeCustomerService(row, cipher)
			if err != nil {
				return err
			}
			previous = *decoded
		} else if len(fallback) > 0 {
			previous = fallback[0]
		}
		cfg := supportconfig.Config{Revision: row.Revision + 1, UpdatedAt: time.Now().UTC(), LLM: supportconfig.LLMConfig{BaseURL: in.LLM.BaseURL, Model: in.LLM.Model, Protocol: in.LLM.Protocol, MaxOutputTokens: in.LLM.MaxOutputTokens, APIKey: in.LLM.APIKey}, JEV: supportconfig.JEVConfig{Enabled: in.JEV.Enabled, Endpoint: in.JEV.Endpoint, Model: in.JEV.Model, APIKey: in.JEV.APIKey}}
		if cfg.LLM.APIKey == "" {
			if previous.LLM.APIKey != "" && !supportconfig.SameOrigin(previous.LLM.BaseURL, cfg.LLM.BaseURL) {
				return fmt.Errorf("%w: 更改客服 LLM 地址的协议、主机或端口时，请重新填写密钥", ErrCustomerServiceInvalid)
			}
			cfg.LLM.APIKey = previous.LLM.APIKey
		}
		if cfg.JEV.APIKey == "" {
			if previous.JEV.APIKey != "" && !supportconfig.SameOrigin(previous.JEV.Endpoint, cfg.JEV.Endpoint) {
				return fmt.Errorf("%w: 更改 JEV 地址的协议、主机或端口时，请重新填写密钥", ErrCustomerServiceInvalid)
			}
			cfg.JEV.APIKey = previous.JEV.APIKey
		}
		if err := supportconfig.ValidateConfig(cfg); err != nil {
			return fmt.Errorf("%w: %s", ErrCustomerServiceInvalid, err.Error())
		}
		llmKey, err := cipher.Seal(cfg.LLM.APIKey, "llm")
		if err != nil {
			return err
		}
		jevKey, err := cipher.Seal(cfg.JEV.APIKey, "jev")
		if err != nil {
			return err
		}
		values := map[string]any{"revision": cfg.Revision, "llm_base_url": cfg.LLM.BaseURL, "llm_model": cfg.LLM.Model, "llm_protocol": cfg.LLM.Protocol, "llm_max_output_tokens": cfg.LLM.MaxOutputTokens, "llm_key_ciphertext": llmKey, "jev_enabled": cfg.JEV.Enabled, "jev_endpoint": cfg.JEV.Endpoint, "jev_model": cfg.JEV.Model, "jev_key_ciphertext": jevKey, "updated_at": cfg.UpdatedAt}
		if err := tx.Model(&row).Updates(values).Error; err != nil {
			return err
		}
		saved = &cfg
		return nil
	})
	return saved, err
}
