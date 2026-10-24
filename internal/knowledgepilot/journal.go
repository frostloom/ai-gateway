package knowledgepilot

import (
	"encoding/json"
	"fmt"
	"github.com/frostloom/ai-gateway/internal/knowledgeeval"
	"os"
	"sync"
)

type Event struct {
	Kind       string                 `json:"kind"`
	TaskID     string                 `json:"task_id"`
	Sequence   int                    `json:"sequence"`
	RequestSHA string                 `json:"request_sha256"`
	Request    json.RawMessage        `json:"request,omitempty"`
	Response   json.RawMessage        `json:"response,omitempty"`
	Status     int                    `json:"http_status,omitempty"`
	Failure    string                 `json:"failure,omitempty"`
	DurationMS float64                `json:"duration_ms,omitempty"`
	Attempt    *knowledgeeval.Attempt `json:"attempt,omitempty"`
}
type Journal struct {
	mu     sync.Mutex
	f      *os.File
	path   string
	events []Event
	closed bool
}

func OpenJournal(path string) (*Journal, error) {
	if e := writeExclusive(path, nil); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if e != nil {
		return nil, e
	}
	return &Journal{f: f, path: path}, nil
}
func (j *Journal) Append(ev Event) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return fmt.Errorf("journal closed")
	}
	b, e := json.Marshal(ev)
	if e != nil {
		return e
	}
	if _, e = j.f.Write(append(b, '\n')); e != nil {
		return e
	}
	if e = j.f.Sync(); e != nil {
		return e
	}
	j.events = append(j.events, ev)
	return nil
}
func (j *Journal) Events() []Event {
	j.mu.Lock()
	defer j.mu.Unlock()
	b, _ := json.Marshal(j.events)
	var out []Event
	json.Unmarshal(b, &out)
	return out
}
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil
	}
	j.closed = true
	return j.f.Close()
}
