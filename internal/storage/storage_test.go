package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// newTestStore opens a Store for tests.
//
// Without LIBSQL_TEST_DSN it falls back to a file: DSN, which this binary cannot
// serve — no sqlite driver is linked in (CGO is off) — so the whole suite skips.
// Point LIBSQL_TEST_DSN at a *freshly started* libsql server to actually run it;
// the tests share one database and use fixed user ids, so a reused server carries
// rows over and breaks the local_id assertions:
//
//	docker run --rm -d -p 8080:8080 ghcr.io/tursodatabase/libsql-server:latest
//	LIBSQL_TEST_DSN=http://localhost:8080 go test ./internal/storage
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("LIBSQL_TEST_DSN")
	if dsn == "" {
		dsn = "file://" + filepath.Join(t.TempDir(), "test.db")
	}
	s, err := Open(dsn)
	if err != nil {
		t.Skipf("libsql unavailable (set LIBSQL_TEST_DSN to run this suite): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func ptr(v int) *int { return &v }

func TestAddWatch_AssignsMonotonicLocalIDs(t *testing.T) {
	s := newTestStore(t)
	uid := int64(42)

	for i, url := range []string{"https://olx.ua/a", "https://olx.ua/b", "https://olx.ua/c"} {
		got, err := s.AddWatch(uid, url)
		if err != nil {
			t.Fatalf("AddWatch[%d]: %v", i, err)
		}
		if got != i+1 {
			t.Fatalf("AddWatch[%d] localID = %d, want %d", i, got, i+1)
		}
	}

	if _, err := s.RemoveWatchByLocalID(uid, 2); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	next, err := s.AddWatch(uid, "https://olx.ua/d")
	if err != nil {
		t.Fatalf("AddWatch after remove: %v", err)
	}
	if next != 4 {
		t.Fatalf("expected localID 4 (no reuse), got %d", next)
	}
}

func TestAddWatch_Idempotent(t *testing.T) {
	s := newTestStore(t)
	uid := int64(1)
	first, err := s.AddWatch(uid, "https://olx.ua/x")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AddWatch(uid, "https://olx.ua/x")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("duplicate add should return same localID; got %d vs %d", first, second)
	}
}

func TestRemoveByLocalID_ReturnsURL(t *testing.T) {
	s := newTestStore(t)
	uid := int64(7)
	_, _ = s.AddWatch(uid, "https://olx.ua/z")
	url, err := s.RemoveWatchByLocalID(uid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://olx.ua/z" {
		t.Fatalf("expected URL z, got %q", url)
	}
	url, err = s.RemoveWatchByLocalID(uid, 99)
	if err != nil {
		t.Fatal(err)
	}
	if url != "" {
		t.Fatalf("expected empty URL for missing ID, got %q", url)
	}
}

func TestFilterRoundTrip(t *testing.T) {
	s := newTestStore(t)
	uid := int64(3)
	_, _ = s.AddWatch(uid, "https://olx.ua/q")

	if ok, err := s.SetPriceFilter(uid, 1, ptr(30000), ptr(60000)); err != nil || !ok {
		t.Fatalf("SetPriceFilter ok=%v err=%v", ok, err)
	}
	if ok, err := s.SetIncludeFilter(uid, 1, []string{"kitchen", "balcony"}); err != nil || !ok {
		t.Fatalf("SetIncludeFilter ok=%v err=%v", ok, err)
	}
	if ok, err := s.SetExcludeFilter(uid, 1, []string{"studio"}); err != nil || !ok {
		t.Fatalf("SetExcludeFilter ok=%v err=%v", ok, err)
	}

	w, err := s.GetWatchByLocalID(uid, 1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if w.PriceMin == nil || *w.PriceMin != 30000 {
		t.Fatalf("PriceMin = %v", w.PriceMin)
	}
	if w.PriceMax == nil || *w.PriceMax != 60000 {
		t.Fatalf("PriceMax = %v", w.PriceMax)
	}
	if len(w.IncludeKw) != 2 || w.IncludeKw[0] != "kitchen" || w.IncludeKw[1] != "balcony" {
		t.Fatalf("IncludeKw = %v", w.IncludeKw)
	}
	if len(w.ExcludeKw) != 1 || w.ExcludeKw[0] != "studio" {
		t.Fatalf("ExcludeKw = %v", w.ExcludeKw)
	}

	if ok, err := s.ClearFilters(uid, 1); err != nil || !ok {
		t.Fatalf("ClearFilters ok=%v err=%v", ok, err)
	}
	w, _ = s.GetWatchByLocalID(uid, 1)
	if w.PriceMin != nil || w.PriceMax != nil || len(w.IncludeKw) != 0 || len(w.ExcludeKw) != 0 {
		t.Fatalf("filters not cleared: %+v", w)
	}
}

func TestUserLanguage(t *testing.T) {
	s := newTestStore(t)
	uid := int64(55)

	lang, err := s.GetUserLanguage(uid)
	if err != nil {
		t.Fatal(err)
	}
	if lang != "" {
		t.Fatalf("unknown user should return empty, got %q", lang)
	}

	if err := s.EnsureUser(uid, "uk"); err != nil {
		t.Fatal(err)
	}
	lang, _ = s.GetUserLanguage(uid)
	if lang != "uk" {
		t.Fatalf("expected uk, got %q", lang)
	}

	if err := s.EnsureUser(uid, "en"); err != nil {
		t.Fatal(err)
	}
	lang, _ = s.GetUserLanguage(uid)
	if lang != "uk" {
		t.Fatalf("EnsureUser should not overwrite; got %q", lang)
	}

	if err := s.SetUserLanguage(uid, "pl"); err != nil {
		t.Fatal(err)
	}
	lang, _ = s.GetUserLanguage(uid)
	if lang != "pl" {
		t.Fatalf("expected pl, got %q", lang)
	}
}

func TestPauseResumeByLocalID(t *testing.T) {
	s := newTestStore(t)
	uid := int64(9)
	_, _ = s.AddWatch(uid, "https://olx.ua/m")

	url, err := s.SetPausedByLocalID(uid, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://olx.ua/m" {
		t.Fatalf("expected URL, got %q", url)
	}
	w, _ := s.GetWatchByLocalID(uid, 1)
	if !w.Paused {
		t.Fatal("expected paused=true")
	}

	url, err = s.ResumeByLocalID(uid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://olx.ua/m" {
		t.Fatalf("expected URL, got %q", url)
	}
	w, _ = s.GetWatchByLocalID(uid, 1)
	if w.Paused {
		t.Fatal("expected paused=false")
	}
}

func TestPauseAllResumeAll(t *testing.T) {
	s := newTestStore(t)
	uid := int64(11)
	_, _ = s.AddWatch(uid, "https://olx.ua/1")
	_, _ = s.AddWatch(uid, "https://olx.ua/2")

	urls, err := s.PauseAllByUser(uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 2 {
		t.Fatalf("expected 2 paused, got %d", len(urls))
	}

	urls, err = s.ResumeAllByUser(uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 2 {
		t.Fatalf("expected 2 resumed, got %d", len(urls))
	}
}

// Regression: AddWatch used to run a read-then-write transaction, whose lock
// upgrade SQLite rejects outright with "database is locked" once any other
// connection has written. The watcher poll loops write via MarkSeen constantly,
// so /addurl failed for every user with an active watch.
func TestAddWatch_SucceedsWhilePollLoopsWrite(t *testing.T) {
	s := newTestStore(t)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = s.MarkSeen(int64(900000+n), fmt.Sprintf("ad-%d-%d", n, time.Now().UnixNano()))
			}
		}(i)
	}
	t.Cleanup(func() { close(stop); wg.Wait() })
	time.Sleep(200 * time.Millisecond)

	for i := 0; i < 20; i++ {
		if _, err := s.AddWatch(int64(800000+i), fmt.Sprintf("https://www.olx.ua/uk/list/%d", i)); err != nil {
			t.Fatalf("AddWatch #%d under concurrent writes: %v", i, err)
		}
	}
}
