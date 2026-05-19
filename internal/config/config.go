package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Settings struct {
	DBHost        string
	DBPort        int
	DBName        string
	DBUser        string
	DBPassword    string
	DatabaseDSN   string
	ClientID      string
	ClientSecret  string
	AccessToken   string
	RefreshToken  string
	TokenPath     string
	TokenStorage  string
	EncryptionKey string
	ScrapeDays    int
}

func LoadDotenv() error {
	return godotenv.Load()
}

func Load() Settings {
	return Settings{
		DBHost:        env("WHOOP_DB_HOST", "localhost"),
		DBPort:        envInt("WHOOP_DB_PORT", 5432),
		DBName:        env("WHOOP_DB_NAME", "health"),
		DBUser:        env("WHOOP_DB_USER", "health"),
		DBPassword:    env("WHOOP_DB_PASSWORD", ""),
		DatabaseDSN:   os.Getenv("WHOOP_DATABASE_URL"),
		ClientID:      os.Getenv("WHOOP_CLIENT_ID"),
		ClientSecret:  os.Getenv("WHOOP_CLIENT_SECRET"),
		AccessToken:   os.Getenv("WHOOP_ACCESS_TOKEN"),
		RefreshToken:  os.Getenv("WHOOP_REFRESH_TOKEN"),
		TokenPath:     env("WHOOP_TOKEN_PATH", defaultTokenPath()),
		TokenStorage:  strings.ToLower(env("WHOOP_TOKEN_STORAGE", "db")),
		EncryptionKey: os.Getenv("WHOOP_ENCRYPTION_KEY"),
		ScrapeDays:    envInt("WHOOP_SCRAPE_DAYS", 7),
	}
}

func (s Settings) DatabaseURL() string {
	if s.DatabaseDSN != "" {
		return s.DatabaseDSN
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(s.DBUser, s.DBPassword),
		Host:   fmt.Sprintf("%s:%d", s.DBHost, s.DBPort),
		Path:   s.DBName,
	}
	return u.String()
}

func (s Settings) SafeDatabaseTarget() string {
	if s.DatabaseDSN != "" {
		u, err := url.Parse(s.DatabaseDSN)
		if err == nil {
			u.User = url.User(u.User.Username())
			return u.String()
		}
		return "<WHOOP_DATABASE_URL>"
	}
	return fmt.Sprintf("%s:%d/%s", s.DBHost, s.DBPort, s.DBName)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func defaultTokenPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "tokens.json"
	}
	return filepath.Join(home, ".config", "whoop-scraper", "tokens.json")
}
