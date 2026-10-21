package persistence

import (
	"sync"

	"retrom/internal/model"
)

// Immutable directory snapshots have a bounded process cache, never a Redis directory ledger.
type contentCache struct {
	mutex   sync.Mutex
	entries map[string][]model.ContentFile
	order   []string
	bytes   int
}

func newContentCache() *contentCache {
	return &contentCache{entries: make(map[string][]model.ContentFile)}
}

func contentWeight(files []model.ContentFile) int {
	weight := 0
	for _, file := range files {
		weight += 256 + len(file.LogicalKey) + len(file.MediaType)
	}
	return weight
}

func (c *contentCache) get(key string) []model.ContentFile {
	if c == nil {
		return nil
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	files := c.entries[key]
	if files == nil {
		return nil
	}
	return append([]model.ContentFile(nil), files...)
}

func (c *contentCache) put(key string, files []model.ContentFile) {
	if c == nil {
		return
	}
	weight := contentWeight(files)
	if weight > 32*1024*1024 {
		return
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.entries[key] != nil {
		return
	}
	for len(c.order) > 0 && (c.bytes+weight > 32*1024*1024 || len(c.order) >= 16) {
		old := c.order[0]
		c.order = c.order[1:]
		c.bytes -= contentWeight(c.entries[old])
		delete(c.entries, old)
	}
	c.entries[key] = append([]model.ContentFile(nil), files...)
	c.order = append(c.order, key)
	c.bytes += weight
}
