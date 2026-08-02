// Package config centralizes environment-driven configuration for the API
// service. It mirrors the simple env()/envInt() helper style used across the
// telemetry backend (no config library).
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for cmd/api.
type Config struct {
	Addr        string
	DatabaseURL string
	RedisAddr   string

	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

	TelemetryBaseURL string
	TelemetryAPIKey  string
	TelemetryTimeout time.Duration
	LiveCacheTTL     time.Duration

	// AWSRegion / BedrockModelID drive AI dashboard composition. Auth is the
	// task's IAM role — no API key to store or rotate. Nova Pro is exposed in
	// ap-southeast-1 only as an inference profile, hence the "apac." prefix.
	AWSRegion      string
	BedrockModelID string
	// MailFrom enables scheduled report email. Empty disables the runner
	// rather than failing startup — a deployment without email should still
	// serve every other report.
	MailFrom string

	// DocumentsBucket enables vehicle/driver document upload (KIR, STNK, SIM).
	// Empty disables only those endpoints — a deployment without the bucket
	// still serves the rest of the API, and the existing read-only document
	// list keeps working for rows imported from the legacy export.
	DocumentsBucket string

	// ColdBucket is the S3 bucket the retention sweeper archives to before it
	// deletes aged rows from the hot DB (shared with telemetry cold storage).
	// Empty leaves the archive-then-delete tables (alerts, driving_events)
	// untouched — the zero-value prunes (webhook logs, expired tokens/links,
	// dead clip rows) still run, since they need no archive.
	ColdBucket string
	// Retention holds the sweeper's schedule and per-table windows. Zero for a
	// window disables that table's prune. See defaults in Load.
	Retention RetentionConfig

	// Routing provider for vehicle-to-hangar directions.
	//
	// "geoapify" (default) is the hosted option whose free tier permits
	// commercial use AND offers map matching — ORS has no matching endpoint at
	// all, and Stadia and GraphHopper forbid commercial use when free. "ors"
	// remains selectable; "osrm" points at a self-hosted engine, which costs a
	// server but removes the quota and keeps fleet positions in-house. Empty
	// leaves routing disabled and every other endpoint working.
	RoutingProvider string
	GeoapifyAPIKey  string
	GeoapifyBaseURL string
	ORSAPIKey       string
	ORSBaseURL      string
	OSRMBaseURL     string

	// CORSOrigins is the allowlist of browser origins (e.g. https://fms.karlo.id).
	// Empty ⇒ reflect any origin (fine for local dev behind the Vite proxy).
	CORSOrigins []string
}

// RetentionConfig bounds how long append-only rows live in the hot DB. The app
// database has no archival of its own (unlike telemetry's Parquet tier), so
// without this every alert, event and delivery log grows forever. Each *Days
// value is a hard cutoff measured from the row's timestamp; 0 disables that
// table's prune entirely (a safety hatch, not a default).
type RetentionConfig struct {
	Enabled  bool
	Interval time.Duration // how often the sweep runs

	// Zero-value tables — pruned outright (no archive; nothing of long-term use).
	WebhookDays      int // webhook_deliveries, by created_at
	RefreshTokenDays int // refresh_tokens past expiry OR revoked, by expires_at
	TrackingLinkDays int // tracking_links past expiry, by expires_at
	DeadClipDays     int // video_clips still pending/failed, by created_at

	// Medium-value tables — archived to ColdBucket as gzipped NDJSON, then
	// deleted. Skipped when ColdBucket is empty so nothing is lost.
	AlertDays        int // alerts, by occurred_at
	DrivingEventDays int // driving_events, by occurred_at
}

// Load reads configuration from the environment, applying sane local-dev
// defaults so `go run ./cmd/api` works against the compose stack out of the box.
func Load() Config {
	return Config{
		Addr:            Env("API_ADDR", ":8080"),
		AWSRegion:       Env("AWS_REGION", "ap-southeast-1"),
		BedrockModelID:  Env("BEDROCK_MODEL_ID", "apac.amazon.nova-pro-v1:0"),
		MailFrom:        Env("MAIL_FROM", ""),
		DocumentsBucket: Env("DOCUMENTS_BUCKET", ""),
		ColdBucket:      Env("COLD_BUCKET", ""),
		Retention: RetentionConfig{
			Enabled:          EnvBool("RETENTION_ENABLED", true),
			Interval:         time.Duration(EnvInt("RETENTION_INTERVAL_HOURS", 24)) * time.Hour,
			WebhookDays:      EnvInt("RETENTION_WEBHOOK_DAYS", 30),
			RefreshTokenDays: EnvInt("RETENTION_REFRESH_TOKEN_DAYS", 30),
			TrackingLinkDays: EnvInt("RETENTION_TRACKING_LINK_DAYS", 7),
			DeadClipDays:     EnvInt("RETENTION_DEAD_CLIP_DAYS", 7),
			AlertDays:        EnvInt("RETENTION_ALERT_DAYS", 548),        // ~18 months
			DrivingEventDays: EnvInt("RETENTION_DRIVING_EVENT_DAYS", 365), // 12 months
		},
		RoutingProvider: Env("ROUTING_PROVIDER", "geoapify"),
		GeoapifyAPIKey:  Env("GEOAPIFY_API_KEY", ""),
		GeoapifyBaseURL: Env("GEOAPIFY_BASE_URL", ""),
		ORSAPIKey:       Env("ORS_API_KEY", ""),
		ORSBaseURL:      Env("ORS_BASE_URL", ""),
		OSRMBaseURL:     Env("OSRM_BASE_URL", ""),
		// Host ports 5433/6380 avoid clashing with the telemetry stack (5432/6379).
		DatabaseURL: Env("DATABASE_URL", "postgres://fms_app:fms_app@localhost:5433/fms_app"),
		RedisAddr:   Env("REDIS_ADDR", "localhost:6380"),

		JWTSecret:       Env("JWT_SECRET", "dev-insecure-secret-change-me"),
		AccessTokenTTL:  time.Duration(EnvInt("ACCESS_TTL_MIN", 15)) * time.Minute,
		RefreshTokenTTL: time.Duration(EnvInt("REFRESH_TTL_DAYS", 30)) * 24 * time.Hour,

		TelemetryBaseURL: Env("TELEMETRY_BASE_URL", "http://localhost:8082"),
		TelemetryAPIKey:  Env("TELEMETRY_API_KEY", ""),
		TelemetryTimeout: time.Duration(EnvInt("TELEMETRY_TIMEOUT_MS", 15000)) * time.Millisecond,
		LiveCacheTTL:     time.Duration(EnvInt("LIVE_CACHE_TTL_SEC", 8)) * time.Second,
		CORSOrigins:      splitCSV(Env("CORS_ORIGINS", "")),
	}
}

// splitCSV splits a comma-separated env value into a trimmed, non-empty list.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Env returns the value of environment variable k, or def if unset/empty.
func Env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// EnvInt returns the integer value of environment variable k, or def.
func EnvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// EnvBool returns the boolean value of environment variable k, or def. Accepts
// the usual strconv truthy/falsey spellings (1/0, true/false, t/f).
func EnvBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
