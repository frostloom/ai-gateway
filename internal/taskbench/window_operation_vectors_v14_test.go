package taskbench

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/frostloom/ai-gateway/internal/observability"
)

// Emits genuine Go wire DATA only. There is no key, signature operation,
// filesystem input/output, preparation, actual manifest or execution role.
// Zero signature strings merely exercise the Python envelope shape parser.
func TestWindowV14OperationWireVectors(t *testing.T) {
	policySHA := windowV8DeclarationSHA(expectedWindowV14OperationPolicy())
	execution := windowV14ExecutionBinding{
		windowV13ExecutionBinding: windowV13ExecutionBinding{
			windowV11ExecutionBinding: windowV11ExecutionBinding{
				windowV8ExecutionBinding: windowV8ExecutionBinding{
					Schema:                  "full-window-delegated-execution-authorization.v14",
					HumanAuthorized:         true,
					RetryPolicyApproved:     true,
					HumanReply:              windowActualFile{"/synthetic/budget.json", windowStoppedV8ApprovalSHA},
					ManifestSHA256:          strings.Repeat("a", 64),
					SourceSHA256:            strings.Repeat("b", 64),
					BinarySHA256:            strings.Repeat("c", 64),
					ScopeSHA256:             strings.Repeat("d", 64),
					ExecutionSHA256:         strings.Repeat("e", 64),
					BudgetDeclarationSHA256: windowV8DeclarationSHA(expectedWindowV14BudgetDeclaration()),
					RetryPolicySHA256:       windowV8DeclarationSHA(expectedWindowV14RetryDeclaration()),
					Parent:                  windowV8Amounts{200000000, 3000000},
					OwnerID:                 "synthetic<&>合成\u2028\u2029\t\\\"",
					SingleOwner:             true,
					NoRelaunch:              true,
				},
				AutonomousAuthorization: windowActualFile{"/synthetic/autonomy.json", windowV9AutonomousAuthorizationSHA},
				GlobalBudget:            windowV8Amounts{200000000, 4000000},
				PrivateSettingsSHA256:   strings.Repeat("f", 64),
				SpoolRoot:               "/synthetic/spool",
				Observer: observability.EvaluationIdentity{
					ObservationReceiptBoundary: "durable_local",
					SpoolRootSHA256:            conversationHash([]byte("/synthetic/spool")),
					SpoolRunID:                 strings.Repeat("2", 64),
					SpoolMaxBytes:              4 << 30,
					Enabled:                    true,
					CaptureContent:             true,
					SampleRate:                 1,
					Environment:                "evaluation",
					Release:                    strings.Repeat("3", 40),
					ExportTimeoutMS:            10000,
					QueueSize:                  4096,
					EndpointSHA256:             strings.Repeat("4", 64),
					ProjectSHA256:              strings.Repeat("5", 64),
				},
			},
			CollectionPolicySHA256:    collectionPreviewFailurePolicySHA(),
			CompletionSHA256:          windowV8DeclarationSHA(expectedWindowCompletionDeclarationV14()),
			ExecutorEquivalenceSHA256: strings.Repeat("7", 64),
		},
		OperationPolicySHA256:   policySHA,
		OperationPolicyApproval: windowActualFile{"/synthetic/timing-approval.json", strings.Repeat("8", 64)},
	}
	approval := windowV14OperationPolicyApprovalBinding{
		Schema:                "full-window-operation-policy-human-approval.v14",
		HumanPolicyApproved:   true,
		HumanReply:            windowActualFile{"/synthetic/合成<&>/operation-policy-human-reply.txt", strings.Repeat("9", 64)},
		ManifestSHA256:        execution.ManifestSHA256,
		SourceSHA256:          execution.SourceSHA256,
		BinarySHA256:          execution.BinarySHA256,
		ScopeSHA256:           execution.ScopeSHA256,
		ExecutionSHA256:       execution.ExecutionSHA256,
		OperationPolicySHA256: policySHA,
	}
	executionMessage, err := windowV14ExecutionAuthorizationMessage(execution)
	if err != nil {
		t.Fatal(err)
	}
	approvalMessage, err := windowV14OperationPolicyApprovalMessage(approval)
	if err != nil {
		t.Fatal(err)
	}
	for _, vector := range []struct {
		binding any
		message []byte
	}{{execution, executionMessage}, {approval, approvalMessage}} {
		marshaled, err := json.Marshal(vector.binding)
		if err != nil || !bytes.Equal(marshaled, vector.message) || bytes.HasSuffix(vector.message, []byte("\n")) {
			t.Fatal("dedicated message differs from Go struct JSON", err)
		}
	}
	type vector struct {
		Document json.RawMessage `json:"document"`
		GoJSON   string          `json:"go_json"`
		SHA256   string          `json:"sha256"`
	}
	makeVector := func(document any, message []byte) vector {
		raw, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		return vector{raw, string(message), conversationHash(message)}
	}
	output := struct {
		Schema           string `json:"schema"`
		NotAuthorization bool   `json:"not_authorization"`
		ZeroSignatures   bool   `json:"zero_signatures_not_verified"`
		Execution        vector `json:"execution"`
		Approval         vector `json:"approval"`
	}{
		"go-generated-v14-operation-wire-data-vectors.v1", true, true,
		makeVector(windowV14ExecutionAuthorization{execution, strings.Repeat("0", 128)}, executionMessage),
		makeVector(windowV14OperationPolicyApproval{approval, strings.Repeat("0", 128)}, approvalMessage),
	}
	raw, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("FULL1220_V14_OPERATION_VECTORS_JSON=%s", raw)
}
