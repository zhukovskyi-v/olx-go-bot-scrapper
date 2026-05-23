package scraper

import "testing"

func TestPaginatedURL(t *testing.T) {
	got, err := PaginatedURL("https://www.olx.ua/d/uk/list/?currency=USD", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://www.olx.ua/d/uk/list/?currency=USD&page=3"
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}

	got, err = PaginatedURL("https://www.olx.ua/d/uk/list/", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://www.olx.ua/d/uk/list/" {
		t.Fatalf("page=1 should not mutate URL, got %s", got)
	}
}
