package domain

import (
	"regexp"
	"strconv"
	"strings"
)

type Filter struct {
	PriceMin *int
	PriceMax *int
	Include  []string
	Exclude  []string
}

var priceRe = regexp.MustCompile(`^\s*([0-9][0-9\s]*)\s*(\S*)\s*$`)

func uahCompatible(currency string) bool {
	switch strings.ToLower(strings.TrimSpace(currency)) {
	case "", "uah", "грн", "грн.":
		return true
	}
	return false
}

func parsePrice(p string) (int, string, bool) {
	if p == "" {
		return 0, "", false
	}
	m := priceRe.FindStringSubmatch(p)
	if m == nil {
		return 0, "", false
	}
	digits := strings.ReplaceAll(m[1], " ", "")
	v, err := strconv.Atoi(digits)
	if err != nil {
		return 0, "", false
	}
	return v, m[2], true
}

func (f Filter) Accept(ad Ad) (bool, string) {
	haystack := strings.ToLower(ad.Title + " " + ad.Description)

	if len(f.Exclude) > 0 {
		for _, kw := range f.Exclude {
			kw = strings.ToLower(kw)
			if kw != "" && strings.Contains(haystack, kw) {
				return false, "exclude:" + kw
			}
		}
	}

	if len(f.Include) > 0 {
		match := false
		for _, kw := range f.Include {
			kw = strings.ToLower(kw)
			if kw != "" && strings.Contains(haystack, kw) {
				match = true
				break
			}
		}
		if !match {
			return false, "include:no-match"
		}
	}

	if f.PriceMin != nil || f.PriceMax != nil {
		value, currency, ok := parsePrice(ad.Price)
		if !ok {
			return true, ""
		}
		if !uahCompatible(currency) {
			return true, ""
		}
		if f.PriceMin != nil && value < *f.PriceMin {
			return false, "price<min"
		}
		if f.PriceMax != nil && value > *f.PriceMax {
			return false, "price>max"
		}
	}

	return true, ""
}

func NormalizeKeywords(words []string) []string {
	seen := make(map[string]struct{}, len(words))
	out := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.ToLower(strings.TrimSpace(w))
		if w == "" {
			continue
		}
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
	}
	return out
}

func ParsePriceRange(s string) (*int, *int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil, false
	}
	idx := strings.Index(s, "-")
	if idx < 0 {
		return nil, nil, false
	}
	left := strings.TrimSpace(s[:idx])
	right := strings.TrimSpace(s[idx+1:])
	if left == "" && right == "" {
		return nil, nil, false
	}
	var min, max *int
	if left != "" {
		v, err := strconv.Atoi(left)
		if err != nil || v < 0 {
			return nil, nil, false
		}
		min = &v
	}
	if right != "" {
		v, err := strconv.Atoi(right)
		if err != nil || v < 0 {
			return nil, nil, false
		}
		max = &v
	}
	if min != nil && max != nil && *min > *max {
		return nil, nil, false
	}
	return min, max, true
}
