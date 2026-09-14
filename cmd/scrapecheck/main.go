// Command scrapecheck fetches one OLX list page with the bot's own scraper and
// reports how many ads it parsed, so a scrape failure is separable from a
// Telegram or storage failure.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/fentezi/olx-scraper/internal/scraper"
)

func main() {
	u := "https://www.olx.ua/uk/nedvizhimost/kvartiry/lvov/q-%D0%B6%D0%BA-%D1%89%D0%B0%D1%81%D0%BB%D0%B8%D0%B2%D0%B8%D0%B9/?currency=USD"
	if len(os.Args) > 1 {
		u = os.Args[1]
	}
	scr, err := scraper.New()
	if err != nil {
		fmt.Println("scraper.New:", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	start := time.Now()
	ads, err := scr.FetchList(ctx, u, "")
	fmt.Printf("FetchList took %s\n", time.Since(start).Round(time.Millisecond))
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
		if st := scraper.StatusOf(err); st != 0 {
			fmt.Printf("  status: %d\n", st)
		}
		if sn := scraper.BodySnippetOf(err); sn != "" {
			fmt.Printf("  body: %s\n", sn)
		}
		os.Exit(1)
	}
	fmt.Printf("  parsed %d ads\n", len(ads))
	for i, a := range ads {
		if i >= 3 {
			break
		}
		fmt.Printf("   [%d] id=%q price=%q title=%q\n", i, a.ID, a.Price, a.Title)
	}
}
