package observability

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDurableRejectsReuseAliasesAndInvalidConfig(t *testing.T) {
	for _, kind := range []string{"existing", "symlink_parent", "relative", "dotdot", "bad_run", "over_cap", "zero_cap"} {
		t.Run(kind, func(t *testing.T) {
			c := spoolConfig(t)
			switch kind {
			case "existing":
				if err := os.Mkdir(c.Root, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink_parent":
				p := filepath.Dir(c.Root)
				alias := p + "-alias"
				if err := os.Symlink(p, alias); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Remove(alias) })
				c.Root = filepath.Join(alias, "spool")
			case "relative":
				c.Root = "relative"
			case "dotdot":
				c.Root = filepath.Dir(c.Root) + "/x/../spool"
			case "bad_run":
				c.RunID = strings.Repeat("A", 64)
			case "over_cap":
				c.MaxBytes = (4 << 30) + 1
			case "zero_cap":
				c.MaxBytes = 0
			}
			s := newDurableSpoolClient(c)
			if err := s.Start(context.Background()); err == nil {
				_ = s.Stop(context.Background())
				t.Fatal("unsafe root/config accepted")
			}
		})
	}
}
func TestDurableRejectsFilesystemInterference(t *testing.T) {
	for _, kind := range []string{"foreign", "hardlink", "symlink", "root_replaced", "lock_replaced", "body_changed"} {
		t.Run(kind, func(t *testing.T) {
			s, c := startSpool(t)
			ctx := context.Background()
			if err := s.UploadTraces(ctx, spoolSpans(3)); err != nil {
				t.Fatal(err)
			}
			body := filepath.Join(c.Root, "00000000000000000001.pb")
			switch kind {
			case "foreign":
				if err := os.WriteFile(filepath.Join(c.Root, "foreign"), []byte("foreign"), 0600); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(body, filepath.Join(filepath.Dir(c.Root), "linked")); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(body, body+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(body+"-old", body); err != nil {
					t.Fatal(err)
				}
			case "root_replaced":
				if err := os.Rename(c.Root, c.Root+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(c.Root, 0700); err != nil {
					t.Fatal(err)
				}
			case "lock_replaced":
				p := filepath.Join(c.Root, "lock")
				if err := os.Rename(p, p+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("fake"), 0600); err != nil {
					t.Fatal(err)
				}
			case "body_changed":
				if err := os.WriteFile(body, []byte("bad"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.status(); err == nil {
				t.Fatal("live receipt accepted interference")
			}
			if err := s.Stop(ctx); err == nil {
				t.Fatal("interference cleanly closed")
			}
		})
	}
}
func TestDurableFaultsPoisonAndRetain(t *testing.T) {
	for _, kind := range []string{"short_write", "write", "file_sync", "dir_sync", "close", "cancel_after_write"} {
		t.Run(kind, func(t *testing.T) {
			s, c := startSpool(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			orig := s.io
			switch kind {
			case "short_write":
				s.io.write = func(f *os.File, b []byte) (int, error) { return orig.write(f, b[:len(b)/2]) }
			case "write":
				s.io.write = func(f *os.File, b []byte) (int, error) {
					n, _ := orig.write(f, b[:len(b)/2])
					return n, errors.New("injected write")
				}
			case "file_sync":
				s.io.sync = func(f *os.File) error {
					if fi, _ := f.Stat(); !fi.IsDir() {
						return errors.New("injected sync")
					}
					return orig.sync(f)
				}
			case "dir_sync":
				s.io.sync = func(f *os.File) error {
					if fi, _ := f.Stat(); fi.IsDir() {
						return errors.New("injected dir sync")
					}
					return orig.sync(f)
				}
			case "close":
				s.io.close = func(f *os.File) error { _ = orig.close(f); return errors.New("injected close") }
			case "cancel_after_write":
				s.io.write = func(f *os.File, b []byte) (int, error) { n, e := orig.write(f, b); cancel(); return n, e }
			}
			if err := s.UploadTraces(ctx, spoolSpans(3)); err == nil {
				t.Fatal("fault acknowledged")
			}
			status, err := s.status()
			if err == nil || !status.Poisoned || status.Batches != 0 {
				t.Fatalf("fault not retained %+v %v", status, err)
			}
			if _, err = os.Stat(filepath.Join(c.Root, "00000000000000000001.pb")); err != nil {
				t.Fatal("original partial evidence removed", err)
			}
			s.io = orig
			if err = s.Stop(context.Background()); err == nil {
				t.Fatal("poison closed successfully")
			}
			if _, err = VerifyClosedDurableSpool(context.Background(), c); err == nil {
				t.Fatal("incomplete spool verified")
			}
		})
	}
}
func TestDurableCapacityAndCloseFailure(t *testing.T) {
	t.Run("capacity", func(t *testing.T) {
		c := spoolConfig(t)
		c.MaxBytes = 4096
		s := newDurableSpoolClient(c)
		if err := s.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer s.Stop(context.Background())
		spans := spoolSpans(3)
		spans[0].ScopeSpans[0].Spans[0].Name = strings.Repeat("x", 4096)
		if err := s.UploadTraces(context.Background(), spans); err == nil {
			t.Fatal("capacity exceeded")
		}
	})
	t.Run("close_sync", func(t *testing.T) {
		s, _ := startSpool(t)
		if err := s.UploadTraces(context.Background(), spoolSpans(3)); err != nil {
			t.Fatal(err)
		}
		s.io.sync = func(*os.File) error { return errors.New("close sync fault") }
		if err := s.Stop(context.Background()); err == nil {
			t.Fatal("failed close success")
		}
		st, err := s.status()
		if err == nil || !st.Poisoned || st.Closed {
			t.Fatalf("close receipt %+v %v", st, err)
		}
	})
}

// Moving the body recheck ahead of the commit lets a changed original receive
// a success receipt. The injected boundary keeps real files and writes intact.
func TestDurableRechecksBodyAfterCommit(t *testing.T) {
	s, c := startSpool(t)
	orig := s.io
	changed := false
	s.io.write = func(f *os.File, b []byte) (int, error) {
		n, e := orig.write(f, b)
		if strings.HasSuffix(f.Name(), ".commit") && !changed {
			changed = true
			if er := os.WriteFile(filepath.Join(c.Root, "00000000000000000001.pb"), []byte("changed during commit"), 0600); er != nil {
				t.Fatal(er)
			}
		}
		return n, e
	}
	if err := s.UploadTraces(context.Background(), spoolSpans(3)); err == nil {
		t.Fatal("changed original received success")
	}
}
func TestDurableRejectsZeroParentIdentity(t *testing.T) {
	s, _ := startSpool(t)
	a := spoolSpans(3)
	a[0].ScopeSpans[0].Spans[0].ParentSpanId = make([]byte, 8)
	if err := s.UploadTraces(context.Background(), a); err == nil {
		t.Fatal("zero parent identity accepted")
	}
}
func TestDurableCloseFailureReachesObserver(t *testing.T) {
	ctx := context.Background()
	c := enabled("https://unused.invalid")
	dc := spoolConfig(t)
	c.DurableSpool = &dc
	o, e := New(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	_, turn := o.StartTurn(ctx, "model")
	turn.End(200, Summary{})
	if e = o.ForceFlush(ctx); e != nil {
		t.Fatal(e)
	}
	original := o.durable.io
	o.durable.io.sync = func(*os.File) error { return errors.New("final fsync failed") }
	if e = o.Shutdown(ctx); e == nil {
		t.Fatal("shutdown swallowed durability failure")
	}
	o.durable.io = original
	if e = o.CheckDurableReceipt(ctx); e == nil {
		t.Fatal("poison receipt accepted")
	}
}
func TestDurableCanceledProcessorCannotCloseSuccessfully(t *testing.T) {
	ctx := context.Background()
	c := enabled("https://unused.invalid")
	dc := spoolConfig(t)
	c.DurableSpool = &dc
	o, e := New(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	o.processor.cancel()
	<-o.processor.done
	st, e := o.DurableStatus()
	if e == nil || !st.Poisoned || st.Closed {
		t.Fatalf("canceled processor closed successfully %+v %v", st, e)
	}
}

func TestDurableClosedReaderRejectsTampering(t *testing.T) {
	for _, kind := range []string{"old_body", "foreign", "hardlink", "header", "close", "commit", "root_alias", "wrong_run", "wrong_cap", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			s, c := startSpool(t)
			ctx := context.Background()
			if e := s.UploadTraces(ctx, spoolSpans(3)); e != nil {
				t.Fatal(e)
			}
			if e := s.UploadTraces(ctx, spoolSpans(4)); e != nil {
				t.Fatal(e)
			}
			if e := s.Stop(ctx); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "old_body":
				p := filepath.Join(c.Root, "00000000000000000001.pb")
				raw, e := os.ReadFile(p)
				if e != nil {
					t.Fatal(e)
				}
				raw[len(raw)-1] ^= 1
				if e = os.WriteFile(p, raw, 0600); e != nil {
					t.Fatal(e)
				}
			case "foreign":
				if e := os.WriteFile(filepath.Join(c.Root, "unowned"), nil, 0600); e != nil {
					t.Fatal(e)
				}
			case "hardlink":
				if e := os.Link(filepath.Join(c.Root, "00000000000000000001.pb"), filepath.Join(filepath.Dir(c.Root), "extra")); e != nil {
					t.Fatal(e)
				}
			case "header", "close", "commit":
				name := map[string]string{"header": "header.json", "close": "closed.json", "commit": "00000000000000000001.commit"}[kind]
				if e := os.WriteFile(filepath.Join(c.Root, name), []byte(`{"fake":true}`), 0600); e != nil {
					t.Fatal(e)
				}
			case "root_alias":
				alias := c.Root + "-alias"
				if e := os.Symlink(c.Root, alias); e != nil {
					t.Fatal(e)
				}
				c.Root = alias
			case "wrong_run":
				c.RunID = strings.Repeat("b", 64)
			case "wrong_cap":
				c.MaxBytes--
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, e := VerifyClosedDurableSpool(ctx, c); e == nil {
				t.Fatal("tampered closed data accepted")
			}
		})
	}
}
func TestDurableCandidateCloseIsOnlyData(t *testing.T) {
	s, c := startSpool(t)
	ctx := context.Background()
	if e := s.UploadTraces(ctx, spoolSpans(3)); e != nil {
		t.Fatal(e)
	}
	original := s.io
	s.io.close = func(f *os.File) error {
		e := original.close(f)
		if f.Name() == "closed.json" {
			return errors.New("close returned failure after bytes persisted")
		}
		return e
	}
	if e := s.Stop(ctx); e == nil {
		t.Fatal("candidate close returned success")
	}
	status, e := s.status()
	if e == nil || status.Closed || !status.Poisoned {
		t.Fatal("poison lost")
	}
	proof, e := VerifyClosedDurableSpool(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	sum := proof.Summary()
	if !sum.DataOnly || !sum.DurabilityReturnUnproven || !sum.OwnerCompletionUnproven {
		t.Fatal("candidate data falsely proves successful close")
	}
}

func TestDurableSmallCapCannotWritePastLimit(t *testing.T) {
	c := spoolConfig(t)
	c.MaxBytes = 32
	s := newDurableSpoolClient(c)
	if e := s.Start(context.Background()); e == nil {
		t.Fatal("undersized control budget accepted")
	}
	var total int64
	if e := filepath.Walk(c.Root, func(_ string, info os.FileInfo, e error) error {
		if e != nil {
			if os.IsNotExist(e) {
				return nil
			}
			return e
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if total > 32 {
		t.Fatalf("partial initialization wrote past cap: %d", total)
	}
}
