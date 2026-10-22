package taskbench

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sync"
)

// CampaignLimits are operator declarations, not account prices or permission.
// Each batch permanently reserves its entire worst-case envelope; no refunds.
type CampaignLimits struct {
	MaxBatches            int   `json:"max_batches"`
	MaxModelCalls         int   `json:"max_model_calls"`
	MaxModelMicroCNY      int64 `json:"max_model_micro_cny"`
	MaxJEVCalls           int   `json:"max_jev_calls"`
	MaxJEVMicroUSD        int64 `json:"max_jev_micro_usd"`
	JEVMicroUSDPerAttempt int64 `json:"jev_micro_usd_per_attempt"`
}

type campaignEvent struct {
	Kind          string          `json:"kind"`
	Contract      string          `json:"contract,omitempty"`
	Limits        *CampaignLimits `json:"limits,omitempty"`
	ID            string          `json:"id,omitempty"`
	Binding       string          `json:"binding,omitempty"`
	ModelCalls    int             `json:"model_calls,omitempty"`
	ModelMicroCNY int64           `json:"model_micro_cny,omitempty"`
	JEVCalls      int             `json:"jev_calls,omitempty"`
	JEVMicroUSD   int64           `json:"jev_micro_usd,omitempty"`
	Failed        bool            `json:"failed,omitempty"`
}

type CampaignBudget struct {
	mu              sync.Mutex
	dir             string
	file, lock      *os.File
	limits          CampaignLimits
	used            campaignEvent
	reserved        map[string]campaignEvent
	pending         string
	stopped, closed bool
}

type CampaignBatchLease struct {
	campaign         *CampaignBudget
	ID, Dir, Binding string
	limits           PairBudgetLimits
	jevCalls         int
	finished         bool
	running          bool
}

var campaignBatchID = regexp.MustCompile(`^batch-[0-9]{3,6}$`)

func (limits CampaignLimits) validate() error {
	if limits.MaxBatches < 1 || limits.MaxBatches > 10000 || limits.MaxModelCalls < 1 || limits.MaxModelCalls > 1000000 || limits.MaxModelMicroCNY < 1 || limits.MaxModelMicroCNY > 1000000000 || limits.MaxJEVCalls < 1 || limits.MaxJEVCalls > 1000000 || limits.MaxJEVMicroUSD < 1 || limits.MaxJEVMicroUSD > 1000000000 || limits.JEVMicroUSDPerAttempt < 1 || limits.JEVMicroUSDPerAttempt > 1000000 {
		return fmt.Errorf("explicit campaign call/separate currency limits required")
	}
	return nil
}

func OpenCampaignBudget(dir, contract string, limits CampaignLimits) (*CampaignBudget, error) {
	if dir == "" || !comparisonDigest(contract) {
		return nil, fmt.Errorf("explicit campaign contract/call/separate currency limits required")
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	c := &CampaignBudget{dir: dir, limits: limits, reserved: map[string]campaignEvent{}}
	c.lock, err = os.OpenFile(filepath.Join(dir, "campaign.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*CampaignBudget, error) { return nil, errors.Join(err, c.Close()) }
	c.file, err = os.OpenFile(filepath.Join(dir, "campaign.jsonl"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return fail(err)
	}
	s := bufio.NewScanner(c.file)
	s.Buffer(make([]byte, 4096), 1<<20)
	line := 0
	for s.Scan() {
		var e campaignEvent
		if json.Unmarshal(s.Bytes(), &e) != nil {
			return fail(fmt.Errorf("invalid campaign journal"))
		}
		if line == 0 {
			if e.Kind != "config" || e.Contract != contract || e.Limits == nil || !reflect.DeepEqual(*e.Limits, limits) {
				return fail(fmt.Errorf("campaign configuration changed"))
			}
		} else if e.Kind == "reserve" && c.pending == "" && !c.stopped {
			if err = c.applyReservation(e); err != nil {
				return fail(err)
			}
		} else if e.Kind == "finish" && e.ID == c.pending && c.pending != "" {
			c.pending = ""
			c.stopped = e.Failed
		} else {
			return fail(fmt.Errorf("campaign journal sequence differs"))
		}
		line++
	}
	if err = s.Err(); err != nil {
		return fail(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fail(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "campaign.lock" || name == "campaign.jsonl" {
			continue
		}
		e, ok := c.reserved[name]
		if !entry.IsDir() || !ok {
			return fail(fmt.Errorf("campaign evidence missing from journal"))
		}
		raw, err := os.ReadFile(filepath.Join(dir, name, "reservation.json"))
		var marker campaignEvent
		if err != nil || json.Unmarshal(raw, &marker) != nil || !reflect.DeepEqual(e, marker) {
			return fail(fmt.Errorf("campaign reservation marker differs"))
		}
	}
	for id := range c.reserved {
		if _, err = os.Stat(filepath.Join(dir, id, "reservation.json")); err != nil {
			return fail(fmt.Errorf("campaign reservation evidence missing"))
		}
	}
	if line == 0 {
		if err = c.write(campaignEvent{Kind: "config", Contract: contract, Limits: &limits}); err != nil {
			return fail(err)
		}
	}
	// An interrupted batch is never automatically retried or refunded.
	if c.pending != "" {
		c.stopped = true
	}
	return c, nil
}

func (c *CampaignBudget) applyReservation(e campaignEvent) error {
	if !campaignBatchID.MatchString(e.ID) || e.Binding == "" || e.ModelCalls < 1 || e.ModelCalls > 256 || e.ModelMicroCNY < 1 || e.JEVCalls < 1 || e.JEVCalls > 256 || e.JEVMicroUSD != int64(e.JEVCalls)*c.limits.JEVMicroUSDPerAttempt {
		return fmt.Errorf("invalid campaign reservation")
	}
	if _, ok := c.reserved[e.ID]; ok {
		return fmt.Errorf("campaign batch already reserved")
	}
	if len(c.reserved) >= c.limits.MaxBatches || c.used.ModelCalls+e.ModelCalls > c.limits.MaxModelCalls || e.ModelMicroCNY > c.limits.MaxModelMicroCNY-c.used.ModelMicroCNY || c.used.JEVCalls+e.JEVCalls > c.limits.MaxJEVCalls || e.JEVMicroUSD > c.limits.MaxJEVMicroUSD-c.used.JEVMicroUSD {
		return fmt.Errorf("campaign total budget exhausted")
	}
	c.used.ModelCalls += e.ModelCalls
	c.used.ModelMicroCNY += e.ModelMicroCNY
	c.used.JEVCalls += e.JEVCalls
	c.used.JEVMicroUSD += e.JEVMicroUSD
	c.reserved[e.ID] = e
	c.pending = e.ID
	return nil
}

func (c *CampaignBudget) write(e campaignEvent) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err = c.file.Write(append(raw, '\n')); err != nil {
		return err
	}
	return c.file.Sync()
}

func (c *CampaignBudget) ReserveBatch(id, binding string, limits PairBudgetLimits, jevCalls int) (*CampaignBatchLease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.stopped || c.pending != "" {
		return nil, fmt.Errorf("campaign stopped or unresolved batch")
	}
	estimate, err := EstimateDeclaredPairBudget(limits)
	if err != nil {
		return nil, err
	}
	e := campaignEvent{Kind: "reserve", ID: id, Binding: binding, ModelCalls: limits.MaxCalls, ModelMicroCNY: estimate.AllAttemptsMicroCNY, JEVCalls: jevCalls, JEVMicroUSD: int64(jevCalls) * c.limits.JEVMicroUSDPerAttempt}
	if err = c.applyReservation(e); err != nil {
		return nil, err
	}
	// Persist before exposing a lease or making any provider call. Any I/O
	// failure leaves this process stopped, even if the journal was partial.
	if err = c.write(e); err != nil {
		c.stopped = true
		return nil, err
	}
	dir := filepath.Join(c.dir, id)
	if err = os.Mkdir(dir, 0700); err != nil {
		c.stopped = true
		return nil, err
	}
	raw, _ := json.Marshal(e)
	file, err := os.OpenFile(filepath.Join(dir, "reservation.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		c.stopped = true
		return nil, err
	}
	_, writeErr := file.Write(raw)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		c.stopped = true
		return nil, err
	}
	return &CampaignBatchLease{campaign: c, ID: id, Dir: dir, Binding: binding, limits: limits, jevCalls: jevCalls}, nil
}

func (l *CampaignBatchLease) Finish(failed bool) error {
	return l.finish(failed, false)
}

func (l *CampaignBatchLease) finish(failed, fromRunner bool) error {
	c := l.campaign
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || l.finished || c.pending != l.ID || (l.running && !fromRunner) {
		return fmt.Errorf("campaign lease not active")
	}
	if err := c.write(campaignEvent{Kind: "finish", ID: l.ID, Failed: failed}); err != nil {
		c.stopped = true
		return err
	}
	l.finished = true
	c.pending = ""
	c.stopped = failed
	return nil
}

func (c *CampaignBudget) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	var errs []error
	if c.file != nil {
		errs = append(errs, c.file.Close())
	}
	if c.lock != nil {
		errs = append(errs, c.lock.Close(), os.Remove(filepath.Join(c.dir, "campaign.lock")))
	}
	return errors.Join(errs...)
}
