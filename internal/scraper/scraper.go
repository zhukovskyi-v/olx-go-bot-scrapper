package scraper

import (
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/PuerkitoBio/goquery"
	"github.com/fentezi/olx-scraper/internal/domain"
)

type Scraper struct {
	HTTPClient *http.Client
}

func New() *Scraper {
	return &Scraper{HTTPClient: defaultHTTPClient()}
}

func (s *Scraper) fetchAndParse(rawURL string) (*goquery.Document, error) {
	body, err := s.fetchHTML(rawURL)
	if err != nil {
		return nil, err
	}
	defer func(body io.ReadCloser) {
		err := body.Close()
		if err != nil {

		}
	}(body)
	return goquery.NewDocumentFromReader(body)
}

func (s *Scraper) FetchList(rawURL string) ([]domain.Ad, error) {
	doc, err := s.fetchAndParse(rawURL)
	if err != nil {
		return nil, err
	}
	return ParseList(doc)
}

func (s *Scraper) FetchDetail(rawURL string) (domain.Ad, error) {
	doc, err := s.fetchAndParse(rawURL)
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
