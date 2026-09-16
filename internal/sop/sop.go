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
	SopEnabled       bool    `json:"sop_enabled"`
	SpeedLimitKmh    float64 `json:"speed_limit_kmh"`
	OverspeedMin     int     `json:"overspeed_min"`
	StopMaxMin       int     `json:"stop_max_min"`
	OfflineMin       int     `json:"offline_min"`
	SensorTimeoutMin int     `json:"sensor_timeout_min"`
	IdleMin          int     `json:"idle_min"`
	// FuelPricePerLitre is the price in force for this company: its own
	// fixed price when set, otherwise the national price for FuelType.
	FuelPricePerLitre float64 `json:"fuel_price_per_litre"`
	// CustomFuelPrice is the company's own price, or nil = follow the
	// national price. On PUT, send null to follow, a number to fix it.
	CustomFuelPrice *float64 `json:"custom_fuel_price"`
	FuelType        string   `json:"fuel_type"`
	// FuelPriceSource explains where FuelPricePerLitre came from.
	FuelPriceSource string  `json:"fuel_price_source"` // national | custom
	FuelPriceSince  string  `json:"fuel_price_since,omitempty"`
	FuelPriceRef    string  `json:"fuel_price_ref,omitempty"`
	IdleLph         float64 `json:"idle_lph"`
	UpdatedAt       string  `json:"updated_at,omitempty"`
}

// NationalPrice is one line of the platform's fuel-price catalogue.
type NationalPrice struct {
	FuelType      string  `json:"fuel_type"`
	Price         float64 `json:"price"`
	EffectiveFrom string  `json:"effective_from"`
	Source        string  `json:"source,omitempty"`
	Note          string  `json:"note,omitempty"`
}

// fallbackPrice: only when the catalogue has no row for the fuel type at all.
const fallbackPrice = 6800.0

func fromRow(ctx context.Context, q *store.Queries, r store.CompanySopSetting) Settings {
	out := Settings{
		SopEnabled: r.SopEnabled, SpeedLimitKmh: numF(r.SpeedLimitKmh), OverspeedMin: int(r.OverspeedMin),
		StopMaxMin: int(r.StopMaxMin), OfflineMin: int(r.OfflineMin), SensorTimeoutMin: int(r.SensorTimeoutMin),
		IdleMin: int(r.IdleMin), FuelType: r.FuelType, IdleLph: numF(r.IdleLph),
		UpdatedAt: r.UpdatedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if custom := conv.NumF(r.FuelPricePerLitre); custom != nil && *custom > 0 {
		out.CustomFuelPrice = custom
		out.FuelPricePerLitre = *custom
		out.FuelPriceSource = "custom"
		return out
	}
	out.FuelPriceSource = "national"
	if np, err := q.LatestFuelPrice(ctx, r.FuelType); err == nil {
		out.FuelPricePerLitre = numF(np.Price)
		out.FuelPriceSince = np.EffectiveFrom.Time.Format("2006-01-02")
		if np.Source != nil {
			out.FuelPriceRef = *np.Source
		}
	} else {
		out.FuelPricePerLitre = fallbackPrice
	}
	return out
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
	q := store.New(tx)
	row, err := q.GetSopSettings(ctx, tenantID)
	if err != nil {
		return Settings{}, err
	}
	return fromRow(ctx, q, row), nil
}

type Handler struct{ pool *db.Pool }

func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/company/sop", h.get)
	r.Get("/v1/fuel/prices", h.prices)
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RoleAdmin))
		ar.Put("/v1/company/sop", h.update)
	})
	// The catalogue is platform-wide: Karlo staff record a national price
	// change once and every company that follows it picks it up.
	r.Group(func(pr chi.Router) {
		pr.Use(auth.RequireRole(auth.RolePlatformAdmin))
		pr.Post("/v1/platform/fuel-prices", h.addPrice)
	})
}

// prices godoc
//
//	@Summary	National fuel-price catalogue (newest first per fuel type)
//	@Tags		fuel
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{array}	sop.NationalPrice
//	@Router		/v1/fuel/prices [get]
func (h *Handler) prices(w http.ResponseWriter, r *http.Request) {
	var out []NationalPrice
	err := db.SystemTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		rows, err := store.New(tx).ListFuelPrices(r.Context())
		if err != nil {
			return err
		}
		out = make([]NationalPrice, 0, len(rows))
		for _, p := range rows {
			np := NationalPrice{FuelType: p.FuelType, Price: numF(p.Price), EffectiveFrom: p.EffectiveFrom.Time.Format("2006-01-02")}
			if p.Source != nil {
				np.Source = *p.Source
			}
			if p.Note != nil {
				np.Note = *p.Note
			}
			out = append(out, np)
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "could not list prices")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// addPrice godoc
//
//	@Summary	Record a national fuel price change (platform staff)
//	@Tags		fuel
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		sop.NationalPrice	true	"price"
//	@Success	200		{object}	sop.NationalPrice
//	@Router		/v1/platform/fuel-prices [post]
func (h *Handler) addPrice(w http.ResponseWriter, r *http.Request) {
	var in NationalPrice
	if err := httpx.DecodeJSON(r, &in); err != nil || in.FuelType == "" || in.Price <= 0 {
		httpx.Error(w, http.StatusBadRequest, "fuel_type and a positive price are required")
		return
	}
	eff, err := conv.Date(&in.EffectiveFrom)
	if err != nil || !eff.Valid {
		httpx.Error(w, http.StatusBadRequest, "effective_from must be YYYY-MM-DD")
		return
	}
	var by *int64
	if uid := auth.UserID(r.Context()); uid != 0 {
		by = &uid
	}
	var src, note *string
	if in.Source != "" {
		src = &in.Source
	}
	if in.Note != "" {
		note = &in.Note
	}
	err = db.SystemTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		_, err := store.New(tx).InsertFuelPrice(r.Context(), store.InsertFuelPriceParams{
			FuelType: in.FuelType, Price: conv.Num(&in.Price), EffectiveFrom: eff, Source: src, Note: note, CreatedBy: by,
		})
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "could not save price")
		return
	}
	httpx.JSON(w, http.StatusOK, in)
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
	if in.FuelType == "" {
		in.FuelType = "biosolar"
	}
	if in.SpeedLimitKmh <= 0 || in.SpeedLimitKmh > 200 || in.OverspeedMin < 1 || in.StopMaxMin < 5 || in.OfflineMin < 5 ||
		in.SensorTimeoutMin < 5 || in.IdleMin < 1 || (in.CustomFuelPrice != nil && *in.CustomFuelPrice <= 0) || in.IdleLph <= 0 {
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
			FuelPricePerLitre: conv.Num(in.CustomFuelPrice), FuelType: in.FuelType, IdleLph: conv.Num(&in.IdleLph), UpdatedBy: by,
		})
		if err != nil {
			return err
		}
		out = fromRow(r.Context(), store.New(tx), row)
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "could not save settings")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
