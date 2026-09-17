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

	// AuthPublicKeyPath is the RS256 public key of the shared authentication
	// service. Set it and tokens minted by authentication-api.karlo.id are
	// accepted alongside this service's own; leave it empty and nothing
	// changes, which is the state every deployment starts in.
	//
	// A PATH rather than the key itself: it is not a secret — it is a public
	// key — but it is multi-line PEM, and multi-line values in a task
	// definition are where a stray \n silently becomes a key that parses to
	// something else.
	AuthPublicKeyPath string

	// AuthPublicKey is the same key inline, for deployments with no filesystem
	// to point at.
	//
	// Both exist because neither works everywhere. A path is right locally and
	// keeps a multi-line PEM out of a task definition, where a stray \n turns
	// it into a key that parses to something else. But a Fargate task has no
	// file to point AT — the key would have to be baked into the image, which
	// makes rotating it a rebuild. So in AWS it arrives inline from an SSM
	// parameter, where newlines survive intact.
	//
	// The path wins when both are set: a file is the more deliberate of the two.
	AuthPublicKey string

	// AuthServiceURL, when set, makes /v1/auth/login and /v1/auth/refresh
	// DELEGATE to the shared authentication service instead of checking this
	// service's own users table. Independent of AuthPublicKeyPath: verifying
	// somebody else's tokens and issuing through somebody else are separate
	// steps, and during a cutover a deployment may sensibly do the first
	// without the second.
	AuthServiceURL string

	// ServiceToken authenticates fms-backend to the auth service for the calls
	// it makes on its OWN behalf rather than a user's — listing companies for
	// the tenant projection, which runs in a background loop with no request
	// and therefore no caller's token to forward.
	ServiceToken string

	// AuthGRPCTarget is the authentication service's gRPC address. Separate
	// from AuthServiceURL because they are different protocols on different
	// ports: HTTP for the login proxy, gRPC for service-to-service calls that
	// carry a service token rather than a user's.
	AuthGRPCTarget string

	// MDGRPCTarget is the master-data service's gRPC address. Unset, the fleet
	// projection does not run and fms_app.vehicles keeps whatever it has — the
	// state of a deployment that has not cut over.
	MDGRPCTarget string

	// MDServiceURL is master data's HTTP address, used for WRITES — creating and
	// updating a vehicle or a driver as the signed-in user.
	//
	// Separate from MDGRPCTarget above, and not merely because of the protocol:
	// the two carry different credentials on purpose. The gRPC path is the
	// background projection and presents a SERVICE token; this path presents the
	// CALLER'S OWN bearer, forwarded verbatim.
	//
	// That distinction is the whole security model of the write-through. A
	// service token here would let fms-backend write anything on anyone's
	// behalf, and master data would be authorising fms-the-service rather than
	// the person — so an FMS user without vehicles.edit upstream would succeed
	// where they should be refused, and FMS would quietly have become a way
	// around master data's permissions. Forwarding the caller also puts a real
	// name in master data's audit trail instead of "fms-backend".
	//
	// Unset, write-through does not run and FMS writes stay local — the state of
	// a deployment that has not cut over.
	MDServiceURL string

	// MDServiceToken is the credential presented to MASTER DATA, which is not
	// necessarily the one the authentication service accepts.
	//
	// The platform issues a token PER CALLING SERVICE and each service accepts
	// all of them (AUTH_SERVICE_TOKEN, MASTERDATA_SERVICE_TOKEN, … in the
	// microservices .env). One shared SERVICE_TOKEN therefore works only while
	// every upstream happens to accept the same string — and locally they do
	// not: auth runs with `dev-service-token` while master data accepts a
	// different set entirely, which is exactly how the fleet projection came up
	// answering "invalid service token" for all five companies while the
	// tenant directory beside it worked.
	//
	// Falls back to SERVICE_TOKEN so a deployment that really does share one
	// needs no extra configuration.
	MDServiceToken string

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
	// AuthCookieDomain switches refresh tokens to an httpOnly cookie on this
	// domain (".karlo.id" in production). Empty keeps them in the JSON body.
	AuthCookieDomain string
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
			AlertDays:        EnvInt("RETENTION_ALERT_DAYS", 548),         // ~18 months
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

		AuthPublicKeyPath: Env("AUTH_PUBLIC_KEY_PATH", ""),
		AuthPublicKey:     Env("AUTH_PUBLIC_KEY", ""),
		AuthServiceURL:    Env("AUTH_SERVICE_URL", ""),
		// SERVICE_TOKEN is the platform's name for this across every service;
		// AUTH_SERVICE_TOKEN is accepted as a fallback because that is what this
		// service called it first. Reading only the latter is what sent an empty
		// string to the auth service and produced "invalid service token" with a
		// perfectly correct token sitting in the environment under the other name.
		ServiceToken:   Env("SERVICE_TOKEN", Env("AUTH_SERVICE_TOKEN", "")),
		AuthGRPCTarget: Env("AUTH_GRPC_TARGET", ""),
		MDGRPCTarget:   Env("MD_GRPC_TARGET", ""),
		MDServiceURL:   Env("MD_SERVICE_URL", ""),
		MDServiceToken: Env("MD_SERVICE_TOKEN", Env("SERVICE_TOKEN", Env("AUTH_SERVICE_TOKEN", ""))),

		TelemetryBaseURL: Env("TELEMETRY_BASE_URL", "http://localhost:8082"),
		TelemetryAPIKey:  Env("TELEMETRY_API_KEY", ""),
		TelemetryTimeout: time.Duration(EnvInt("TELEMETRY_TIMEOUT_MS", 15000)) * time.Millisecond,
		LiveCacheTTL:     time.Duration(EnvInt("LIVE_CACHE_TTL_SEC", 8)) * time.Second,
		CORSOrigins:      splitCSV(Env("CORS_ORIGINS", "")),
		AuthCookieDomain: Env("AUTH_COOKIE_DOMAIN", ""),
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
