package agent

import (
	"context"
	"fmt"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

// Confirmation and observability tests exercise a scripted write against fakeAdmin.
// Production recharge stays closed, including when the demo environment flag is set.
func confirmationFixtureToolbox(admin *AdminClient) *Toolbox {
	tb := NewToolbox(admin)
	fixture := tb.tools["recharge"]
	fixture.preview = func(ctx context.Context, args map[string]any, tenantID uint64) (string, error) {
		amount, err := argInt(args, "amount_money")
		if err != nil {
			return "", err
		}
		if amount <= 0 {
			return "", fmt.Errorf("充值金额必须大于 0")
		}
		return fmt.Sprintf("测试写操作：将模拟充值 ¥%d。确认后到账。", amount), nil
	}
	fixture.run = func(ctx context.Context, args map[string]any, tenantID uint64, idemKey string) (string, error) {
		amount, err := argInt(args, "amount_money")
		if err != nil {
			return "", err
		}
		var order store.RechargeOrder
		if err := tb.postJSON(ctx, "/admin/recharge", map[string]any{"tenant_id": tenantID, "amount_money": amount, "idem_key": idemKey}, &order); err != nil {
			return "", err
		}
		if err := tb.postJSON(ctx, "/admin/recharge/pay", map[string]any{"order_no": order.OrderNo, "tenant_id": tenantID}, nil); err != nil {
			return "", err
		}
		return fmt.Sprintf("充值成功：订单 %s 已支付，到账 ¥%d（%d 分）。", order.OrderNo, order.AmountMoney, order.AmountCent), nil
	}
	return tb
}
