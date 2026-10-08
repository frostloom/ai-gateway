package taskbench

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func Run(ctx context.Context, o Options) Report {
	data, _ := json.Marshal(o.Cases)
	hash := sha256.Sum256(data)
	report := Report{Schema: "taskbench.v2", RunID: randomID(), StartedAt: time.Now().UTC(), Metadata: o.Metadata, DatasetSHA256: hex.EncodeToString(hash[:]), Limitations: []string{"Small frozen billing regression suite; not general model accuracy.", "Clarification is a slot-oriented text rubric, not semantic understanding proof.", "Subscription read identity accepts the exact observed plan ID or canonical catalogue name; contradictory IDs/names fail. The v1 oracle was overly strict about the 套餐# prefix and its historical reports remain unchanged.", "Synthetic followups have zero user think time; fixture setup excluded, final state verification included.", "Completion requires authoritative state, confirmation timing and parameter evidence; tool selection alone earns no credit.", "Cancellation no-write observation is separate from successful cancellation, which also requires no pending action.", "Fixture tenants, credentials hashes, business records and sessions retained for historical verification."}}
	if o.Client == nil {
		o.Client = &http.Client{Timeout: 120 * time.Second}
	}
	for i, c := range o.Cases {
		report.Results = append(report.Results, runCase(ctx, o, report.RunID, c))
		if o.OnProgress != nil {
			o.OnProgress(i+1, len(o.Cases))
		}
	}
	report.Metrics = Summarize(report.Results)
	return report
}
func runCase(ctx context.Context, o Options, runID string, c Case) Result {
	r := Result{Case: c}
	fail := func(err error) { r.Errors = append(r.Errors, err.Error()) }
	if o.Backend == nil {
		r.SetupError = true
		fail(fmt.Errorf("nil fixture backend"))
		return r
	}
	f, err := o.Backend.Setup(ctx, runID, c)
	r.Fixture = f
	if err != nil {
		r.SetupError = true
		fail(fmt.Errorf("fixture: %w", err))
		return r
	}
	initial, err := o.Backend.Snapshot(ctx, f, "")
	r.Initial = initial
	r.Final = initial
	if err != nil {
		r.SetupError = true
		fail(fmt.Errorf("initial snapshot: %w", err))
		return r
	}
	start := time.Now()
	session := ""
	turn := func(message, stage string) bool {
		t := Turn{Message: message, Stage: stage}
		raw, _ := json.Marshal(map[string]string{"message": message, "session_id": session})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.BaseURL, "/")+"/chat", bytes.NewReader(raw))
		begun := time.Now()
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+f.APIKey)
			var resp *http.Response
			resp, err = o.Client.Do(req)
			if err == nil {
				t.Status = resp.StatusCode
				body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
				resp.Body.Close()
				t.RawBody = string(body)
				err = readErr
				if err == nil && resp.StatusCode == 200 {
					err = json.Unmarshal(body, &t.Response)
				}
			}
		}
		t.LatencyMS = float64(time.Since(begun)) / float64(time.Millisecond)
		if err != nil {
			t.Error = err.Error()
			fail(fmt.Errorf("%s request: %w", stage, err))
		}
		if t.Status == 200 && (o.Metadata.JevMode == "on" || o.Metadata.JevMode == "off") {
			if t.Response.Telemetry == nil || t.Response.Telemetry.JevEnabled == nil {
				t.Error += " missing JEV mode telemetry"
				fail(fmt.Errorf("cannot attest declared JEV mode"))
			} else if *t.Response.Telemetry.JevEnabled != (o.Metadata.JevMode == "on") {
				t.Error += " JEV mode mismatch"
				fail(fmt.Errorf("observed JEV mode contradicts report metadata"))
			}
		}
		if t.Response.SessionID != "" {
			if session != "" && session != t.Response.SessionID {
				fail(fmt.Errorf("session unexpectedly changed"))
			}
			session = t.Response.SessionID
		}
		snap, snapErr := o.Backend.Snapshot(ctx, f, session)
		t.Snapshot = snap
		r.Final = snap
		if snapErr != nil {
			t.Error += " snapshot: " + snapErr.Error()
			fail(fmt.Errorf("%s snapshot: %w", stage, snapErr))
		}
		if stage != "confirm" && stage != "duplicate_confirm" && !sameDomain(initial, snap) {
			r.PrematureWrite = true
		}
		if t.Response.Pending {
			r.PreviewObserved = true
			p, pErr := o.Backend.Pending(ctx, session)
			t.Pending = &p
			if pErr != nil {
				t.Error += " pending: " + pErr.Error()
				fail(fmt.Errorf("pending state: %w", pErr))
			} else if PendingCorrect(c, f, p) && stage != "request_missing" {
				r.PreviewCorrect = true
				if r.TimeToPreviewMS == nil {
					ms := float64(time.Since(start)) / float64(time.Millisecond)
					r.TimeToPreviewMS = &ms
				}
			} else {
				fail(fmt.Errorf("unexpected pending tool or parameters at %s", stage))
			}
		}
		r.Turns = append(r.Turns, t)
		return err == nil && snapErr == nil && t.Status == 200 && t.Error == ""
	}
	message := strings.ReplaceAll(c.Message, "{order_no}", f.OrderNo)
	stage := "request"
	if c.MissingSlot != "" {
		stage = "request_missing"
	}
	ok := turn(message, stage)
	if ok && c.MissingSlot != "" {
		first := r.Turns[0]
		r.ClarificationOffered = Clarifies(first.Response.Reply, c.MissingSlot) && !first.Response.Pending && sameDomain(initial, first.Snapshot)
		if !r.ClarificationOffered {
			fail(fmt.Errorf("missing-slot first turn did not ask for %s", c.MissingSlot))
			ok = false
		} else {
			ok = turn(c.Followup, "supply_slot")
		}
	} else if ok && !r.Turns[0].Response.Pending {
		r.RedundantClarification = Clarifies(r.Turns[0].Response.Reply, "amount") || Clarifies(r.Turns[0].Response.Reply, "plan")
	}
	if ok && !c.Read && !r.PrematureWrite {
		last := r.Turns[len(r.Turns)-1]
		if last.Response.Pending && last.Pending != nil && PendingCorrect(c, f, *last.Pending) {
			if c.Cancel {
				ok = turn("取消，不执行这次操作。", "cancel")
			} else {
				ok = turn("确认", "confirm")
			}
			if ok && c.DuplicateConfirm && !c.Cancel {
				before := r.Final
				ok = turn("确认", "duplicate_confirm")
				if !sameDomain(before, r.Final) {
					fail(fmt.Errorf("duplicate confirmation changed business state"))
				}
			}
		} else {
			fail(fmt.Errorf("expected parameter-correct pending action before authorization"))
		}
	}
	// One fresh final oracle read catches projection failures after the last response.
	final, err := o.Backend.Snapshot(ctx, f, session)
	if err != nil {
		fail(fmt.Errorf("final snapshot: %w", err))
	} else {
		r.Final = final
	}
	r.DurationMS = float64(time.Since(start)) / float64(time.Millisecond)
	Score(&r)
	return r
}
