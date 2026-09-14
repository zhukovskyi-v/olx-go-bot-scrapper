// Command tgcheck probes the Telegram Bot API behind TOKEN and reports why
// long polling receives nothing. It never prints the token.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/fentezi/olx-scraper/internal/config"
)

func call(token, method string, form url.Values) (bool, json.RawMessage, string, int) {
	if form == nil {
		form = url.Values{}
	}
	resp, err := http.PostForm("https://api.telegram.org/bot"+token+"/"+method, form)
	if err != nil {
		return false, nil, "transport: " + err.Error(), 0
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
		ErrorCode   int             `json:"error_code"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return false, nil, "bad json: " + string(body[:min(200, len(body))]), resp.StatusCode
	}
	return out.OK, out.Result, out.Description, out.ErrorCode
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Println("config:", err)
		os.Exit(1)
	}
	tok := cfg.Token
	// Shape only, never the secret.
	fmt.Printf("TOKEN shape: id=%s len=%d\n\n", strings.SplitN(tok, ":", 2)[0], len(tok))

	fmt.Println("== getMe ==")
	ok, res, desc, code := call(tok, "getMe", nil)
	if !ok {
		fmt.Printf("  FAIL (%d): %s\n", code, desc)
		os.Exit(1)
	}
	var me struct {
		Username string `json:"username"`
		ID       int64  `json:"id"`
	}
	json.Unmarshal(res, &me)
	fmt.Printf("  OK  @%s (id %d)\n\n", me.Username, me.ID)

	fmt.Println("== getWebhookInfo ==")
	ok, res, desc, code = call(tok, "getWebhookInfo", nil)
	if !ok {
		fmt.Printf("  FAIL (%d): %s\n", code, desc)
	} else {
		var wh struct {
			URL                string `json:"url"`
			PendingUpdateCount int    `json:"pending_update_count"`
			LastErrorMessage   string `json:"last_error_message"`
			LastErrorDate      int64  `json:"last_error_date"`
		}
		json.Unmarshal(res, &wh)
		if wh.URL == "" {
			fmt.Printf("  no webhook set (good for long polling)\n")
		} else {
			fmt.Printf("  !! WEBHOOK SET: %s\n", wh.URL)
			fmt.Printf("     -> long polling will NEVER receive updates while this is set\n")
		}
		fmt.Printf("  pending_update_count: %d\n", wh.PendingUpdateCount)
		if wh.LastErrorMessage != "" {
			fmt.Printf("  last_error: %s (%s)\n", wh.LastErrorMessage, time.Unix(wh.LastErrorDate, 0))
		}
	}

	conflictProbe(tok, 25)
	fmt.Println("\n== getUpdates (what the bot's poller actually does) ==")
	ok, res, desc, code = call(tok, "getUpdates", url.Values{
		"timeout": {"0"}, "limit": {"1"},
	})
	if !ok {
		fmt.Printf("  FAIL (%d): %s\n", code, desc)
		if code == 409 {
			fmt.Println("     -> 409 Conflict: another process is polling this same token")
			fmt.Println("        (a deployed instance, or a second local copy)")
		}
	} else {
		var ups []json.RawMessage
		json.Unmarshal(res, &ups)
		fmt.Printf("  OK  %d update(s) waiting\n", len(ups))
	}
}
