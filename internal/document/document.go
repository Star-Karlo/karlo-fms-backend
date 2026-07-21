// Package document handles vehicle and driver document files — KIR, STNK, SIM,
// insurance — on top of the shared `documents` table.
//
// The read side already existed (internal/license lists documents and computes
// expiry buckets, and rows were imported from the legacy fleet export). What
// was missing was the file itself: `s3_key` has been in the schema since 0006
// but nothing ever populated it. This package is that missing half.
//
// UPLOADS GO STRAIGHT TO S3, not through this API. The client asks for a
// presigned PUT, uploads to it directly, and the API never sees the bytes.
// Scans and photos are megabytes; proxying them would push large bodies through
// a task sized for JSON, and would tie up a request for the duration of a
// phone's upload on a bad connection.
//
// SECURITY. A presigned URL is a bearer credential — anyone holding it can use
// it until it expires. Two consequences shape the code below:
//
//   - The API is the only tenancy boundary. S3 knows nothing about tenants, so
//     every signing path first proves the caller's tenant owns the row (RLS
//     does the work) before minting anything. The tenant-scoped key prefix is
//     for traceability, NOT enforcement.
//   - URLs are short-lived, and download URLs more so than upload ones: an
//     upload URL is used immediately by the client that asked for it, while a
//     download URL tends to end up in a browser history or a pasted link.
package document

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

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

// URL lifetimes. See the package comment: a download URL leaks more readily
// than an upload URL, so it lives a quarter as long.
const (
	uploadURLTTL   = 15 * time.Minute
	downloadURLTTL = 5 * time.Minute
)

// maxUploadBytes bounds one document at 25 MB — comfortably above a phone photo
// of an STNK, far below anything that would strain the bucket. Enforced by
// signing the exact content length, so an oversized body is rejected BY S3 and
// never occupies the API at all.
const maxUploadBytes = 25 << 20

// allowedMIME is what a document may be. An allowlist rather than a blocklist:
// these are scans, and anything outside this set is a mistake or an attempt to
// use the bucket as general storage.
var allowedMIME = map[string]string{
	"application/pdf": ".pdf",
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/heic":      ".heic", // iPhone default; users will hit this
	"image/webp":      ".webp",
}

// ownerTypes are the entities a document may hang off. Matches the values the
// existing license listing already groups by.
var ownerTypes = map[string]bool{"vehicle": true, "driver": true}

// Kinds is the closed set of document types. Open-ended `kind` was already in
// the schema, but the UI needs a list and reminders need consistent spelling —
// "STNK" and "stnk" as separate kinds would split a vehicle's history in two.
var Kinds = []string{"STNK", "KIR", "SIM", "INSURANCE", "OTHER"}

// unsafeChars strips anything that would make a key awkward to handle. The
// original filename is preserved in the DB title; this only sanitises the key.
var unsafeChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// Handler exposes document write/download endpoints.
type Handler struct {
	pool     *pgxpool.Pool
	bucket   string
	presign  *s3.PresignClient
	s3       *s3.Client
	disabled bool
}

// NewHandler builds the handler. A missing bucket disables the endpoints
// (503) rather than failing startup — see config.DocumentsBucket.
func NewHandler(ctx context.Context, pool *pgxpool.Pool, region, bucket string) *Handler {
	h := &Handler{pool: pool, bucket: bucket}
	if bucket == "" {
		h.disabled = true
		return h
	}
	cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(region))
	if err != nil {
		// Same reasoning as the AI handlers: a broken AWS config disables one
		// feature loudly at first use, rather than preventing the API booting.
		h.disabled = true
		return h
	}
	h.s3 = s3.NewFromConfig(cfg)
	h.presign = s3.NewPresignClient(h.s3)
	return h
}

// Routes mounts the document endpoints.
//
// Registered as FLAT PATHS, deliberately. Using r.Route("/v1/documents", …)
// here mounts a subrouter that owns the whole prefix — and because this package
// defines no GET at "/", the existing `GET /v1/documents` in internal/license
// (which drives the expiry-reminder page) started returning 405 rather than its
// listing. Same-pattern-different-method registrations coexist fine; a subrouter
// on a path another package already serves does not.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/v1/documents", h.create)
	r.Get("/v1/documents/kinds", h.kinds)
	r.Get("/v1/documents/{id}/download", h.download)
	r.Delete("/v1/documents/{id}", h.remove)
	// Convenience listing for a single vehicle's document drawer, so the UI does
	// not have to page the whole tenant-wide list to show four rows.
	r.Get("/v1/vehicles/{id}/documents", h.listForVehicle)
}

func (h *Handler) unavailable(w http.ResponseWriter) bool {
	if h.disabled {
		httpx.Error(w, http.StatusServiceUnavailable, "document storage is not configured on this deployment")
		return true
	}
	return false
}

// kinds godoc
//
//	@Summary	Document kinds the UI should offer
//	@Tags		documents
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{array}	string
//	@Router		/v1/documents/kinds [get]
func (h *Handler) kinds(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, Kinds)
}

// CreateRequest asks for an upload slot.
type CreateRequest struct {
	OwnerType string  `json:"owner_type"` // vehicle | driver
	OwnerID   int64   `json:"owner_id"`
	Kind      string  `json:"kind"`   // STNK | KIR | SIM | INSURANCE | OTHER
	Number    *string `json:"number"` // document number as printed
	Expiry    *string `json:"expiry"` // YYYY-MM-DD; drives expiry reminders
	Filename  string  `json:"filename"`
	MIME      string  `json:"mime"`
	Size      int64   `json:"size"`
}

// CreateResponse carries the new row plus the URL to PUT the file to.
type CreateResponse struct {
	ID        int64  `json:"id"`
	S3Key     string `json:"s3_key"`
	UploadURL string `json:"upload_url"`
	ExpiresIn int    `json:"expires_in_seconds"`
	// The client MUST send exactly these on the PUT or S3 rejects the
	// signature. Returned explicitly so the caller does not have to infer them.
	RequiredHeaders map[string]string `json:"required_headers"`
}

// create godoc
//
//	@Summary		Create a document record and get a presigned upload URL
//	@Description	Two-step upload: this creates the row and returns a short-lived S3 PUT URL. The client then PUTs the file directly to that URL with exactly the headers in `required_headers`. The row exists before the file does; until the PUT completes, download returns 404.
//	@Tags			documents
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		document.CreateRequest	true	"document metadata"
//	@Success		201		{object}	document.CreateResponse
//	@Failure		400		{object}	map[string]string
//	@Failure		503		{object}	map[string]string
//	@Router			/v1/documents [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	if h.unavailable(w) {
		return
	}
	var req CreateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if !ownerTypes[req.OwnerType] {
		httpx.Error(w, http.StatusBadRequest, "owner_type must be 'vehicle' or 'driver'")
		return
	}
	if req.OwnerID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "owner_id is required")
		return
	}
	ext, ok := allowedMIME[req.MIME]
	if !ok {
		httpx.Error(w, http.StatusBadRequest,
			"unsupported file type "+strconv.Quote(req.MIME)+"; allowed: PDF, JPEG, PNG, HEIC, WebP")
		return
	}
	if req.Size <= 0 || req.Size > maxUploadBytes {
		httpx.Error(w, http.StatusBadRequest,
			fmt.Sprintf("size must be between 1 byte and %d MB", maxUploadBytes>>20))
		return
	}
	if req.Kind == "" {
		req.Kind = "OTHER"
	}
	if req.Expiry != nil && *req.Expiry != "" {
		if _, err := time.Parse("2006-01-02", *req.Expiry); err != nil {
			httpx.Error(w, http.StatusBadRequest, "expiry must be YYYY-MM-DD")
			return
		}
	} else {
		req.Expiry = nil
	}

	tenantID := auth.TenantID(r.Context())
	var id int64
	var key string

	err := db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		// Prove the owner exists AND belongs to this tenant before minting a
		// URL. RLS scopes the lookup, so a foreign id simply returns no rows —
		// this is the tenancy check, not a nicety.
		table := "vehicles"
		if req.OwnerType == "driver" {
			table = "employees"
		}
		var exists bool
		if err := tx.QueryRow(r.Context(),
			`SELECT true FROM `+table+` WHERE id = $1`, req.OwnerID).Scan(&exists); err != nil {
			return fmt.Errorf("%s %d not found", req.OwnerType, req.OwnerID)
		}

		if err := tx.QueryRow(r.Context(), `
INSERT INTO documents (tenant_id, owner_type, owner_id, kind, title, number, expiry, mime)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING id`,
			tenantID, req.OwnerType, req.OwnerID, req.Kind,
			nullIfEmpty(req.Filename), req.Number, req.Expiry, req.MIME,
		).Scan(&id); err != nil {
			return err
		}

		// Key includes the row id, so re-uploading the same filename for the
		// same vehicle can never collide with a different document.
		key = fmt.Sprintf("tenant/%d/%s/%d/%d-%s",
			tenantID, req.OwnerType, req.OwnerID, id, safeName(req.Filename, ext))
		_, err := tx.Exec(r.Context(), `UPDATE documents SET s3_key = $1 WHERE id = $2`, key, id)
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Signing ContentLength and ContentType binds the URL to this exact upload:
	// the holder cannot use it to store a 2 GB file or a different media type.
	signed, err := h.presign.PresignPutObject(r.Context(), &s3.PutObjectInput{
		Bucket:        aws.String(h.bucket),
		Key:           aws.String(key),
		ContentType:   aws.String(req.MIME),
		ContentLength: aws.Int64(req.Size),
	}, s3.WithPresignExpires(uploadURLTTL))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "could not create upload URL: "+err.Error())
		return
	}

	httpx.JSON(w, http.StatusCreated, CreateResponse{
		ID:        id,
		S3Key:     key,
		UploadURL: signed.URL,
		ExpiresIn: int(uploadURLTTL.Seconds()),
		RequiredHeaders: map[string]string{
			"Content-Type":   req.MIME,
			"Content-Length": strconv.FormatInt(req.Size, 10),
		},
	})
}

// download godoc
//
//	@Summary		Presigned download URL for a document
//	@Description	Returns a short-lived URL rather than the bytes, so the file is served by S3 directly. 404 if the record exists but no file was ever uploaded to it.
//	@Tags			documents
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		int	true	"document id"
//	@Success		200	{object}	map[string]string
//	@Failure		404	{object}	map[string]string
//	@Router			/v1/documents/{id}/download [get]
func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	if h.unavailable(w) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}

	var key, mime, title *string
	err = db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		// RLS scopes this: another tenant's id returns no rows, which surfaces
		// as a 404 rather than leaking that the document exists.
		return tx.QueryRow(r.Context(),
			`SELECT s3_key, mime, title FROM documents WHERE id = $1`, id).Scan(&key, &mime, &title)
	})
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "document not found")
		return
	}
	if key == nil || *key == "" {
		// A row imported from the legacy export, or one whose upload never
		// completed. Distinguished from "not found" because the difference is
		// actionable: the user should re-upload.
		httpx.Error(w, http.StatusNotFound, "no file has been uploaded for this document")
		return
	}

	// Content-Disposition makes the browser download under the original
	// filename rather than the opaque S3 key.
	var disp *string
	if title != nil && *title != "" {
		d := fmt.Sprintf(`attachment; filename="%s"`, safeName(*title, ""))
		disp = &d
	}
	in := &s3.GetObjectInput{Bucket: aws.String(h.bucket), Key: key}
	if disp != nil {
		in.ResponseContentDisposition = disp
	}
	if mime != nil && *mime != "" {
		in.ResponseContentType = mime
	}

	signed, err := h.presign.PresignGetObject(r.Context(), in, s3.WithPresignExpires(downloadURLTTL))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "could not create download URL: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"url":                signed.URL,
		"expires_in_seconds": int(downloadURLTTL.Seconds()),
	})
}

// remove godoc
//
//	@Summary		Delete a document and its file
//	@Description	Removes the record and best-effort deletes the S3 object. The bucket is versioned, so a deletion is recoverable by an administrator.
//	@Tags			documents
//	@Security		BearerAuth
//	@Param			id	path	int	true	"document id"
//	@Success		204
//	@Failure		404	{object}	map[string]string
//	@Router			/v1/documents/{id} [delete]
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	if h.unavailable(w) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}

	var key *string
	err = db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		// DELETE ... RETURNING so the row is gone and its key captured in one
		// statement — no window where a second request deletes the same object.
		return tx.QueryRow(r.Context(),
			`DELETE FROM documents WHERE id = $1 RETURNING s3_key`, id).Scan(&key)
	})
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "document not found")
		return
	}

	// The record is authoritative and already gone; a failed object delete
	// leaves an orphan in a versioned bucket rather than a broken UI, so it is
	// not worth failing the request over.
	if key != nil && *key != "" {
		_, _ = h.s3.DeleteObject(r.Context(), &s3.DeleteObjectInput{
			Bucket: aws.String(h.bucket), Key: key,
		})
	}
	w.WriteHeader(http.StatusNoContent)
}

// VehicleDocument is one document in a vehicle's drawer.
type VehicleDocument struct {
	ID       int64   `json:"id"`
	Kind     *string `json:"kind"`
	Title    *string `json:"title"`
	Number   *string `json:"number"`
	Expiry   *string `json:"expiry"`
	MIME     *string `json:"mime"`
	HasFile  bool    `json:"has_file"`
	Uploaded string  `json:"uploaded_at"`
}

// listForVehicle godoc
//
//	@Summary	Documents attached to one vehicle
//	@Tags		documents
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path	int	true	"vehicle id"
//	@Success	200	{array}	document.VehicleDocument
//	@Router		/v1/vehicles/{id}/documents [get]
func (h *Handler) listForVehicle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	out := []VehicleDocument{}
	err = db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
SELECT id, kind, title, number, to_char(expiry, 'YYYY-MM-DD'), mime,
       (s3_key IS NOT NULL AND s3_key <> '') AS has_file,
       to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF')
FROM documents
WHERE owner_type = 'vehicle' AND owner_id = $1
ORDER BY kind, created_at DESC`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d VehicleDocument
			if err := rows.Scan(&d.ID, &d.Kind, &d.Title, &d.Number, &d.Expiry, &d.MIME,
				&d.HasFile, &d.Uploaded); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// safeName reduces a user-supplied filename to something safe in an S3 key,
// preserving a recognisable stem so a key is still readable in the console.
func safeName(name, fallbackExt string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == "/" {
		return "document" + fallbackExt
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if ext == "" {
		ext = fallbackExt
	}
	stem = unsafeChars.ReplaceAllString(stem, "-")
	stem = strings.Trim(stem, "-")
	if stem == "" {
		stem = "document"
	}
	if len(stem) > 60 {
		stem = stem[:60]
	}
	return stem + ext
}

func nullIfEmpty(s string) *string {
	t := strings.TrimSpace(s)
	if t == "" {
		return nil
	}
	return &t
}
