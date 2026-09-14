package telegram

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v3"
)

// LoggingPoller is a long poller that reports getUpdates failures.
//
// telebot's own LongPoller routes every getUpdates error through Bot.debug,
// which throws it away unless Settings.Verbose is set — and Verbose also dumps
// the body of every API call, so the stock choice is total silence or total
// noise. Silence hides the one failure that matters most here: HTTP 409, which
// Telegram returns when a second process polls the same token. Only one
// getUpdates consumer per token is allowed, so a forgotten `go run`, an IDE run
// configuration left running, or a deployed copy is enough to make the bot
// receive nothing at all while still logging a clean, healthy startup.
//
// It also backs off. telebot's loop retries a failed getUpdates immediately,
// which spins a core when the failure returns instantly, as 409 does.
type LoggingPoller struct {
	Timeout      time.Duration
	LastUpdateID int
	Log          *slog.Logger

	// conflictLogged keeps the 409 explanation to once per streak rather than
	// once per retry, while still leaving a warn line on every occurrence.
	conflictLogged bool
}

const (
	pollMinBackoff = 1 * time.Second
	pollMaxBackoff = 30 * time.Second
)

// Poll implements tele.Poller.
func (p *LoggingPoller) Poll(b *tele.Bot, dest chan tele.Update, stop chan struct{}) {
	backoff := pollMinBackoff
	for {
		select {
		case <-stop:
			return
		default:
		}

		updates, err := p.getUpdates(b)
		if err != nil {
			p.logErr(err)
			if !sleepStop(stop, backoff) {
				return
			}
			backoff = min(backoff*2, pollMaxBackoff)
			continue
		}

		if p.conflictLogged {
			p.Log.Info("telegram polling recovered: this instance now owns the token")
			p.conflictLogged = false
		}
		backoff = pollMinBackoff

		for _, u := range updates {
			p.LastUpdateID = u.ID
			select {
			case dest <- u:
			case <-stop:
				return
			}
		}
	}
}

func (p *LoggingPoller) getUpdates(b *tele.Bot) ([]tele.Update, error) {
	data, err := b.Raw("getUpdates", map[string]string{
		"offset":  strconv.Itoa(p.LastUpdateID + 1),
		"timeout": strconv.Itoa(int(p.Timeout / time.Second)),
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result []tele.Update `json:"result"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}

// isConflict reports whether err is Telegram's 409.
//
// It has to match on the text as well as the type: telebot only builds a typed
// *tele.Error for descriptions it recognises, and the conflict text is not one
// of them, so extractOk falls through to a plain fmt.Errorf. errors.As alone
// therefore never matches a 409. The type check stays first in case a later
// telebot version starts typing it.
func isConflict(err error) bool {
	var apiErr *tele.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code == 409
	}
	return strings.Contains(err.Error(), "terminated by other getUpdates")
}

// logErr turns a poll failure into a line that names the cause. A 409 is a
// configuration mistake, not a blip, so it gets the explanation rather than the
// bare API text.
func (p *LoggingPoller) logErr(err error) {
	if isConflict(err) {
		p.Log.Warn("telegram getUpdates conflict", "err", err.Error())
		if !p.conflictLogged {
			p.Log.Error("another process is polling this bot token — " +
				"Telegram allows exactly one getUpdates consumer, so updates " +
				"will be split between the instances and this one will look dead. " +
				"Stop the other copy (a deployed instance, an IDE run configuration, " +
				"or a stray `go run`) and keep only one running.")
			p.conflictLogged = true
		}
		return
	}
	p.Log.Warn("telegram getUpdates failed", "err", err.Error())
}

func sleepStop(stop chan struct{}, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-stop:
		return false
	case <-t.C:
		return true
	}
}
