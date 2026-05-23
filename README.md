# OLX Telegram Bot

Telegram bot that watches OLX search/category pages and notifies you about new
listings as they appear. Supports multiple watches per user, price/keyword
filters, pause/resume, and four UI languages.

## Features

- Multiple watch URLs per user, each with a short numeric ID (`#1`, `#2`, …)
- Per-watch filters: price range, keyword include/exclude
- Non-destructive `/pause` and `/resume` — bot stops sending while paused, watch state is kept
- `/list` and `/remove` for managing watches
- UI in Ukrainian (default), Russian, English, or Polish — auto-detected from
  Telegram client locale, overridable via `/lang`
- Listing notification with image, title, price, location, and a direct link

## Commands

| Command | What it does |
| --- | --- |
| `/start` | Greet, ensure user record exists, auto-pick UI language |
| `/addurl` | Prompt for an OLX URL and add it as a new watch |
| `/list` | Show all your watches with IDs, filters, and status |
| `/remove <id>` | Delete a watch (and stop its background polling) |
| `/pause [id]` | Pause one watch (with `id`) or all of them (no arg) — non-destructive |
| `/resume [id]` | Resume one or all paused watches |
| `/filter <id> price 30000-60000` | Set a price range. Open-ended `30000-` and `-60000` also work. |
| `/filter <id> include word1,word2` | Notify only when at least one keyword matches title/description |
| `/filter <id> exclude word3` | Skip ads whose title/description contains any of these keywords |
| `/filter <id> clear` | Remove all filters from a watch |
| `/name <id> <text>` | Give a watch a human-readable name (≤64 chars) |
| `/lang [code]` | Show current UI language or switch to `uk`, `ru`, `en`, `pl` |
| `/help` | Localized command list |

### Filter semantics

- Filters never affect what is stored as "seen" — an ad you've already had filtered
  out won't be replayed when you relax the filter.
- Keyword matching is case-insensitive substring on `title + description`.
  `include` is OR (any keyword matches), `exclude` is AND-of-negations (none may match),
  and `exclude` beats `include`.
- Price comparison treats foreign-currency ads (anything that isn't UAH / грн) as
  a pass-through — better to over-notify than silently drop. A warning is logged.

## Installation

```bash
git clone https://github.com/fentezi/olx-scraper
cd olx-scraper
go build -o bot ./cmd/bot
```

Create a `.env` file (or export the variables directly):

```dotenv
TOKEN=<telegram-bot-token>
DB_URL=libsql://<your-db>.turso.io?authToken=<token>
# Or a local file for development:
# DB_URL=file:./olx.db
ENV=local            # local | prod | (anything else uses text logging)
```

Run:

```bash
./bot
```

## Layout

```
cmd/bot/                Entry point (wires config, store, scraper, notifier, watcher, telegram).
internal/
  config/               .env + os.Getenv loader.
  domain/               Pure types and logic: Ad, Watch, Filter, ParsePriceRange.
  scraper/              HTTP client + OLX JSON-LD parser + pagination.
  notifier/             Telegram send wrapper with global + per-chat rate limiters.
  storage/              libsql/SQLite Store; schema + watch/seen/user persistence.
  watcher/              Supervisor + poll loop + 30-day purge ticker.
  telegram/             Bot handlers, inline callbacks, keyboards, pending-input state.
  i18n/                 Localized message catalog (uk, ru, en, pl).
  logger/               slog handlers (pretty console, JSON file, text stdout).
```

## Tests

```bash
go test ./...
```

Covers: storage migrations / local-ID assignment / filter persistence, filter
matching logic, i18n key parity across all four languages, rate-limit error
parsing, and pagination URL building.
