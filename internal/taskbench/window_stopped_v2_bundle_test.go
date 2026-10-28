package taskbench

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestStoppedV2OriginalRecordingAllowsNullWithoutWeakeningReadiness(t *testing.T) {
	r := collectionRecording{PolicyID: collectionPolicyID, PolicySHA256: collectionPolicySHA()}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("null")) {
		t.Fatal("fixture does not contain original nullable fields")
	}
	if _, err := decodeWindowStoppedV2Recording(raw); err != nil {
		t.Fatalf("original nullable record rejected: %v", err)
	}
	var strict collectionRecording
	if err := decodeWindowActualJSON(raw, &strict); err == nil {
		t.Fatal("actual readiness null rule weakened")
	}
	for _, bad := range [][]byte{[]byte(`{"policy_id":"a","policy_id":"b"}`), []byte(`{"unknown_source_field":true}`), append(append([]byte{}, raw...), []byte(` {}`)...)} {
		if _, err := decodeWindowStoppedV2Recording(bad); err == nil {
			t.Fatal("ambiguous or unknown original JSON accepted")
		}
	}
}
