//go:build darwin

package taskbench

import (
	"fmt"
	"os"
	"syscall"
)

func historyCTimeNS(info os.FileInfo) (int64, error) {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("history ctime unavailable")
	}
	return s.Ctimespec.Nano(), nil
}
