package knowledge

import (
	"container/list"
	"sync"
	"time"
)

const cacheCapacity = 256
const cacheLifetime = 5 * time.Minute

type cacheEntry struct {
	key     string
	result  Result
	expires time.Time
}

type searchCache struct {
	mu       sync.Mutex
	entries  map[string]*list.Element
	order    *list.List
	capacity int
	lifetime time.Duration
	now      func() time.Time
}

func newSearchCache() *searchCache {
	return &searchCache{entries: map[string]*list.Element{}, order: list.New(), capacity: cacheCapacity, lifetime: cacheLifetime, now: time.Now}
}

func (c *searchCache) get(key string) (Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return Result{}, false
	}
	entry := element.Value.(cacheEntry)
	if !c.now().Before(entry.expires) {
		delete(c.entries, key)
		c.order.Remove(element)
		return Result{}, false
	}
	c.order.MoveToFront(element)
	return cloneResult(entry.result), true
}

func (c *searchCache) put(key string, result Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := cacheEntry{key: key, result: cloneResult(result), expires: c.now().Add(c.lifetime)}
	if element, ok := c.entries[key]; ok {
		element.Value = entry
		c.order.MoveToFront(element)
		return
	}
	c.entries[key] = c.order.PushFront(entry)
	for c.order.Len() > c.capacity {
		oldest := c.order.Back()
		delete(c.entries, oldest.Value.(cacheEntry).key)
		c.order.Remove(oldest)
	}
}

func cloneResult(result Result) Result {
	copy := result
	copy.Hits = make([]Hit, len(result.Hits))
	for i, hit := range result.Hits {
		copy.Hits[i] = hit
		copy.Hits[i].Sources = append([]Source(nil), hit.Sources...)
	}
	return copy
}
