package domain

import "testing"

func ptr(v int) *int { return &v }

func TestAccept_EmptyFilter(t *testing.T) {
	f := Filter{}
	ok, _ := f.Accept(Ad{Title: "anything", Price: "100 UAH"})
	if !ok {
		t.Fatal("empty filter must accept any ad")
	}
}

func TestAccept_PriceRange(t *testing.T) {
	cases := []struct {
		name  string
		f     Filter
		price string
		want  bool
	}{
		{"in range", Filter{PriceMin: ptr(30000), PriceMax: ptr(60000)}, "45000 UAH", true},
		{"below min", Filter{PriceMin: ptr(30000), PriceMax: ptr(60000)}, "20000 UAH", false},
		{"above max", Filter{PriceMin: ptr(30000), PriceMax: ptr(60000)}, "70000 UAH", false},
		{"only min, above", Filter{PriceMin: ptr(30000)}, "50000 UAH", true},
		{"only min, below", Filter{PriceMin: ptr(30000)}, "1000 UAH", false},
		{"only max, below", Filter{PriceMax: ptr(60000)}, "50000 UAH", true},
		{"only max, above", Filter{PriceMax: ptr(60000)}, "70000 UAH", false},
		{"hryvnia token", Filter{PriceMin: ptr(30000)}, "45 000 грн", true},
		{"foreign currency passes", Filter{PriceMin: ptr(30000), PriceMax: ptr(60000)}, "500 USD", true},
		{"unparseable passes", Filter{PriceMin: ptr(30000)}, "Договорная", true},
		{"empty price passes", Filter{PriceMin: ptr(30000)}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, _ := c.f.Accept(Ad{Price: c.price})
			if ok != c.want {
				t.Fatalf("got %v, want %v", ok, c.want)
			}
		})
	}
}

func TestAccept_Include_OR(t *testing.T) {
	f := Filter{Include: []string{"kitchen", "balcony"}}
	if ok, _ := f.Accept(Ad{Title: "Apartment with Kitchen"}); !ok {
		t.Error("title match should accept")
	}
	if ok, _ := f.Accept(Ad{Description: "has BALCONY view"}); !ok {
		t.Error("description match should accept")
	}
	if ok, _ := f.Accept(Ad{Title: "bare apartment"}); ok {
		t.Error("no match should reject")
	}
}

func TestAccept_Exclude_AND(t *testing.T) {
	f := Filter{Exclude: []string{"studio", "basement"}}
	if ok, _ := f.Accept(Ad{Title: "Big house"}); !ok {
		t.Error("no excluded word — accept")
	}
	if ok, _ := f.Accept(Ad{Title: "Studio loft"}); ok {
		t.Error("excluded word should reject")
	}
	if ok, _ := f.Accept(Ad{Description: "in the BASEMENT"}); ok {
		t.Error("excluded word in description should reject")
	}
}

func TestAccept_ExcludeBeatsInclude(t *testing.T) {
	f := Filter{
		Include: []string{"kitchen"},
		Exclude: []string{"studio"},
	}
	ad := Ad{Title: "Studio with kitchen"}
	if ok, _ := f.Accept(ad); ok {
		t.Fatal("exclude must win over include")
	}
}

func TestAccept_CaseInsensitive(t *testing.T) {
	f := Filter{Include: []string{"KITCHEN"}}
	if ok, _ := f.Accept(Ad{Title: "kitchen view"}); !ok {
		t.Error("include should be case-insensitive")
	}
}

func TestParsePriceRange(t *testing.T) {
	cases := []struct {
		in       string
		min, max *int
		ok       bool
	}{
		{"30000-60000", ptr(30000), ptr(60000), true},
		{"30000-", ptr(30000), nil, true},
		{"-60000", nil, ptr(60000), true},
		{"  30000 - 60000  ", ptr(30000), ptr(60000), true},
		{"60000-30000", nil, nil, false},
		{"abc-60000", nil, nil, false},
		{"30000", nil, nil, false},
		{"", nil, nil, false},
		{"-", nil, nil, false},
		{"-1000-", nil, nil, false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			min, max, ok := ParsePriceRange(c.in)
			if ok != c.ok {
				t.Fatalf("ok=%v want %v", ok, c.ok)
			}
			if !ok {
				return
			}
			if (min == nil) != (c.min == nil) || (min != nil && *min != *c.min) {
				t.Fatalf("min=%v want %v", min, c.min)
			}
			if (max == nil) != (c.max == nil) || (max != nil && *max != *c.max) {
				t.Fatalf("max=%v want %v", max, c.max)
			}
		})
	}
}

func TestNormalizeKeywords(t *testing.T) {
	got := NormalizeKeywords([]string{" Kitchen ", "balcony", "", "KITCHEN", " "})
	if len(got) != 2 || got[0] != "kitchen" || got[1] != "balcony" {
		t.Fatalf("got %v", got)
	}
}
