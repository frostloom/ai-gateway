//go:build !darwin && !linux

package taskbench

import (
	"fmt"
	"os"
)

func historyCTimeNS(os.FileInfo) (int64, error) {
	return 0, fmt.Errorf("history identity platform unsupported")
}
