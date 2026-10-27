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
		Schema   string   `json:"schema"`
		Status   string   `json:"status"`
		Contract Contract `json:"contract"`
		Approval Approval `json:"approval"`
	}
	if knowledgepilot.StrictDecode(pb, &pc) != nil || pc.Schema != "knowledge-independent-claim.v1" || pc.Status != "consumed_before_execution" || CheckApproval(pc.Contract, pc.Approval) != nil {
		return nil, fmt.Errorf("parent claim invalid")
	}
	var r Result
	if json.Unmarshal(rb, &r) != nil || r.Schema != "knowledge-answer100-recording.v1" || r.EvidenceKind != "real_provider" || r.Status != "stopped" || !r.Cleanup || r.Complete || r.Contract.SHA() != pc.Contract.SHA() || r.Contract.PriorRecordingSHA != "" {
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
	if scan.Err() != nil || len(events) != len(r.Events) || len(events)%2 != 0 {
		return nil, fmt.Errorf("journal incomplete")
	}
	cost := int64(0)
	calls := 0
	counted := map[string]int{}
	for i := 0; i < len(events); i += 2 {
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
	if len(skip) == 0 || len(skip) >= 200 || turns != r.ActualUserTurns {
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
