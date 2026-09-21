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
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
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
	"github.com/karlo/fms-backend/internal/driversync"
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
	"github.com/karlo/fms-backend/internal/platform/authctx"
	"github.com/karlo/fms-backend/internal/platform/config"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/platform/iamclient"
	"github.com/karlo/fms-backend/internal/platform/mdclient"
	"github.com/karlo/fms-backend/internal/platform/mdevents"
	"github.com/karlo/fms-backend/internal/platform/mdwrite"
	"github.com/karlo/fms-backend/internal/platform/tenantdir"
	"github.com/karlo/fms-backend/internal/platformadmin"
	"github.com/karlo/fms-backend/internal/prefs"
	"github.com/karlo/fms-backend/internal/report"
	"github.com/karlo/fms-backend/internal/retention"
	"github.com/karlo/fms-backend/internal/roles"
	"github.com/karlo/fms-backend/internal/routing"
	"github.com/karlo/fms-backend/internal/scorecard"
	"github.com/karlo/fms-backend/internal/sop"
	"github.com/karlo/fms-backend/internal/telemetry"
	"github.com/karlo/fms-backend/internal/telemetrysync"
	"github.com/karlo/fms-backend/internal/tenantsync"
	"github.com/karlo/fms-backend/internal/tracker"
	"github.com/karlo/fms-backend/internal/trackersync"
	"github.com/karlo/fms-backend/internal/trackinglink"
	"github.com/karlo/fms-backend/internal/vehicle"
	"github.com/karlo/fms-backend/internal/vehiclesync"
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

	// Single sign-on against the shared authentication service. Off unless a
	// public key is configured, so this is inert on a deployment that has not
	// cut over. Fatal if a key IS configured and cannot be read: starting
	// anyway would silently refuse every token from the auth service, which
	// looks like an outage in the login rather than a missing file here.
	sso, err := auth.NewSSO(authPublicKey(cfg.AuthPublicKeyPath, cfg.AuthPublicKey))
	if err != nil {
		log.Fatalf("sso: %v", err)
	}
	if sso != nil {
		log.Printf("sso: accepting access tokens from the shared authentication service")
		// Platform revocations (a TMS logout, a suspension) bite on the next
		// request. REDIS_ADDR is the platform's redis.karlo.internal in Jakarta;
		// empty in local runs, where signature + expiry remain the only check.
		if cfg.RedisAddr != "" {
			rev := authctx.NewRevocations()
			rev.Follow(ctx, cfg.RedisAddr)
			sso.WithRevocations(rev)
		}
	}

	// Which customers exist is decided by the authentication service now. The
	// fourteen background workers and dropdowns that iterate tenants keep
	// reading the local table; this keeps that table in step, and — the part
	// that matters — creates the row for a customer onboarded upstream, who
	// would otherwise be skipped by every one of them in silence.
	iamConn, err := iamclient.New(cfg.AuthGRPCTarget, "fms-backend", cfg.ServiceToken)
	if err != nil {
		log.Fatalf("auth grpc: %v", err)
	}
	// Declared as the INTERFACE and assigned only when there is a real
	// directory. `var d *tenantdir.Directory` assigned unconditionally would
	// give a non-nil interface holding a nil pointer, and the nil check in
	// actingFor would pass straight through to a method call on nil.
	var tenantDir auth.TenantResolver
	// The concrete directory, for the write-through's tenant -> company lookup.
	// Nil when there is no directory; mdwrite.CallerFor then refuses any staff
	// write made via ?account_id rather than misfiling it upstream.
	var companyDir mdwrite.CompanyResolver
	var syncWakers []mdevents.Waker // the projections POST /v1/platform/sync refreshes
	if iamConn != nil {
		defer iamConn.Close()
		// Entitlements as they are NOW (Company Access), not as the token
		// had them: one ValidateToken per user per minute.
		if sso != nil {
			sso.WithLiveAccess(iamConn, time.Minute)
			log.Printf("sso: FMS access resolved live from the authentication service (cached 1m per user)")
		}
		if dir := tenantdir.New(iamConn, time.Minute); dir != nil {
			tenantDir = dir
			companyDir = dir
			if sync := tenantsync.New(pool, dir); sync != nil {
				go sync.Run(ctx, 5*time.Minute)
				log.Printf("tenantsync: projecting companies from %s", cfg.AuthGRPCTarget)
			}

			// The fleet projection needs BOTH: the directory to know which
			// companies are FMS customers, and master data to read their
			// vehicles. Nested here rather than beside it because without a
			// directory there is nothing to iterate.
			mdConn, err := mdclient.New(cfg.MDGRPCTarget, "fms-backend", cfg.MDServiceToken)
			if err != nil {
				log.Fatalf("masterdata grpc: %v", err)
			}
			if mdConn != nil {
				defer mdConn.Close()
				// Two minutes is the safety net; a change notice on the
				// platform Redis (mdevents) or POST /v1/platform/sync
				// refreshes at once.
				var wakers []mdevents.Waker
				if vs := vehiclesync.New(pool, dir, mdConn); vs != nil {
					go vs.Run(ctx, 2*time.Minute)
					wakers = append(wakers, vs)
					log.Printf("vehiclesync: projecting the fleet from %s", cfg.MDGRPCTarget)
				}
				// Drivers need the same projection as vehicles, and for a
				// sharper reason: without a local employees row a driver has no
				// bigint, and vehicles.current_driver_id cannot reference them
				// at all. A company onboarded after the migration could not
				// assign ANY driver until this ran.
				if ds := driversync.New(pool, dir, mdConn); ds != nil {
					go ds.Run(ctx, 2*time.Minute)
					wakers = append(wakers, ds)
					log.Printf("driversync: projecting drivers from %s", cfg.MDGRPCTarget)
				}
				// Devices last: a fitting resolves through vehicles.master_data_id,
				// which vehiclesync fills on the same notice.
				if ts := trackersync.New(pool, dir, mdConn, cfg.TrackerFittings); ts != nil {
					go ts.Run(ctx, 2*time.Minute)
					wakers = append(wakers, ts)
					log.Printf("trackersync: projecting devices from %s (fittings: %v)", cfg.MDGRPCTarget, cfg.TrackerFittings)
				}
				mdevents.Follow(ctx, cfg.RedisAddr, wakers...)
				syncWakers = wakers
			}
		}
	}
	authH := auth.NewHandler(auth.NewService(pool, tm)).WithCookieDomain(cfg.AuthCookieDomain)
	if up := auth.NewUpstream(cfg.AuthServiceURL, 10*time.Second); up != nil {
		authH = authH.WithUpstream(up, sso)
		log.Printf("auth: login and refresh delegate to %s", cfg.AuthServiceURL)
	}
	// WRITE-THROUGH to master data, as the signed-in user.
	//
	// Separate from mdConn above and deliberately so: that one is the background
	// projection and carries a SERVICE token; this carries the CALLER'S bearer,
	// forwarded per request. Using a service token for writes would authorise
	// fms-the-service rather than the person, so an FMS user without
	// vehicles.edit upstream would succeed where master data means to refuse
	// them.
	//
	// Unset MD_SERVICE_URL leaves both nil and writes stay local — the state
	// before cutover, and what produced vehicles TMS could not see.
	mdWriter := mdwrite.New(cfg.MDServiceURL, 10*time.Second)
	if mdWriter != nil {
		log.Printf("mdwrite: vehicle and driver writes also go to %s as the calling user", cfg.MDServiceURL)
	}

	vehicleH := vehicle.NewHandler(vehicle.NewService(pool).WithMasterData(mdWriter)).WithResolver(companyDir)
	fleetGroupH := fleetgroup.NewHandler(fleetgroup.NewService(pool))
	driverH := driver.NewHandler(pool).WithMasterData(mdWriter).WithResolver(companyDir)
	trackerH := tracker.NewHandler(pool).WithMasterData(mdWriter).WithResolver(companyDir)
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
	dashcamH := dashcam.NewHandler(ctx, pool, tel, cfg.AWSRegion).WithMasterData(mdWriter, companyDir)
	featureH := features.NewHandler(pool)
	rolesH := roles.NewHandler(pool)
	platformH := platformadmin.NewHandler(pool, liveSvc).WithMasterData(mdWriter).WithResolver(companyDir).WithSyncWakers(syncWakers...)
	apiTokenH := apitoken.NewHandler(pool).WithIAM(iamConn)
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

	// Public (pre-auth) endpoints, rate-limited per client IP. Login is also
	// throttled by the auth service, but a brute-force or a scraper of public
	// tracking links should be stopped here before it costs an upstream call.
	// Keyed by RealIP, which chi's middleware set from X-Forwarded-For (the
	// ALB) above.
	r.Group(func(pub chi.Router) {
		pub.Use(httprate.LimitByIP(30, time.Minute))
		authH.PublicRoutes(pub)
	})
	r.Group(func(pub chi.Router) {
		pub.Use(httprate.LimitByIP(60, time.Minute))
		trackingH.PublicRoutes(pub)
	})

	// Authenticated endpoints.
	r.Group(func(pr chi.Router) {
		pr.Use(auth.AuthenticatorWith(tm, sso, tenantDir))
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
		gated(pr, chain(featureH.RequireFeature("dashcams"), rolesH.Require("dashcams.manage")), dashcamH.Routes)
		featureH.Routes(pr)
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
		sop.NewHandler(pool).Routes(pr)
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
	// Bounded server: a client that opens a connection and never finishes its
	// headers, or trickles a body, must not hold a worker forever (slowloris).
	// WriteTimeout is generous because history and report responses are big;
	// long-running work (report render, clip download) is asynchronous anyway.
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	log.Fatal(srv.ListenAndServe())
}

// mustReadKey loads the auth service's public key, or returns nil when none is
// configured — which disables SSO rather than failing, because a deployment
// that has not cut over must keep starting.
//
// A configured path that cannot be read IS fatal. Continuing would leave a
// service that starts cleanly and then refuses every token from the auth
// service, which gets diagnosed as a login outage rather than a missing file.
func authPublicKey(path, inline string) []byte {
	if path == "" {
		// Inline, as it arrives in AWS. Trimmed because a parameter store value
		// picks up a trailing newline on its way through a shell, and
		// ParseRSAPublicKeyFromPEM is not fussy about that — but being explicit
		// costs nothing and the failure it prevents is a service that starts
		// and then refuses every token.
		return []byte(strings.TrimSpace(inline))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("auth public key %s: %v", path, err)
	}
	return b
}
