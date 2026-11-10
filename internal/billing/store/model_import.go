package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrProviderChanged = errors.New("channel changed during discovery; discover again")

// ModelImportItem uses pointers so an explicit zero price differs from a missing price.
type ModelImportItem struct {
	ModelID         string `json:"model_id"`
	Name            string `json:"name,omitempty"`
	Vendor          string `json:"vendor,omitempty"`
	ContextLen      *int   `json:"context_len,omitempty"`
	InputPriceCent  *int64 `json:"input_price_cent"`
	OutputPriceCent *int64 `json:"output_price_cent"`
}

type ModelImportResult struct {
	ProviderID       uint64   `json:"provider_id"`
	Created          []Model  `json:"created"`
	Skipped          []Model  `json:"skipped"`
	ConfiguredModels []string `json:"configured_models"`
}

func (s *Store) GetProviderForDiscovery(ctx context.Context, id uint64) (*Provider, error) {
	var p Provider
	if err := s.db.WithContext(ctx).First(&p, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProviderNotFound
		}
		return nil, err
	}
	if p.Status == ProviderDeleted {
		return nil, ErrProviderNotFound
	}
	return &p, nil
}

func validImportText(value string, max int, required bool) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > max || value != strings.TrimSpace(value) || (required && value == "") {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsControl)
}

func ValidateModelImport(items []ModelImportItem) error {
	if len(items) == 0 || len(items) > 100 {
		return errors.New("select between 1 and 100 models")
	}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if !validImportText(item.ModelID, 64, true) || item.ModelID == "*" || !validImportText(item.Name, 128, false) || !validImportText(item.Vendor, 32, false) {
			return errors.New("invalid model id, name or vendor")
		}
		if seen[item.ModelID] {
			return errors.New("duplicate model id")
		}
		seen[item.ModelID] = true
		if item.ContextLen != nil && (*item.ContextLen <= 0 || int64(*item.ContextLen) > 2147483647) {
			return errors.New("context length must be between 1 and 2147483647 tokens")
		}
		for _, price := range []*int64{item.InputPriceCent, item.OutputPriceCent} {
			if price != nil && (*price < 0 || *price > 1000000000) {
				return errors.New("prices must be between 0 and 1000000000")
			}
		}
	}
	return nil
}

// ImportProviderModels commits routing configuration and catalog additions together.
// allowed is obtained by the server from a fresh authenticated discovery request.
func (s *Store) ImportProviderModels(ctx context.Context, snapshot Provider, items []ModelImportItem, allowed map[string]bool) (*ModelImportResult, error) {
	if err := ValidateModelImport(items); err != nil {
		return nil, err
	}
	result := &ModelImportResult{ProviderID: snapshot.ID, Created: []Model{}, Skipped: []Model{}, ConfiguredModels: []string{}}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current Provider
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, snapshot.ID).Error; err != nil {
			return err
		}
		if current.Status != 0 {
			return errors.New("channel must be active to import models")
		}
		if current.BaseURL != snapshot.BaseURL || current.UpstreamKey != snapshot.UpstreamKey || !current.UpdatedAt.Equal(snapshot.UpdatedAt) || current.Models != snapshot.Models {
			return ErrProviderChanged
		}
		var configured []string
		if err := json.Unmarshal([]byte(current.Models), &configured); err != nil {
			return errors.New("invalid channel model configuration")
		}
		explicit := make(map[string]bool, len(configured))
		for _, id := range configured {
			if id != "*" {
				explicit[id] = true
			}
		}
		for _, item := range items {
			if !allowed[item.ModelID] && !explicit[item.ModelID] {
				return errors.New("model was not confirmed by upstream or channel configuration")
			}
			if !explicit[item.ModelID] {
				configured = append(configured, item.ModelID)
				explicit[item.ModelID] = true
			}
			var existing Model
			lookup := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("model_id = ?", item.ModelID).First(&existing)
			if lookup.Error == nil {
				result.Skipped = append(result.Skipped, existing)
				continue
			}
			if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
				return lookup.Error
			}
			if item.ContextLen == nil || item.InputPriceCent == nil || item.OutputPriceCent == nil {
				return errors.New("new models require context length and explicit input and output prices")
			}
			name := item.Name
			if name == "" {
				name = item.ModelID
			}
			vendor := item.Vendor
			if vendor == "" {
				vendor = "Unknown"
			}
			m := Model{ModelID: item.ModelID, Name: name, Vendor: vendor, ContextLen: *item.ContextLen, InputPriceCent: *item.InputPriceCent, OutputPriceCent: *item.OutputPriceCent, Status: 0, Tags: "[]"}
			// A duplicate-key error leaves a MySQL transaction usable. Read the
			// existing row with a current read, preserving its prices and status.
			created := tx.Create(&m)
			if created.Error != nil {
				var duplicate *mysql.MySQLError
				if !errors.As(created.Error, &duplicate) || duplicate.Number != 1062 {
					return created.Error
				}
				m = Model{}
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("model_id = ?", item.ModelID).First(&m).Error; err != nil {
					return err
				}
				result.Skipped = append(result.Skipped, m)
			} else {
				result.Created = append(result.Created, m)
			}
		}
		raw, err := json.Marshal(configured)
		if err != nil {
			return err
		}
		if err := tx.Model(&current).Update("models", string(raw)).Error; err != nil {
			return err
		}
		result.ConfiguredModels = configured
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.forgetCachedModels()
	return result, nil
}
