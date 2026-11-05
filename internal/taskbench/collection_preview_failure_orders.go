package taskbench

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

// This private companion reveals only recharge_orders. Its complete typed rows
// must reproduce the existing full SQL capture, including nullable paid_at.
func collectionPreviewFailureOrdersTable(in collectionPreviewFailureOrders) (qualityTableCapture, error) {
	columns := []string{"id", "tenant_id", "order_no", "amount_money", "amount_cent", "status", "paid_at", "created_at", "updated_at"}
	fail := func() (qualityTableCapture, error) {
		return qualityTableCapture{}, fmt.Errorf("complete private orders unavailable")
	}
	if !reflect.DeepEqual(in.Columns, columns) || in.Rows == nil || len(in.Rows) > 100000 {
		return fail()
	}
	out := qualityTableCapture{Columns: append([]string(nil), columns...), Rows: []string{}}
	ids, names := map[uint64]bool{}, map[string]bool{}
	for _, row := range in.Rows {
		if row.ID == 0 || row.TenantID == 0 || row.OrderNo == "" || len(row.OrderNo) > 64 || ids[row.ID] || names[row.OrderNo] || row.Status == "" || len(row.Status) > 16 || row.CreatedAt.IsZero() || row.UpdatedAt.IsZero() || (row.PaidAt != nil && row.PaidAt.IsZero()) {
			return fail()
		}
		ids[row.ID], names[row.OrderNo] = true, true
		type cell struct {
			Null  bool   `json:"null"`
			Bytes []byte `json:"bytes"`
		}
		values := []string{strconv.FormatUint(row.ID, 10), strconv.FormatUint(row.TenantID, 10), row.OrderNo, strconv.FormatInt(row.AmountMoney, 10), strconv.FormatInt(row.AmountCent, 10), row.Status, "", row.CreatedAt.Format(time.RFC3339Nano), row.UpdatedAt.Format(time.RFC3339Nano)}
		cells := make([]cell, len(values))
		for i, value := range values {
			cells[i] = cell{Bytes: []byte(value)}
		}
		if row.PaidAt == nil {
			cells[6] = cell{Null: true}
		} else {
			cells[6] = cell{Bytes: []byte(row.PaidAt.Format(time.RFC3339Nano))}
		}
		raw, err := json.Marshal(cells)
		if err != nil {
			return fail()
		}
		out.Rows = append(out.Rows, conversationHash(raw))
	}
	sort.Strings(out.Rows)
	return out, nil
}
func collectionPreviewFailureOrdersBound(in collectionPreviewFailureEvidence, f Fixture) bool {
	if in.Orders == nil {
		return false
	}
	table, err := collectionPreviewFailureOrdersTable(*in.Orders)
	if err != nil || !reflect.DeepEqual(table, in.Before.Tables["recharge_orders"]) || !reflect.DeepEqual(table, in.After.Tables["recharge_orders"]) {
		return false
	}
	// Public own/control projections must also match the complete private table.
	check := func(state ConversationState) bool {
		snapshots := map[uint64]Snapshot{f.TenantID: state.Own}
		for tenant, s := range state.Controls {
			snapshots[tenant] = s
		}
		for tenant, s := range snapshots {
			expected := map[uint64]store.RechargeOrder{}
			for _, row := range in.Orders.Rows {
				if row.TenantID == tenant {
					expected[row.ID] = row
				}
			}
			if len(expected) != len(s.Orders) {
				return false
			}
			seen := map[uint64]bool{}
			for _, row := range s.Orders {
				want, ok := expected[row.ID]
				if !ok || seen[row.ID] || !qualityJSONEqual(want, row) {
					return false
				}
				seen[row.ID] = true
			}
		}
		return true
	}
	return check(in.Before.State) && check(in.After.State)
}
func collectionPreviewMissingOrderFailure(tr Trace, args map[string]any, in collectionPreviewFailureEvidence, f Fixture) bool {
	if tr.Tool != "refund_recharge" || !collectionPreviewFailureOrdersBound(in, f) || windowOnlyKeys(args, "tenant_id", "order_no") != nil {
		return false
	}
	order, ok := args["order_no"].(string)
	if !ok || order == "" || len(order) > 64 || strings.TrimSpace(order) != order {
		return false
	}
	for _, row := range in.Orders.Rows {
		if row.TenantID == f.TenantID && row.OrderNo == order {
			return false
		}
	}
	return tr.Result == "操作不可执行：未找到你名下的订单 "+order
}
