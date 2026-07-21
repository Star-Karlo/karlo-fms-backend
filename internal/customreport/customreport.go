package customreport

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/report"
)

// fleetTZ defines what a "day" means in a range preset. Same reasoning as the
// telemetry rollup: the fleet runs in WIB, so a UTC day boundary would put the
// night shift in the wrong report.
const fleetTZ = "Asia/Jakarta"

// maxRowsPerSection bounds one section's contribution to a document.
//
// The underlying reports already cap their own inputs (5,000 telemetry points,
// 10,000 alert rows), but a custom report multiplies those by up to 12
// sections, and the whole document is built in memory before a byte is sent.
// A truncated section says so in the document rather than quietly ending.
const maxRowsPerSection = 20000

// Service turns saved specs into documents.
type Service struct {
	pool    *db.Pool
	reports *report.Service
	// narrator is optional. Without it (Bedrock unconfigured) a document still
	// renders — tables, no prose — rather than failing, because a report the
	// user can read beats an error about a feature they did not ask for.
	narrator Narrator
}

func NewService(pool *db.Pool, reports *report.Service) *Service {
	return &Service{pool: pool, reports: reports}
}

// WithNarrator attaches the prose writer. Separate from the constructor to keep
// the dependency one-way: the AI package imports this one for its types, so
// this one must not import the AI package.
func (s *Service) WithNarrator(n Narrator) *Service {
	s.narrator = n
	return s
}

// Definition is a saved report as stored.
type Definition struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Spec        Spec      `json:"spec"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// KnownSources is the set of catalog keys a section may reference. Derived from
// the report catalog rather than duplicated, so a report retired there cannot
// linger here as a valid choice.
func KnownSources() map[string]bool {
	out := make(map[string]bool, len(report.Catalog))
	for _, m := range report.Catalog {
		if m.Runnable {
			out[m.Key] = true
		}
	}
	return out
}

// ---- running ----------------------------------------------------------------

// Build runs every section of a spec and assembles the document.
//
// Sections run SEQUENTIALLY on purpose. Several catalog reports fetch up to
// 5,000 telemetry points over HTTP, and firing twelve of those at the tracking
// service concurrently to save a few seconds on a download is a poor trade
// against the live traffic sharing that service.
func (s *Service) Build(ctx context.Context, tenantID int64, spec Spec) (Doc, error) {
	loc, err := time.LoadLocation(fleetTZ)
	if err != nil {
		loc = time.UTC
	}
	from, to, err := spec.Range.Resolve(time.Now(), loc)
	if err != nil {
		return Doc{}, err
	}

	doc := Doc{
		Title: spec.Name,
		Notes: spec.Notes,
		// Label in FLEET-LOCAL time. An absolute range arrives as UTC — a
		// browser sending "1 July" in WIB sends 30 Jun 17:00Z — so labelling it
		// as parsed made a report for 1–21 July announce itself as 30 Jun to 21
		// Jul. The rows were right and the heading was a day out, which is the
		// version of wrong that gets believed.
		RangeLabel: spec.Range.Label(from.In(loc), to.In(loc)),
		Generated:  time.Now().In(loc),
	}

	for i, sec := range spec.Sections {
		req := report.RunRequest{
			From: from.Format(time.RFC3339),
			To:   to.Format(time.RFC3339),
		}
		applyParams(&req, sec.Params)

		res, err := s.reports.Run(ctx, tenantID, sec.Source, req)
		if err != nil {
			// One bad section must not cost the whole document — the user gets
			// the rest plus a line saying what failed and why, which is more
			// useful than a 400 with no report at all.
			doc.Sections = append(doc.Sections, DocSection{
				Title: sectionTitle(sec, i),
				Note:  fmt.Sprintf("Could not be generated: %s", err.Error()),
			})
			continue
		}
		doc.Sections = append(doc.Sections, toDocSection(sec, res, i))
	}
	return doc, nil
}

// applyParams copies a section's params onto the report request.
//
// Explicitly field by field rather than by unmarshalling the map: RunRequest
// carries From/To that were already resolved from the range, and a params blob
// that happened to contain "from" would silently override the window the user
// chose.
func applyParams(req *report.RunRequest, params map[string]any) {
	for k, v := range params {
		switch strings.ToLower(k) {
		case "vehicle_id":
			if id, ok := toInt64(v); ok {
				req.VehicleID = &id
			}
		case "alert_code":
			if s, ok := v.(string); ok {
				req.AlertCode = s
			}
		case "limit":
			if f, ok := toFloat(v); ok {
				req.Limit = f
			}
		}
	}
}

func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		return i, err == nil
	}
	return 0, false
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	}
	return 0, false
}

func sectionTitle(sec Section, i int) string {
	if strings.TrimSpace(sec.Title) != "" {
		return sec.Title
	}
	return fmt.Sprintf("Section %d", i+1)
}

// toDocSection applies the section's column selection to a report result and
// flattens values to strings.
func toDocSection(sec Section, res *report.Result, i int) DocSection {
	title := sectionTitle(sec, i)
	if sec.Title == "" && res.Title != "" {
		title = res.Title
	}

	// Which columns, in which order, under which names.
	type pick struct {
		key   string
		label string
	}
	var picks []pick
	var missing []string
	if len(sec.Columns) == 0 {
		for _, c := range res.Columns {
			picks = append(picks, pick{c.Key, c.Label})
		}
	} else {
		byKey := make(map[string]string, len(res.Columns))
		for _, c := range res.Columns {
			byKey[c.Key] = c.Label
		}
		for _, c := range sec.Columns {
			label, known := byKey[c.Key]
			if !known {
				// A column the source does not produce is skipped rather than
				// rendered as blanks — but SAID SO below, not dropped quietly.
				// Silently omitting it produced a section with one column and
				// no data that looked like a broken report rather than a
				// mistaken spec, and the AI writes these column lists.
				missing = append(missing, c.Key)
				continue
			}
			if c.Label != "" {
				label = c.Label
			}
			picks = append(picks, pick{c.Key, label})
		}
		// Every requested column was unknown — fall back to the source's own
		// rather than emitting a table with no columns.
		if len(picks) == 0 {
			for _, c := range res.Columns {
				picks = append(picks, pick{c.Key, c.Label})
			}
		}
	}
	if len(missing) > 0 {
		available := make([]string, 0, len(res.Columns))
		for _, c := range res.Columns {
			available = append(available, c.Key)
		}
		sec.Note = strings.TrimSpace(fmt.Sprintf("%s Requested column(s) not available from this report: %s. Available: %s.",
			sec.Note, strings.Join(missing, ", "), strings.Join(available, ", ")))
	}

	out := DocSection{Title: title, Note: sec.Note}
	if res.Note != "" {
		out.Note = strings.TrimSpace(out.Note + " " + res.Note)
	}
	for _, p := range picks {
		out.Headers = append(out.Headers, p.label)
	}

	rows := res.Rows
	truncated := 0
	if len(rows) > maxRowsPerSection {
		truncated = len(rows) - maxRowsPerSection
		rows = rows[:maxRowsPerSection]
	}

	numeric := make([]bool, len(picks))
	for i := range numeric {
		numeric[i] = true // until a non-numeric value proves otherwise
	}
	for _, r := range rows {
		row := make([]string, len(picks))
		for c, p := range picks {
			v := r[p.key]
			row[c] = cellString(v)
			if numeric[c] && !isNumeric(v) {
				numeric[c] = false
			}
		}
		out.Rows = append(out.Rows, row)
	}
	// An all-empty column is not a numeric column; marking it so would make
	// Excel treat blanks as zeroes.
	for c := range numeric {
		if len(out.Rows) == 0 {
			numeric[c] = false
		}
	}
	out.Numeric = numeric

	if truncated > 0 {
		out.Note = strings.TrimSpace(fmt.Sprintf("%s Showing the first %d rows; %d more were omitted.",
			out.Note, maxRowsPerSection, truncated))
	}
	return out
}

func isNumeric(v any) bool {
	switch v.(type) {
	case float64, float32, int, int64, int32:
		return true
	}
	return false
}

// cellString flattens a report value for a document cell.
//
// The catalog reports already format their own dates and use "—" for missing
// values, so this mostly passes text through; the work is making nil and
// floats predictable rather than printing Go syntax into a customer's report.
func cellString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "Yes"
		}
		return "No"
	case float64:
		// %g would render 1e+06 in a report; trim trailing zeroes instead.
		s := strconv.FormatFloat(t, 'f', -1, 64)
		return s
	case time.Time:
		return t.Format("2006-01-02 15:04")
	default:
		return fmt.Sprint(t)
	}
}

// narrate adds prose to a document when the format calls for it.
//
// Failure is deliberately non-fatal: if the model is unavailable or slow, the
// user gets their tables. A missing paragraph is a worse report; a failed
// download is no report at all.
func (s *Service) narrate(ctx context.Context, doc *Doc, spec Spec, format Format) {
	if s.narrator == nil || !wantsNarrative(spec, format) {
		return
	}
	n, err := s.narrator.Narrate(ctx, BuildDigest(*doc, spec))
	if err != nil {
		log.Printf("customreport: narration unavailable, rendering tables only: %v", err)
		return
	}
	applyNarrative(doc, n)
}

// ---- storage ----------------------------------------------------------------

func (s *Service) List(ctx context.Context, tenantID, userID int64) ([]Definition, error) {
	var out []Definition
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
SELECT id, name, description, spec, created_at, updated_at
FROM report_definitions WHERE user_id = $1 ORDER BY updated_at DESC`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d Definition
			var raw []byte
			if err := rows.Scan(&d.ID, &d.Name, &d.Description, &raw, &d.CreatedAt, &d.UpdatedAt); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &d.Spec); err != nil {
				// A spec we cannot parse is listed with an empty body rather
				// than failing the whole list — the user can still delete it.
				d.Spec = Spec{Name: d.Name}
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if out == nil {
		out = []Definition{}
	}
	return out, err
}

func (s *Service) Get(ctx context.Context, tenantID, userID, id int64) (*Definition, error) {
	var d Definition
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		var raw []byte
		err := tx.QueryRow(ctx, `
SELECT id, name, description, spec, created_at, updated_at
FROM report_definitions WHERE id = $1 AND user_id = $2`, id, userID).
			Scan(&d.ID, &d.Name, &d.Description, &raw, &d.CreatedAt, &d.UpdatedAt)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, &d.Spec)
	})
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Service) Save(ctx context.Context, tenantID, userID int64, id *int64, spec Spec, description string) (*Definition, error) {
	if err := spec.Validate(KnownSources()); err != nil {
		return nil, err
	}
	spec.Version = SpecVersion
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}

	var d Definition
	err = db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		if id != nil {
			return tx.QueryRow(ctx, `
UPDATE report_definitions SET name = $1, description = $2, spec = $3, updated_at = now()
WHERE id = $4 AND user_id = $5
RETURNING id, name, description, created_at, updated_at`,
				spec.Name, description, raw, *id, userID).
				Scan(&d.ID, &d.Name, &d.Description, &d.CreatedAt, &d.UpdatedAt)
		}
		return tx.QueryRow(ctx, `
INSERT INTO report_definitions (tenant_id, user_id, name, description, spec)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, name, description, created_at, updated_at`,
			tenantID, userID, spec.Name, description, raw).
			Scan(&d.ID, &d.Name, &d.Description, &d.CreatedAt, &d.UpdatedAt)
	})
	if err != nil {
		return nil, err
	}
	d.Spec = spec
	return &d, nil
}

func (s *Service) Delete(ctx context.Context, tenantID, userID, id int64) error {
	return db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM report_definitions WHERE id = $1 AND user_id = $2`, id, userID)
		return err
	})
}

// ---- http -------------------------------------------------------------------

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/reports/custom", h.list)
	r.Post("/v1/reports/custom", h.save)
	r.Delete("/v1/reports/custom/{id}", h.delete)
	r.Post("/v1/reports/custom/{id}/render", h.renderSaved)
	// Render without saving, so a proposal can be previewed or downloaded
	// before the user commits to naming it.
	r.Post("/v1/reports/custom/render", h.renderAdhoc)
}

// list returns the caller's saved reports.
//
//	@Summary		List saved custom reports
//	@Tags			reports
//	@Produce		json
//	@Success		200	{array}	customreport.Definition
//	@Security		BearerAuth
//	@Router			/v1/reports/custom [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.List(r.Context(), auth.TenantID(r.Context()), auth.UserID(r.Context()))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "could not list reports")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

type saveRequest struct {
	ID          *int64 `json:"id,omitempty"`
	Description string `json:"description,omitempty"`
	Spec        Spec   `json:"spec"`
}

// save creates or updates a saved report.
//
//	@Summary		Save a custom report
//	@Tags			reports
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	customreport.Definition
//	@Failure		400	{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/v1/reports/custom [post]
func (h *Handler) save(w http.ResponseWriter, r *http.Request) {
	var req saveRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	d, err := h.svc.Save(r.Context(), auth.TenantID(r.Context()), auth.UserID(r.Context()), req.ID, req.Spec, req.Description)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

// delete removes a saved report.
//
//	@Summary		Delete a custom report
//	@Tags			reports
//	@Success		204
//	@Security		BearerAuth
//	@Router			/v1/reports/custom/{id} [delete]
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.Delete(r.Context(), auth.TenantID(r.Context()), auth.UserID(r.Context()), id); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "could not delete")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// renderSaved runs a saved report and returns the file.
//
//	@Summary		Download a saved custom report
//	@Tags			reports
//	@Param			format	query	string	false	"xlsx | pdf | docx | csv"
//	@Produce		octet-stream
//	@Success		200
//	@Security		BearerAuth
//	@Router			/v1/reports/custom/{id}/render [post]
func (h *Handler) renderSaved(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	d, err := h.svc.Get(r.Context(), auth.TenantID(r.Context()), auth.UserID(r.Context()), id)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "report not found")
		return
	}
	h.write(w, r, d.Spec)
}

// renderAdhoc renders a spec that has not been saved.
//
//	@Summary		Download an unsaved custom report
//	@Tags			reports
//	@Accept			json
//	@Param			format	query	string	false	"xlsx | pdf | docx | csv"
//	@Produce		octet-stream
//	@Success		200
//	@Security		BearerAuth
//	@Router			/v1/reports/custom/render [post]
func (h *Handler) renderAdhoc(w http.ResponseWriter, r *http.Request) {
	var spec Spec
	if err := httpx.DecodeJSON(r, &spec); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := spec.Validate(KnownSources()); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.write(w, r, spec)
}

func (h *Handler) write(w http.ResponseWriter, r *http.Request, spec Spec) {
	format, err := spec.ValidFormat(r.URL.Query().Get("format"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	doc, err := h.svc.Build(r.Context(), auth.TenantID(r.Context()), spec)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	h.svc.narrate(r.Context(), &doc, spec, format)
	out, err := Render(doc, format)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "could not render the document")
		return
	}
	w.Header().Set("Content-Type", out.ContentType)
	// The filename is derived from the report title through safeFilename, which
	// strips everything outside [A-Za-z0-9 -_] — a title cannot inject a header.
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", out.Filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(out.Body)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(out.Body)
}
