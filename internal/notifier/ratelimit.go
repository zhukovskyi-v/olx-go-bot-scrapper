package notifier

import (
	"sync"

	"golang.org/x/time/rate"
)

func newGlobalLimiter() *rate.Limiter {
	return rate.NewLimiter(rate.Limit(25), 30)
}

type chatLimiters struct {
	mu sync.Mutex
	m  map[int64]*rate.Limiter
}

func newChatLimiters() *chatLimiters {
	return &chatLimiters{m: make(map[int64]*rate.Limiter)}
}

func (c *chatLimiters) get(id int64) *rate.Limiter {
	c.mu.Lock()
	defer c.mu.Unlock()
	if l, ok := c.m[id]; ok {
		return l
	}
	l := rate.NewLimiter(rate.Limit(1), 3)
	c.m[id] = l
	return l
}
