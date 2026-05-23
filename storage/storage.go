package storage

import (
	"database/sql"
	_ "embed"
	"fmt"
	"time"

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
	_, _ = db.Exec("ALTER TABLE watches ADD COLUMN paused INTEGER NOT NULL DEFAULT 0")
	_, _ = db.Exec("ALTER TABLE watches ADD COLUMN bootstrapped INTEGER NOT NULL DEFAULT 0")
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) AddWatch(userID int64, url string) error {
	_, err := s.db.Exec(
		`INSERT INTO watches (user_id, url, created_at) VALUES (?, ?, ?)
		 ON CONFLICT(user_id, url) DO NOTHING`,
		userID, url, time.Now().Unix(),
	)
	return err
}

func (s *Store) RemoveWatch(userID int64, url string) error {
	_, err := s.db.Exec(`DELETE FROM watches WHERE user_id = ? AND url = ?`, userID, url)
	return err
}

func (s *Store) ListWatches(userID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT url FROM watches WHERE user_id = ? AND paused = 0`, userID)
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
	return urls, rows.Err()
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

func (s *Store) ResumeWatch(userID int64) ([]string, error) {
	if _, err := s.db.Exec(`UPDATE watches SET paused = 0 WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	return s.ListWatches(userID)
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
