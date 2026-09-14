package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// conflictProbe issues one long getUpdates. If no other process is polling this
// token, Telegram holds the connection for the full timeout and returns empty.
// If another instance polls, Telegram terminates THIS request with 409 as soon
// as the other one arrives, which is the signal we are looking for.
func conflictProbe(tok string, seconds int) {
	fmt.Printf("\n== conflict probe: one getUpdates held for %ds ==\n", seconds)
	start := time.Now()
	ok, _, desc, code := call(tok, "getUpdates", url.Values{
		"timeout": {fmt.Sprint(seconds)}, "limit": {"1"},
	})
	el := time.Since(start).Round(time.Millisecond)
	if !ok {
		fmt.Printf("  returned after %s -> FAIL (%d): %s\n", el, code, desc)
		if code == 409 {
			fmt.Println("  >> ANOTHER INSTANCE IS POLLING THIS TOKEN (Railway deploy, or a second local copy)")
		}
		return
	}
	fmt.Printf("  returned after %s -> OK, no conflict\n", el)
	if el < time.Duration(seconds)*time.Second/2 {
		fmt.Println("  (returned early: an update arrived, or the connection was cut)")
	} else {
		fmt.Println("  >> held the full window: nothing else is polling this token")
	}
}

var _ = json.Marshal
