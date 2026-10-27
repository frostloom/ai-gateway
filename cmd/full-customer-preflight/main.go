// full-customer-preflight prepares all source tasks offline. It has no paid
// mode, credential discovery, fixture environment or provider transport.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"

	"github.com/frostloom/ai-gateway/internal/taskbench"
)

type preflightRow struct {
	Key                   string `json:"key"`
	SourceID              string `json:"source_case_id"`
	Profile               string `json:"profile"`
	Scenario              string `json:"scenario"`
	Turns                 int    `json:"turns"`
	PairRunContract       string `json:"pair_run_contract"`
	ConfigurationPrepared bool   `json:"configuration_prepared"`
	PilotWhitelistMember  bool   `json:"pilot_whitelist_member"`
}

type preflightReport struct {
	Schema                     string                                 `json:"schema"`
	SourceCases                int                                    `json:"source_cases"`
	PairedArms                 int                                    `json:"paired_arms"`
	PlannedUserTurns           int                                    `json:"planned_user_turns"`
	BoundaryCount              int                                    `json:"boundary_count"`
	DefinitionSHA256           string                                 `json:"definition_sha256"`
	ManifestSHA256             string                                 `json:"manifest_sha256"`
	ProfileSHA256              string                                 `json:"profile_sha256"`
	SourceRevision             string                                 `json:"source_revision"`
	PreparedRuntime            taskbench.ConversationComparisonConfig `json:"prepared_runtime"`
	PilotWhitelistPrimaryCount int                                    `json:"pilot_whitelist_primary_count"`
	Rows                       []preflightRow                         `json:"rows"`
	AllowPaid                  bool                                   `json:"allow_paid"`
	ReadyForPaidRun            bool                                   `json:"ready_for_paid_run"`
	ModelCalls                 int                                    `json:"model_calls"`
	JEVCalls                   int                                    `json:"jev_calls"`
	Limitations                []string                               `json:"limitations"`
}

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	f := flag.NewFlagSet("full-customer-preflight", flag.ContinueOnError)
	definitions := f.String("definitions", "eval/task-definitions/2026-10-12-full-dev-v1", "frozen definitions")
	profilesDir := f.String("profiles", "eval/runtime-profiles/2026-10-13-six-profiles-v1", "pinned runtime profiles")
	revision := f.String("revision", "", "explicit source revision; code provenance must be archived separately")
	out := f.String("out", "", "new offline JSON path")
	if f.Parse(args) != nil || f.NArg() != 0 || *out == "" || !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(*revision) {
		return 2
	}
	if _, err := os.Lstat(*out); !os.IsNotExist(err) {
		return 2
	}
	source, err := taskbench.LoadConversations(*definitions)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	full, err := taskbench.PrepareFullCustomerPair(source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	profiles, err := taskbench.LoadPairedProfiles(*profilesDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	small, err := taskbench.PrepareSmallPair(source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	whitelist := map[string]bool{}
	for _, c := range small.Suite.Cases {
		whitelist[c.Key()] = true
	}
	p := preflightReport{Schema: "full-customer-offline-preflight.v1", SourceCases: len(full.Suite.Cases), PairedArms: 2 * len(full.Suite.Cases), BoundaryCount: len(full.BoundaryKeys), DefinitionSHA256: source.DefinitionSHA256, ManifestSHA256: source.ManifestSHA256, ProfileSHA256: profiles.SHA256, SourceRevision: *revision, Limitations: []string{
		"Per-case configuration preparation only; no actual Handler, fixture, provider or semantic evaluation.",
		"Existing RunAgentPair still restricts execution to the historical 12-case pilot whitelist; this CLI does not widen it.",
		"Primary development definitions are not independently blind gold; review and full-set request envelope remain unverified.",
		"The source revision is an operator declaration, not proof of the compiled source; retain source hashes separately.",
	}}
	for _, c := range full.Suite.Cases {
		suite := full.Suite
		suite.Cases = []taskbench.ConversationCase{c}
		cfg, err := taskbench.PrepareAgentPairConfig(suite, profiles, taskbench.PairModelSettings{BaseURL: "https://api.deepseek.com/v1", Model: "deepseek-flash", Protocol: "openai"}, "recorded_run", *revision, "jev-1.13.0")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		p.PreparedRuntime = cfg.Model
		p.Rows = append(p.Rows, preflightRow{c.Key(), c.SourceID, c.Scenario.Profile, c.Scenario.ID, len(c.Scenario.Turns), taskbench.PairRunContract(cfg), true, whitelist[c.Key()]})
		p.PlannedUserTurns += 2 * len(c.Scenario.Turns)
		if whitelist[c.Key()] {
			p.PilotWhitelistPrimaryCount++
		}
	}
	// The per-case profile set varies. Do not represent the last case's profile
	// subset as a complete-set runtime configuration.
	p.PreparedRuntime.ProfileSHA256 = nil
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return 2
	}
	file, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 2
	}
	_, writeErr := file.Write(append(raw, '\n'))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return 2
	}
	fmt.Printf("Offline configuration: %d sources, %d arms, %d defined turns; historical pilot whitelist covers %d primary tasks; paid readiness false.\n", p.SourceCases, p.PairedArms, p.PlannedUserTurns, p.PilotWhitelistPrimaryCount)
	return 0
}
