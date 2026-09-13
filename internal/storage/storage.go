package storage

import (
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/fentezi/olx-scraper/internal/domain"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

//go:embed schema.sql
var schemaSQL string

type Store struct {
	db *sql.DB
}

func Open(dsn string) (*Store, error) {
	db, err := sql.Open("libsql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open libsql: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping libsql: %w", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	// Idempotent column additions. libsql/sqlite errors on duplicate add — ignored.
	for _, stmt := range []string{
		`ALTER TABLE watches ADD COLUMN paused INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE watches ADD COLUMN bootstrapped INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE watches ADD COLUMN local_id INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE watches ADD COLUMN name TEXT`,
		`ALTER TABLE watches ADD COLUMN price_min INTEGER`,
		`ALTER TABLE watches ADD COLUMN price_max INTEGER`,
		`ALTER TABLE watches ADD COLUMN include_kw TEXT`,
		`ALTER TABLE watches ADD COLUMN exclude_kw TEXT`,
	} {
		_, _ = db.Exec(stmt)
	}
	// Backfill local_id for any pre-existing rows. Only touches rows where local_id=0.
	if _, err := db.Exec(`
		UPDATE watches SET local_id = (
		    SELECT COUNT(*) FROM watches w2
		    WHERE w2.user_id = watches.user_id
		      AND (w2.created_at < watches.created_at
		           OR (w2.created_at = watches.created_at AND w2.rowid <= watches.rowid))
		) WHERE local_id = 0
	`); err != nil {
		return nil, fmt.Errorf("backfill local_id: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// AddWatch inserts a watch for the user if not present and returns the local_id.
// If the URL already exists for this user, returns the existing local_id without error.
//
// Deliberately transaction-free. A transaction that reads first and writes later
// holds only a shared lock and must upgrade it on the INSERT; SQLite refuses that
// upgrade with SQLITE_BUSY ("database is locked") as soon as another connection
// has written in between, and it fails immediately rather than waiting, so a
// busy_timeout does not help. The watcher poll loops write via MarkSeen
// continuously, so under any real load that upgrade essentially always loses.
//
// Computing local_id inside the INSERT keeps it correct: SQLite serializes
// writers, so the MAX subquery is evaluated while this statement holds the write
// lock and cannot race a concurrent AddWatch for the same user.
func (s *Store) AddWatch(userID int64, url string) (int, error) {
	if _, err := s.db.Exec(
		`INSERT INTO watches (user_id, url, created_at, local_id)
		 VALUES (?, ?, ?, (SELECT COALESCE(MAX(local_id), 0) + 1 FROM watches WHERE user_id = ?))
		 ON CONFLICT (user_id, url) DO NOTHING`,
		userID, url, time.Now().Unix(), userID,
	); err != nil {
		return 0, err
	}

	// Covers both the row just inserted and a pre-existing one (conflict ignored).
	var localID int
	if err := s.db.QueryRow(
		`SELECT local_id FROM watches WHERE user_id = ? AND url = ?`,
		userID, url,
	).Scan(&localID); err != nil {
		return 0, err
	}
	return localID, nil
}

func (s *Store) RemoveWatchByLocalID(userID int64, localID int) (string, error) {
	var url string
	err := s.db.QueryRow(
		`SELECT url FROM watches WHERE user_id = ? AND local_id = ?`,
		userID, localID,
	).Scan(&url)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if _, err := s.db.Exec(
		`DELETE FROM watches WHERE user_id = ? AND local_id = ?`,
		userID, localID,
	); err != nil {
		return "", err
	}
	return url, nil
}

func scanWatch(row interface{ Scan(...any) error }) (domain.Watch, error) {
	var (
		w         domain.Watch
		name      sql.NullString
		priceMin  sql.NullInt64
		priceMax  sql.NullInt64
		includeKw sql.NullString
		excludeKw sql.NullString
		paused    int
		boot      int
	)
	if err := row.Scan(
		&w.UserID, &w.URL, &w.LocalID, &name, &w.CreatedAt,
		&paused, &boot, &priceMin, &priceMax, &includeKw, &excludeKw,
	); err != nil {
		return domain.Watch{}, err
	}
	w.Name = name.String
	w.Paused = paused == 1
	w.Bootstrapped = boot == 1
	if priceMin.Valid {
		v := int(priceMin.Int64)
		w.PriceMin = &v
	}
	if priceMax.Valid {
		v := int(priceMax.Int64)
		w.PriceMax = &v
	}
	w.IncludeKw = splitKw(includeKw.String)
	w.ExcludeKw = splitKw(excludeKw.String)
	return w, nil
}

const watchColumns = `user_id, url, local_id, name, created_at, paused, bootstrapped, price_min, price_max, include_kw, exclude_kw`

func (s *Store) GetWatchByLocalID(userID int64, localID int) (domain.Watch, error) {
	row := s.db.QueryRow(
		`SELECT `+watchColumns+` FROM watches WHERE user_id = ? AND local_id = ?`,
		userID, localID,
	)
	return scanWatch(row)
}

func (s *Store) GetWatchByURL(userID int64, url string) (domain.Watch, error) {
	row := s.db.QueryRow(
		`SELECT `+watchColumns+` FROM watches WHERE user_id = ? AND url = ?`,
		userID, url,
	)
	return scanWatch(row)
}

func (s *Store) ListWatchesDetailed(userID int64) ([]domain.Watch, error) {
	rows, err := s.db.Query(
		`SELECT `+watchColumns+` FROM watches WHERE user_id = ? ORDER BY local_id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Watch
	for rows.Next() {
		w, err := scanWatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) AllWatches() (map[int64][]string, error) {
	rows, err := s.db.Query(`SELECT user_id, url FROM watches WHERE paused = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64][]string)
	for rows.Next() {
		var uid int64
		var u string
		if err := rows.Scan(&uid, &u); err != nil {
			return nil, err
		}
		out[uid] = append(out[uid], u)
	}
	return out, rows.Err()
}

func (s *Store) MarkSeen(userID int64, adID string) error {
	_, err := s.db.Exec(
		`INSERT INTO seen_ads (user_id, ad_id, seen_at) VALUES (?, ?, ?)
		 ON CONFLICT(user_id, ad_id) DO UPDATE SET seen_at = excluded.seen_at`,
		userID, adID, time.Now().Unix(),
	)
	return err
}

func (s *Store) HasSeen(userID int64, adID string) (bool, error) {
	var one int
	err := s.db.QueryRow(
		`SELECT 1 FROM seen_ads WHERE user_id = ? AND ad_id = ? LIMIT 1`,
		userID, adID,
	).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) PurgeOldSeen(olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan).Unix()
	_, err := s.db.Exec(`DELETE FROM seen_ads WHERE seen_at < ?`, cutoff)
	return err
}

func (s *Store) SetPaused(userID int64, url string, paused bool) error {
	p := 0
	if paused {
		p = 1
	}
	_, err := s.db.Exec(`UPDATE watches SET paused = ? WHERE user_id = ? AND url = ?`, p, userID, url)
	return err
}

func (s *Store) SetPausedByLocalID(userID int64, localID int, paused bool) (string, error) {
	var url string
	err := s.db.QueryRow(
		`SELECT url FROM watches WHERE user_id = ? AND local_id = ?`,
		userID, localID,
	).Scan(&url)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	p := 0
	if paused {
		p = 1
	}
	if _, err := s.db.Exec(
		`UPDATE watches SET paused = ? WHERE user_id = ? AND local_id = ?`,
		p, userID, localID,
	); err != nil {
		return "", err
	}
	return url, nil
}

func (s *Store) PauseAllByUser(userID int64) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT url FROM watches WHERE user_id = ? AND paused = 0`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var urls []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		urls = append(urls, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(urls) == 0 {
		return nil, nil
	}
	if _, err := s.db.Exec(
		`UPDATE watches SET paused = 1 WHERE user_id = ? AND paused = 0`,
		userID,
	); err != nil {
		return nil, err
	}
	return urls, nil
}

func (s *Store) ResumeByLocalID(userID int64, localID int) (string, error) {
	var url string
	var paused int
	err := s.db.QueryRow(
		`SELECT url, paused FROM watches WHERE user_id = ? AND local_id = ?`,
		userID, localID,
	).Scan(&url, &paused)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if _, err := s.db.Exec(
		`UPDATE watches SET paused = 0 WHERE user_id = ? AND local_id = ?`,
		userID, localID,
	); err != nil {
		return "", err
	}
	return url, nil
}

func (s *Store) ResumeAllByUser(userID int64) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT url FROM watches WHERE user_id = ? AND paused = 1`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var urls []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		urls = append(urls, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(urls) == 0 {
		return nil, nil
	}
	if _, err := s.db.Exec(
		`UPDATE watches SET paused = 0 WHERE user_id = ?`,
		userID,
	); err != nil {
		return nil, err
	}
	return urls, nil
}

func (s *Store) IsBootstrapped(userID int64, url string) (bool, error) {
	var b int
	err := s.db.QueryRow(
		`SELECT bootstrapped FROM watches WHERE user_id = ? AND url = ?`,
		userID, url,
	).Scan(&b)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return b == 1, nil
}

func (s *Store) SetBootstrapped(userID int64, url string) error {
	_, err := s.db.Exec(
		`UPDATE watches SET bootstrapped = 1 WHERE user_id = ? AND url = ?`,
		userID, url,
	)
	return err
}

func (s *Store) SetWatchName(userID int64, localID int, name string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE watches SET name = ? WHERE user_id = ? AND local_id = ?`,
		name, userID, localID,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) SetPriceFilter(userID int64, localID int, min, max *int) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE watches SET price_min = ?, price_max = ? WHERE user_id = ? AND local_id = ?`,
		nullableInt(min), nullableInt(max), userID, localID,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) SetIncludeFilter(userID int64, localID int, words []string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE watches SET include_kw = ? WHERE user_id = ? AND local_id = ?`,
		joinKw(words), userID, localID,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) SetExcludeFilter(userID int64, localID int, words []string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE watches SET exclude_kw = ? WHERE user_id = ? AND local_id = ?`,
		joinKw(words), userID, localID,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) ClearFilters(userID int64, localID int) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE watches SET price_min = NULL, price_max = NULL,
		                    include_kw = NULL, exclude_kw = NULL
		 WHERE user_id = ? AND local_id = ?`,
		userID, localID,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) EnsureUser(userID int64, defaultLang string) error {
	_, err := s.db.Exec(
		`INSERT INTO users (user_id, language, created_at) VALUES (?, ?, ?)
		 ON CONFLICT(user_id) DO NOTHING`,
		userID, defaultLang, time.Now().Unix(),
	)
	return err
}

func (s *Store) GetUserLanguage(userID int64) (string, error) {
	var lang string
	err := s.db.QueryRow(
		`SELECT language FROM users WHERE user_id = ?`,
		userID,
	).Scan(&lang)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return lang, err
}

func (s *Store) SetUserLanguage(userID int64, lang string) error {
	_, err := s.db.Exec(
		`INSERT INTO users (user_id, language, created_at) VALUES (?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET language = excluded.language`,
		userID, lang, time.Now().Unix(),
	)
	return err
}

func splitKw(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func joinKw(words []string) any {
	if len(words) == 0 {
		return nil
	}
	return strings.Join(words, ",")
}

func nullableInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}
