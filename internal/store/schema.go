package store

const SchemaSQL = `
CREATE TABLE IF NOT EXISTS whoop_oauth_tokens (
    id INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    access_token TEXT NOT NULL,
    refresh_token TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    token_type VARCHAR(50) DEFAULT 'bearer',
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS whoop_user_profile (
    user_id VARCHAR(100) PRIMARY KEY,
    email VARCHAR(255),
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS whoop_body_measurement (
    id SERIAL PRIMARY KEY,
    user_id VARCHAR(100) NOT NULL,
    height_meter DECIMAL(5, 3),
    weight_kilogram DECIMAL(8, 3),
    max_heart_rate INTEGER,
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_whoop_body_measurement_user_id ON whoop_body_measurement(user_id);

CREATE TABLE IF NOT EXISTS whoop_cycle (
    id VARCHAR(100) PRIMARY KEY,
    user_id VARCHAR(100) NOT NULL,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ,
    timezone_offset VARCHAR(10),
    score_state VARCHAR(50),
    score_strain DECIMAL(8, 4),
    score_kilojoule DECIMAL(12, 3),
    score_average_heart_rate INTEGER,
    score_max_heart_rate INTEGER,
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_whoop_cycle_start_time ON whoop_cycle(start_time);
CREATE INDEX IF NOT EXISTS idx_whoop_cycle_user_id ON whoop_cycle(user_id);

CREATE TABLE IF NOT EXISTS whoop_recovery (
    cycle_id VARCHAR(100) PRIMARY KEY,
    user_id VARCHAR(100) NOT NULL,
    sleep_id VARCHAR(100),
    score_recovery_score INTEGER,
    score_resting_heart_rate INTEGER,
    score_hrv_rmssd_milli DECIMAL(10, 3),
    score_spo2_percentage DECIMAL(6, 3),
    score_skin_temp_celsius DECIMAL(6, 3),
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_whoop_recovery_user_id ON whoop_recovery(user_id);

CREATE TABLE IF NOT EXISTS whoop_sleep (
    id VARCHAR(100) PRIMARY KEY,
    user_id VARCHAR(100) NOT NULL,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ,
    timezone_offset VARCHAR(10),
    nap BOOLEAN DEFAULT FALSE,
    score_stage_summary_total_in_bed_time_milli BIGINT,
    score_stage_summary_total_awake_time_milli BIGINT,
    score_stage_summary_total_no_data_time_milli BIGINT,
    score_stage_summary_total_light_sleep_time_milli BIGINT,
    score_stage_summary_total_slow_wave_sleep_time_milli BIGINT,
    score_stage_summary_total_rem_sleep_time_milli BIGINT,
    score_stage_summary_sleep_cycle_count INTEGER,
    score_stage_summary_disturbance_count INTEGER,
    score_sleep_needed_baseline_milli BIGINT,
    score_sleep_needed_need_from_sleep_debt_milli BIGINT,
    score_sleep_needed_need_from_recent_strain_milli BIGINT,
    score_sleep_needed_need_from_recent_nap_milli BIGINT,
    score_respiratory_rate DECIMAL(6, 3),
    score_sleep_performance_percentage DECIMAL(6, 3),
    score_sleep_consistency_percentage DECIMAL(6, 3),
    score_sleep_efficiency_percentage DECIMAL(6, 3),
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_whoop_sleep_start_time ON whoop_sleep(start_time);
CREATE INDEX IF NOT EXISTS idx_whoop_sleep_user_id ON whoop_sleep(user_id);

CREATE TABLE IF NOT EXISTS whoop_workout (
    id VARCHAR(100) PRIMARY KEY,
    user_id VARCHAR(100) NOT NULL,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ,
    timezone_offset VARCHAR(10),
    sport_id INTEGER,
    score_strain DECIMAL(8, 4),
    score_average_heart_rate INTEGER,
    score_max_heart_rate INTEGER,
    score_kilojoule DECIMAL(12, 3),
    score_percent_recorded DECIMAL(8, 4),
    score_distance_meter DECIMAL(14, 3),
    score_altitude_gain_meter DECIMAL(12, 3),
    score_altitude_change_meter DECIMAL(12, 3),
    score_zone_duration_zone_zero_milli BIGINT,
    score_zone_duration_zone_one_milli BIGINT,
    score_zone_duration_zone_two_milli BIGINT,
    score_zone_duration_zone_three_milli BIGINT,
    score_zone_duration_zone_four_milli BIGINT,
    score_zone_duration_zone_five_milli BIGINT,
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_whoop_workout_start_time ON whoop_workout(start_time);
CREATE INDEX IF NOT EXISTS idx_whoop_workout_user_id ON whoop_workout(user_id);
`
