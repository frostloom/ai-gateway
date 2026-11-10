package admin

import (
	"github.com/frostloom/ai-gateway/internal/billing/store"
	"log/slog"
	"net/http"
)

func registerVouchers(mux *http.ServeMux, st *store.Store, log *slog.Logger) {
	mux.HandleFunc("/admin/vouchers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodGet {
			rows, err := st.ListBalanceVouchers(r.Context())
			if err != nil {
				log.Error("list vouchers", "err", err)
				writeErr(w, 500, "读取兑换码失败")
				return
			}
			writeJSON(w, 200, map[string]any{"items": rows})
			return
		}
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			AmountMoney  int64 `json:"amount_money"`
			Count        int   `json:"count"`
			ValidityDays int   `json:"validity_days"`
		}
		if !parseBody(w, r, &in) {
			return
		}
		issued, err := st.CreateBalanceVouchers(r.Context(), in.AmountMoney, in.Count, in.ValidityDays)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"items": issued})
	})
	mux.HandleFunc("/portal/redeem", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !requirePOST(w, r) {
			return
		}
		var in struct {
			TenantID uint64 `json:"tenant_id"`
			Code     string `json:"code"`
		}
		if !parseBody(w, r, &in) {
			return
		}
		result, err := st.RedeemBalanceVoucher(r.Context(), in.TenantID, in.Code)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, result)
	})
}
