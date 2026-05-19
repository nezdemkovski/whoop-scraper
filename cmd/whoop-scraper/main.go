package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"whoop-scraper/internal/auth"
	"whoop-scraper/internal/config"
	"whoop-scraper/internal/scraper"
	"whoop-scraper/internal/store"
	"whoop-scraper/internal/whoop"
)

const version = "0.1.0"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx := context.Background()

	if err := config.LoadDotenv(); err != nil {
		logger.Debug("failed to load .env", "error", err)
	}

	cfg := config.Load()
	if len(os.Args) < 2 {
		usage()
		return
	}

	var err error
	switch os.Args[1] {
	case "--version", "version":
		fmt.Printf("whoop-scraper %s\n", version)
	case "auth":
		err = cmdAuth(ctx, logger, cfg, os.Args[2:])
	case "test-api":
		err = cmdTestAPI(ctx, cfg)
	case "init-db":
		err = cmdInitDB(ctx, logger, cfg, os.Args[2:])
	case "scrape":
		err = cmdScrape(ctx, logger, cfg, os.Args[2:])
	default:
		usage()
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}

	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`Usage:
  whoop-scraper auth [--status] [--refresh] [--port 8080] [--no-browser]
  whoop-scraper test-api
  whoop-scraper init-db [--print-sql]
  whoop-scraper scrape [--days 7] [--start-date YYYY-MM-DD] [--end-date YYYY-MM-DD]
  whoop-scraper version

Environment:
  WHOOP_CLIENT_ID, WHOOP_CLIENT_SECRET
  WHOOP_DATABASE_URL or WHOOP_DB_HOST/PORT/NAME/USER/PASSWORD/SCHEMA
  WHOOP_ACCESS_TOKEN, WHOOP_REFRESH_TOKEN for first database bootstrap
  WHOOP_TOKEN_STORAGE=db|file, WHOOP_ENCRYPTION_KEY, WHOOP_SCRAPE_DAYS`)
}

func cmdInitDB(ctx context.Context, logger *slog.Logger, cfg config.Settings, args []string) error {
	fs := flag.NewFlagSet("init-db", flag.ExitOnError)
	printSQL := fs.Bool("print-sql", false, "print SQL schema without executing it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *printSQL {
		fmt.Print(store.SchemaSQL)
		return nil
	}

	db, err := store.Open(ctx, cfg.DatabaseURL(), cfg.DBSchema)
	if err != nil {
		return err
	}
	defer db.Close()

	logger.Info("initializing database schema", "dsn", cfg.SafeDatabaseTarget(), "schema", cfg.DBSchema)
	return store.InitSchema(ctx, db)
}

func cmdAuth(ctx context.Context, logger *slog.Logger, cfg config.Settings, args []string) error {
	fs := flag.NewFlagSet("auth", flag.ExitOnError)
	status := fs.Bool("status", false, "show current token status")
	refresh := fs.Bool("refresh", false, "force refresh tokens")
	port := fs.Int("port", 8080, "local OAuth callback port")
	noBrowser := fs.Bool("no-browser", false, "print authorization URL without opening a browser")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return errors.New("WHOOP_CLIENT_ID and WHOOP_CLIENT_SECRET must be set")
	}

	storage, cleanup, err := tokenStorage(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	oauth := auth.New(cfg.ClientID, cfg.ClientSecret, storage, logger)
	if *status {
		tokens, err := storage.Load(ctx)
		if err != nil {
			return err
		}
		if tokens == nil {
			return errors.New("no tokens stored; run 'whoop-scraper auth'")
		}
		fmt.Printf("Access token: %s...\n", prefix(tokens.AccessToken, 20))
		fmt.Printf("Expires at: %s\n", tokens.ExpiresAt.Format(time.RFC3339))
		fmt.Printf("Expired: %t\n", tokens.Expired())
		return nil
	}
	if *refresh {
		tokens, err := oauth.Refresh(ctx, "")
		if err != nil {
			return err
		}
		fmt.Println("Tokens refreshed successfully.")
		fmt.Printf("New access token: %s...\n", prefix(tokens.AccessToken, 20))
		fmt.Printf("Expires at: %s\n", tokens.ExpiresAt.Format(time.RFC3339))
		return nil
	}

	tokens, err := oauth.AuthorizeInteractive(ctx, *port, !*noBrowser)
	if err != nil {
		return err
	}
	fmt.Println("Authorization successful.")
	fmt.Printf("Access token: %s...\n", prefix(tokens.AccessToken, 20))
	return nil
}

func cmdTestAPI(ctx context.Context, cfg config.Settings) error {
	storage, cleanup, err := tokenStorage(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	client := whoop.NewClient(auth.New(cfg.ClientID, cfg.ClientSecret, storage, nil), nil)
	profile, err := client.UserProfile(ctx)
	if err != nil {
		if errors.Is(err, auth.ErrNoTokens) {
			return errors.New("no valid token; run 'whoop-scraper auth' first")
		}
		return err
	}
	fmt.Println("API connection successful.")
	fmt.Printf("User ID: %v\n", profile["user_id"])
	fmt.Printf("Email: %v\n", profile["email"])
	fmt.Printf("First name: %v\n", profile["first_name"])
	return nil
}

func cmdScrape(ctx context.Context, logger *slog.Logger, cfg config.Settings, args []string) error {
	fs := flag.NewFlagSet("scrape", flag.ExitOnError)
	days := fs.Int("days", cfg.ScrapeDays, "number of days to scrape")
	startDate := fs.String("start-date", "", "start date YYYY-MM-DD")
	endDate := fs.String("end-date", "", "end date YYYY-MM-DD")
	if err := fs.Parse(args); err != nil {
		return err
	}

	db, err := store.Open(ctx, cfg.DatabaseURL(), cfg.DBSchema)
	if err != nil {
		return err
	}
	defer db.Close()

	storage, err := store.NewTokenStorage(ctx, db, cfg.EncryptionKey, cfg.AccessToken, cfg.RefreshToken)
	if err != nil {
		return err
	}
	s := scraper.New(*days, *startDate, *endDate, whoop.NewClient(auth.New(cfg.ClientID, cfg.ClientSecret, storage, logger), nil), store.New(db), logger)
	stats := s.ScrapeAll(ctx)

	fmt.Printf("\nScrape completed (%s to %s)\n", s.StartDate, s.EndDate)
	fmt.Println("Results:")
	total := 0
	for _, name := range []string{"user_profile", "body_measurement", "cycles", "recovery", "sleep", "workouts"} {
		stat, ok := stats[name]
		if !ok {
			continue
		}
		if stat.Success {
			total += stat.Records
			fmt.Printf("  %s: %d records\n", name, stat.Records)
		} else {
			fmt.Printf("  %s: FAILED - %s\n", name, stat.Error)
		}
	}
	fmt.Printf("\nTotal: %d records\n", total)

	out, _ := json.Marshal(stats)
	logger.Debug("scrape stats", "stats", string(out))
	return nil
}

func tokenStorage(ctx context.Context, cfg config.Settings) (auth.TokenStorage, func(), error) {
	if cfg.TokenStorage == "file" {
		return auth.NewFileStorage(cfg.TokenPath, cfg.AccessToken, cfg.RefreshToken), func() {}, nil
	}
	db, err := store.Open(ctx, cfg.DatabaseURL(), cfg.DBSchema)
	if err != nil {
		return nil, func() {}, err
	}
	storage, err := store.NewTokenStorage(ctx, db, cfg.EncryptionKey, cfg.AccessToken, cfg.RefreshToken)
	if err != nil {
		db.Close()
		return nil, func() {}, err
	}
	return storage, db.Close, nil
}

func prefix(value string, n int) string {
	if len(value) <= n {
		return value
	}
	return value[:n]
}

var _ = http.MethodGet
