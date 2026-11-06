package taskbench

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// This is candidate policy DATA. It contains no human grant or execution role.
// Actual V14 entry must separately verify a bound operation-policy approval.
type windowV14OperationPolicy struct {
	Schema        string `json:"schema"`
	WindowSeconds int    `json:"window_seconds"`
	ModelSeconds  int    `json:"model_seconds"`
	JEVSeconds    int    `json:"jev_seconds"`
}

func expectedWindowV14OperationPolicy() windowV14OperationPolicy {
	return windowV14OperationPolicy{"full-window-operation-policy.v14", 600, 90, 10}
}

func (p windowV14OperationPolicy) validate() error {
	if p != expectedWindowV14OperationPolicy() {
		return fmt.Errorf("v14 exact operation-policy candidate required")
	}
	return nil
}

// A known epoch is mandatory; zero can never become an unbounded deadline.
// Returning the V14 duration is not authorization to open a V14 execution.
func windowOperationLimitForEpoch(epoch int) (time.Duration, error) {
	if epoch >= 1 && epoch <= 13 {
		return 300 * time.Second, nil
	}
	if epoch == 14 {
		return 600 * time.Second, nil
	}
	return 0, fmt.Errorf("unknown window operation-policy epoch")
}

// Scope selection never grants authority. V14 requires its complete frozen
// declarations even for deadline DATA; unknown schemas cannot choose a timer.
func windowOperationLimitForScope(scope fullWindowScope) (time.Duration, error) {
	const prefix = "full-window-scope.v"
	if !strings.HasPrefix(scope.Schema, prefix) {
		return 0, fmt.Errorf("unknown window operation scope")
	}
	epoch, err := strconv.Atoi(strings.TrimPrefix(scope.Schema, prefix))
	if err != nil || scope.Schema != prefix+strconv.Itoa(epoch) {
		return 0, fmt.Errorf("unknown window operation scope")
	}
	if epoch == 14 {
		if err := validateWindowV14Declarations(scope); err != nil {
			return 0, err
		}
	} else if hasWindowV14Fields(scope) {
		return 0, fmt.Errorf("legacy timing policy injection")
	}
	return windowOperationLimitForEpoch(epoch)
}
