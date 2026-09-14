// Package driver provides CRUD for the employee (driver/staff) registry.
package driver

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/platform/mdwrite"
	"github.com/karlo/fms-backend/internal/store"
)

// DTO is the JSON shape of an employee/driver.
type DTO struct {
	ID            int64      `json:"id"`
	EmployeeNo    *string    `json:"employee_no"`
	FullName      string     `json:"full_name"`
	Phone         *string    `json:"phone"`
	Role          string     `json:"role"`
	LicenseNo     *string    `json:"license_no"`
	LicenseClass  *string    `json:"license_class"`
	LicenseExpiry *string    `json:"license_expiry"`
	Status        string     `json:"status"`
	CreatedAt     *time.Time `json:"created_at"`
	UpdatedAt     *time.Time `json:"updated_at"`
}

// CreateRequest is the create body.
type CreateRequest struct {
	EmployeeNo    *string `json:"employee_no"`
	FullName      string  `json:"full_name"`
	Phone         *string `json:"phone"`
	Role          *string `json:"role"`
	LicenseNo     *string `json:"license_no"`
	LicenseClass  *string `json:"license_class"`
	LicenseExpiry *string `json:"license_expiry"`
	Status        *string `json:"status"`
}

// UpdateRequest is the update body (patch-style).
type UpdateRequest struct {
	EmployeeNo    *string `json:"employee_no"`
	FullName      *string `json:"full_name"`
	Phone         *string `json:"phone"`
	Role          *string `json:"role"`
	LicenseNo     *string `json:"license_no"`
	LicenseClass  *string `json:"license_class"`
	LicenseExpiry *string `json:"license_expiry"`
	Status        *string `json:"status"`
}

func fromRow(e store.Employee) DTO {
	return DTO{
		ID: e.ID, EmployeeNo: e.EmployeeNo, FullName: e.FullName, Phone: e.Phone, Role: e.Role,
		LicenseNo: e.LicenseNo, LicenseClass: e.LicenseClass, LicenseExpiry: conv.DateStr(e.LicenseExpiry),
		Status: e.Status, CreatedAt: conv.Time(e.CreatedAt), UpdatedAt: conv.Time(e.UpdatedAt),
	}
}

func iface(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

// Handler exposes driver endpoints.
type Handler struct {
	pool *db.Pool
	md   *mdwrite.Client         // nil before cutover: writes stay local
	dir  mdwrite.CompanyResolver // tenant id -> company, for staff acting via ?account_id
}

// NewHandler builds the driver handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// WithMasterData makes writes reach karlo_masterdata as well as fms_app.
//
// Without it a driver created here exists only in FMS: TMS never sees them, and
// — because FMS's own screens create drivers upstream — the reverse case leaves
// a master-data driver with no employees row, no bigint, and no way to be
// attached to a vehicle. For a company onboarded after the migration that meant
// every driver was unassignable.
func (h *Handler) WithMasterData(md *mdwrite.Client) *Handler { h.md = md; return h }

// WithResolver — see vehicle.Handler.WithResolver.
func (h *Handler) WithResolver(dir mdwrite.CompanyResolver) *Handler { h.dir = dir; return h }

func (h *Handler) caller(w http.ResponseWriter, r *http.Request) (mdwrite.Caller, bool) {
	c, err := mdwrite.CallerFor(r, auth.TenantID(r.Context()), auth.EffectiveTenant(r), h.dir)
	if err != nil {
		httpx.Error(w, http.StatusConflict, err.Error())
		return c, false
	}
	return c, true
}

// mdDriver maps an FMS create/update onto master data's vocabulary. Only the
// fields master data OWNS travel: role and the FMS bigint are ours.
func mdDriver(employeeNo, fullName, phone, licenseNo, licenseClass, licenseExpiry, status *string) mdwrite.Driver {
	d := mdwrite.Driver{}
	if fullName != nil {
		d.FullName = *fullName
	}
	if phone != nil {
		d.Phone = *phone
	}
	if employeeNo != nil {
		d.EmployeeNo = *employeeNo
	}
	if licenseNo != nil {
		d.LicenseNo = *licenseNo
	}
	if licenseClass != nil {
		d.LicenseClass = *licenseClass
	}
	if licenseExpiry != nil {
		d.LicenseExpiry = *licenseExpiry
	}
	if status != nil {
		d.Status = *status
	}
	return d
}

// mdStatus turns a master-data failure into the status the CALLER should see.
//
// A 403 from master data is usually correct — the user may genuinely not create
// drivers there — and reporting it as a 500 would send someone hunting a bug in
// FMS instead of granting a permission. Same for a 409 on a duplicate.
func mdStatus(err error) (int, string) {
	var e *mdwrite.Error
	if errors.As(err, &e) {
		switch e.Status {
		case http.StatusForbidden, http.StatusUnauthorized, http.StatusConflict, http.StatusBadRequest:
			return e.Status, "master data refused this: " + e.Body
		}
	}
	return http.StatusBadGateway, "could not reach master data: " + err.Error()
}

// Routes mounts driver endpoints (reads: any user; writes: manager+).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/drivers", h.list)
	r.Get("/v1/drivers/{id}", h.get)
	r.Group(func(mr chi.Router) {
		mr.Use(auth.RequireRole(auth.RoleManager))
		mr.Post("/v1/drivers", h.create)
		mr.Put("/v1/drivers/{id}", h.update)
		mr.Delete("/v1/drivers/{id}", h.del)
	})
}

func id64(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil
}

// list godoc
//
//	@Summary	List drivers/employees
//	@Tags		drivers
//	@Security	BearerAuth
//	@Produce	json
//	@Param		q		query		string	false	"search"
//	@Param		role	query		string	false	"filter role"
//	@Param		status	query		string	false	"filter status"
//	@Success	200	{object}	httpx.List[driver.DTO]
//	@Router		/v1/drivers [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParseList(r)
	role := conv.StrPtr(r.URL.Query().Get("role"))
	// Platform admin may list another account's drivers via ?account_id, so the
	// vehicle form can assign a driver when editing that account's vehicle.
	tid := auth.EffectiveTenant(r)
	var items []DTO
	var total int64
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		q := store.New(tx)
		rows, err := q.ListEmployees(r.Context(), store.ListEmployeesParams{Q: conv.StrPtr(p.Q), Role: role, Status: conv.StrPtr(p.Status), Lim: int32(p.Limit), Off: int32(p.Offset)})
		if err != nil {
			return err
		}
		items = make([]DTO, 0, len(rows))
		for _, e := range rows {
			items = append(items, fromRow(e))
		}
		total, err = q.CountEmployees(r.Context(), store.CountEmployeesParams{Q: conv.StrPtr(p.Q), Role: role, Status: conv.StrPtr(p.Status)})
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[DTO]{Items: items, Total: total})
}

// get godoc
//
//	@Summary	Get a driver
//	@Tags		drivers
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path		int	true	"id"
//	@Success	200	{object}	driver.DTO
//	@Router		/v1/drivers/{id} [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var dto DTO
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		e, err := store.New(tx).GetEmployee(r.Context(), id)
		if err != nil {
			return err
		}
		dto = fromRow(e)
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	httpx.JSON(w, http.StatusOK, dto)
}

// create godoc
//
//	@Summary	Create a driver
//	@Tags		drivers
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		driver.CreateRequest	true	"driver"
//	@Success	201		{object}	driver.DTO
//	@Router		/v1/drivers [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil || req.FullName == "" {
		httpx.Error(w, http.StatusBadRequest, "full_name is required")
		return
	}
	exp, err := conv.Date(req.LicenseExpiry)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "license_expiry must be YYYY-MM-DD")
		return
	}
	tid := auth.TenantID(r.Context())

	// MASTER DATA FIRST, on purpose.
	//
	// Master data owns the driver register; FMS holds a projection of it. Create
	// upstream first and the local row can be inserted already carrying the
	// pairing — born assignable. Do it the other way round and a failure
	// upstream leaves a local driver that TMS cannot see and that nothing will
	// ever pair, which is precisely the state this whole change exists to end.
	//
	// The cost of this ordering is the opposite failure: master data created,
	// local insert fails, and there is now a driver upstream with no FMS row.
	// That one is recoverable — the backfill pairs it — and it is visible in
	// both products, so it is the better direction to fail in.
	var mdID *string
	var caller mdwrite.Caller
	if h.md != nil {
		var ok bool
		if caller, ok = h.caller(w, r); !ok {
			return
		}
		id, err := h.md.CreateDriver(r.Context(), caller, mdDriver(
			req.EmployeeNo, &req.FullName, req.Phone,
			req.LicenseNo, req.LicenseClass, req.LicenseExpiry, req.Status,
		))
		if err != nil {
			code, msg := mdStatus(err)
			httpx.Error(w, code, msg)
			return
		}
		if id != "" {
			mdID = &id
		}
	}

	var dto DTO
	err = db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		e, err := store.New(tx).CreateEmployee(r.Context(), store.CreateEmployeeParams{
			TenantID: tid, FullName: req.FullName, EmployeeNo: req.EmployeeNo, Phone: req.Phone,
			Role: iface(req.Role), LicenseNo: req.LicenseNo, LicenseClass: req.LicenseClass,
			LicenseExpiry: exp, Status: iface(req.Status), MasterDataID: mdID,
		})
		if err != nil {
			return err
		}
		dto = fromRow(e)
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Point master data back at the FMS row. Best-effort AFTER the commit: the
	// driver is already usable in both products without it, and failing the
	// request here would report an error for a create that succeeded. The
	// backfill re-stamps anything that misses.
	if h.md != nil && mdID != nil {
		if err := h.md.StampDriverFMSID(r.Context(), caller, *mdID, dto.ID); err != nil {
			log.Printf("driver: created %d / master data %s but could not stamp fmsEmployeeId: %v", dto.ID, *mdID, err)
		}
	}
	httpx.JSON(w, http.StatusCreated, dto)
}

// update godoc
//
//	@Summary	Update a driver
//	@Tags		drivers
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int						true	"id"
//	@Param		body	body		driver.UpdateRequest	true	"fields"
//	@Success	200		{object}	driver.DTO
//	@Router		/v1/drivers/{id} [put]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req UpdateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	exp, err := conv.Date(req.LicenseExpiry)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "license_expiry must be YYYY-MM-DD")
		return
	}
	var dto DTO
	var mdID *string
	err = db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		q := store.New(tx)
		// Read the pairing before the write, inside the same transaction, so
		// the upstream call below targets the record this row actually belongs
		// to rather than one resolved by a second lookup that may have moved.
		if before, err := q.GetEmployee(r.Context(), id); err == nil {
			mdID = before.MasterDataID
		}
		e, err := q.UpdateEmployee(r.Context(), store.UpdateEmployeeParams{
			ID: id, EmployeeNo: req.EmployeeNo, FullName: req.FullName, Phone: req.Phone, Role: req.Role,
			LicenseNo: req.LicenseNo, LicenseClass: req.LicenseClass, LicenseExpiry: exp, Status: req.Status,
		})
		if err != nil {
			return err
		}
		dto = fromRow(e)
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Mirror upstream. Local-first here, unlike create: the row already exists
	// in both places, so the ordering cannot strand anything — it can only leave
	// the two briefly out of step, which the next edit or a backfill corrects.
	//
	// An UNPAIRED driver is skipped rather than created upstream. It predates
	// write-through and belongs to the backfill, which pairs deliberately and in
	// bulk; inventing a master-data record here would race that and could
	// produce two upstream drivers for one person.
	if h.md != nil && mdID != nil {
		d := mdDriver(req.EmployeeNo, req.FullName, req.Phone,
			req.LicenseNo, req.LicenseClass, req.LicenseExpiry, req.Status)
		caller, ok := h.caller(w, r)
		if !ok {
			return
		}
		if err := h.md.UpdateDriver(r.Context(), caller, *mdID, d); err != nil {
			code, msg := mdStatus(err)
			// The local write has already committed, so this cannot be undone by
			// returning an error — but the caller must still be told the change
			// did not reach TMS, or they will believe it did.
			log.Printf("driver %d: local update committed, master data %s rejected it: %v", id, *mdID, err)
			httpx.Error(w, code, "saved in FMS, but master data rejected the change: "+msg)
			return
		}
	}
	httpx.JSON(w, http.StatusOK, dto)
}

// del godoc
//
//	@Summary	Delete a driver
//	@Tags		drivers
//	@Security	BearerAuth
//	@Param		id	path	int	true	"id"
//	@Success	204
//	@Router		/v1/drivers/{id} [delete]
func (h *Handler) del(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var mdID *string
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		q := store.New(tx)
		if before, err := q.GetEmployee(r.Context(), id); err == nil {
			mdID = before.MasterDataID
		}
		return q.DeleteEmployee(r.Context(), id)
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Remove upstream too, or the driver reappears: they remain in master data,
	// and a later driversync pass would project them straight back into FMS.
	if h.md != nil && mdID != nil {
		caller, ok := h.caller(w, r)
		if !ok {
			return
		}
		if err := h.md.DeleteDriver(r.Context(), caller, *mdID); err != nil {
			code, msg := mdStatus(err)
			log.Printf("driver %d: deleted locally, master data %s still holds it: %v", id, *mdID, err)
			httpx.Error(w, code, "removed from FMS, but master data refused the delete: "+msg)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
