package scraper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

const browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"

// maxSnippetBytes bounds how much of a failing response body is kept for logs.
const maxSnippetBytes = 256

// transportBackoff holds the wait before each retry of a transport-level
// failure; its length is the number of retries. Deliberately one: OLX sits
// behind a CloudFront WAF that answers 403 when it decides to throttle, so
// extra attempts add load to the edge that is already refusing us. One retry
// covers a genuine blip; anything beyond that fights the rate limiter. It is a
// var so tests can shorten it.
var transportBackoff = []time.Duration{2 * time.Second}

type RateLimitError struct {
	RetryAfter time.Duration
	Status     int
	URL        string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited (status %d) for %s, retry after %s", e.Status, e.URL, e.RetryAfter)
}

type HTTPError struct {
	Status int
	URL    string
	// BodySnippet holds the first bytes of the failing response. A 403 from an
	// anti-bot page and a 403 from a blocked IP are the same status code but
	// very different problems, and the body is the only way to tell them apart.
	BodySnippet string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http %d for %s", e.Status, e.URL)
}

// StatusOf reports the HTTP status carried by err, or 0 when err is not an
// HTTP failure. Callers use it to put the status code in a log attribute.
func StatusOf(err error) int {
	var httpError *HTTPError
	if errors.As(err, &httpError) {
		return httpError.Status
	}
	var rateLimit *RateLimitError
	if errors.As(err, &rateLimit) {
		return rateLimit.Status
	}
	return 0
}

// IsTimeout reports whether err is a transport timeout rather than an HTTP
// response failure. Callers use it to level a routine network blip below a
// block or a parser break. The check is on the Timeout method rather than on
// net.Error so it holds for whichever error type fhttp returns.
func IsTimeout(err error) bool {
	var timeouter interface{ Timeout() bool }
	return errors.As(err, &timeouter) && timeouter.Timeout()
}

// doWithRetry sends a request, retrying transport-level failures with a
// jittered backoff. Only the error path retries: a transport failure carries no
// response, so there is no body to leak between attempts. Response failures
// (HTTPError, RateLimitError) never reach here — they are the caller's business.
//
// It is generic because the two scrapers speak to different clients: the
// browser one returns fhttp responses, the proxy one stdlib responses.
func doWithRetry[R any](ctx context.Context, send func() (*R, error)) (*R, int, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		resp, err := send()
		if err == nil {
			return resp, attempt + 1, nil
		}
		lastErr = err
		if attempt >= len(transportBackoff) || !retryableTransport(ctx, err) {
			return nil, attempt + 1, lastErr
		}
		if !sleepContext(ctx, jitter(transportBackoff[attempt])) {
			return nil, attempt + 1, lastErr
		}
	}
}

// retryableTransport reports whether a transport failure is worth another try.
// A caller's context ending is not: cancellation means shutdown and a caller
// deadline means the caller already decided how long it would wait. A client
// timeout is, and it is distinguishable because it does not wrap
// context.DeadlineExceeded.
func retryableTransport(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// jitter spreads a backoff by ±25% so parallel workers that failed together do
// not retry in lockstep.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	spread := int64(d) / 2
	return d - time.Duration(spread/2) + time.Duration(rand.Int64N(spread+1))
}

// sleepContext waits for d, reporting false if ctx ended first.
func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// BodySnippetOf returns the captured response body for HTTP failures, if any.
func BodySnippetOf(err error) string {
	var httpError *HTTPError
	if errors.As(err, &httpError) {
		return httpError.BodySnippet
	}
	return ""
}

// bodySnippet reads at most maxSnippetBytes from body for diagnostics. Read
// errors are intentionally ignored: this is best-effort context attached to an
// error that is already being returned.
func bodySnippet(body io.Reader) string {
	buf := make([]byte, maxSnippetBytes)
	n, _ := io.ReadFull(io.LimitReader(body, maxSnippetBytes), buf)
	return strings.TrimSpace(string(buf[:n]))
}

type browserHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

func newBrowserHTTPClient() (browserHTTPClient, error) {
	idleTimeout := 90 * time.Second
	client, err := tlsclient.NewHttpClient(
		tlsclient.NewNoopLogger(),
		tlsclient.WithTimeoutSeconds(20),
		tlsclient.WithClientProfile(profiles.Chrome_146),
		tlsclient.WithCookieJar(tlsclient.NewCookieJar()),
		tlsclient.WithTransportOptions(&tlsclient.TransportOptions{
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     &idleTimeout,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("create browser HTTP client: %w", err)
	}
	return client, nil
}

func defaultProxyHTTPClient() *stdhttp.Client {
	transport := stdhttp.DefaultTransport.(*stdhttp.Transport).Clone()
	transport.MaxIdleConnsPerHost = 4
	transport.IdleConnTimeout = 90 * time.Second

	return &stdhttp.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
	}
}

func parseRetryAfter(h string) time.Duration {
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := stdhttp.ParseTime(h); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}

func (s *Scraper) fetchHTML(ctx context.Context, url, referer string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create OLX request: %w", err)
	}

	setBrowserHeaders(req, referer)

	// The request is a GET with no body, so resending it is safe.
	resp, attempts, err := doWithRetry(ctx, func() (*http.Response, error) {
		return s.httpClient.Do(req)
	})
	if err != nil {
		return nil, fmt.Errorf("send OLX request (%d attempts): %w", attempts, err)
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
		return nil, &RateLimitError{RetryAfter: retry, Status: resp.StatusCode, URL: url}
	}
	return nil, &HTTPError{
		Status:      resp.StatusCode,
		URL:         url,
		BodySnippet: bodySnippet(resp.Body),
	}
}

// setBrowserHeaders writes the navigation headers Chrome sends for a top-level
// document request. referer is the page the link was clicked on; empty means the
// URL was opened directly, which is what a bare search URL looks like.
//
// Two things here are deliberate rather than cosmetic. There is no
// cache-control: Chrome only sends max-age=0 on an explicit reload, so sending
// it everywhere made every request look like a forced refresh. And sec-fetch-site
// tracks the referer: a detail page reached from a search result is same-origin,
// and claiming "none" while the URL carries OLX's own search_reason parameter is
// self-contradictory.
func setBrowserHeaders(req *http.Request, referer string) {
	fetchSite := "none"
	if referer != "" {
		fetchSite = "same-origin"
	}
	req.Header = http.Header{
		"sec-ch-ua":                 {`"Not_A Brand";v="99", "Chromium";v="146", "Google Chrome";v="146"`},
		"sec-ch-ua-mobile":          {"?0"},
		"sec-ch-ua-platform":        {`"Windows"`},
		"upgrade-insecure-requests": {"1"},
		"user-agent":                {browserUserAgent},
		"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"},
		// zstd has been in Chrome's navigation Accept-Encoding since 123, and the
		// TLS fingerprint here claims 146. fhttp decodes it (http2/../transport.go).
		"accept-encoding": {"gzip, deflate, br, zstd"},
		"accept-language": {"uk-UA,uk;q=0.9,en;q=0.8"},
		"sec-fetch-site":  {fetchSite},
		"sec-fetch-mode":  {"navigate"},
		"sec-fetch-user":  {"?1"},
		"sec-fetch-dest":  {"document"},
		"priority":        {"u=0, i"},
		http.HeaderOrderKey: {
			"sec-ch-ua",
			"sec-ch-ua-mobile",
			"sec-ch-ua-platform",
			"upgrade-insecure-requests",
			"user-agent",
			"accept",
			"sec-fetch-site",
			"sec-fetch-mode",
			"sec-fetch-user",
			"sec-fetch-dest",
			"referer",
			"accept-encoding",
			"accept-language",
			"priority",
		},
	}
	if referer != "" {
		// Assigned directly rather than via Set: Set canonicalises to "Referer",
		// which would not match the lowercase key in the header order above.
		req.Header["referer"] = []string{referer}
	}
}
