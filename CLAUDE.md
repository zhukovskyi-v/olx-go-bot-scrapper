# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
go build ./...                      # compile every package
go build -o bot ./cmd/bot           # build the binary
./bot                               # run (requires .env with TOKEN + DB_URL)
go test ./...                       # full test suite (no network)
go test ./internal/storage -run X   # single test
go vet ./...
go mod tidy
```

Required env vars (`.env` loaded by `internal/config` or shell):
- `TOKEN` — Telegram bot token (fatal if missing)
- `DB_URL` — `libsql://<host>?authToken=<t>` or `file:./olx.db` for local dev (fatal if missing)
- `ENV` — `local` (pretty console logger), `prod` (JSON to `slog.log`), other (text on stdout)

## Architecture

Single-binary Telegram bot. Long-polls Telegram, scrapes OLX listing pages, persists state in libsql/SQLite. Pragmatic Go layout: `cmd/bot` is the entry point; everything else lives under `internal/` and depends only on packages below it in the layering.

### Layout

```
cmd/bot/main.go                  Wires config → store → scraper → notifier → super → bot.
internal/
  config/      Config struct + Load() (godotenv + os.Getenv).
  domain/      Pure types and logic. Imports stdlib only.
                 ad.go      domain.Ad
                 watch.go   domain.Watch
                 filter.go  Filter, Accept, NormalizeKeywords, ParsePriceRange.
  scraper/     OLX HTTP + JSON-LD parsing.
                 scraper.go  Scraper struct, FetchList, FetchDetail, PaginatedURL.
                 http.go     TLS-fingerprinted browser client, fetchHTML, errors.
                 parser.go   ParseList, ParseDetail.
  notifier/    Telegram send + rate limiting.
                 telegram.go  Notifier struct, SendAd, SendText.
                 ratelimit.go global + per-chat limiters.
  storage/     libsql/SQLite. Returns domain.Watch.
  watcher/     Concurrency + poll lifecycle.
                 supervisor.go  Spawn/Cancel/HasRunning over cancelFuncs map.
                 loop.go        parseLoop + pollOnce (methods on *Supervisor).
                 purge.go       PurgeLoop daily ticker.
  telegram/    Bot delivery layer.
                 bot.go        Bot struct + NewBot + AttachSupervisor.
                 handlers.go   Command + OnText handlers (methods on *Bot).
                 callbacks.go  Inline-button handlers + Unique routing keys.
                 keyboards.go  Reply + inline keyboard builders.
                 menu.go       initMenuLabels, sendWithMenu, action keys.
                 pending.go    pendingKind + pending input state.
                 lang.go       UserLang cache + applyLang.
                 format.go     formatWatchCard, formatFilters, truncate.
                 urlcheck.go   OLX-domain URL regex.
  i18n/        Localized strings (uk, ru, en, pl).
  logger/      slog handlers (pretty console, JSON file, text stdout).
```

### Dependency direction

`domain` → leaf (no internal deps). `config`, `i18n`, `logger` → leaves. `scraper`, `storage`, `notifier` → depend on `domain` (+ `i18n` for `notifier`). `watcher` → `domain`, `scraper`, `storage`, `notifier`, `i18n`. `telegram` → everything except `config`/`cmd`. `cmd/bot` wires them all.

### Wiring (note: order matters)

`Bot` needs `*watcher.Supervisor` for handlers that spawn/cancel watches. `Supervisor` needs a `LangOf` callback (`func(int64) string`) for poll-loop messages. To avoid mutual construction:

```go
bot := telegram.NewBot(tb, store, notif, log)
super := watcher.NewSupervisor(store, scr, notif, log, bot.UserLang)
bot.AttachSupervisor(super)
bot.Register(ctx)
```

`Register(ctx)` stores the root context on the Bot so handlers can pass it to `super.Spawn`.

### Concurrency model

- `cmd/bot/main.go` runs `tb.Start()` in a goroutine; blocks on `signal.NotifyContext` (SIGINT/SIGTERM).
- One goroutine **per `(userID, listURL)` pair**, tracked in `Supervisor.cancels map[int64]map[string]context.CancelFunc` guarded by `Supervisor.mu`. `Spawn` cancels any existing entry for the same key before starting — duplicate `/addurl` never leaks goroutines.
- Pending-input wizard state (`/addurl` URL prompt, `/filter` prompts) lives in `Bot.pending` guarded by `Bot.pendingMu`. A user has at most one pending input at a time.
- `Supervisor.PurgeLoop` runs `store.PurgeOldSeen(30d)` daily.

### Polling lifecycle (`Supervisor.parseLoop` → `Supervisor.pollOnce`)

1. Reload `domain.Watch` row each iteration (filters/name may change live).
2. `pollOnce` walks up to `maxPagesPerPoll=5` pages (`scraper.PaginatedURL`). Stops early when a page contains any ad already in `seen_ads` — assumes OLX list is newest-first.
3. For each unseen ad: optionally fetch the detail page to enrich (description/price/image override list values), `MarkSeen`, run `domain.Filter.Accept`, send via `notifier.SendAd`.
4. **Bootstrap**: a brand-new watch (`Bootstrapped=false`) only walks page 1 and marks-seen-without-notifying, then flips `bootstrapped=1`. Prevents 20-40 alerts on first poll.
5. **Failure handling**: `consecutiveFailures` counter. At `notifyFailureCount=5` send user "OLX unavailable" notice. At `pauseFailureCount=50` set `paused=1` and cancel the watch. `*scraper.RateLimitError` → sleep `RetryAfter` (cap 10min); other errors → exponential backoff (cap 5min).
6. Poll cadence on success: jittered 30-120s.

### Rate limiting

`notifier.Notifier` wraps every `tb.Send` behind two `golang.org/x/time/rate` limiters:
- Global: 25 msg/s burst 30
- Per-chat: 1 msg/s burst 3 (lazy `chatLimiters` map)

On `tele.FloodError`, sleep `RetryAfter` and retry once.

### Persistence (`internal/storage`)

- libsql driver (`github.com/tursodatabase/libsql-client-go`). Works against Turso cloud or local SQLite file (`file:` DSN).
- `schema.sql` is embedded via `//go:embed` and applied on `Open`. Additional columns are added through idempotent `ALTER TABLE ADD COLUMN` blocks in `Open` — extend that block rather than editing `schema.sql` for new columns (old prod DBs need the ALTER path).
- `local_id` is a per-user 1-indexed watch number the user sees in commands (`#1`, `#2`). Assigned in `AddWatch` as `MAX(local_id)+1` inside a transaction. `Open` also backfills `local_id=0` rows.
- `watches` keyed by `(user_id, url)`. Filters stored as `price_min/max INTEGER NULL` and `include_kw/exclude_kw TEXT NULL` (comma-joined via `joinKw`/`splitKw`).
- `seen_ads` keyed by `(user_id, ad_id)` with `seen_at` epoch; purged > 30d.
- `users` table only stores per-user UI language.

### Parser (`internal/scraper/parser.go`)

`ParseList` reads the `application/ld+json` `Product` block whose `offers.@type == "AggregateOffer"`. `ParseDetail` reads the per-ad `Product` block keyed by `sku`. Ad ID derived from URL via `-ID([A-Za-z0-9]+)\.html` regex (list page) or `sku` field (detail page) — these need not match exactly; treat both as opaque strings. **Don't change parser or `domain.Ad` schema without updating fixtures**; OLX JSON-LD is the contract.

### Filtering (`internal/domain`)

- `Accept` is `exclude` AND-of-negations → `include` OR-of-matches → price range.
- Keyword match: case-insensitive substring on `title + " " + description`.
- Price: parsed via `parsePrice` regex. Only UAH-compatible currencies (`""`, `uah`, `грн`, `грн.`) are price-compared; foreign-currency ads always pass (over-notify > silently drop).
- Filters never write to `seen_ads` — filtered-out ads stay seen, so relaxing a filter does not replay them.

### i18n (`internal/i18n`)

- Four languages: `uk` (default), `ru`, `en`, `pl`. `i18n.Supported` is the canonical list.
- `T(lang, key, args...)` returns localized string; missing keys fall back to `DefaultLang` then to the key itself (loud failure).
- `messages` map lives in `catalog.go`. Tests assert key parity across all four languages — adding a key in one language requires adding it in all.
- Per-user language cached in `Bot.langCache sync.Map`; persisted via `users.language`. On `/start` Telegram's `LanguageCode` is normalized (`uk-UA` → `uk`) as the default.
- Reply-keyboard menu labels are localized; `Bot.menuLabelAction` (built once at `NewBot`) maps any localized button text back to a canonical action key.

### Telegram callback routing

Inline buttons use `Unique` strings (`cbWatchPause`, `cbFilterPrice`, …) as routing keys. `Data` carries the `local_id` as a string. `parseCBLocalID` is the common decoder.
