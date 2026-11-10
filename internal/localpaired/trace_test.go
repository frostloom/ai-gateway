package localpaired

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestExportWaitsForLocalFile(t *testing.T) {
	dir := t.TempDir()
	body := []byte("otel-trace-body")
	var found string
	path, err := SaveAndExport(context.Background(), dir, "http://127.0.0.1:9/unused", body, func(ctx context.Context, endpoint string, posted []byte) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			got, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				return err
			}
			if bytes.Equal(got, body) {
				found = filepath.Join(dir, entry.Name())
			}
		}
		if !bytes.Equal(posted, body) {
			t.Fatalf("posted body = %q, want %q", posted, body)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == "" {
		t.Fatal("directory did not contain body when post was called")
	}
	if path != found {
		t.Fatalf("path = %q, file present during post = %q", path, found)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("path content = %q, want %q", got, body)
	}
}

func TestExportDoesNotCountRemoteWithoutLocal(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o755)
	})

	calls := 0
	_, err := SaveAndExport(context.Background(), dir, "http://127.0.0.1:9/unused", []byte("body"), func(ctx context.Context, endpoint string, body []byte) error {
		calls++
		return nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 0 {
		t.Fatalf("post calls = %d, want 0", calls)
	}
}

func TestDefaultEndpointIsLocalLangfuse(t *testing.T) {
	const want = "http://127.0.0.1:13000/api/public/otel/v1/traces"
	body := []byte("span-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/public/otel/v1/traces" {
			http.Error(w, "path", http.StatusNotFound)
			return
		}
		got, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(got, body) {
			http.Error(w, "body", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	var used string
	_, err := SaveAndExport(context.Background(), t.TempDir(), "", body, func(ctx context.Context, endpoint string, payload []byte) error {
		used = endpoint
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/public/otel/v1/traces", bytes.NewReader(payload))
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status %s", resp.Status)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if used != want {
		t.Fatalf("endpoint = %q, want %q", used, want)
	}
}
