package taskbench

import (
	"fmt"
	"sync"
)

type windowStoppedReadKind uint8

const (
	windowStoppedReadFile windowStoppedReadKind = iota
	windowStoppedReadBytes
)

type windowStoppedReadSpec struct {
	path     string
	sha256   string
	maxBytes int64
	kind     windowStoppedReadKind
}

// Slots bound active historical reads across overlapping callers. There are no
// resident workers or retained file results. Readers must not acquire slots or
// historical locks themselves: the caller holds its original lock throughout.
var windowStoppedReadSlots = make(chan struct{}, 4)

func readWindowStoppedBatch4(specs []windowStoppedReadSpec) ([][]byte, error) {
	for _, spec := range specs {
		if spec.kind != windowStoppedReadFile && spec.kind != windowStoppedReadBytes {
			return nil, fmt.Errorf("stopped read kind rejected")
		}
	}
	return runWindowStoppedReadBatch4(len(specs), func(i int) ([]byte, error) {
		s := specs[i]
		if s.kind == windowStoppedReadBytes {
			return readWindowActualBytes(s.path, s.sha256, s.maxBytes)
		}
		return readWindowActualFile(s.path, s.sha256, s.maxBytes)
	})
}

func runWindowStoppedReadBatch4(n int, read func(int) ([]byte, error)) ([][]byte, error) {
	if n < 1 || n > 4 || read == nil {
		return nil, fmt.Errorf("stopped read batch size rejected")
	}
	raw := make([][]byte, n)
	errs := make([]error, n)
	var readers sync.WaitGroup
	for i := range raw {
		windowStoppedReadSlots <- struct{}{}
		readers.Add(1)
		go func(i int) {
			defer readers.Done()
			defer func() { <-windowStoppedReadSlots }()
			raw[i], errs[i] = read(i)
		}(i)
	}
	// Even an early read error cannot release the caller's old-owner lock while
	// another reader is active. Input order, not completion order, selects errors.
	readers.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return raw, nil
}
