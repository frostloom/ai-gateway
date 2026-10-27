package knowledgecompare

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/knowledgepilot"
	"os"
)

// AcquireCompletion is a separately authorized continuation of the SAME parent
// allocation. It retains the original permanent claim and all settled charges.
// Caller supplies the fixed common-dir child path derived from prior SHA.
func AcquireCompletion(path string, c Contract, a Approval, parent, prior, journal string) (*Capability, error) {
	if e := CheckApproval(c, a); e != nil {
		return nil, e
	}
	if c.PriorRecordingSHA == "" || c.MaxMicroCNY != 20000000 {
		return nil, fmt.Errorf("completion binding missing")
	}
	read := func(p, want string) ([]byte, error) {
		b, e := os.ReadFile(p)
		if e != nil || knowledgepilot.SHA(b) != want {
			return nil, fmt.Errorf("prior evidence digest differs")
		}
		return b, nil
	}
	pb, e := read(parent, c.ParentClaimSHA)
	if e != nil {
		return nil, e
	}
	rb, e := read(prior, c.PriorRecordingSHA)
	if e != nil {
		return nil, e
	}
	jb, e := read(journal, c.PriorJournalSHA)
	if e != nil {
		return nil, e
	}
	var pc struct {
		Schema         string          `json:"schema"`
		Status         string          `json:"status"`
		Contract       Contract        `json:"contract"`
		Approval       Approval        `json:"approval"`
		InheritedCalls int             `json:"inherited_calls,omitempty"`
		InheritedUpper int64           `json:"inherited_upper_micro_cny,omitempty"`
		Completed      map[string]bool `json:"completed_slots,omitempty"`
	}
	if knowledgepilot.StrictDecode(pb, &pc) != nil || (pc.Schema != "knowledge-independent-claim.v1" && pc.Schema != "knowledge-completion-claim.v1") || pc.Status != "consumed_before_execution" || CheckApproval(pc.Contract, pc.Approval) != nil {
		return nil, fmt.Errorf("parent claim invalid")
	}
	var r Result
	if json.Unmarshal(rb, &r) != nil || r.Schema != "knowledge-answer100-recording.v1" || r.EvidenceKind != "real_provider" || r.Status != "stopped" || !r.Cleanup || r.Complete || r.Contract.SHA() != pc.Contract.SHA() {
		return nil, fmt.Errorf("closed original segment required")
	}
	old := r.Contract
	if old.SuiteSHA != c.SuiteSHA || old.CorpusSHA != c.CorpusSHA || old.MainSHA != c.MainSHA || old.PriceSHA != c.PriceSHA || old.MaxMicroCNY != c.MaxMicroCNY || old.MaxCalls != c.MaxCalls || old.MaxBytes != c.MaxBytes || old.MaxOutput != c.MaxOutput || old.InputCeiling != c.InputCeiling || old.InputRate != c.InputRate || old.OutputRate != c.OutputRate || old.FundingPool != c.FundingPool {
		return nil, fmt.Errorf("parent protocol/allocation differs")
	}
	var events []Event
	scan := bufio.NewScanner(bytes.NewReader(jb))
	scan.Buffer(make([]byte, 4096), 8<<20)
	for scan.Scan() {
		var e Event
		if json.Unmarshal(scan.Bytes(), &e) != nil {
			return nil, fmt.Errorf("invalid journal")
		}
		events = append(events, e)
	}
	if scan.Err() != nil || len(events) != len(r.Events) || false {
		return nil, fmt.Errorf("journal incomplete")
	}
	cost := pc.InheritedUpper
	calls := pc.InheritedCalls
	if calls < 0 || cost < 0 || calls >= c.MaxCalls || cost >= c.MaxMicroCNY {
		return nil, fmt.Errorf("invalid inherited allocation")
	}
	if pc.Schema == "knowledge-independent-claim.v1" && (calls != 0 || cost != 0 || len(pc.Completed) > 0) {
		return nil, fmt.Errorf("original allocation has invalid inheritance")
	}
	for k := range events {
		a, _ := json.Marshal(events[k])
		b, _ := json.Marshal(r.Events[k])
		if !bytes.Equal(a, b) {
			return nil, fmt.Errorf("recording/journal differs")
		}
	}
	counted := map[string]int{}
	for i := 0; i < len(events); i += 2 {
		if events[i].Kind == "external_halt" && (events[i].Failure == "observation_flush" || events[i].Failure == "observation_counters") && i == len(events)-1 {
			break
		}
		if i+1 >= len(events) {
			return nil, fmt.Errorf("unsettled journal tail")
		}
		intent, usage := events[i], events[i+1]
		if intent.Kind != "intent" || usage.Kind != "usage" || intent.Sequence != calls+1 || usage.Sequence != intent.Sequence || intent.Task != usage.Task || intent.Turn != usage.Turn || intent.RequestSHA != knowledgepilot.SHA(intent.Request) {
			return nil, fmt.Errorf("journal pairing differs")
		}
		for k := i; k < i+2; k++ {
			a, _ := json.Marshal(events[k])
			b, _ := json.Marshal(r.Events[k])
			if !bytes.Equal(a, b) {
				return nil, fmt.Errorf("recording/journal differs")
			}
		}
		in, out, hit, miss, e := Usage(usage.Response)
		if e != nil || in != usage.Input || out != usage.Output || hit != usage.CacheHit || miss != usage.CacheMiss || usage.UpperCost != price(in, out, old) {
			return nil, fmt.Errorf("unsettled native usage")
		}
		cost += usage.UpperCost
		calls++
		counted[intent.Task]++
	}
	if r.Budget.Hold != 0 || r.Budget.UnknownCharge || r.Budget.Calls != calls || r.Budget.KnownUpper != cost || cost+price(c.InputCeiling, int64(c.MaxOutput), c) > c.MaxMicroCNY || calls >= c.MaxCalls {
		return nil, fmt.Errorf("parent accounting incomplete")
	}
	cases := map[string]Case{}
	for _, v := range r.Suite.Cases {
		cases[v.ID] = v
	}
	skip := map[string]bool{}
	turns := 0
	for id, done := range pc.Completed {
		if !done || len(id) < 3 {
			return nil, fmt.Errorf("invalid prior slot")
		}
		def, ok := cases[id[:len(id)-2]]
		arm := id[len(id)-1:]
		if !ok || (arm != "a" && arm != "b") {
			return nil, fmt.Errorf("unknown prior slot")
		}
		skip[id] = true
		turns += len(def.Turns)
	}
	if len(skip) != r.PriorTasks || turns != r.PriorUserTurns {
		return nil, fmt.Errorf("ancestral coverage differs")
	}
	for _, t := range r.Tasks {
		id := t.CaseID + "-" + t.Arm
		def, ok := cases[t.CaseID]
		if !ok || (t.Arm != "a" && t.Arm != "b") || skip[id] || !t.BusinessUnchanged || !t.OwnedCleanup || len(t.Turns) != len(def.Turns) || t.TerminatedReason != "" {
			return nil, fmt.Errorf("prior task disposition incomplete")
		}
		for i, v := range t.Turns {
			if v.Index != i+1 || v.Question != def.Turns[i] || v.HTTPStatus != 200 {
				return nil, fmt.Errorf("prior turn incomplete")
			}
		}
		skip[id] = true
		turns += len(t.Turns)
	}
	for id := range counted {
		if !skip[id] {
			return nil, fmt.Errorf("unowned prior attempt")
		}
	}
	if len(skip) == 0 || len(skip) >= 200 || turns != r.ActualUserTurns+r.PriorUserTurns {
		return nil, fmt.Errorf("prior coverage differs")
	}
	claim := struct {
		Schema         string          `json:"schema"`
		Status         string          `json:"status"`
		Contract       Contract        `json:"contract"`
		Approval       Approval        `json:"approval"`
		InheritedCalls int             `json:"inherited_calls"`
		InheritedUpper int64           `json:"inherited_upper_micro_cny"`
		Completed      map[string]bool `json:"completed_slots"`
	}{"knowledge-completion-claim.v1", "consumed_before_execution", c, a, calls, cost, skip}
	b, e := json.Marshal(claim)
	if e != nil {
		return nil, e
	}
	if e = writeNew(path, append(b, '\n')); e != nil {
		return nil, e
	}
	return &Capability{c: c, skip: skip, inherited: GuardState{Calls: calls, KnownUpper: cost}, priorTurns: turns}, nil
}
