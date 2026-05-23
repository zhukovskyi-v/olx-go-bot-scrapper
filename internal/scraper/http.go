package scraper

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type RateLimitError struct {
	RetryAfter time.Duration
	Status     int
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited (status %d), retry after %s", e.Status, e.RetryAfter)
}

type HTTPError struct {
	Status int
	URL    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http %d for %s", e.Status, e.URL)
}

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

func parseRetryAfter(h string) time.Duration {
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}

func (s *Scraper) fetchHTML(url string) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "uk-UA,uk;q=0.9")

	resp, err := s.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}

	if resp.StatusCode == http.StatusOK {
		return resp.Body, nil
	}

	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		retry := parseRetryAfter(resp.Header.Get("Retry-After"))
		if retry <= 0 {
			retry = 60 * time.Second
		}
		return nil, &RateLimitError{RetryAfter: retry, Status: resp.StatusCode}
	}
	return nil, &HTTPError{Status: resp.StatusCode, URL: url}
}
