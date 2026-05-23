package main

import (
	"context"
	"testing"
	"time"
)

func TestChatLimiterSpacing(t *testing.T) {
	l := chatLimiter(424242)
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

func TestPaginatedURL(t *testing.T) {
	got, err := paginatedURL("https://www.olx.ua/d/uk/list/?currency=USD", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://www.olx.ua/d/uk/list/?currency=USD&page=3"
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}

	got, err = paginatedURL("https://www.olx.ua/d/uk/list/", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://www.olx.ua/d/uk/list/" {
		t.Fatalf("page=1 should not mutate URL, got %s", got)
	}
}
