// Package driver provides CRUD for the employee (driver/staff) registry.
package driver

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
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
type Handler struct{ pool *db.Pool }

// NewHandler builds the driver handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

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
	tid := auth.TenantID(r.Context())
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
	var dto DTO
	err = db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		e, err := store.New(tx).CreateEmployee(r.Context(), store.CreateEmployeeParams{
			TenantID: tid, FullName: req.FullName, EmployeeNo: req.EmployeeNo, Phone: req.Phone,
			Role: iface(req.Role), LicenseNo: req.LicenseNo, LicenseClass: req.LicenseClass,
			LicenseExpiry: exp, Status: iface(req.Status),
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
	err = db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		e, err := store.New(tx).UpdateEmployee(r.Context(), store.UpdateEmployeeParams{
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
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		return store.New(tx).DeleteEmployee(r.Context(), id)
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
