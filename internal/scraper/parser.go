package scraper

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/PuerkitoBio/goquery"
	"github.com/fentezi/olx-scraper/internal/domain"
)

type areaServed struct {
	Name string `json:"name"`
}

type offer struct {
	Type          string      `json:"@type"`
	Name          string      `json:"name"`
	URL           string      `json:"url"`
	Image         []string    `json:"image"`
	Price         json.Number `json:"price"`
	PriceCurrency string      `json:"priceCurrency"`
	AreaServed    areaServed  `json:"areaServed"`
}

type aggregateOffer struct {
	Type       string     `json:"@type"`
	AreaServed areaServed `json:"areaServed"`
	Offers     []offer    `json:"offers"`
}

type productList struct {
	Type   string         `json:"@type"`
	Offers aggregateOffer `json:"offers"`
}

type productDetail struct {
	Type        string   `json:"@type"`
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Image       []string `json:"image"`
	Description string   `json:"description"`
	SKU         string   `json:"sku"`
	Offers      offer    `json:"offers"`
}

var idRegex = regexp.MustCompile(`-ID([A-Za-z0-9]+)\.html`)

func extractID(url string) string {
	m := idRegex.FindStringSubmatch(url)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func priceString(p json.Number, currency string) string {
	if p == "" {
		return ""
	}
	if f, err := p.Float64(); err == nil {
		whole := int64(f)
		if float64(whole) == f {
			return fmt.Sprintf("%s %s", strconv.FormatInt(whole, 10), currency)
		}
	}
	return fmt.Sprintf("%s %s", p.String(), currency)
}

func ParseList(doc *goquery.Document) ([]domain.Ad, error) {
	if doc == nil {
		return nil, errors.New("nil document")
	}

	var ads []domain.Ad
	var found bool

	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		var pl productList
		if err := json.Unmarshal([]byte(s.Text()), &pl); err != nil {
			return true
		}
		if pl.Type != "Product" || pl.Offers.Type != "AggregateOffer" {
			return true
		}
		found = true
		city := pl.Offers.AreaServed.Name
		for _, o := range pl.Offers.Offers {
			ads = append(ads, domain.Ad{
				ID:       extractID(o.URL),
				URL:      o.URL,
				Title:    o.Name,
				Price:    priceString(o.Price, o.PriceCurrency),
				Currency: o.PriceCurrency,
				City:     city,
				District: o.AreaServed.Name,
				Images:   o.Image,
			})
		}
		return false
	})

	if !found {
		return nil, errors.New("no AggregateOffer JSON-LD block found")
	}
	return ads, nil
}

func ParseDetail(doc *goquery.Document) (domain.Ad, error) {
	if doc == nil {
		return domain.Ad{}, errors.New("nil document")
	}

	var ad domain.Ad
	var found bool

	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		var pd productDetail
		if err := json.Unmarshal([]byte(s.Text()), &pd); err != nil {
			return true
		}
		if pd.Type != "Product" || pd.SKU == "" {
			return true
		}
		found = true
		ad = domain.Ad{
			ID:          pd.SKU,
			URL:         pd.URL,
			Title:       pd.Name,
			Price:       priceString(pd.Offers.Price, pd.Offers.PriceCurrency),
			Currency:    pd.Offers.PriceCurrency,
			District:    pd.Offers.AreaServed.Name,
			Images:      pd.Image,
			Description: pd.Description,
		}
		return false
	})

	if !found {
		return domain.Ad{}, errors.New("no Product JSON-LD block with sku found")
	}
	return ad, nil
}
