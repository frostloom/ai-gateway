package taskbench

import (
	"os"
	"path/filepath"
	"testing"
)

func stoppedV2PayloadFixture(t *testing.T) (string, windowPayloadManifest) {
	t.Helper()
	s, _ := fixtureWindowPayloadStore(t, "recorded_run")
	body := []byte(windowProtocolModelBody)
	id := payloadRequest("model", body)
	if err := s.writeWindowRequest(1, id, body); err != nil {
		t.Fatal(err)
	}
	if err := s.writeWindowResponse(1, id, 200, []byte(`{"usage":{"input_tokens":1,"output_tokens":1}}`), true, "complete"); err != nil {
		t.Fatal(err)
	}
	id.Attempt = 2
	if err := s.writeWindowRequest(2, id, body); err != nil {
		t.Fatal(err)
	}
	if err := s.writeWindowResponse(2, id, 0, nil, false, "round_trip"); err != nil {
		t.Fatal(err)
	}
	m, _, err := verifyWindowPayloadStore(s)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	return s.dir, m
}

func TestStoppedV2PayloadReadsCompleteAndUnavailableOriginals(t *testing.T) {
	dir, m := stoppedV2PayloadFixture(t)
	if err := readWindowStoppedV2Payload(dir, m); err != nil {
		t.Fatalf("retained original payloads rejected: %v", err)
	}
}

func TestStoppedV2PayloadRejectsChangedOmittedAndExtraOriginals(t *testing.T) {
	for _, name := range []string{"store.json", "request-000001.json", "request-000001.body", "response-000001.json", "response-000001.body", "request-000002.json", "request-000002.body", "response-000002.json"} {
		for _, mode := range []string{"changed", "missing", "symlink"} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				dir, m := stoppedV2PayloadFixture(t)
				path := filepath.Join(dir, name)
				if mode == "changed" {
					os.WriteFile(path, []byte("changed"), 0600)
				} else {
					os.Remove(path)
					if mode == "symlink" {
						outside := filepath.Join(stoppedEvidenceRoot(t), "copy")
						os.WriteFile(outside, []byte("changed"), 0600)
						os.Symlink(outside, path)
					}
				}
				if err := readWindowStoppedV2Payload(dir, m); err == nil {
					t.Fatal("original mutation accepted")
				}
			})
		}
	}
	for _, name := range []string{"request-000003.body", "response-000002.body", "extra-dir"} {
		t.Run("extra/"+name, func(t *testing.T) {
			dir, m := stoppedV2PayloadFixture(t)
			if name == "extra-dir" {
				os.Mkdir(filepath.Join(dir, name), 0700)
			} else {
				os.WriteFile(filepath.Join(dir, name), []byte("unaccounted"), 0600)
			}
			if err := readWindowStoppedV2Payload(dir, m); err == nil {
				t.Fatal("unaccounted payload accepted")
			}
		})
	}
}
