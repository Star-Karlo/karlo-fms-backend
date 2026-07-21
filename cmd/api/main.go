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
	"github.com/karlo/fms-backend/internal/alert"
	"github.com/karlo/fms-backend/internal/apitoken"
	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/company"
	"github.com/karlo/fms-backend/internal/controltower"
	"github.com/karlo/fms-backend/internal/customreport"
	"github.com/karlo/fms-backend/internal/dashboard"
	"github.com/karlo/fms-backend/internal/driver"
	"github.com/karlo/fms-backend/internal/fleetgroup"
	"github.com/karlo/fms-backend/internal/document"
	"github.com/karlo/fms-backend/internal/fuel"
	"github.com/karlo/fms-backend/internal/geofence"
	"github.com/karlo/fms-backend/internal/license"
	"github.com/karlo/fms-backend/internal/liveview"
	"github.com/karlo/fms-backend/internal/mailer"
	"github.com/karlo/fms-backend/internal/maintenance"
	"github.com/karlo/fms-backend/internal/notifengine"
	"github.com/karlo/fms-backend/internal/notifrule"
	"github.com/karlo/fms-backend/internal/platform/config"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/prefs"
	"github.com/karlo/fms-backend/internal/report"
	"github.com/karlo/fms-backend/internal/scorecard"
	"github.com/karlo/fms-backend/internal/telemetry"
	"github.com/karlo/fms-backend/internal/telemetrysync"
	"github.com/karlo/fms-backend/internal/tracker"
	"github.com/karlo/fms-backend/internal/trackinglink"
	"github.com/karlo/fms-backend/internal/users"
	"github.com/karlo/fms-backend/internal/vehicle"
)

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
	alertH := alert.NewHandler(pool)
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
	usersH := users.NewHandler(pool)
	apiTokenH := apitoken.NewHandler(pool)
	dashboardH := dashboard.NewHandler(pool)
	// AI dashboard composition (Amazon Nova Pro on Bedrock). A missing or
	// unauthorised Bedrock config disables just this endpoint (503) rather
	// than failing startup.
	aiDashH := aidash.NewHandler(ctx, cfg.AWSRegion, cfg.BedrockModelID)
	geofenceH := geofence.NewHandler(pool)
	fuelH := fuel.NewHandler(pool)
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
		vehicleH.Routes(pr)
		fleetGroupH.Routes(pr)
		driverH.Routes(pr)
		trackerH.Routes(pr)
		liveH.Routes(pr)
		alertH.Routes(pr)
		controlH.Routes(pr)
		notifRuleH.Routes(pr)
		reportH.Routes(pr)
		customReportH.Routes(pr)
		aiReportH.Routes(pr)
		licenseH.Routes(pr)
		maintenanceH.Routes(pr)
		scorecardH.Routes(pr)
		trackingH.Routes(pr)
		usersH.Routes(pr)
		apiTokenH.Routes(pr)
		dashboardH.Routes(pr)
		geofenceH.Routes(pr)
		fuelH.Routes(pr)
		documentH.Routes(pr)
		telemetrySyncH.Routes(pr)
		aiDashH.Routes(pr)
		companyH.Routes(pr)
		prefsH.Routes(pr)
	})

	log.Printf("fms-backend api on %s  (swagger: http://localhost%s/swagger/index.html)", cfg.Addr, cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, r))
}
