package scraper

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type stubRT struct {
	status     int
	retryAfter string
}

func (s *stubRT) RoundTrip(req *http.Request) (*http.Response, error) {
	hdr := http.Header{}
	if s.retryAfter != "" {
		hdr.Set("Retry-After", s.retryAfter)
	}
	return &http.Response{
		StatusCode: s.status,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     hdr,
	}, nil
}

func newStubScraper(rt http.RoundTripper) *Scraper {
	return &Scraper{HTTPClient: &http.Client{Transport: rt}}
}

func TestFetchHTML_RateLimitedSeconds(t *testing.T) {
	s := newStubScraper(&stubRT{status: 429, retryAfter: "7"})

	_, err := s.fetchHTML("http://example.invalid/")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("expected *RateLimitError, got %T: %v", err, err)
	}
	if rl.RetryAfter != 7*time.Second {
		t.Fatalf("expected 7s, got %s", rl.RetryAfter)
	}
	if rl.Status != 429 {
		t.Fatalf("expected status 429, got %d", rl.Status)
	}
}

func TestFetchHTML_RateLimitedDefaults(t *testing.T) {
	s := newStubScraper(&stubRT{status: 503})

	_, err := s.fetchHTML("http://example.invalid/")
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("expected *RateLimitError, got %v", err)
	}
	if rl.RetryAfter != 60*time.Second {
		t.Fatalf("expected default 60s, got %s", rl.RetryAfter)
	}
}

func TestFetchHTML_OtherStatusReturnsHTTPError(t *testing.T) {
	s := newStubScraper(&stubRT{status: 403})

	_, err := s.fetchHTML("http://example.invalid/")
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("expected *HTTPError, got %v", err)
	}
	if he.Status != 403 {
		t.Fatalf("expected 403, got %d", he.Status)
	}
}
