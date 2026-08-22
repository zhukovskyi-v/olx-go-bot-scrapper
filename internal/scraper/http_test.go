package scraper

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	http "github.com/bogdanfinn/fhttp"
)

// stubClient answers each request from responses in order, so a test can script
// a transport failure followed by a success.
type stubClient struct {
	responses []stubResponse
	requests  []*http.Request
}

type stubResponse struct {
	status     int
	retryAfter string
	body       string
	err        error
}

func (c *stubClient) Do(req *http.Request) (*http.Response, error) {
	c.requests = append(c.requests, req)
	r := c.responses[min(len(c.requests)-1, len(c.responses)-1)]
	if r.err != nil {
		return nil, r.err
	}
	hdr := http.Header{}
	if r.retryAfter != "" {
		hdr.Set("Retry-After", r.retryAfter)
	}
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     hdr,
	}, nil
}

func newStubScraper(responses ...stubResponse) (*Scraper, *stubClient) {
	c := &stubClient{responses: responses}
	return &Scraper{httpClient: c}, c
}

// shortenBackoff keeps retry tests from actually sleeping seconds.
func shortenBackoff(t *testing.T) {
	t.Helper()
	original := transportBackoff
	transportBackoff = []time.Duration{time.Millisecond}
	t.Cleanup(func() { transportBackoff = original })
}

type timeoutError struct{}

func (timeoutError) Error() string { return "i/o timeout" }
func (timeoutError) Timeout() bool { return true }

func TestFetchHTML_RateLimitedSeconds(t *testing.T) {
	s, _ := newStubScraper(stubResponse{status: 429, retryAfter: "7"})

	_, err := s.fetchHTML(context.Background(), "http://example.invalid/", "")
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
	if StatusOf(err) != 429 {
		t.Fatalf("StatusOf: expected 429, got %d", StatusOf(err))
	}
}

func TestFetchHTML_RateLimitedDefaults(t *testing.T) {
	s, _ := newStubScraper(stubResponse{status: 503})

	_, err := s.fetchHTML(context.Background(), "http://example.invalid/", "")
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("expected *RateLimitError, got %v", err)
	}
	if rl.RetryAfter != 60*time.Second {
		t.Fatalf("expected default 60s, got %s", rl.RetryAfter)
	}
}

func TestFetchHTML_OtherStatusReturnsHTTPError(t *testing.T) {
	s, _ := newStubScraper(stubResponse{status: 403, body: "  <html>Access Denied</html>  "})

	_, err := s.fetchHTML(context.Background(), "http://example.invalid/", "")
	var he *HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("expected *HTTPError, got %v", err)
	}
	if he.Status != 403 {
		t.Fatalf("expected 403, got %d", he.Status)
	}
	if got := BodySnippetOf(err); got != "<html>Access Denied</html>" {
		t.Fatalf("expected trimmed body snippet, got %q", got)
	}
}

func TestFetchHTML_BodySnippetIsBounded(t *testing.T) {
	s, _ := newStubScraper(stubResponse{status: 403, body: strings.Repeat("x", maxSnippetBytes*3)})

	_, err := s.fetchHTML(context.Background(), "http://example.invalid/", "")
	if got := len(BodySnippetOf(err)); got != maxSnippetBytes {
		t.Fatalf("expected snippet capped at %d bytes, got %d", maxSnippetBytes, got)
	}
}

func TestFetchHTML_RetriesTransportFailure(t *testing.T) {
	shortenBackoff(t)
	s, c := newStubScraper(
		stubResponse{err: errors.New("connection reset")},
		stubResponse{status: 200, body: "ok"},
	)

	body, err := s.fetchHTML(context.Background(), "http://example.invalid/", "")
	if err != nil {
		t.Fatalf("expected the retry to succeed, got %v", err)
	}
	defer body.Close()
	if len(c.requests) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(c.requests))
	}
}

func TestFetchHTML_GivesUpAfterBackoffExhausted(t *testing.T) {
	shortenBackoff(t)
	s, c := newStubScraper(stubResponse{err: timeoutError{}})

	_, err := s.fetchHTML(context.Background(), "http://example.invalid/", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(c.requests) != len(transportBackoff)+1 {
		t.Fatalf("expected %d attempts, got %d", len(transportBackoff)+1, len(c.requests))
	}
	if !IsTimeout(err) {
		t.Fatalf("expected a timeout error, got %v", err)
	}
	if StatusOf(err) != 0 {
		t.Fatalf("a transport failure carries no status, got %d", StatusOf(err))
	}
}

func TestFetchHTML_CancelledContextDoesNotRetry(t *testing.T) {
	shortenBackoff(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s, c := newStubScraper(stubResponse{err: context.Canceled})

	if _, err := s.fetchHTML(ctx, "http://example.invalid/", ""); err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(c.requests) != 1 {
		t.Fatalf("expected a single attempt, got %d", len(c.requests))
	}
}

func TestFetchHTML_BrowserHeaders(t *testing.T) {
	s, c := newStubScraper(stubResponse{status: 200}, stubResponse{status: 200})

	body, err := s.fetchHTML(context.Background(), "http://example.invalid/", "")
	if err != nil {
		t.Fatal(err)
	}
	body.Close()

	// Read the map directly rather than through Get: the headers are written
	// lowercase on purpose so they match the order key, and Get canonicalises.
	first := c.requests[0].Header
	if got := first["user-agent"]; len(got) != 1 || got[0] != browserUserAgent {
		t.Fatalf("user-agent: got %v", got)
	}
	if got := first["sec-fetch-site"]; len(got) != 1 || got[0] != "none" {
		t.Fatalf("no referer should mean sec-fetch-site none, got %v", got)
	}
	if _, ok := first["referer"]; ok {
		t.Fatal("no referer header should be sent when there is no referer")
	}
	if len(first[http.HeaderOrderKey]) == 0 {
		t.Fatal("expected a header order to be set")
	}

	const referer = "https://www.olx.ua/d/uk/list/"
	body, err = s.fetchHTML(context.Background(), "http://example.invalid/ad", referer)
	if err != nil {
		t.Fatal(err)
	}
	body.Close()

	second := c.requests[1].Header
	if got := second["referer"]; len(got) != 1 || got[0] != referer {
		t.Fatalf("referer: got %v", got)
	}
	if got := second["sec-fetch-site"]; len(got) != 1 || got[0] != "same-origin" {
		t.Fatalf("a referer should mean sec-fetch-site same-origin, got %v", got)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter(""); got != 0 {
		t.Fatalf("empty header: got %s", got)
	}
	if got := parseRetryAfter("nonsense"); got != 0 {
		t.Fatalf("unparseable header: got %s", got)
	}
	if got := parseRetryAfter("12"); got != 12*time.Second {
		t.Fatalf("seconds form: got %s", got)
	}
}
