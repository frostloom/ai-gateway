package taskbench

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// A declaration and receipt are data, not provider capabilities. This first-
// window primitive deliberately has no resume, release, or advancement API.
// Later windows require a separately verified owned execution checkpoint.
type fullWindowClaimDeclaration struct {
	ScopeSHA256    string                  `json:"scope_sha256"`
	ScheduleSHA256 string                  `json:"schedule_sha256"`
	PolicySHA256   string                  `json:"policy_sha256"`
	Windows        []fullWindowDeclaration `json:"windows"`
	ModelTotal     int64                   `json:"declared_model_micro_cny"`
	JEVTotal       int64                   `json:"declared_jev_micro_usd"`
}
type fullWindowClaimReceipt struct {
	Schema           string                     `json:"schema"`
	Status           string                     `json:"status"`
	Nonce            string                     `json:"nonce"`
	Index            int                        `json:"index"`
	PreviousSHA256   string                     `json:"previous_sha256,omitempty"`
	Declaration      fullWindowClaimDeclaration `json:"declaration"`
	HistoricalClaims map[string]string          `json:"historical_claims"`
	ModelPermanent   int64                      `json:"permanent_model_micro_cny"`
	JEVPermanent     int64                      `json:"permanent_jev_micro_usd"`
	SHA256           string                     `json:"-"`
}

// claimFirstFullWindow is intentionally private and unwired. It re-reads all
// historical claims and rederives the complete source schedule before touching
// the new registry. Neither arbitrary hashes nor a receipt authorize HTTP.
func claimFirstFullWindow(root, historicalDir string, scope fullWindowScope, source ConversationSuite, profiles PairedProfiles, bindings fullWindowBindings, frozenSHA string) (fullWindowClaimReceipt, error) {
	if scope.Schema != "full-window-scope.v1" || scope.BaselineEvidenceSHA256 != "" {
		return fullWindowClaimReceipt{}, fmt.Errorf("legacy first-claim scope version rejected")
	}
	if _, e := loadFullWindowBaseline(historicalDir); e != nil {
		return fullWindowClaimReceipt{}, e
	}
	h, tot, e := validateFullWindowScope(scope, source, profiles, bindings)
	if e != nil {
		return fullWindowClaimReceipt{}, e
	}
	if !comparisonDigest(frozenSHA) || h != frozenSHA {
		return fullWindowClaimReceipt{}, fmt.Errorf("scope is not the frozen declaration")
	}
	return createFirstFullWindowClaim(root, fullWindowClaimDeclaration{ScopeSHA256: h, ScheduleSHA256: scope.ScheduleSHA256, PolicySHA256: bindings.PolicySHA256, Windows: scope.Windows, ModelTotal: tot.ModelCap, JEVTotal: tot.JEVCap})
}

func (d fullWindowClaimDeclaration) validate() error {
	for _, h := range []string{d.ScopeSHA256, d.ScheduleSHA256, d.PolicySHA256} {
		if !comparisonDigest(h) {
			return fmt.Errorf("missing claim identity")
		}
	}
	if len(d.Windows) < 37 || len(d.Windows) > 3660 || d.ModelTotal < 1 || d.ModelTotal > 70938880 || d.JEVTotal < 1 || d.JEVTotal > 776896 {
		return fmt.Errorf("declared scope exceeds remaining currencies")
	}
	var model, jev int64
	ordinal, modelCalls, jevCalls := 1, 0, 0
	for i, w := range d.Windows {
		// Check bounded remaining counts before multiplication or addition;
		// otherwise a coherently wrapped declaration can match final totals.
		if w.MaxModelCalls < 1 || w.MaxModelCalls > 58860-modelCalls || w.MaxJEVCalls < 1 || w.MaxJEVCalls > 5886-jevCalls {
			return fmt.Errorf("window attempt count exceeds fixed remaining totals")
		}
		if w.ID != fmt.Sprintf("window-%03d", i+1) || w.FirstOrdinal != ordinal || w.LastOrdinal < w.FirstOrdinal || w.LastOrdinal-w.FirstOrdinal >= 100 || w.LastOrdinal > 3660 || w.ModelCap < 1 || w.JEVCap < 1 || w.ModelCap > d.ModelTotal-model || w.JEVCap > d.JEVTotal-jev || w.MaxModelCalls < 1 || w.MaxJEVCalls < 1 || w.MaxModelCalls != 10*w.MaxJEVCalls {
			return fmt.Errorf("invalid ordered permanent window declaration")
		}
		model += w.ModelCap
		jev += w.JEVCap
		ordinal = w.LastOrdinal + 1
		modelCalls += w.MaxModelCalls
		jevCalls += w.MaxJEVCalls
	}
	if ordinal != 3661 || model != d.ModelTotal || jev != d.JEVTotal || modelCalls != 58860 || jevCalls != 5886 {
		return fmt.Errorf("incomplete scope totals")
	}
	return nil
}

func createFirstFullWindowClaim(root string, d fullWindowClaimDeclaration) (fullWindowClaimReceipt, error) {
	var out fullWindowClaimReceipt
	if e := d.validate(); e != nil {
		return out, e
	}
	if root == "" {
		return out, fmt.Errorf("owned global registry required")
	}
	if e := os.Mkdir(root, 0700); e != nil && !os.IsExist(e) {
		return out, e
	}
	info, e := os.Lstat(root)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return out, fmt.Errorf("registry must be a real directory")
	}
	lock, e := os.OpenFile(filepath.Join(root, ".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return out, e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); e != nil {
		return out, e
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	entries, e := os.ReadDir(root)
	if e != nil {
		return out, e
	}
	for _, entry := range entries {
		if entry.Name() != ".lock" {
			return out, fmt.Errorf("existing or incomplete registry requires audit; no reclaim or scope switch")
		}
	}
	nonce := make([]byte, 16)
	if _, e = rand.Read(nonce); e != nil {
		return out, e
	}
	// Detach caller-owned slices before hashing/persisting the permanent receipt.
	d.Windows = append([]fullWindowDeclaration(nil), d.Windows...)
	out = fullWindowClaimReceipt{Schema: "full-window-claim.v1", Status: "consumed_before_execution", Nonce: hex.EncodeToString(nonce), Index: 1, Declaration: d, HistoricalClaims: fullWindowCanonicalClaims(), ModelPermanent: 29061120 + d.Windows[0].ModelCap, JEVPermanent: 223104 + d.Windows[0].JEVCap}
	raw, e := json.Marshal(out)
	if e != nil {
		return fullWindowClaimReceipt{}, e
	}
	raw = append(raw, '\n')
	f, e := os.OpenFile(filepath.Join(root, "claim-000001.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return fullWindowClaimReceipt{}, e
	}
	n, writeErr := f.Write(raw)
	if writeErr == nil && n != len(raw) {
		writeErr = fmt.Errorf("short permanent claim write")
	}
	syncErr := f.Sync()
	closeErr := f.Close()
	// Once O_EXCL creates a claim, every failure retains the evidence permanently.
	if e = errors.Join(writeErr, syncErr, closeErr, windowSyncDirectory(root), windowSyncDirectory(filepath.Dir(root))); e != nil {
		return fullWindowClaimReceipt{}, e
	}
	out.SHA256 = conversationHash(raw)
	return out, nil
}
