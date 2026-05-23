package notifier

import "sync"

const descCacheMaxEntries = 5000

type descCache struct {
	mu    sync.Mutex
	m     map[string]string
	order []string
}

func newDescCache() *descCache {
	return &descCache{m: make(map[string]string)}
}

func (c *descCache) Put(id, desc string) {
	if id == "" || desc == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.m[id]; exists {
		c.m[id] = desc
		return
	}
	c.m[id] = desc
	c.order = append(c.order, id)
	if len(c.order) > descCacheMaxEntries {
		evict := c.order[0]
		c.order = c.order[1:]
		delete(c.m, evict)
	}
}

func (c *descCache) Get(id string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.m[id]
	return s, ok
}
