package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"whoop-scraper/internal/auth"
)

type DB = pgxpool.Pool

func Open(ctx context.Context, databaseURL string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 4
	cfg.MinConns = 0
	cfg.MaxConnLifetime = time.Hour
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func InitSchema(ctx context.Context, db *DB) error {
	_, err := db.Exec(ctx, SchemaSQL)
	return err
}

type TokenStorage struct {
	db           *DB
	cipher       cipher.AEAD
	accessToken  string
	refreshToken string
}

func NewTokenStorage(ctx context.Context, db *DB, encryptionKey, accessToken, refreshToken string) (*TokenStorage, error) {
	storage := &TokenStorage{db: db, accessToken: accessToken, refreshToken: refreshToken}
	if encryptionKey != "" {
		aead, err := newAEAD(encryptionKey)
		if err != nil {
			return nil, err
		}
		storage.cipher = aead
	}
	_, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS whoop_oauth_tokens (
		id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
		access_token TEXT NOT NULL,
		refresh_token TEXT NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		token_type VARCHAR(50) DEFAULT 'bearer',
		updated_at TIMESTAMPTZ DEFAULT NOW()
	)`)
	return storage, err
}

func (s *TokenStorage) Save(ctx context.Context, tokens auth.Tokens) error {
	accessToken, err := s.encrypt(tokens.AccessToken)
	if err != nil {
		return err
	}
	refreshToken, err := s.encrypt(tokens.RefreshToken)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO whoop_oauth_tokens (id, access_token, refresh_token, expires_at, token_type, updated_at)
		VALUES (1, $1, $2, $3, $4, NOW())
		ON CONFLICT (id) DO UPDATE SET
			access_token = EXCLUDED.access_token,
			refresh_token = EXCLUDED.refresh_token,
			expires_at = EXCLUDED.expires_at,
			token_type = EXCLUDED.token_type,
			updated_at = NOW()
	`, accessToken, refreshToken, tokens.ExpiresAt, tokens.TokenType)
	return err
}

func (s *TokenStorage) Load(ctx context.Context) (*auth.Tokens, error) {
	var tokens auth.Tokens
	err := s.db.QueryRow(ctx, `
		SELECT access_token, refresh_token, expires_at, COALESCE(token_type, 'bearer')
		FROM whoop_oauth_tokens
		WHERE id = 1
	`).Scan(&tokens.AccessToken, &tokens.RefreshToken, &tokens.ExpiresAt, &tokens.TokenType)
	if err == nil {
		var decryptErr error
		tokens.AccessToken, decryptErr = s.decrypt(tokens.AccessToken)
		if decryptErr != nil {
			return nil, decryptErr
		}
		tokens.RefreshToken, decryptErr = s.decrypt(tokens.RefreshToken)
		if decryptErr != nil {
			return nil, decryptErr
		}
		return &tokens, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if s.accessToken != "" && s.refreshToken != "" {
		return &auth.Tokens{
			AccessToken:  s.accessToken,
			RefreshToken: s.refreshToken,
			ExpiresAt:    time.Now().UTC(),
			TokenType:    "bearer",
		}, nil
	}
	return nil, nil
}

func (s *TokenStorage) Clear(ctx context.Context) error {
	_, err := s.db.Exec(ctx, "DELETE FROM whoop_oauth_tokens WHERE id = 1")
	return err
}

func (s *TokenStorage) encrypt(value string) (string, error) {
	if s.cipher == nil {
		return value, nil
	}
	nonce := make([]byte, s.cipher.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	encrypted := s.cipher.Seal(nonce, nonce, []byte(value), nil)
	return "aesgcm:" + base64.RawURLEncoding.EncodeToString(encrypted), nil
}

func (s *TokenStorage) decrypt(value string) (string, error) {
	if s.cipher == nil {
		return value, nil
	}
	const prefix = "aesgcm:"
	if len(value) < len(prefix) || value[:len(prefix)] != prefix {
		return "", errors.New("encrypted token is missing aesgcm prefix")
	}
	data, err := base64.RawURLEncoding.DecodeString(value[len(prefix):])
	if err != nil {
		return "", err
	}
	if len(data) < s.cipher.NonceSize() {
		return "", errors.New("encrypted token is too short")
	}
	nonce, ciphertext := data[:s.cipher.NonceSize()], data[s.cipher.NonceSize():]
	plain, err := s.cipher.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func newAEAD(key string) (cipher.AEAD, error) {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(key)
	}
	if err != nil {
		raw = []byte(key)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("WHOOP_ENCRYPTION_KEY must decode to 32 bytes for AES-256-GCM, got %d", len(raw))
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

type Store struct {
	db *DB
}

func New(db *DB) *Store {
	return &Store{db: db}
}

func (s *Store) UpsertUserProfile(ctx context.Context, data map[string]any) error {
	raw := mustJSON(data)
	_, err := s.db.Exec(ctx, `
		INSERT INTO whoop_user_profile (user_id, email, first_name, last_name, raw, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			email = EXCLUDED.email,
			first_name = EXCLUDED.first_name,
			last_name = EXCLUDED.last_name,
			raw = EXCLUDED.raw,
			updated_at = NOW()
	`, str(data, "user_id", "unknown"), nullable(data["email"]), nullable(data["first_name"]), nullable(data["last_name"]), raw)
	return err
}

func (s *Store) UpsertBodyMeasurement(ctx context.Context, data map[string]any) error {
	raw := mustJSON(data)
	userID := str(data, "user_id", "unknown")
	_, err := s.db.Exec(ctx, `
		INSERT INTO whoop_body_measurement (user_id, height_meter, weight_kilogram, max_heart_rate, raw, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			height_meter = EXCLUDED.height_meter,
			weight_kilogram = EXCLUDED.weight_kilogram,
			max_heart_rate = EXCLUDED.max_heart_rate,
			raw = EXCLUDED.raw,
			updated_at = NOW()
	`, userID, nullable(data["height_meter"]), nullable(data["weight_kilogram"]), nullable(data["max_heart_rate"]), raw)
	return err
}

func (s *Store) UpsertCycles(ctx context.Context, records []map[string]any) (int, error) {
	for _, record := range records {
		score := nested(record, "score")
		_, err := s.db.Exec(ctx, `
			INSERT INTO whoop_cycle
				(id, user_id, start_time, end_time, timezone_offset, score_state,
				 score_strain, score_kilojoule, score_average_heart_rate, score_max_heart_rate, raw, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
			ON CONFLICT (id) DO UPDATE SET
				end_time = EXCLUDED.end_time,
				timezone_offset = EXCLUDED.timezone_offset,
				score_state = EXCLUDED.score_state,
				score_strain = EXCLUDED.score_strain,
				score_kilojoule = EXCLUDED.score_kilojoule,
				score_average_heart_rate = EXCLUDED.score_average_heart_rate,
				score_max_heart_rate = EXCLUDED.score_max_heart_rate,
				raw = EXCLUDED.raw,
				updated_at = NOW()
		`, str(record, "id", ""), str(record, "user_id", "unknown"), nullable(record["start"]), nullable(record["end"]), nullable(record["timezone_offset"]), nullable(record["score_state"]), nullable(score["strain"]), nullable(score["kilojoule"]), nullable(score["average_heart_rate"]), nullable(score["max_heart_rate"]), mustJSON(record))
		if err != nil {
			return 0, err
		}
	}
	return len(records), nil
}

func (s *Store) UpsertRecovery(ctx context.Context, records []map[string]any) (int, error) {
	for _, record := range records {
		score := nested(record, "score")
		_, err := s.db.Exec(ctx, `
			INSERT INTO whoop_recovery
				(cycle_id, user_id, sleep_id, score_recovery_score, score_resting_heart_rate,
				 score_hrv_rmssd_milli, score_spo2_percentage, score_skin_temp_celsius, raw, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
			ON CONFLICT (cycle_id) DO UPDATE SET
				sleep_id = EXCLUDED.sleep_id,
				score_recovery_score = EXCLUDED.score_recovery_score,
				score_resting_heart_rate = EXCLUDED.score_resting_heart_rate,
				score_hrv_rmssd_milli = EXCLUDED.score_hrv_rmssd_milli,
				score_spo2_percentage = EXCLUDED.score_spo2_percentage,
				score_skin_temp_celsius = EXCLUDED.score_skin_temp_celsius,
				raw = EXCLUDED.raw,
				updated_at = NOW()
		`, str(record, "cycle_id", ""), str(record, "user_id", "unknown"), nullable(record["sleep_id"]), nullable(score["recovery_score"]), nullable(score["resting_heart_rate"]), nullable(score["hrv_rmssd_milli"]), nullable(score["spo2_percentage"]), nullable(score["skin_temp_celsius"]), mustJSON(record))
		if err != nil {
			return 0, err
		}
	}
	return len(records), nil
}

func (s *Store) UpsertSleep(ctx context.Context, records []map[string]any) (int, error) {
	for _, record := range records {
		score := nested(record, "score")
		stage := nested(score, "stage_summary")
		needed := nested(score, "sleep_needed")
		_, err := s.db.Exec(ctx, `
			INSERT INTO whoop_sleep
				(id, user_id, start_time, end_time, timezone_offset, nap,
				 score_stage_summary_total_in_bed_time_milli, score_stage_summary_total_awake_time_milli,
				 score_stage_summary_total_no_data_time_milli, score_stage_summary_total_light_sleep_time_milli,
				 score_stage_summary_total_slow_wave_sleep_time_milli, score_stage_summary_total_rem_sleep_time_milli,
				 score_stage_summary_sleep_cycle_count, score_stage_summary_disturbance_count,
				 score_sleep_needed_baseline_milli, score_sleep_needed_need_from_sleep_debt_milli,
				 score_sleep_needed_need_from_recent_strain_milli, score_sleep_needed_need_from_recent_nap_milli,
				 score_respiratory_rate, score_sleep_performance_percentage,
				 score_sleep_consistency_percentage, score_sleep_efficiency_percentage, raw, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, NOW())
			ON CONFLICT (id) DO UPDATE SET
				end_time = EXCLUDED.end_time,
				timezone_offset = EXCLUDED.timezone_offset,
				nap = EXCLUDED.nap,
				score_stage_summary_total_in_bed_time_milli = EXCLUDED.score_stage_summary_total_in_bed_time_milli,
				score_stage_summary_total_awake_time_milli = EXCLUDED.score_stage_summary_total_awake_time_milli,
				score_stage_summary_total_no_data_time_milli = EXCLUDED.score_stage_summary_total_no_data_time_milli,
				score_stage_summary_total_light_sleep_time_milli = EXCLUDED.score_stage_summary_total_light_sleep_time_milli,
				score_stage_summary_total_slow_wave_sleep_time_milli = EXCLUDED.score_stage_summary_total_slow_wave_sleep_time_milli,
				score_stage_summary_total_rem_sleep_time_milli = EXCLUDED.score_stage_summary_total_rem_sleep_time_milli,
				score_stage_summary_sleep_cycle_count = EXCLUDED.score_stage_summary_sleep_cycle_count,
				score_stage_summary_disturbance_count = EXCLUDED.score_stage_summary_disturbance_count,
				score_sleep_needed_baseline_milli = EXCLUDED.score_sleep_needed_baseline_milli,
				score_sleep_needed_need_from_sleep_debt_milli = EXCLUDED.score_sleep_needed_need_from_sleep_debt_milli,
				score_sleep_needed_need_from_recent_strain_milli = EXCLUDED.score_sleep_needed_need_from_recent_strain_milli,
				score_sleep_needed_need_from_recent_nap_milli = EXCLUDED.score_sleep_needed_need_from_recent_nap_milli,
				score_respiratory_rate = EXCLUDED.score_respiratory_rate,
				score_sleep_performance_percentage = EXCLUDED.score_sleep_performance_percentage,
				score_sleep_consistency_percentage = EXCLUDED.score_sleep_consistency_percentage,
				score_sleep_efficiency_percentage = EXCLUDED.score_sleep_efficiency_percentage,
				raw = EXCLUDED.raw,
				updated_at = NOW()
		`, str(record, "id", ""), str(record, "user_id", "unknown"), nullable(record["start"]), nullable(record["end"]), nullable(record["timezone_offset"]), boolValue(record["nap"]), nullable(stage["total_in_bed_time_milli"]), nullable(stage["total_awake_time_milli"]), nullable(stage["total_no_data_time_milli"]), nullable(stage["total_light_sleep_time_milli"]), nullable(stage["total_slow_wave_sleep_time_milli"]), nullable(stage["total_rem_sleep_time_milli"]), nullable(stage["sleep_cycle_count"]), nullable(stage["disturbance_count"]), nullable(needed["baseline_milli"]), nullable(needed["need_from_sleep_debt_milli"]), nullable(needed["need_from_recent_strain_milli"]), nullable(needed["need_from_recent_nap_milli"]), nullable(score["respiratory_rate"]), nullable(score["sleep_performance_percentage"]), nullable(score["sleep_consistency_percentage"]), nullable(score["sleep_efficiency_percentage"]), mustJSON(record))
		if err != nil {
			return 0, err
		}
	}
	return len(records), nil
}

func (s *Store) UpsertWorkouts(ctx context.Context, records []map[string]any) (int, error) {
	for _, record := range records {
		score := nested(record, "score")
		zone := nested(score, "zone_durations")
		_, err := s.db.Exec(ctx, `
			INSERT INTO whoop_workout
				(id, user_id, start_time, end_time, timezone_offset, sport_id,
				 score_strain, score_average_heart_rate, score_max_heart_rate, score_kilojoule,
				 score_percent_recorded, score_distance_meter, score_altitude_gain_meter,
				 score_altitude_change_meter, score_zone_duration_zone_zero_milli,
				 score_zone_duration_zone_one_milli, score_zone_duration_zone_two_milli,
				 score_zone_duration_zone_three_milli, score_zone_duration_zone_four_milli,
				 score_zone_duration_zone_five_milli, raw, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, NOW())
			ON CONFLICT (id) DO UPDATE SET
				end_time = EXCLUDED.end_time,
				timezone_offset = EXCLUDED.timezone_offset,
				sport_id = EXCLUDED.sport_id,
				score_strain = EXCLUDED.score_strain,
				score_average_heart_rate = EXCLUDED.score_average_heart_rate,
				score_max_heart_rate = EXCLUDED.score_max_heart_rate,
				score_kilojoule = EXCLUDED.score_kilojoule,
				score_percent_recorded = EXCLUDED.score_percent_recorded,
				score_distance_meter = EXCLUDED.score_distance_meter,
				score_altitude_gain_meter = EXCLUDED.score_altitude_gain_meter,
				score_altitude_change_meter = EXCLUDED.score_altitude_change_meter,
				score_zone_duration_zone_zero_milli = EXCLUDED.score_zone_duration_zone_zero_milli,
				score_zone_duration_zone_one_milli = EXCLUDED.score_zone_duration_zone_one_milli,
				score_zone_duration_zone_two_milli = EXCLUDED.score_zone_duration_zone_two_milli,
				score_zone_duration_zone_three_milli = EXCLUDED.score_zone_duration_zone_three_milli,
				score_zone_duration_zone_four_milli = EXCLUDED.score_zone_duration_zone_four_milli,
				score_zone_duration_zone_five_milli = EXCLUDED.score_zone_duration_zone_five_milli,
				raw = EXCLUDED.raw,
				updated_at = NOW()
		`, str(record, "id", ""), str(record, "user_id", "unknown"), nullable(record["start"]), nullable(record["end"]), nullable(record["timezone_offset"]), nullable(record["sport_id"]), nullable(score["strain"]), nullable(score["average_heart_rate"]), nullable(score["max_heart_rate"]), nullable(score["kilojoule"]), nullable(score["percent_recorded"]), nullable(score["distance_meter"]), nullable(score["altitude_gain_meter"]), nullable(score["altitude_change_meter"]), nullable(zone["zone_zero_milli"]), nullable(zone["zone_one_milli"]), nullable(zone["zone_two_milli"]), nullable(zone["zone_three_milli"]), nullable(zone["zone_four_milli"]), nullable(zone["zone_five_milli"]), mustJSON(record))
		if err != nil {
			return 0, err
		}
	}
	return len(records), nil
}

func str(data map[string]any, key, fallback string) string {
	value, ok := data[key]
	if !ok || value == nil {
		return fallback
	}
	return fmt.Sprint(value)
}

func nested(data map[string]any, key string) map[string]any {
	value, ok := data[key].(map[string]any)
	if !ok || value == nil {
		return map[string]any{}
	}
	return value
}

func nullable(value any) any {
	if value == nil {
		return nil
	}
	return value
}

func boolValue(value any) bool {
	parsed, ok := value.(bool)
	return ok && parsed
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return `{}`
	}
	return string(data)
}
