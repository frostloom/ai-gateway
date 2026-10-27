package taskbench

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

// These concrete, read-only captures never issue a continuation proof.
// Callers hold driver.mu while no Handler phase is active. State retains the
// exact observation; a new independent oracle read checks that material first.
func captureWindowQualityOwner(d *windowOwnedDriver) error {
	if d == nil || d.permit == nil {
		return fmt.Errorf("quality capture owner unavailable")
	}
	p := d.permit
	p.mu.Lock()
	defer p.mu.Unlock()
	if !d.setupDone || d.phase != nil || d.environment == nil || d.environment.closed || d.oracle == nil || d.oracle.env != d.environment || d.oracle.f.TenantID != d.fixture.TenantID || p.guardWindowOwnedLocked(d) != nil {
		return fmt.Errorf("quality capture owner unavailable")
	}
	return nil
}
func captureWindowQualityEffects(ctx context.Context, d *windowOwnedDriver) (qualityEffectCapture, error) {
	reject := func() (qualityEffectCapture, error) {
		return qualityEffectCapture{}, fmt.Errorf("quality effect capture unavailable")
	}
	if ctx == nil || ctx.Err() != nil || captureWindowQualityOwner(d) != nil {
		return reject()
	}
	rawState := d.initial
	if len(d.observations) > 0 {
		rawState = d.observations[len(d.observations)-1].State
	}
	live, err := d.oracle.State(ctx, d.fixture, d.session)
	if err != nil || !sameWindowQualityCapturedState(rawState, live) {
		return reject()
	}
	var database string
	if err = d.environment.db.WithContext(ctx).Raw("SELECT DATABASE()").Scan(&database).Error; err != nil || database != d.environment.name {
		return reject()
	}
	type tableName struct {
		Name string `gorm:"column:TABLE_NAME"`
		Kind string `gorm:"column:TABLE_TYPE"`
	}
	var actual []tableName
	if err = d.environment.db.WithContext(ctx).Raw("SELECT TABLE_NAME,TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() ORDER BY TABLE_NAME").Scan(&actual).Error; err != nil {
		return reject()
	}
	expected := append(qualityPreviewTableNames(), "agent_audit_log")
	sort.Strings(expected)
	if len(actual) != len(expected) {
		return reject()
	}
	for i, row := range actual {
		if row.Name != expected[i] || row.Kind != "BASE TABLE" {
			return reject()
		}
	}
	out := qualityEffectCapture{State: rawState, Tables: map[string]qualityTableCapture{}, Audits: []store.AgentAuditLog{}, Sessions: []store.AgentSession{}}
	for _, name := range qualityPreviewTableNames() {
		// Only fixed schema identifiers appear here; caller data is never SQL.
		rows, e := d.environment.db.WithContext(ctx).Raw("SELECT * FROM `" + name + "` LIMIT 100001").Rows()
		if e != nil {
			return reject()
		}
		table, e := captureWindowQualityRows(rows)
		closeErr := rows.Close()
		if e != nil || closeErr != nil {
			return reject()
		}
		out.Tables[name] = table
	}
	if err = d.environment.db.WithContext(ctx).Order("id").Limit(100001).Find(&out.Audits).Error; err != nil || len(out.Audits) > 100000 {
		return reject()
	}
	if err = d.environment.db.WithContext(ctx).Order("id").Limit(100001).Find(&out.Sessions).Error; err != nil || len(out.Sessions) > 100000 {
		return reject()
	}
	sessionTable, err := qualitySessionTableCapture(out.Sessions, out.Tables["agent_sessions"].Columns)
	if err != nil || !reflect.DeepEqual(sessionTable, out.Tables["agent_sessions"]) {
		return reject()
	}
	if err = captureWindowQualityOwner(d); err != nil {
		return reject()
	}
	return out, nil
}
func captureWindowQualityRows(rows *sql.Rows) (qualityTableCapture, error) {
	out := qualityTableCapture{Rows: []string{}}
	reject := func() (qualityTableCapture, error) {
		return qualityTableCapture{}, fmt.Errorf("quality table capture unavailable")
	}
	if rows == nil {
		return reject()
	}
	columns, err := rows.Columns()
	if err != nil || len(columns) == 0 {
		return reject()
	}
	seen := map[string]bool{}
	for _, column := range columns {
		if column == "" || seen[column] {
			return reject()
		}
		seen[column] = true
	}
	out.Columns = append([]string(nil), columns...)
	for rows.Next() {
		if len(out.Rows) >= 100000 {
			return reject()
		}
		values := make([]sql.NullString, len(columns))
		args := make([]any, len(columns))
		for i := range values {
			args[i] = &values[i]
		}
		if err = rows.Scan(args...); err != nil {
			return reject()
		}
		type cell struct {
			Null  bool   `json:"null"`
			Bytes []byte `json:"bytes"`
		}
		cells := make([]cell, len(values))
		for i, value := range values {
			if len(value.String) > 1<<20 {
				return reject()
			}
			cells[i] = cell{Null: !value.Valid}
			if value.Valid {
				cells[i].Bytes = []byte(value.String)
			}
		}
		raw, e := json.Marshal(cells)
		if e != nil {
			return reject()
		}
		out.Rows = append(out.Rows, conversationHash(raw))
	}
	if rows.Err() != nil {
		return reject()
	}
	sort.Strings(out.Rows)
	return out, nil
}
func sameWindowQualityCapturedState(a, b ConversationState) bool {
	_, az := a.CapturedAt.Zone()
	_, bz := b.CapturedAt.Zone()
	if az != bz {
		return false
	}
	if a.CapturedAt.IsZero() || b.CapturedAt.IsZero() || a.CapturedAt.Format("2006-01-02") != b.CapturedAt.Format("2006-01-02") || a.AnswerAuthority == nil || b.AnswerAuthority == nil || !a.AnswerAuthority.CapturedAt.Equal(a.CapturedAt) || !b.AnswerAuthority.CapturedAt.Equal(b.CapturedAt) {
		return false
	}
	// Clone before normalizing only capture annotations. Business times, quotas,
	// bill windows, audit rows and all authority values remain exact.
	ac, e := cloneWindowOwned(a)
	if e != nil {
		return false
	}
	bc, e := cloneWindowOwned(b)
	if e != nil {
		return false
	}
	ac.CapturedAt = time.Time{}
	bc.CapturedAt = time.Time{}
	ac.AnswerAuthority.CapturedAt = time.Time{}
	bc.AnswerAuthority.CapturedAt = time.Time{}
	return reflect.DeepEqual(ac, bc)
}
func captureWindowQualitySession(ctx context.Context, d *windowOwnedDriver, allowAbsent bool) (json.RawMessage, *qualityPendingCapture, error) {
	reject := func() (json.RawMessage, *qualityPendingCapture, error) {
		return nil, nil, fmt.Errorf("quality session capture unavailable")
	}
	if ctx == nil || ctx.Err() != nil || captureWindowQualityOwner(d) != nil {
		return reject()
	}
	if d.session == "" {
		if allowAbsent && len(d.observations) == 0 {
			return json.RawMessage("{}"), nil, nil
		}
		return reject()
	}
	raw, err := d.environment.rdb.Get(ctx, "agent:session:"+d.session).Bytes()
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return reject()
	}
	obj, err := windowJSONObject(raw)
	if err != nil || windowOnlyKeys(obj, "history", "pending_action") != nil {
		return reject()
	}
	if _, ok := obj["history"].([]any); !ok {
		return reject()
	}
	value, ok := obj["pending_action"]
	if !ok || value == nil {
		return append(json.RawMessage(nil), raw...), nil, nil
	}
	pending, ok := value.(map[string]any)
	if !ok || len(pending) != 5 || windowOnlyKeys(pending, "tool", "args", "preview", "idem_key", "tenant_id") != nil {
		return reject()
	}
	packed, err := json.Marshal(pending)
	if err != nil {
		return reject()
	}
	var parsed struct {
		Tool     string         `json:"tool"`
		Args     map[string]any `json:"args"`
		Preview  string         `json:"preview"`
		IdemKey  string         `json:"idem_key"`
		TenantID uint64         `json:"tenant_id"`
	}
	decoder := json.NewDecoder(bytes.NewReader(packed))
	decoder.UseNumber()
	if decoder.Decode(&parsed) != nil || parsed.TenantID != d.fixture.TenantID || !isConversationWrite(parsed.Tool) || parsed.Args == nil || strings.TrimSpace(parsed.Preview) == "" || strings.TrimSpace(parsed.IdemKey) == "" {
		return reject()
	}
	out := &qualityPendingCapture{Pending: Pending{Tool: parsed.Tool, Args: parsed.Args, TenantID: parsed.TenantID}, Preview: parsed.Preview, IdemKey: parsed.IdemKey, SessionID: d.session, Raw: append(json.RawMessage(nil), raw...)}
	return append(json.RawMessage(nil), raw...), out, nil
}
