package notifier

import (
	"context"
	"testing"
	"time"
)

func TestChatLimiterSpacing(t *testing.T) {
	c := newChatLimiters()
	l := c.get(424242)
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 5; i++ {
		if err := l.Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// burst 3 free; remaining 2 at 1/s ≥ 2s wall time.
	if elapsed := time.Since(start); elapsed < 1500*time.Millisecond {
		t.Fatalf("limiter too loose: %s", elapsed)
	}
}
