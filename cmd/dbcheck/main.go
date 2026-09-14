// Command dbcheck probes the database behind DB_URL and reports which layer
// breaks: connectivity, reads, the local_id column, or writes.
//
// It exists because every storage failure surfaces in Telegram as the same
// opaque "could not save" line, which cannot distinguish an expired token from
// a database that has gone read-only.
//
// Run it with the same DB_URL the bot uses (picked up from the shell or the
// local dotenv file, exactly like the bot does):
//
//	go run ./cmd/dbcheck
//
// The write probe inserts one row under a sentinel user_id that no Telegram
// account can have (real ids are positive) and deletes it again, so it never
// touches real data.
package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

const probeUser = -999

func main() {
	_ = godotenv.Load()
	dsn := os.Getenv("DB_URL")
	if dsn == "" {
		fmt.Println("DB_URL is not set")
		os.Exit(1)
	}
	// Print only the scheme: the DSN carries the auth token.
	scheme, _, _ := strings.Cut(dsn, "://")
	fmt.Printf("DB_URL scheme: %s://...\n\n", scheme)

	db, err := sql.Open("libsql", dsn)
	if err != nil {
		fail("open", err)
	}
	defer db.Close()

	step("connect", func() error { return db.Ping() })

	step("read: SELECT 1", func() error {
		var n int
		return db.QueryRow(`SELECT 1`).Scan(&n)
	})

	step("read: watches table", func() error {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM watches`).Scan(&n); err != nil {
			return err
		}
		fmt.Printf("       (%d rows)\n", n)
		return nil
	})

	step("read: local_id column exists", func() error {
		var n int
		return db.QueryRow(`SELECT COUNT(local_id) FROM watches`).Scan(&n)
	})

	step("WRITE: insert probe row", func() error {
		_, err := db.Exec(
			`INSERT INTO watches (user_id, url, created_at, local_id)
			 VALUES (?, ?, ?, 1) ON CONFLICT (user_id, url) DO NOTHING`,
			probeUser, "https://example.invalid/dbcheck", time.Now().Unix(),
		)
		return err
	})

	step("WRITE: delete probe row", func() error {
		_, err := db.Exec(`DELETE FROM watches WHERE user_id = ?`, probeUser)
		return err
	})

	fmt.Println("\nAll checks passed: the database accepts reads and writes.")
}

func step(name string, fn func() error) {
	if err := fn(); err != nil {
		fail(name, err)
	}
	fmt.Printf("  OK   %s\n", name)
}

func fail(name string, err error) {
	fmt.Printf("  FAIL %s\n\n       %v\n", name, err)
	os.Exit(1)
}
