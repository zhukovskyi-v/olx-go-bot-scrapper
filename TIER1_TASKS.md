# Tier 1 production-hardening tasks

Self-contained brief for a fresh Claude Code session. Execute items in order — each builds on the previous. Default permission: file edits + `go build` / `go test` / `go mod` / `git`. Ask before any destructive action.

## Project context

- Go 1.26 Telegram bot. Long-poll Telegram + scrape OLX listing JSON-LD.
- Entry: `main.go`. Parsers: `internal/olx.go` (`ParseList`, `ParseDetail`). HTTP fetch: `utils/utils.go`. Models: `models/models.go` (`Ad` struct).
- State today is in-memory: `urls`, `urlState`, `stopChannels`, `seen` maps in `main.go` guarded by `stateMu`.
- One user → one watched URL today. Already drops chromedp. JSON-LD selectors stable.
- Russian/Ukrainian UI strings. Keep strings as-is unless task says otherwise.

Do NOT touch parser logic in `internal/olx.go` or `models/models.go` schema — they were just refactored and tested. Add fields only if needed.

---

## Task 0 — `.env` file support

**Goal:** App reads `TOKEN`, `ENV`, and future config from a project-root `.env` file. No need to `export` in shell.

**Implementation:**
- `github.com/joho/godotenv` already in `go.mod` (used by `logger/logger.go`).
- In `main()`, before reading any env var, call `_ = godotenv.Load()` once. Ignore "file not found" error so prod (real env vars) still works.
- Remove the duplicate `godotenv.Load()` call inside `logger.getEnvVariable` — load once at startup.
- Create `.env.example` in repo root with:
  ```
  TOKEN=your-telegram-bot-token
  ENV=local
  ```
- Add `.env` to `.gitignore`. Do not commit a real `.env`.

**Verify:**
- `echo "TOKEN=fake" > .env && go run .` reads `TOKEN=fake` (will exit on bot init — that's fine, confirms it was read; check `log.Error("missing token")` does NOT fire).
- `git status` shows `.env` ignored, `.env.example` tracked.

---

## Task 1 — SQLite persistence

**Goal:** Restart no longer loses `seen` ad IDs or watched URLs. Users keep their state.

**Tech choice:** `modernc.org/sqlite` (pure-Go driver, no CGO, builds on Oracle ARM / scratch Docker / Fly with zero pain). Driver name: `sqlite`. Do NOT pull `mattn/go-sqlite3`.

**Schema** (`db/schema.sql` — embed via `embed.FS` or run on startup):

```sql
CREATE TABLE IF NOT EXISTS watches (
    user_id    INTEGER NOT NULL,
    url        TEXT    NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, url)
);

CREATE TABLE IF NOT EXISTS seen_ads (
    user_id   INTEGER NOT NULL,
    ad_id     TEXT    NOT NULL,
    seen_at   INTEGER NOT NULL,
    PRIMARY KEY (user_id, ad_id)
);

CREATE INDEX IF NOT EXISTS idx_seen_user ON seen_ads(user_id);
```

**Files:**
- New `storage/storage.go` package with `type Store struct { db *sql.DB }` and methods:
  - `Open(path string) (*Store, error)` — runs migrations.
  - `Close() error`.
  - `AddWatch(userID int64, url string) error`.
  - `RemoveWatch(userID int64, url string) error`.
  - `ListWatches(userID int64) ([]string, error)`.
  - `AllWatches() (map[int64][]string, error)` — used on startup to resume loops.
  - `MarkSeen(userID int64, adID string) error`.
  - `HasSeen(userID int64, adID string) (bool, error)`.
  - `PurgeOldSeen(olderThan time.Duration) error` — keep table from growing forever; call once/day.

Use `sql.Stmt` prepared statements or simple `db.Exec` — small table, no perf concern. Wrap in single transaction where multiple inserts in one batch.

**main.go changes:**
- DB path from env `DB_PATH`, default `./olx-bot.db`.
- Open store at startup, close on shutdown.
- Replace in-memory `seen` map with `store.HasSeen` / `store.MarkSeen`.
- Replace `urls` map with `store.AddWatch` (called from `/addurl` text handler).
- On startup, call `store.AllWatches()` and spawn a `parseLoop` for every existing watch.
- Keep `stopChannels` in memory — restart anyway resets them.
- `stateMu` still guards in-memory maps; DB has its own concurrency.

**Verify:**
- Start bot, `/start` + `/addurl` + URL. Stop bot (Ctrl-C). Restart bot. Watch resumes without re-sending all old ads.
- `sqlite3 olx-bot.db 'select count(*) from seen_ads;'` returns >0 after one poll cycle.

---

## Task 2 — Graceful shutdown

**Goal:** SIGTERM/SIGINT stops bot cleanly: cancel parseLoops, flush+close DB, exit 0.

**Implementation:**
- Top-level `ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)` in `main()`.
- Pass `ctx` into `parseLoop(ctx, …)`. Replace `stopChan` per-user with `context.WithCancel(parentCtx)` per watch — stored in `stopChannels` map as `cancelFunc` instead of `chan struct{}`.
- `parseLoop` loop: `select { case <-ctx.Done(): return; case <-timer.C: }`. Drop the dual-select pattern.
- `b.Start()` blocks. Run it in a goroutine; wait on `<-ctx.Done()`, then `b.Stop()`, then close store.
- `/stopparse` calls the user's cancel func, removes from `stopChannels`, deletes watch from DB.

**Verify:**
- `kill -TERM <pid>` → process exits within 1s, logs `shutting down`, DB file consistent.
- Run again — watches resume from DB.

---

## Task 3 — Single loop per user per URL

**Goal:** No goroutine leaks. Repeated `/addurl` for same user does not spawn duplicate loops.

**Implementation:**
- Key `stopChannels` (now `cancelFuncs`) by `(userID, url)` not just `userID`. Map `map[int64]map[string]context.CancelFunc`.
- Before spawning new loop for `(userID, url)`: if existing cancel present, call it, wait briefly (no need to block — fire-and-forget is fine since context cancel is instant).
- `/stopparse` cancels ALL watches for user (current behavior). Plus add `/stopwatch <url>` later if scope grows — out of Tier 1.

**Verify:**
- Manually trigger `/addurl` with same URL twice. `goroutine` count via `runtime.NumGoroutine()` logged at startup vs after — should not climb on repeat.
- Add a temporary debug log `log.Info("parseLoop started", "id", id, "url", url)` and `defer log.Info("parseLoop exited", ...)`. Confirm exit count == start count after duplicate `/addurl`.

---

## Task 4 — OLX rate-limit + backoff

**Goal:** Survive 429/403/5xx without spinning. Honor `Retry-After`. Notify user after sustained failure.

**Implementation in `utils/utils.go`:**
- `fetchHTML` returns the raw `*http.Response` (or a wrapper) so caller can read status + headers on error.
- On non-200: read `Retry-After` header (seconds or HTTP-date). Return typed error `RateLimitError{RetryAfter time.Duration}` so loop can backoff that long. Other 5xx: exponential backoff via `time.Duration(math.Min(60, base*2^attempt))*time.Second` capped at 5min.
- Use a single shared `http.Client` with `Timeout: 20*time.Second` and `Transport: &http.Transport{ MaxIdleConnsPerHost: 4, IdleConnTimeout: 90s }`. Replace `http.DefaultClient`.

**Implementation in `main.go` parseLoop:**
- Track `consecutiveFailures int`. Reset on success.
- On `RateLimitError`, sleep `err.RetryAfter` (cap 10 min) instead of normal interval.
- After 5 consecutive failures, send Telegram message to user: `"Парсинг временно приостановлен — OLX недоступен. Повторим автоматически."` Continue trying with capped backoff.
- After 50 consecutive failures, cancel watch + persist a `paused=1` flag (add column to `watches`). Send user a message explaining and instruction to `/resume`. (Add `/resume` command.)

**Verify:**
- Hard to simulate real 429. Add a temporary local test that returns `*errors.New` with HTTP 429 status from a stub `RoundTripper` — confirm `RateLimitError` returned and loop sleeps.
- Run normally for 5+ minutes against real OLX. No tight-loop on any single error.

---

## Task 5 — Telegram throttle

**Goal:** Burst of N new ads does not trigger Telegram "Too Many Requests" (429).

**Limits:**
- ~1 message per second per chat.
- ~30 messages per second across all chats.

**Implementation:**
- Add `golang.org/x/time/rate` (single dep). Two limiters:
  - Global: `rate.NewLimiter(rate.Limit(25), 30)` — 25 msg/s, burst 30.
  - Per-chat: `map[int64]*rate.Limiter` lazily populated, each `rate.NewLimiter(rate.Limit(1), 3)` — 1 msg/s burst 3. Guard map with `sync.Mutex`.
- Wrap all `b.Send` calls in `SendMessageAd` (and any other senders) with: `globalLimiter.Wait(ctx)` then `chatLimiter(id).Wait(ctx)` before sending.
- On Telegram-returned `tele.FloodError` (or generic error containing `429`/`too many requests`): sleep `RetryAfter` from error, retry once.

**Verify:**
- Smoke test: in a unit test, call `SendMessageAd` 50 times in tight loop with a stubbed bot. Total wall time should be ~2s+ for one chat (1 msg/s).
- Live test: find a busy OLX query; first poll produces many sends; observe even spacing in logs, no Telegram 429 in logs.

---

## Task 6 — Pagination

**Goal:** Catch all new ads, not just the top 10 on page 1.

**Implementation:**
- OLX list URLs accept `?page=N` (1-indexed). Default = 1.
- In `parseLoop`: iterate pages 1..N. For each page, call `ParseList`. Walk ads. Stop iterating pages as soon as a page yields an ad already in `seen` for this user (assumption: list is sorted newest-first). Cap at 5 pages safety.
- First poll for a brand-new watch: do NOT send the full page 1 (that's 20–40 alerts at once and useless noise). Instead mark all ads on page 1 as seen and only alert on the next poll onward. Add a `bootstrapped` column to `watches` (or a presence-of-any-row check in `seen_ads` for that user_id).

**Verify:**
- Add `/addurl` for a busy query. First poll: zero Telegram messages, `seen_ads` populated.
- Wait one cycle. Confirm at most a handful of fresh alerts.
- Drop `seen_ads` rows for a watch, restart → simulates "missed" ads. Confirm loop walks page 2+ until it finds a seen row.

---

## Cross-cutting

- **Imports added across Tier 1:** `modernc.org/sqlite`, `database/sql`, `golang.org/x/time/rate`, `os/signal`, `syscall`, `context`.
- Run `go mod tidy` after every task. Confirm `go build ./...` clean.
- Add fixture-based test for `ParseList` + `ParseDetail` (Tier 3, but write while still fresh): save the two HTML files from `https://www.olx.ua/uk/nedvizhimost/kvartiry/prodazha-kvartir/lvov/?currency=USD&search%5Bfilter_enum_commission%5D%5B0%5D=1` and a sample detail page into `internal/testdata/`, write table-driven tests. Prevents silent regression when OLX changes JSON-LD.
- No behavior changes to caption format or commands beyond what's listed.

## Out of scope (do NOT do in this run)

- Multiple URLs per user UI changes (`/list`, `/remove`) beyond DB support.
- Filters (keyword, price).
- i18n.
- Dockerfile, CI, metrics, healthz, Sentry.
- Anti-bot (cookie jar, UA rotation, proxies).
- Schema/parser changes.

## Acceptance checklist

- [ ] `.env` loaded, `.env.example` committed, `.env` gitignored.
- [ ] `olx-bot.db` created on first run, survives restart.
- [ ] SIGTERM exits cleanly in <2s.
- [ ] Duplicate `/addurl` does not leak goroutines.
- [ ] Loop survives a synthetic 429 with `Retry-After`.
- [ ] Burst of new ads spaced ≥1s/chat in logs.
- [ ] Brand-new watch bootstraps silently; next poll sends only delta.
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` all pass.
- [ ] No new direct deps beyond `modernc.org/sqlite` and `golang.org/x/time/rate`.
