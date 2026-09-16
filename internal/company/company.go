// Package company exposes the tenant's own company profile — legal name, NPWP,
// address, contact details and logo.
//
// THE TENANCY RULE IS DIFFERENT HERE, and it is the whole reason this package
// is small and careful.
//
// Every other table in this schema is protected by row-level security, so a
// forgotten WHERE clause returns nothing rather than someone else's data. The
// `tenants` table has no RLS: login must resolve a tenant before any tenant
// scope exists, and its rows describe the tenants themselves rather than data
// belonging to one. So the database will NOT catch a mistake here. An UPDATE
// without an explicit id would rewrite every customer's company profile.
//
// Consequently:
//   - the tenant id always comes from auth.TenantID(ctx), never from the request
//   - there is no endpoint that takes a tenant id as a parameter
//   - both statements below carry `WHERE id = $1` and are the only SQL here
package company

import (
	"fmt"
	"net/http"
	"time"

	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

const (
	logoURLTTL      = 15 * time.Minute
	maxLogoBytes    = 5 << 20 // 5 MB is generous for a logo
	logoDownloadTTL = 60 * time.Minute
)

// allowedLogoMIME is narrower than the document allowlist: a logo is displayed
// inline in the app and on reports, so PDF and HEIC are not useful here.
var allowedLogoMIME = map[string]string{
	"image/png":     ".png",
	"image/jpeg":    ".jpg",
	"image/webp":    ".webp",
	"image/svg+xml": ".svg",
}

// Profile is the company record.
type Profile struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Slug       string  `json:"slug"`
	LegalName  *string `json:"legal_name"`
	TaxID      *string `json:"tax_id"`
	Address    *string `json:"address"`
	City       *string `json:"city"`
	Province   *string `json:"province"`
	PostalCode *string `json:"postal_code"`
	Country    *string `json:"country"`
	Phone      *string `json:"phone"`
	Email      *string `json:"email"`
	Website    *string `json:"website"`
	// HasLogo rather than the key: the key is an internal detail and the client
	// only needs to know whether to show the logo or the upload prompt.
	HasLogo bool `json:"has_logo"`
}

// UpdateRequest patches the company profile. Name is required; everything else
// is optional and may be cleared by sending an empty string.
type UpdateRequest struct {
	Name       string  `json:"name"`
	LegalName  *string `json:"legal_name"`
	TaxID      *string `json:"tax_id"`
	Address    *string `json:"address"`
	City       *string `json:"city"`
	Province   *string `json:"province"`
	PostalCode *string `json:"postal_code"`
	Country    *string `json:"country"`
	Phone      *string `json:"phone"`
	Email      *string `json:"email"`
	Website    *string `json:"website"`
}

// Handler serves the company profile endpoints.
type Handler struct {
	pool    *pgxpool.Pool
	bucket  string
	s3      *s3.Client
	presign *s3.PresignClient
}

// NewHandler builds the handler. An empty bucket disables only the logo
// endpoints; the profile itself still works.
func NewHandler(ctx context.Context, pool *pgxpool.Pool, region, bucket string) *Handler {
	h := &Handler{pool: pool, bucket: bucket}
	if bucket == "" {
		return h
	}
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(region))
	if err != nil {
		return h
	}
	h.s3 = s3.NewFromConfig(cfg)
	h.presign = s3.NewPresignClient(h.s3)
	return h
}

// Routes mounts the company endpoints.
//
// The profile itself (name, address, tax id…) lives in the shared IAM since
// the platform cutover and is read/written there by the Company Profile
// page; this handler keeps only the logo, which is FMS-hosted in S3.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/company/logo", h.logoURL)
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RoleAdmin))
		ar.Post("/v1/company/logo", h.logoUpload)
		ar.Delete("/v1/company/logo", h.logoDelete)
	})
}

const selectCols = `id, name, slug, legal_name, tax_id, address, city, province,
	postal_code, country, phone, email, website, (logo_key IS NOT NULL AND logo_key <> '')`

// LogoUploadRequest asks for a logo upload slot.
type LogoUploadRequest struct {
	Filename string `json:"filename"`
	MIME     string `json:"mime"`
	Size     int64  `json:"size"`
}

// logoUpload godoc
//
//	@Summary		Presigned URL for uploading the company logo (admin only)
//	@Description	Same two-step flow as document upload: this returns a short-lived S3 PUT URL signed with the exact size and content type. One logo per company — uploading replaces the previous one.
//	@Tags			company
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		company.LogoUploadRequest	true	"file metadata"
//	@Success		200		{object}	map[string]interface{}
//	@Failure		400		{object}	map[string]string
//	@Failure		503		{object}	map[string]string
//	@Router			/v1/company/logo [post]
func (h *Handler) logoUpload(w http.ResponseWriter, r *http.Request) {
	if h.presign == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "file storage is not configured on this deployment")
		return
	}
	var req LogoUploadRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	ext, ok := allowedLogoMIME[req.MIME]
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "logo must be PNG, JPEG, WebP or SVG")
		return
	}
	if req.Size <= 0 || req.Size > maxLogoBytes {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("logo must be under %d MB", maxLogoBytes>>20))
		return
	}

	tenantID := auth.TenantID(r.Context())
	// Fixed key per tenant: a company has exactly one logo, so a new upload
	// overwrites rather than accumulating orphans. The bucket is versioned, so
	// the previous logo remains recoverable.
	key := fmt.Sprintf("tenant/%d/company/logo%s", tenantID, ext)

	if err := db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		_, e := tx.Exec(r.Context(),
			`UPDATE tenants SET logo_key = $2, updated_at = now() WHERE id = $1`, tenantID, key)
		return e
	}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	signed, err := h.presign.PresignPutObject(r.Context(), &s3.PutObjectInput{
		Bucket:        aws.String(h.bucket),
		Key:           aws.String(key),
		ContentType:   aws.String(req.MIME),
		ContentLength: aws.Int64(req.Size),
	}, s3.WithPresignExpires(logoURLTTL))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "could not create upload URL: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"upload_url":         signed.URL,
		"expires_in_seconds": int(logoURLTTL.Seconds()),
		"required_headers":   map[string]string{"Content-Type": req.MIME},
	})
}

// logoURL godoc
//
//	@Summary		Presigned URL to display the company logo
//	@Description	Longer-lived than a document link because the logo is rendered inline on every page that shows it; a five-minute URL would expire while a report was still open.
//	@Tags			company
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	map[string]string
//	@Failure		404	{object}	map[string]string
//	@Router			/v1/company/logo [get]
func (h *Handler) logoURL(w http.ResponseWriter, r *http.Request) {
	if h.presign == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "file storage is not configured on this deployment")
		return
	}
	tenantID := auth.TenantID(r.Context())
	var key *string
	err := db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT logo_key FROM tenants WHERE id = $1`, tenantID).Scan(&key)
	})
	if err != nil || key == nil || *key == "" {
		httpx.Error(w, http.StatusNotFound, "no logo uploaded")
		return
	}
	signed, err := h.presign.PresignGetObject(r.Context(), &s3.GetObjectInput{
		Bucket: aws.String(h.bucket), Key: key,
	}, s3.WithPresignExpires(logoDownloadTTL))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"url":                signed.URL,
		"expires_in_seconds": int(logoDownloadTTL.Seconds()),
	})
}

// logoDelete godoc
//
//	@Summary	Remove the company logo (admin only)
//	@Tags		company
//	@Security	BearerAuth
//	@Success	204
//	@Router		/v1/company/logo [delete]
func (h *Handler) logoDelete(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.TenantID(r.Context())
	var key *string
	err := db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(),
			`UPDATE tenants SET logo_key = NULL, updated_at = now() WHERE id = $1 RETURNING logo_key`,
			tenantID).Scan(&key)
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The record is authoritative and already cleared; an orphaned object in a
	// versioned bucket is not worth failing the request over.
	if h.s3 != nil && key != nil && *key != "" {
		_, _ = h.s3.DeleteObject(r.Context(), &s3.DeleteObjectInput{
			Bucket: aws.String(h.bucket), Key: key,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}
