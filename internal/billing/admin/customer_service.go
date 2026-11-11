package admin

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/frostloom/ai-gateway/internal/billing/store"
	"github.com/frostloom/ai-gateway/internal/supportconfig"
)

// RegisterCustomerService installs control-plane routes. The outer billing
// handler verifies the control token, and the gateway requires an admin session.
func RegisterCustomerService(mux *http.ServeMux, st *store.Store, log *slog.Logger) {
	cipher, cipherErr := supportconfig.NewCipher(os.Getenv(supportconfig.MasterKeyEnv))
	fallback := supportconfig.DefaultFromEnv(os.Getenv)
	mux.HandleFunc("/admin/customer-service", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.Header().Set("Allow", "GET, POST")
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if r.Method == http.MethodGet {
			if cipherErr != nil {
				// With no cipher we can show environmental setup status, but must not pretend
				// that a persisted encrypted config was read successfully.
				view := supportconfig.Redact(&fallback)
				view.EncryptionReady = false
				writeJSON(w, http.StatusOK, view)
				return
			}
			cfg, err := st.LoadCustomerServiceConfig(r.Context(), cipher)
			if err != nil {
				log.Error("customer service config read failed")
				writeErr(w, http.StatusServiceUnavailable, "客服配置读取失败，请检查服务端加密配置")
				return
			}
			if cfg == nil {
				cfg = &fallback
			}
			view := supportconfig.Redact(cfg)
			view.EncryptionReady = true
			writeJSON(w, http.StatusOK, view)
			return
		}
		if cipherErr != nil {
			writeErr(w, http.StatusServiceUnavailable, "请先配置服务端客服密钥加密主密钥")
			return
		}
		var in *supportconfig.Update
		r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&in); err != nil || in == nil {
			writeErr(w, http.StatusBadRequest, "配置内容无效")
			return
		}
		if dec.Decode(new(any)) != io.EOF {
			writeErr(w, http.StatusBadRequest, "配置内容无效")
			return
		}
		cfg, err := st.SaveCustomerServiceConfig(r.Context(), cipher, *in, fallback)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrCustomerServiceConflict):
				writeErr(w, http.StatusConflict, err.Error())
			case errors.Is(err, store.ErrCustomerServiceInvalid):
				writeErr(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, supportconfig.ErrEncryptionUnavailable):
				writeErr(w, http.StatusServiceUnavailable, "客服密钥加密服务不可用")
			default:
				log.Error("customer service config save failed")
				writeErr(w, http.StatusInternalServerError, "客服配置保存失败")
			}
			return
		}
		view := supportconfig.Redact(cfg)
		view.EncryptionReady = true
		writeJSON(w, http.StatusOK, view)
	})
}
