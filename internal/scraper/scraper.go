package scraper

import (
	"context"
	"net/url"
	"strconv"

	"github.com/PuerkitoBio/goquery"
	"github.com/fentezi/olx-scraper/internal/domain"
)

type Scraper struct {
	httpClient browserHTTPClient
}

func New() (*Scraper, error) {
	client, err := newBrowserHTTPClient()
	if err != nil {
		return nil, err
	}
	return &Scraper{httpClient: client}, nil
}

func (s *Scraper) fetchAndParse(ctx context.Context, rawURL, referer string) (*goquery.Document, error) {
	body, err := s.fetchHTML(ctx, rawURL, referer)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return goquery.NewDocumentFromReader(body)
}

// FetchList reads a search-results page. referer is the page it was reached
// from — empty for the first page of a poll, the previous page when paginating.
func (s *Scraper) FetchList(ctx context.Context, rawURL, referer string) ([]domain.Ad, error) {
	doc, err := s.fetchAndParse(ctx, rawURL, referer)
	if err != nil {
		return nil, err
	}
	return ParseList(doc)
}

// FetchDetail reads a single ad page. referer is the list page the ad was found
// on, which is what makes the request look like a clicked search result.
func (s *Scraper) FetchDetail(ctx context.Context, rawURL, referer string) (domain.Ad, error) {
	doc, err := s.fetchAndParse(ctx, rawURL, referer)
	if err != nil {
		return domain.Ad{}, err
	}
	return ParseDetail(doc)
}

func PaginatedURL(raw string, page int) (string, error) {
	if page <= 1 {
		return raw, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("page", strconv.Itoa(page))
	u.RawQuery = q.Encode()
	return u.String(), nil
}
