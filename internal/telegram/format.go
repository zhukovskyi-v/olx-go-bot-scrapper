package telegram

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/fentezi/olx-scraper/internal/domain"
	"github.com/fentezi/olx-scraper/internal/i18n"
)

func formatBound(v *int) string {
	if v == nil {
		return "∞"
	}
	return strconv.Itoa(*v)
}

func formatFilters(lang string, w domain.Watch) []string {
	var out []string
	if w.PriceMin != nil || w.PriceMax != nil {
		out = append(out, i18n.T(lang, "list.filter_price", formatBound(w.PriceMin), formatBound(w.PriceMax)))
	}
	if len(w.IncludeKw) > 0 {
		out = append(out, i18n.T(lang, "list.filter_include", strings.Join(w.IncludeKw, ", ")))
	}
	if len(w.ExcludeKw) > 0 {
		out = append(out, i18n.T(lang, "list.filter_exclude", strings.Join(w.ExcludeKw, ", ")))
	}
	return out
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func formatWatchCard(lang string, w domain.Watch) string {
	label := w.Name
	if label == "" {
		label = truncate(w.URL, 60)
	}
	status := i18n.T(lang, "list.status_active")
	if w.Paused {
		status = i18n.T(lang, "list.status_paused")
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "#%d — %s [%s]", w.LocalID, label, status)
	if w.Name != "" {
		sb.WriteString("\n  ")
		sb.WriteString(truncate(w.URL, 60))
	}
	filterLines := formatFilters(lang, w)
	if len(filterLines) == 0 {
		sb.WriteString("\n  ")
		sb.WriteString(i18n.T(lang, "list.no_filters"))
	} else {
		for _, line := range filterLines {
			sb.WriteString("\n  ")
			sb.WriteString(line)
		}
	}
	return sb.String()
}
