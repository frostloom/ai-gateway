package comparison

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/frostloom/ai-gateway/internal/billing/store"
)

func TestAuthBulkheadLimitsAndReleases(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup
	fn := func(ctx context.Context, hash string) (*store.APIKey, error) {
		entered <- struct{}{}
		<-release
		return &store.APIKey{ID: 1, TenantID: 1, KeyHash: hash}, nil
	}
	lim := newAuthBulkhead(2, fn)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := lim.validate(context.Background(), "a"); e != nil {
				t.Error(e)
			}
		}()
	}
	<-entered
	<-entered
	if _, e := lim.validate(context.Background(), "b"); !errors.Is(e, errAuthBusy) {
		t.Fatalf("expected explicit overload: %v", e)
	}
	close(release)
	wg.Wait()
	if _, e := lim.validate(context.Background(), "c"); e != nil {
		t.Fatalf("permit not released: %v", e)
	}
}

func TestAuthBulkheadWaitsAndHonorsCancellation(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	fn := func(ctx context.Context, hash string) (*store.APIKey, error) {
		if hash == "first" {
			entered <- struct{}{}
			<-release
		}
		return &store.APIKey{ID: 1, TenantID: 1, KeyHash: hash}, nil
	}
	lim := newWaitingAuthBulkhead(1, 200*time.Millisecond, fn)
	firstDone := make(chan error, 1)
	go func() { _, e := lim.validate(context.Background(), "first"); firstDone <- e }()
	<-entered
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := lim.validate(canceled, "cancel"); !errors.Is(e, context.Canceled) {
		t.Fatalf("cancellation: %v", e)
	}
	secondDone := make(chan error, 1)
	go func() { _, e := lim.validate(context.Background(), "second"); secondDone <- e }()
	close(release)
	if e := <-firstDone; e != nil {
		t.Fatal(e)
	}
	if e := <-secondDone; e != nil {
		t.Fatalf("waiting request failed: %v", e)
	}
}

func TestAuthBulkheadWaitExpires(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	lim := newWaitingAuthBulkhead(1, 25*time.Millisecond, func(context.Context, string) (*store.APIKey, error) {
		close(entered)
		<-release
		return &store.APIKey{ID: 1}, nil
	})
	done := make(chan struct{})
	go func() { defer close(done); _, _ = lim.validate(context.Background(), "first") }()
	<-entered
	if _, e := lim.validate(context.Background(), "second"); !errors.Is(e, errAuthBusy) {
		t.Fatalf("expected bounded wait rejection: %v", e)
	}
	close(release)
	<-done
}

func TestAuthBulkheadBoundedQueue(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	fn := func(ctx context.Context, hash string) (*store.APIKey, error) {
		if hash == "active" {
			close(entered)
			<-release
		}
		return &store.APIKey{ID: 1}, nil
	}
	lim := newBoundedAuthBulkhead(1, 1, time.Second, fn)
	active := make(chan error, 1)
	go func() { _, e := lim.validate(context.Background(), "active"); active <- e }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() { _, e := lim.validate(ctx, "waiting"); waiting <- e }()
	end := time.Now().Add(time.Second)
	for len(lim.queued) != 1 && time.Now().Before(end) {
		time.Sleep(time.Millisecond)
	}
	if len(lim.queued) != 1 {
		t.Fatal("waiting request did not enter queue")
	}
	if _, e := lim.validate(context.Background(), "excess"); !errors.Is(e, errAuthBusy) {
		t.Fatalf("queue must reject excess: %v", e)
	}
	cancel()
	if e := <-waiting; !errors.Is(e, context.Canceled) {
		t.Fatalf("queued cancel: %v", e)
	}
	if len(lim.queued) != 0 {
		t.Fatal("queue permit was not released")
	}
	close(release)
	if e := <-active; e != nil {
		t.Fatal(e)
	}
}

func TestAuthBulkheadQueueSlotReleasedOnAdmission(t *testing.T) {
	activeStarted, activeRelease := make(chan struct{}), make(chan struct{})
	queuedStarted, queuedRelease := make(chan struct{}), make(chan struct{})
	fn := func(ctx context.Context, hash string) (*store.APIKey, error) {
		if hash == "active" {
			close(activeStarted)
			<-activeRelease
		} else {
			close(queuedStarted)
			<-queuedRelease
		}
		return &store.APIKey{ID: 1}, nil
	}
	lim := newBoundedAuthBulkhead(1, 1, time.Second, fn)
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { _, e := lim.validate(context.Background(), "active"); first <- e }()
	<-activeStarted
	go func() { _, e := lim.validate(context.Background(), "queued"); second <- e }()
	end := time.Now().Add(time.Second)
	for len(lim.queued) != 1 && time.Now().Before(end) {
		time.Sleep(time.Millisecond)
	}
	if len(lim.queued) != 1 {
		close(activeRelease)
		close(queuedRelease)
		<-first
		<-second
		t.Fatal("request was not queued")
	}
	close(activeRelease)
	<-queuedStarted
	if len(lim.queued) != 0 {
		close(queuedRelease)
		<-first
		<-second
		t.Fatal("admitted request retained queue slot")
	}
	close(queuedRelease)
	if e := <-first; e != nil {
		t.Fatal(e)
	}
	if e := <-second; e != nil {
		t.Fatal(e)
	}
}
