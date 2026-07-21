// Package aireport composes custom report definitions from a natural-language
// description, using Amazon Nova Pro on Bedrock.
//
// THE MODEL DOES NOT WRITE A QUERY. It picks from the reports that already
// exist and says how to present them, so anything it can express a user could
// have built by hand — and the composed spec runs through exactly the same
// report service, under the same tenancy, as the catalog reports it is made of.
// That is what makes it safe to hand a mid-tier model a report builder.
//
// Unlike the dashboard composer, the catalog is built HERE rather than sent by
// the client: reports live in the backend, so this is where their keys, columns
// and requirements are known. Sending it from the browser would put the source
// of truth on the far side of the network from the code that runs it.
//
// WHY A REPAIR LOOP: Bedrock's toolConfig has no equivalent of Anthropic's
// `strict: true`, so validation is load-bearing rather than a safety net. We
// validate, feed the specific failures back, and retry a bounded number of
// times.
package aireport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/go-chi/chi/v5"
	"github.com/xuri/excelize/v2"

	"github.com/karlo/fms-backend/internal/customreport"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Nova Pro is only exposed as an INFERENCE PROFILE in ap-southeast-1 — the bare
// model id is rejected. The "apac." prefix is the cross-region profile.
const defaultModelID = "apac.amazon.nova-pro-v1:0"

const maxRepairAttempts = 2

// Uploaded spreadsheets are read only for their HEADERS and a few sample rows,
// so there is no reason to accept a large one. The limit is on the request body
// before anything is parsed.
const maxUploadBytes = 5 << 20 // 5 MiB

// sampleRows is how many data rows are shown to the model. Enough to tell a
// date column from a plate column; few enough that a 50,000-row file costs the
// same as a 5-row one.
const sampleRows = 3

type Handler struct {
	client  *bedrockruntime.Client
	modelID string
}

// NewHandler builds the handler. A nil client means the feature is disabled —
// the endpoint returns 503 rather than the whole API failing to start, so a
// missing Bedrock permission cannot take down the app.
func NewHandler(ctx context.Context, region, modelID string) *Handler {
	if modelID == "" {
		modelID = defaultModelID
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return &Handler{modelID: modelID}
	}
	return &Handler{client: bedrockruntime.NewFromConfig(cfg), modelID: modelID}
}

func (h *Handler) Routes(r chi.Router) {
	r.Post("/v1/ai/report/compose", h.compose)
	r.Post("/v1/ai/report/inspect-file", h.inspectFile)
}

// ---- wire types -------------------------------------------------------------

type composeRequest struct {
	Prompt string `json:"prompt"`
	Locale string `json:"locale,omitempty"`
	// Headers and Sample come from a spreadsheet the user already uses, via
	// inspect-file. Kept as plain data rather than a file here so composing is
	// a normal JSON call and the upload happens once.
	Headers []string `json:"headers,omitempty"`
	Sample  string   `json:"sample,omitempty"`
	// Current is the report being edited, if any.
	Current *customreport.Spec `json:"current,omitempty"`
}

type composeResponse struct {
	Spec customreport.Spec `json:"spec"`
	// Unmapped names spreadsheet headers no report can supply. Surfaced rather
	// than dropped: a report that quietly omits a column the user asked for is
	// worse than one that says it cannot fill it.
	Unmapped []string `json:"unmapped,omitempty"`
	Notes    string   `json:"notes,omitempty"`
	Attempts int      `json:"attempts"`
	Usage    usage    `json:"usage"`
}

type usage struct {
	InputTokens  int32 `json:"input_tokens"`
	OutputTokens int32 `json:"output_tokens"`
}

// ---- inspect-file -----------------------------------------------------------

type inspectResponse struct {
	Sheet   string   `json:"sheet"`
	Headers []string `json:"headers"`
	Sample  string   `json:"sample"`
	Rows    int      `json:"rows"`
}

// inspectFile reads an uploaded spreadsheet's headers and a few sample rows.
//
//	@Summary		Inspect an uploaded spreadsheet
//	@Description	Reads column headers and a few sample rows from an .xlsx so a report can be shaped to match it. The file is never stored.
//	@Tags			ai
//	@Accept			mpfd
//	@Produce		json
//	@Param			file	formData	file	true	"an .xlsx file"
//	@Success		200		{object}	aireport.inspectResponse
//	@Failure		400		{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/v1/ai/report/inspect-file [post]
func (h *Handler) inspectFile(w http.ResponseWriter, r *http.Request) {
	// Bound the body BEFORE parsing: ParseMultipartForm on an unbounded
	// request is a memory-exhaustion invitation.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		httpx.Error(w, http.StatusBadRequest, "file is too large or not a valid upload (max 5 MB)")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "no file provided")
		return
	}
	defer file.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(file); err != nil {
		httpx.Error(w, http.StatusBadRequest, "could not read the file")
		return
	}

	out, err := inspectXLSX(buf.Bytes())
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("could not read %q: %s", hdr.Filename, err.Error()))
		return
	}
	// The file is deliberately NOT stored. Only its shape is useful, and
	// keeping a customer's spreadsheet on disk is a liability with no payoff.
	httpx.JSON(w, http.StatusOK, out)
}

func inspectXLSX(body []byte) (*inspectResponse, error) {
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("not a readable .xlsx file")
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("the workbook has no sheets")
	}
	sheet := sheets[0]
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("could not read sheet %q", sheet)
	}

	// Real reports often open with a title or a blank line, so the header row
	// is not reliably row 1. Take the first row with two or more non-empty
	// cells — a title row has one.
	headerIdx := -1
	for i, row := range rows {
		filled := 0
		for _, c := range row {
			if strings.TrimSpace(c) != "" {
				filled++
			}
		}
		if filled >= 2 {
			headerIdx = i
			break
		}
	}
	if headerIdx < 0 {
		return nil, fmt.Errorf("could not find a header row in sheet %q", sheet)
	}

	var headers []string
	for _, c := range rows[headerIdx] {
		if s := strings.TrimSpace(c); s != "" {
			headers = append(headers, s)
		}
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("no column headers found")
	}

	var sample strings.Builder
	for i := headerIdx + 1; i < len(rows) && i <= headerIdx+sampleRows; i++ {
		cells := make([]string, 0, len(rows[i]))
		for _, c := range rows[i] {
			cells = append(cells, strings.TrimSpace(c))
		}
		sample.WriteString(strings.Join(cells, " | "))
		sample.WriteString("\n")
	}

	return &inspectResponse{
		Sheet:   sheet,
		Headers: headers,
		Sample:  strings.TrimSpace(sample.String()),
		Rows:    max(0, len(rows)-headerIdx-1),
	}, nil
}

// ---- compose ----------------------------------------------------------------

// compose builds a custom report definition from a description.
//
//	@Summary		Compose a custom report with AI
//	@Description	Turns a description (and optionally a spreadsheet's headers) into a report spec. The spec is returned for review; nothing is saved.
//	@Tags			ai
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	aireport.composeResponse
//	@Failure		400	{object}	map[string]string
//	@Failure		503	{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/v1/ai/report/compose [post]
func (h *Handler) compose(w http.ResponseWriter, r *http.Request) {
	if h.client == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "AI report composition is not configured")
		return
	}
	var req composeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	// Headers alone are a complete request — "make me this spreadsheet" needs
	// no prose.
	if strings.TrimSpace(req.Prompt) == "" && len(req.Headers) == 0 {
		httpx.Error(w, http.StatusBadRequest, "describe the report, or upload a spreadsheet to match")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	out, err := h.run(ctx, req)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) run(ctx context.Context, req composeRequest) (*composeResponse, error) {
	tool := emitTool()
	msgs := []brtypes.Message{{
		Role:    brtypes.ConversationRoleUser,
		Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: userPrompt(req)}},
	}}

	var total usage
	for attempt := 1; attempt <= maxRepairAttempts+1; attempt++ {
		resp, err := h.client.Converse(ctx, &bedrockruntime.ConverseInput{
			ModelId:  aws.String(h.modelID),
			Messages: msgs,
			System:   []brtypes.SystemContentBlock{&brtypes.SystemContentBlockMemberText{Value: systemPrompt}},
			ToolConfig: &brtypes.ToolConfiguration{
				Tools: []brtypes.Tool{tool},
				// Force the call rather than hope for it: without strict schema
				// enforcement, at least guarantee a tool_use comes back.
				ToolChoice: &brtypes.ToolChoiceMemberTool{
					Value: brtypes.SpecificToolChoice{Name: aws.String("emit_report")},
				},
			},
			InferenceConfig: &brtypes.InferenceConfiguration{MaxTokens: aws.Int32(3000)},
		})
		if err != nil {
			return nil, fmt.Errorf("bedrock: %w", err)
		}
		if resp.Usage != nil {
			total.InputTokens += aws.ToInt32(resp.Usage.InputTokens)
			total.OutputTokens += aws.ToInt32(resp.Usage.OutputTokens)
		}

		input, ok := toolInput(resp)
		if !ok {
			msgs = append(msgs, assistantEcho(resp), userText("Call emit_report. Do not reply with prose."))
			continue
		}

		spec, unmapped, problems := validate(input)
		if len(problems) == 0 {
			return &composeResponse{Spec: spec, Unmapped: unmapped, Attempts: attempt, Usage: total}, nil
		}
		if attempt > maxRepairAttempts {
			// Out of retries. Return whatever survived rather than nothing: the
			// user reviews the spec before saving it, so a partial report they
			// can fix beats an error they cannot.
			return &composeResponse{
				Spec:     spec,
				Unmapped: unmapped,
				Notes:    strings.Join(problems, "; "),
				Attempts: attempt,
				Usage:    total,
			}, nil
		}
		msgs = append(msgs, assistantEcho(resp), userText(repairPrompt(problems)))
	}
	return nil, fmt.Errorf("no valid report after %d attempts", maxRepairAttempts+1)
}

func repairPrompt(problems []string) string {
	return "That report has problems. Fix ONLY these and call emit_report again:\n- " +
		strings.Join(problems, "\n- ")
}

// ---- bedrock helpers --------------------------------------------------------

func toolInput(resp *bedrockruntime.ConverseOutput) (map[string]any, bool) {
	out, ok := resp.Output.(*brtypes.ConverseOutputMemberMessage)
	if !ok {
		return nil, false
	}
	for _, blk := range out.Value.Content {
		use, ok := blk.(*brtypes.ContentBlockMemberToolUse)
		if !ok {
			continue
		}
		doc, err := use.Value.Input.MarshalSmithyDocument()
		if err != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(doc, &m) == nil {
			return m, true
		}
	}
	return nil, false
}

func assistantEcho(resp *bedrockruntime.ConverseOutput) brtypes.Message {
	if out, ok := resp.Output.(*brtypes.ConverseOutputMemberMessage); ok {
		return out.Value
	}
	return brtypes.Message{Role: brtypes.ConversationRoleAssistant}
}

func userText(s string) brtypes.Message {
	return brtypes.Message{
		Role:    brtypes.ConversationRoleUser,
		Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: s}},
	}
}
