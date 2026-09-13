package logger

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"os"

	"github.com/fatih/color"
)

type PrettyHandler struct {
	slog.Handler
	l *log.Logger
}

func (p *PrettyHandler) Handle(ctx context.Context, r slog.Record) error {
	level := r.Level.String() + ":"

	switch r.Level {
	case slog.LevelDebug:
		level = color.MagentaString(level)
	case slog.LevelInfo:
		level = color.BlueString(level)
	case slog.LevelWarn:
		level = color.YellowString(level)
	case slog.LevelError:
		level = color.RedString(level)
	}
	fields := make(map[string]interface{}, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		fields[a.Key] = a.Value.Any()
		return true
	})

	var b []byte
	var err error

	if len(fields) > 0 {
		b, err = json.MarshalIndent(fields, "", " ")
		if err != nil {
			return err
		}

	}
	timeStr := r.Time.Format("[15:04:05]")
	msg := color.CyanString(r.Message)

	p.l.Println(timeStr, level, msg, color.WhiteString(string(b)))

	return nil
}

func NewPrettyHandler(out io.Writer, opts slog.HandlerOptions) *PrettyHandler {
	return &PrettyHandler{
		Handler: slog.NewTextHandler(out, &opts),
		l:       log.New(out, "", 0),
	}
}

// New returns a slog.Logger configured for the given env tag
// ("local" → pretty console, "prod" → JSON on stdout, anything else → text stdout debug).
func New(env string) *slog.Logger {
	var handler slog.Handler
	switch env {
	case "local":
		handler = newConsoleHandler(slog.LevelInfo)
	case "prod":
		handler = newJSONHandler()
	default:
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
	}
	return slog.New(handler)
}

func newConsoleHandler(level slog.Level) slog.Handler {
	return NewPrettyHandler(os.Stdout, slog.HandlerOptions{Level: level})
}

// newJSONHandler emits structured JSON on stdout. Container platforms (Railway,
// Cloud Run, Fly) collect stdout only — a log file written inside the container
// is invisible to them and is discarded with the container when the process
// exits, which hides the very errors worth reading.
func newJSONHandler() slog.Handler {
	return slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
}
