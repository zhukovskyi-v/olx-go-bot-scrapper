package config

import (
	"errors"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Token string
	DBURL string
	Env   string
}

// Load reads .env (if present) then resolves required env vars.
// Returns an error if TOKEN or DB_URL is missing.
func Load() (*Config, error) {
	_ = godotenv.Load()

	c := &Config{
		Token: os.Getenv("TOKEN"),
		DBURL: os.Getenv("DB_URL"),
		Env:   os.Getenv("ENV"),
	}
	if c.Token == "" {
		return nil, errors.New("missing TOKEN")
	}
	if c.DBURL == "" {
		return nil, errors.New("missing DB_URL (libsql://...?authToken=... or file:./olx.db)")
	}
	return c, nil
}
