// Package sop holds a company's operating-procedure parameters — the
// thresholds behind "kendaraan bergerak > X km/h selama > 30 menit",
// "berhenti > 5 jam", "offline > 30 menit" (feedback epic 1.1). They are the
// customer's numbers: FMS ships defaults and never decides them.
//
// GET /v1/company/sop    any authenticated user (the numbers explain the alerts)
// PUT /v1/company/sop    administrators
//
// The notification engine's SOP pass reads Settings each tick; fuel & cost
// reads fuel_price_per_litre and idle_lph.
package sop

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

// Settings is the wire shape.
type Settings struct {
	SopEnabled        bool    `json:"sop_enabled"`
	SpeedLimitKmh     float64 `json:"speed_limit_kmh"`
	OverspeedMin      int     `json:"overspeed_min"`
	StopMaxMin        int     `json:"stop_max_min"`
	OfflineMin        int     `json:"offline_min"`
	SensorTimeoutMin  int     `json:"sensor_timeout_min"`
	IdleMin           int     `json:"idle_min"`
	FuelPricePerLitre float64 `json:"fuel_price_per_litre"`
	IdleLph           float64 `json:"idle_lph"`
	UpdatedAt         string  `json:"updated_at,omitempty"`
}

func fromRow(r store.CompanySopSetting) Settings {
	return Settings{
		SopEnabled: r.SopEnabled, SpeedLimitKmh: numF(r.SpeedLimitKmh), OverspeedMin: int(r.OverspeedMin),
		StopMaxMin: int(r.StopMaxMin), OfflineMin: int(r.OfflineMin), SensorTimeoutMin: int(r.SensorTimeoutMin),
		IdleMin: int(r.IdleMin), FuelPricePerLitre: numF(r.FuelPricePerLitre), IdleLph: numF(r.IdleLph),
		UpdatedAt: r.UpdatedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func numF(n pgtype.Numeric) float64 {
	if f := conv.NumF(n); f != nil {
		return *f
	}
	return 0
}

// Load returns the company's settings (defaults on first call). Used by the
// engine and by fuel analysis; both run inside their own tenant transaction.
func Load(ctx context.Context, tx pgx.Tx, tenantID int64) (Settings, error) {
	row, err := store.New(tx).GetSopSettings(ctx, tenantID)
	if err != nil {
		return Settings{}, err
	}
	return fromRow(row), nil
}

type Handler struct{ pool *db.Pool }

func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/company/sop", h.get)
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RoleAdmin))
		ar.Put("/v1/company/sop", h.update)
	})
}

// get godoc
//
//	@Summary	The company's operating-procedure thresholds (SOP)
//	@Tags		company
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	sop.Settings
//	@Router		/v1/company/sop [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	var out Settings
	err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		var e error
		out, e = Load(r.Context(), tx, auth.EffectiveTenant(r))
		return e
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "could not load settings")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// update godoc
//
//	@Summary	Set the company's SOP thresholds (admin)
//	@Tags		company
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		sop.Settings	true	"settings"
//	@Success	200		{object}	sop.Settings
//	@Router		/v1/company/sop [put]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var in Settings
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	if in.SpeedLimitKmh <= 0 || in.SpeedLimitKmh > 200 || in.OverspeedMin < 1 || in.StopMaxMin < 5 || in.OfflineMin < 5 ||
		in.SensorTimeoutMin < 5 || in.IdleMin < 1 || in.FuelPricePerLitre <= 0 || in.IdleLph <= 0 {
		httpx.Error(w, http.StatusBadRequest, "a threshold is out of range")
		return
	}
	var by *int64
	if uid := auth.UserID(r.Context()); uid != 0 {
		by = &uid
	}
	var out Settings
	err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		row, err := store.New(tx).UpdateSopSettings(r.Context(), store.UpdateSopSettingsParams{
			TenantID: auth.EffectiveTenant(r), SopEnabled: in.SopEnabled,
			SpeedLimitKmh: conv.Num(&in.SpeedLimitKmh), OverspeedMin: int32(in.OverspeedMin), StopMaxMin: int32(in.StopMaxMin),
			OfflineMin: int32(in.OfflineMin), SensorTimeoutMin: int32(in.SensorTimeoutMin), IdleMin: int32(in.IdleMin),
			FuelPricePerLitre: conv.Num(&in.FuelPricePerLitre), IdleLph: conv.Num(&in.IdleLph), UpdatedBy: by,
		})
		if err != nil {
			return err
		}
		out = fromRow(row)
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "could not save settings")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
