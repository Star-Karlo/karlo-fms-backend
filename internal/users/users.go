// Package users provides admin management of system login accounts.
package users

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

// DTO is the public shape of a user (never the password hash).
type DTO struct {
	ID          int64      `json:"id"`
	Email       string     `json:"email"`
	FullName    *string    `json:"full_name"`
	Role        string     `json:"role"`
	Status      string     `json:"status"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   *time.Time `json:"created_at"`
}

// CreateRequest is the new-user body.
type CreateRequest struct {
	Email    string  `json:"email"`
	FullName *string `json:"full_name"`
	Role     string  `json:"role"`
	Password string  `json:"password"`
}

// UpdateRequest patches a user. A non-empty Password resets the login password
// (bcrypt-rehashed); omit it to leave the password unchanged.
type UpdateRequest struct {
	FullName *string `json:"full_name"`
	Role     *string `json:"role"`
	Status   *string `json:"status"`
	Password *string `json:"password"`
}

func fromRow(u store.User) DTO {
	return DTO{ID: u.ID, Email: u.Email, FullName: u.FullName, Role: u.Role, Status: u.Status, LastLoginAt: conv.Time(u.LastLoginAt), CreatedAt: conv.Time(u.CreatedAt)}
}

// Handler exposes user-management endpoints (admin only).
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts user endpoints — all require admin.
func (h *Handler) Routes(r chi.Router) {
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RoleAdmin))
		ar.Get("/v1/users", h.list)
		ar.Post("/v1/users", h.create)
		ar.Put("/v1/users/{id}", h.update)
		ar.Delete("/v1/users/{id}", h.del)
	})
}

func id64(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil
}

// list godoc
//
//	@Summary	List system users (admin)
//	@Tags		users
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	httpx.List[users.DTO]
//	@Router		/v1/users [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParseList(r)
	tid := auth.TenantID(r.Context())
	var items []DTO
	var total int64
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		st := store.New(tx)
		rows, err := st.ListUsers(r.Context(), store.ListUsersParams{Q: conv.StrPtr(p.Q), Status: conv.StrPtr(p.Status), Lim: int32(p.Limit), Off: int32(p.Offset)})
		if err != nil {
			return err
		}
		items = make([]DTO, 0, len(rows))
		for _, u := range rows {
			items = append(items, fromRow(u))
		}
		total, err = st.CountUsers(r.Context(), store.CountUsersParams{Q: conv.StrPtr(p.Q), Status: conv.StrPtr(p.Status)})
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[DTO]{Items: items, Total: total})
}

// create godoc
//
//	@Summary	Create a system user (admin)
//	@Tags		users
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		users.CreateRequest	true	"user"
//	@Success	201		{object}	users.DTO
//	@Router		/v1/users [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Email == "" || req.Password == "" {
		httpx.Error(w, http.StatusBadRequest, "email and password are required")
		return
	}
	if req.Role == "" {
		req.Role = auth.RoleOperator
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "hash failed")
		return
	}
	tid := auth.TenantID(r.Context())
	var dto DTO
	e := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		u, err := store.New(tx).CreateUser(r.Context(), store.CreateUserParams{
			TenantID: tid, Email: req.Email, PasswordHash: string(hash), FullName: req.FullName, Role: req.Role, Status: "active",
		})
		if err != nil {
			return err
		}
		dto = fromRow(u)
		return nil
	})
	if e != nil {
		httpx.Error(w, http.StatusBadRequest, e.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, dto)
}

// update godoc
//
//	@Summary	Update a system user (admin)
//	@Tags		users
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int					true	"id"
//	@Param		body	body		users.UpdateRequest	true	"fields"
//	@Success	200		{object}	users.DTO
//	@Router		/v1/users/{id} [put]
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
	if req.Password != nil && len(*req.Password) < 8 {
		httpx.Error(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	var dto DTO
	e := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		u, err := store.New(tx).UpdateUser(r.Context(), store.UpdateUserParams{ID: id, FullName: req.FullName, Role: req.Role, Status: req.Status})
		if err != nil {
			return err
		}
		// Optional password reset — RLS scopes this UPDATE to the admin's tenant,
		// so an admin can only reset users in their own account.
		if req.Password != nil && *req.Password != "" {
			hash, herr := bcrypt.GenerateFromPassword([]byte(*req.Password), 12)
			if herr != nil {
				return herr
			}
			if _, herr := tx.Exec(r.Context(), `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, string(hash)); herr != nil {
				return herr
			}
		}
		dto = fromRow(u)
		return nil
	})
	if e != nil {
		httpx.Error(w, http.StatusBadRequest, e.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, dto)
}

// del godoc
//
//	@Summary	Delete a system user (admin)
//	@Tags		users
//	@Security	BearerAuth
//	@Param		id	path	int	true	"id"
//	@Success	204
//	@Router		/v1/users/{id} [delete]
func (h *Handler) del(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	if id == auth.UserID(r.Context()) {
		httpx.Error(w, http.StatusBadRequest, "cannot delete yourself")
		return
	}
	e := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		return store.New(tx).DeleteUser(r.Context(), id)
	})
	if e != nil {
		httpx.Error(w, http.StatusInternalServerError, e.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
