package scraper

import (
	"context"
	"log/slog"
	"os"

	"whoop-scraper/internal/store"
	"whoop-scraper/internal/whoop"
)

type Stat struct {
	Success bool   `json:"success"`
	Records int    `json:"records,omitempty"`
	Error   string `json:"error,omitempty"`
}

type Scraper struct {
	Days      int
	StartDate string
	EndDate   string
	client    *whoop.Client
	store     *store.Store
	logger    *slog.Logger
}

func New(days int, startDate, endDate string, client *whoop.Client, store *store.Store, logger *slog.Logger) *Scraper {
	if startDate == "" || endDate == "" {
		startDate, endDate = whoop.DateRange(days)
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return &Scraper{Days: days, StartDate: startDate, EndDate: endDate, client: client, store: store, logger: logger}
}

func (s *Scraper) ScrapeAll(ctx context.Context) map[string]Stat {
	stats := map[string]Stat{}
	stats["user_profile"] = s.scrapeOne(ctx, "user_profile", func(ctx context.Context) (int, error) {
		data, err := s.client.UserProfile(ctx)
		if err != nil {
			return 0, err
		}
		return 1, s.store.UpsertUserProfile(ctx, data)
	})
	stats["body_measurement"] = s.scrapeOne(ctx, "body_measurement", func(ctx context.Context) (int, error) {
		data, err := s.client.BodyMeasurement(ctx)
		if err != nil {
			return 0, err
		}
		return 1, s.store.UpsertBodyMeasurement(ctx, data)
	})
	stats["cycles"] = s.scrapeOne(ctx, "cycles", func(ctx context.Context) (int, error) {
		records, err := s.client.Cycles(ctx, s.StartDate, s.EndDate)
		if err != nil {
			return 0, err
		}
		return s.store.UpsertCycles(ctx, records)
	})
	stats["recovery"] = s.scrapeOne(ctx, "recovery", func(ctx context.Context) (int, error) {
		records, err := s.client.Recovery(ctx, s.StartDate, s.EndDate)
		if err != nil {
			return 0, err
		}
		return s.store.UpsertRecovery(ctx, records)
	})
	stats["sleep"] = s.scrapeOne(ctx, "sleep", func(ctx context.Context) (int, error) {
		records, err := s.client.Sleep(ctx, s.StartDate, s.EndDate)
		if err != nil {
			return 0, err
		}
		return s.store.UpsertSleep(ctx, records)
	})
	stats["workouts"] = s.scrapeOne(ctx, "workouts", func(ctx context.Context) (int, error) {
		records, err := s.client.Workouts(ctx, s.StartDate, s.EndDate)
		if err != nil {
			return 0, err
		}
		return s.store.UpsertWorkouts(ctx, records)
	})
	return stats
}

func (s *Scraper) scrapeOne(ctx context.Context, name string, fn func(context.Context) (int, error)) Stat {
	s.logger.Info("scraping endpoint", "endpoint", name)
	count, err := fn(ctx)
	if err != nil {
		s.logger.Error("endpoint scrape failed", "endpoint", name, "error", err)
		return Stat{Success: false, Error: err.Error()}
	}
	s.logger.Info("endpoint scrape completed", "endpoint", name, "records", count)
	return Stat{Success: true, Records: count}
}
