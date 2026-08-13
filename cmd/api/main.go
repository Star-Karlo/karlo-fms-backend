// Command api is the FMS application/platform service: auth + RBAC, master-data
// CRUD (vehicles, trackers, drivers, fleet groups), and a live-view endpoint
// that joins the registry with positions from the telemetry pipeline.
//
//	@title						Karlo FMS Platform API
//	@version					0.1
//	@description				Application API for the Fleet Management System: authentication, master data, and live vehicle tracking (joined with the telemetry service).
//	@BasePath					/
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Type "Bearer" followed by a space and the access token.
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger/v2"

	_ "github.com/karlo/fms-backend/cmd/api/docs"
	"github.com/karlo/fms-backend/internal/aidash"
	"github.com/karlo/fms-backend/internal/aireport"
	"github.com/karlo/fms-backend/internal/airule"
	"github.com/karlo/fms-backend/internal/alert"
	"github.com/karlo/fms-backend/internal/apitoken"
	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/coldstore"
	"github.com/karlo/fms-backend/internal/company"
	"github.com/karlo/fms-backend/internal/controltower"
	"github.com/karlo/fms-backend/internal/customreport"
	"github.com/karlo/fms-backend/internal/dashboard"
	"github.com/karlo/fms-backend/internal/dashcam"
	"github.com/karlo/fms-backend/internal/document"
	"github.com/karlo/fms-backend/internal/driver"
	"github.com/karlo/fms-backend/internal/driverscore"
	"github.com/karlo/fms-backend/internal/features"
	"github.com/karlo/fms-backend/internal/fleetgroup"
	"github.com/karlo/fms-backend/internal/fuel"
	"github.com/karlo/fms-backend/internal/fuelanalysis"
	"github.com/karlo/fms-backend/internal/geofence"
	"github.com/karlo/fms-backend/internal/hangar"
	"github.com/karlo/fms-backend/internal/insight"
	"github.com/karlo/fms-backend/internal/license"
	"github.com/karlo/fms-backend/internal/liveview"
	"github.com/karlo/fms-backend/internal/mailer"
	"github.com/karlo/fms-backend/internal/maintenance"
	"github.com/karlo/fms-backend/internal/notifengine"
	"github.com/karlo/fms-backend/internal/notifrule"
	"github.com/karlo/fms-backend/internal/platform/config"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/platformadmin"
	"github.com/karlo/fms-backend/internal/prefs"
	"github.com/karlo/fms-backend/internal/report"
	"github.com/karlo/fms-backend/internal/retention"
	"github.com/karlo/fms-backend/internal/roles"
	"github.com/karlo/fms-backend/internal/routing"
	"github.com/karlo/fms-backend/internal/scorecard"
	"github.com/karlo/fms-backend/internal/telemetry"
	"github.com/karlo/fms-backend/internal/telemetrysync"
	"github.com/karlo/fms-backend/internal/tenants"
	"github.com/karlo/fms-backend/internal/tracker"
	"github.com/karlo/fms-backend/internal/trackinglink"
	"github.com/karlo/fms-backend/internal/users"
	"github.com/karlo/fms-backend/internal/vehicle"
	"github.com/karlo/fms-backend/internal/webhook"
)

// gated mounts a handler's routes inside a group guarded by a feature middleware,
// so every route that handler registers returns 403 when the feature is disabled
// for the caller's tenant (platform admins bypass the gate).
func gated(r chi.Router, mw func(http.Handler) http.Handler, routes func(chi.Router)) {
	r.Group(func(gr chi.Router) {
		gr.Use(mw)
		routes(gr)
	})
}

// chain composes middlewares into one (applied left-to-right), so a module can be
// guarded by BOTH its account feature and the caller's role permission.
func chain(mws ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		for i := len(mws) - 1; i >= 0; i-- {
			next = mws[i](next)
		}
		return next
	}
}

func main() {
	ctx := context.Background()
	cfg := config.Load()

	pool, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	// Components.
	tm := auth.NewTokenManager(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	authH := auth.NewHandler(auth.NewService(pool, tm))
	vehicleH := vehicle.NewHandler(vehicle.NewService(pool))
	fleetGroupH := fleetgroup.NewHandler(fleetgroup.NewService(pool))
	driverH := driver.NewHandler(pool)
	trackerH := tracker.NewHandler(pool)
	tel := telemetry.NewClient(cfg.TelemetryBaseURL, cfg.TelemetryAPIKey, cfg.TelemetryTimeout)
	liveSvc := liveview.NewService(pool, tel)
	liveH := liveview.NewHandler(liveSvc)
	// Cold storage reader/writer, shared by the alert hot+cold merge and the
	// retention sweeper. Nil when COLD_BUCKET is unset (alerts stay hot-only).
	cold := coldstore.New(ctx, cfg.AWSRegion, cfg.ColdBucket)
	alertH := alert.NewHandler(pool, cold, cfg.Retention.AlertDays)
	controlH := controltower.NewHandler(controltower.NewService(pool, liveSvc))
	notifRuleH := notifrule.NewHandler(pool)
	// The report service borrows liveview for the daily rollup join, so the
	// fleet-wide historical reports do not duplicate it.
	reportSvc := report.NewService(pool, tel).WithFleetHistory(liveSvc)
	reportH := report.NewHandler(reportSvc)
	// Custom reports are a named COMBINATION of the catalog reports above, so
	// they share that service rather than re-querying anything themselves.
	aiReportH := aireport.NewHandler(ctx, cfg.AWSRegion, cfg.BedrockModelID)
	// The AI package writes report prose; the report package renders it. The
	// dependency runs one way (aireport -> customreport), so the narrator is
	// injected rather than imported.
	// Scheduled reports need a mail transport. Without MAIL_FROM the service
	// still serves every report; only the schedule runner stays off, which is
	// better than a runner that renders reports every morning and drops them.
	customReportSvc := customreport.NewService(pool, reportSvc).WithNarrator(aiReportH)
	if cfg.MailFrom != "" {
		sender, err := mailer.NewSES(ctx, cfg.AWSRegion, cfg.MailFrom)
		if err != nil {
			log.Printf("mailer: disabled (%v)", err)
		} else {
			customReportSvc = customReportSvc.WithMailer(sender)
			go customReportSvc.RunSchedules(ctx)
		}
	} else {
		log.Printf("mailer: MAIL_FROM not set; scheduled reports are disabled")
	}
	customReportH := customreport.NewHandler(customReportSvc)
	licenseH := license.NewHandler(pool)
	maintenanceH := maintenance.NewHandler(pool)
	scorecardH := scorecard.NewHandler(pool)
	trackingH := trackinglink.NewHandler(pool, liveSvc)
	dashcamH := dashcam.NewHandler(ctx, pool, tel, cfg.AWSRegion)
	usersH := users.NewHandler(pool)
	tenantH := tenants.NewHandler(pool)
	featureH := features.NewHandler(pool)
	rolesH := roles.NewHandler(pool)
	platformH := platformadmin.NewHandler(pool, liveSvc)
	apiTokenH := apitoken.NewHandler(pool)
	webhookH := webhook.NewHandler(pool)
	dashboardH := dashboard.NewHandler(pool)
	// AI dashboard composition (Amazon Nova Pro on Bedrock). A missing or
	// unauthorised Bedrock config disables just this endpoint (503) rather
	// than failing startup.
	aiDashH := aidash.NewHandler(ctx, cfg.AWSRegion, cfg.BedrockModelID)
	// AI notification-rule composition (same Bedrock model, same disable-on-503).
	aiRuleH := airule.NewHandler(ctx, cfg.AWSRegion, cfg.BedrockModelID)
	geofenceH := geofence.NewHandler(pool)
	fuelH := fuel.NewHandler(pool)
	fuelAnalysisH := fuelanalysis.NewHandler(pool, liveSvc)
	// Findings rather than figures: each vehicle compared with its own past,
	// its peers, or a threshold. Reads the daily rollup through liveview.
	insightH := insight.NewHandler(pool, liveSvc)
	// Routing: OpenRouteService today, a self-hosted OSRM later. Nil when no
	// provider is configured, which the route endpoint answers as 503.
	router := routing.New(routing.Config{
		Provider:     cfg.RoutingProvider,
		GeoapifyKey:  cfg.GeoapifyAPIKey,
		GeoapifyBase: cfg.GeoapifyBaseURL,
		ORSKey:       cfg.ORSAPIKey,
		ORSBase:      cfg.ORSBaseURL,
		OSRMBase:     cfg.OSRMBaseURL,
	})
	if router == nil {
		log.Printf("routing: no provider configured; routes and road snapping are disabled")
	} else if m, ok := routing.AsMatcher(router); ok {
		// Matching is optional per provider — ORS has no such endpoint — so it
		// is attached only when the chosen one actually supports it.
		liveH.WithMatcher(m)
		log.Printf("routing: using %s (with road snapping)", router.Name())
	} else {
		log.Printf("routing: using %s (no road snapping — provider has no map matching)", router.Name())
	}
	hangarH := hangar.NewHandler(pool, liveSvc).WithRouter(router)
	// Document upload/download. An empty DOCUMENTS_BUCKET disables just
	// these endpoints (503) rather than failing startup.
	documentH := document.NewHandler(ctx, pool, cfg.AWSRegion, cfg.DocumentsBucket)
	companyH := company.NewHandler(ctx, pool, cfg.AWSRegion, cfg.DocumentsBucket)
	prefsH := prefs.NewHandler(pool)
	telemetrySyncH := telemetrysync.NewHandler(pool)

	// Background engine: evaluate attached notification rules vs live telemetry
	// and persist alerts. Runs for the life of the process.
	go notifengine.New(pool, tel, 60*time.Second).Run(ctx)
	// Background collector: engine hours, odometer sync, battery tamper and
	// geofence enter/exit transitions from the live feed.
	go telemetrysync.New(pool, tel, 2*time.Minute).Run(ctx)
	// Dashcam alarms: poll the telemetry service and raise FMS alerts. No-op
	// effect until cameras report; safe when telemetry has none.
	go dashcam.NewAlarmPoller(pool, tel).Run(ctx)
	// Completes pending clip saves in the background (start Save -> ready/failed).
	go dashcamH.ReconcileClips(ctx)
	// Retention sweeper: the app DB has no cold tier of its own, so this bounds
	// the append-only tables — pruning zero-value logs and archiving aged
	// alerts/driving_events to the cold bucket before deleting them. The same
	// sweeper backs the admin restore endpoint (retentionH).
	retentionSweeper := retention.New(pool, cold, cfg.Retention)
	go retentionSweeper.Run(ctx)
	retentionH := retention.NewHandler(retentionSweeper)
	go dashcam.NewGreenPoller(pool, tel).Run(ctx)
	go webhook.NewDispatcher(pool).Run(ctx)
	// Nightly driver safety scoring from the daily rollup — needs the same
	// fleet-history service the insights use. Recomputes the current month
	// each run so scores stay current as days complete.
	scoreCollector := driverscore.New(pool, liveSvc, driverscore.DefaultPolicy)
	scorecardH.WithRecomputer(scoreCollector)
	go scoreCollector.Run(ctx)

	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Logger, middleware.Recoverer)
	r.Use(httpx.CORS(cfg.CORSOrigins))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	r.Get("/swagger/*", httpSwagger.WrapHandler)

	// Public (pre-auth) endpoints.
	authH.PublicRoutes(r)
	trackingH.PublicRoutes(r)

	// Authenticated endpoints.
	r.Group(func(pr chi.Router) {
		pr.Use(auth.Authenticator(tm))
		authH.PrivateRoutes(pr)
		gated(pr, rolesH.RequireRW("vehicles.view", "vehicles.edit"), vehicleH.Routes)
		fleetGroupH.Routes(pr)
		gated(pr, rolesH.RequireRW("drivers.view", "drivers.edit"), driverH.Routes)
		trackerH.Routes(pr)
		liveH.Routes(pr)
		alertH.Routes(pr)
		gated(pr, chain(featureH.RequireFeature("controltower"), rolesH.Require("controltower.view")), controlH.Routes)
		gated(pr, rolesH.RequireRW("notifications.view", "notifications.manage"), notifRuleH.Routes)
		gated(pr, rolesH.Require("reports.view"), reportH.Routes)
		gated(pr, chain(featureH.RequireFeature("custom_report"), rolesH.Require("reports.custom")), customReportH.Routes)
		gated(pr, chain(featureH.RequireFeature("ai_reporting"), rolesH.Require("reports.ai")), aiReportH.Routes)
		aiRuleH.Routes(pr)
		gated(pr, chain(featureH.RequireFeature("license"), rolesH.RequireRW("license.view", "license.manage")), licenseH.Routes)
		gated(pr, chain(featureH.RequireFeature("maintenance"), rolesH.RequireRW("maintenance.view", "maintenance.manage")), maintenanceH.Routes)
		scorecardH.Routes(pr)
		gated(pr, chain(featureH.RequireFeature("share_link"), rolesH.Require("share_link.view")), trackingH.Routes)
		usersH.Routes(pr)
		gated(pr, chain(featureH.RequireFeature("dashcams"), rolesH.Require("dashcams.manage")), dashcamH.Routes)
		tenantH.Routes(pr)
		featureH.Routes(pr)
		rolesH.Routes(pr)
		apiTokenH.Routes(pr)
		webhookH.Routes(pr)
		dashboardH.Routes(pr)
		gated(pr, chain(featureH.RequireFeature("geofence"), rolesH.RequireRW("geofence.view", "geofence.manage")), geofenceH.Routes)
		fuelH.Routes(pr)
		fuelAnalysisH.Routes(pr)
		insightH.Routes(pr)
		gated(pr, chain(featureH.RequireFeature("hangars"), rolesH.RequireRW("hangars.view", "hangars.manage")), hangarH.Routes)
		documentH.Routes(pr)
		telemetrySyncH.Routes(pr)
		gated(pr, chain(featureH.RequireFeature("ai_dashboard"), rolesH.Require("dashboard.ai")), aiDashH.Routes)
		companyH.Routes(pr)
		prefsH.Routes(pr)
		retentionH.Routes(pr)
		platformH.Routes(pr)
	})

	// External integration API: authenticated by a per-tenant API token (not a
	// user JWT), read-only, keyed by IMEI. Its own group so it never inherits the
	// JWT authenticator. RLS still scopes every query to the token's tenant.
	r.Group(func(ir chi.Router) {
		ir.Use(apiTokenH.Authenticator())
		liveH.IntegrationRoutes(ir)
	})

	log.Printf("fms-backend api on %s  (swagger: http://localhost%s/swagger/index.html)", cfg.Addr, cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, r))
}
