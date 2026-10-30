package taskbench

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowCampaignContextCanceledAdmission(t *testing.T) {
	for _, step := range []string{"ledger", "advance", "claim", "write"} {
		t.Run(step, func(t *testing.T) {
			p, b := fixtureWindowCampaign(t)
			c, err := startWindowCampaign(p, b)
			if err != nil {
				t.Fatal(err)
			}
			defer c.closeWindowCampaign()
			beforeModel, beforeJEV := c.model, c.jev
			var token *windowCampaignOwnedCheckpoint
			if step == "advance" {
				token = liveFixtureWindowCampaign(t, c)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			switch step {
			case "ledger":
				_, err = c.openWindowCampaignLedgerContext(ctx)
			case "advance":
				_, err = c.advanceWindowCampaignContext(ctx, token)
			case "claim":
				err = c.persistClaimWindowCampaignContext(ctx, 1, c.claimSHA, "")
			case "write":
				err = c.persistWindowCampaignContext(ctx, "unadmitted.json", struct{}{})
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if c.model != beforeModel || c.jev != beforeJEV {
				t.Fatal("pre-admission cancellation charged cap")
			}
			for _, name := range []string{"claim-000002.json", "checkpoint-000001.json", "unadmitted.json"} {
				if _, err := os.Lstat(filepath.Join(c.root, name)); !os.IsNotExist(err) {
					t.Fatalf("canceled admission wrote %s", name)
				}
			}
		})
	}
}

func TestWindowCampaignBoundContextCannotBeBroadened(t *testing.T) {
	for _, wrapper := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit-background", true: "legacy-wrapper"}[wrapper], func(t *testing.T) {
			p, b := fixtureWindowCampaign(t)
			c, err := startWindowCampaign(p, b)
			if err != nil {
				t.Fatal(err)
			}
			defer c.closeWindowCampaign()
			ctx, cancel := context.WithCancel(context.Background())
			if _, err = c.openWindowCampaignLedgerContext(ctx); err != nil {
				t.Fatal(err)
			}
			token := &windowCampaignOwnedCheckpoint{owner: c, ledger: c.ledger, index: c.index}
			cancel()
			if wrapper {
				_, err = c.advanceWindowCampaign(token)
			} else {
				_, err = c.advanceWindowCampaignContext(context.Background(), token)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("bound cancellation lost: %v", err)
			}
			if _, err = os.Lstat(filepath.Join(c.root, "claim-000002.json")); !os.IsNotExist(err) {
				t.Fatal("next claim written")
			}
		})
	}
}

type cancelWindowCampaignJournal struct {
	*os.File
	cancel                context.CancelFunc
	at                    string
	writes, syncs, closes *int
}

func (f *cancelWindowCampaignJournal) Write(b []byte) (int, error) {
	*f.writes++
	if f.at == "write" {
		f.cancel()
	}
	return f.File.Write(b)
}
func (f *cancelWindowCampaignJournal) Sync() error {
	*f.syncs++
	if f.at == "sync" {
		f.cancel()
	}
	return f.File.Sync()
}
func (f *cancelWindowCampaignJournal) Close() error { *f.closes++; return f.File.Close() }

func TestWindowCampaignContextDurabilityCancellationRetainsCap(t *testing.T) {
	for _, at := range []string{"open", "write", "sync"} {
		t.Run(at, func(t *testing.T) {
			p, b := fixtureWindowCampaign(t)
			c, err := startWindowCampaign(p, b)
			if err != nil {
				t.Fatal(err)
			}
			defer c.closeWindowCampaign()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			writes, syncs, closes := 0, 0, 0
			c.openFile = func(path string) (windowJournalFile, error) {
				f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err != nil {
					return nil, err
				}
				if strings.Contains(path, "claim-000002") {
					if at == "open" {
						cancel()
					}
					return &cancelWindowCampaignJournal{f, cancel, at, &writes, &syncs, &closes}, nil
				}
				return f, nil
			}
			beforeModel, beforeJEV := c.model, c.jev
			err = c.persistClaimWindowCampaignContext(ctx, 1, c.claimSHA, "")
			if !errors.Is(err, context.Canceled) || !c.poisoned {
				t.Fatalf("canceled claim succeeded: %v poisoned=%v", err, c.poisoned)
			}
			if c.model != beforeModel+10 || c.jev != beforeJEV+1 {
				t.Fatal("attempted cap released")
			}
			name := "claim-000002.json"
			raw, err := os.ReadFile(filepath.Join(c.root, name))
			if err != nil || len(raw) == 0 || c.files[name] != conversationHash(raw) {
				t.Fatal("durable evidence lost")
			}
			if writes != 1 || syncs != 1 || closes != 1 || c.index != 1 {
				t.Fatalf("durability/index: %d/%d/%d index%d", writes, syncs, closes, c.index)
			}
		})
	}
}

func TestWindowCampaignContextCheckpointCancellationStopsNextClaim(t *testing.T) {
	p, b := fixtureWindowCampaign(t)
	c, err := startWindowCampaign(p, b)
	if err != nil {
		t.Fatal(err)
	}
	defer c.closeWindowCampaign()
	token := liveFixtureWindowCampaign(t, c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writes, syncs, closes := 0, 0, 0
	c.openFile = func(path string) (windowJournalFile, error) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return nil, err
		}
		return &cancelWindowCampaignJournal{f, cancel, "sync", &writes, &syncs, &closes}, nil
	}
	beforeModel, beforeJEV := c.model, c.jev
	sha, err := c.advanceWindowCampaignContext(ctx, token)
	if !errors.Is(err, context.Canceled) || sha != "" || !c.poisoned {
		t.Fatalf("late checkpoint advanced: %s %v", sha, err)
	}
	if c.model != beforeModel || c.jev != beforeJEV {
		t.Fatal("next cap attempted after checkpoint cancellation")
	}
	name := "checkpoint-000001.json"
	raw, err := os.ReadFile(filepath.Join(c.root, name))
	if err != nil || c.files[name] != conversationHash(raw) || writes != 1 || syncs != 1 || closes != 1 {
		t.Fatal("checkpoint durability evidence lost")
	}
	if _, err := os.Lstat(filepath.Join(c.root, "claim-000002.json")); !os.IsNotExist(err) {
		t.Fatal("next claim written")
	}
}

func TestWindowCampaignContextCancellationWhileWaitingForMutex(t *testing.T) {
	p, b := fixtureWindowCampaign(t)
	c, err := startWindowCampaign(p, b)
	if err != nil {
		t.Fatal(err)
	}
	defer c.closeWindowCampaign()
	token := liveFixtureWindowCampaign(t, c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.mu.Lock()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() { close(started); _, err := c.advanceWindowCampaignContext(ctx, token); done <- err }()
	<-started
	cancel()
	c.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("wait lost cancellation: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(c.root, "checkpoint-000001.json")); !os.IsNotExist(err) {
		t.Fatal("checkpoint written after canceled wait")
	}
}

func TestWindowCampaignContextNextLedgerGetsItsOwnOperation(t *testing.T) {
	p, b := fixtureWindowCampaign(t)
	c, err := startWindowCampaign(p, b)
	if err != nil {
		t.Fatal(err)
	}
	defer c.closeWindowCampaign()
	token := liveFixtureWindowCampaign(t, c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// This fixture token already owns the open ledger; bind its operation exactly
	// as the private ledger-open boundary does, without creating a second ledger.
	c.acquisitionContext = ctx
	c.acquisitionContextIndex = c.index
	if _, err = c.advanceWindowCampaignContext(ctx, token); err != nil {
		t.Fatal(err)
	}
	cancel()
	next, nextCancel := context.WithCancel(context.Background())
	defer nextCancel()
	if _, err = c.openWindowCampaignLedgerContext(next); err != nil {
		t.Fatalf("previous window cancellation leaked: %v", err)
	}
	if c.acquisitionContext != next || c.acquisitionContextIndex != 2 {
		t.Fatal("next operation was not bound")
	}
	nextCancel()
	if err = c.persistClaimWindowCampaignContext(context.Background(), 2, c.claimSHA, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("next cancellation bypassed: %v", err)
	}
}

func TestWindowCampaignContextHistoryKeepsBothCancellations(t *testing.T) {
	bound, boundCancel := context.WithCancel(context.Background())
	defer boundCancel()
	c := &windowCampaign{acquisitionContext: bound}
	joined, done := c.windowCampaignHistoryContextLocked(context.Background())
	defer done()
	boundCancel()
	<-joined.Done()
	if !errors.Is(joined.Err(), context.Canceled) {
		t.Fatal("bound operation cancellation lost")
	}
	bound2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	supplied, cancelSupplied := context.WithCancel(context.Background())
	defer cancelSupplied()
	c.acquisitionContext = bound2
	joined2, done2 := c.windowCampaignHistoryContextLocked(supplied)
	defer done2()
	cancelSupplied()
	<-joined2.Done()
	if !errors.Is(joined2.Err(), context.Canceled) || bound2.Err() != nil {
		t.Fatal("supplied cancellation lost or canceled its parent")
	}
}
