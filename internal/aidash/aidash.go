// Package aidash composes dashboard layouts from a natural-language
// description, using Amazon Nova Pro on Bedrock.
//
// WHY THE CATALOG COMES FROM THE CLIENT: the metric catalog lives in the
// frontend (features/dashboard/metrics/registry.ts) because that is where
// metrics are defined, normalised and rendered. Duplicating it here would
// create two sources of truth that silently drift — the model would compose
// specs against a catalog the client can no longer render. So the client sends
// the catalog it actually has, and this package is a thin, credentialed proxy
// that adds the system prompt and the tool schema.
//
// WHY A REPAIR LOOP: Bedrock's toolConfig has no equivalent of Anthropic's
// `strict: true`. There is no hard guarantee the tool input matches the schema,
// so validation here is load-bearing rather than a safety net — we validate,
// feed structured errors back, and retry a bounded number of times.
package aidash

import (
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

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Nova Pro is only exposed as an INFERENCE PROFILE in ap-southeast-1 — the bare
// "amazon.nova-pro-v1:0" model id is rejected. The "apac." prefix is the
// cross-region profile.
const defaultModelID = "apac.amazon.nova-pro-v1:0"

const maxRepairAttempts = 2

// Handler serves the compose endpoint.
type Handler struct {
	client  *bedrockruntime.Client
	modelID string
}

// NewHandler builds the handler. A nil client means the feature is disabled —
// the endpoint then returns 503 rather than the whole API failing to start,
// so a missing Bedrock permission can't take down the app.
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
	r.Post("/v1/ai/dashboard/compose", h.compose)
}

// ---- wire types -------------------------------------------------------------

type composeRequest struct {
	Prompt  string          `json:"prompt"`
	Catalog json.RawMessage `json:"catalog" swaggertype:"object"` // built client-side from the metric registry
	Current json.RawMessage `json:"current" swaggertype:"object"` // existing tiles, so "add X" works
	// Locale is the user's UI language. Asking the model to "reply in the
	// language of the request" was not enough on its own — an Indonesian
	// request came back with English titles — so the expected language is
	// stated outright rather than inferred.
	Locale string `json:"locale"`
}

type composeResponse struct {
	Tiles    json.RawMessage `json:"tiles" swaggertype:"object"`
	Notes    string          `json:"notes,omitempty"`
	Attempts int             `json:"attempts"`
	Usage    usage           `json:"usage"`
}

type usage struct {
	InputTokens  int32 `json:"input_tokens"`
	OutputTokens int32 `json:"output_tokens"`
}

// ---- handler ----------------------------------------------------------------

// compose godoc
//
//	@Summary		Compose a dashboard from a description
//	@Description	Given the caller's role, a natural-language description and the
//	@Description	client's metric catalog, returns a list of tiles (metric + params +
//	@Description	visualisation + layout). The caller validates and may reject them —
//	@Description	the model is a proposer, not an authority.
//	@Tags			ai
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		aidash.composeRequest	true	"prompt + catalog"
//	@Success		200		{object}	aidash.composeResponse
//	@Failure		400		{object}	map[string]string	"prompt or catalog missing"
//	@Failure		503		{object}	map[string]string	"AI composition not configured"
//	@Router			/v1/ai/dashboard/compose [post]
func (h *Handler) compose(w http.ResponseWriter, r *http.Request) {
	if h.client == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "AI composition is not configured")
		return
	}

	var req composeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" || len(req.Catalog) == 0 {
		httpx.Error(w, http.StatusBadRequest, "prompt and catalog are required")
		return
	}

	// The role shapes the DEFAULT emphasis of a generated board. It is not a
	// security control — that stays server-side in the metric endpoints and in
	// what the client puts in the catalog it sends.
	role := "operator"
	if c, ok := auth.FromContext(r.Context()); ok && c.Role != "" {
		role = c.Role
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	out, err := h.run(ctx, role, req)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) run(ctx context.Context, role string, req composeRequest) (*composeResponse, error) {
	tool := emitTool()
	msgs := []brtypes.Message{{
		Role:    brtypes.ConversationRoleUser,
		Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{Value: userPrompt(role, req)}},
	}}

	var total usage
	for attempt := 1; attempt <= maxRepairAttempts+1; attempt++ {
		resp, err := h.client.Converse(ctx, &bedrockruntime.ConverseInput{
			ModelId:  aws.String(h.modelID),
			Messages: msgs,
			System: []brtypes.SystemContentBlock{
				&brtypes.SystemContentBlockMemberText{Value: systemPrompt},
			},
			ToolConfig: &brtypes.ToolConfiguration{
				Tools: []brtypes.Tool{tool},
				// Force the call rather than hope for it. Without strict schema
				// enforcement, at least guarantee we get a tool_use back.
				ToolChoice: &brtypes.ToolChoiceMemberTool{
					Value: brtypes.SpecificToolChoice{Name: aws.String("emit_dashboard")},
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
			// No tool call despite ToolChoice. Retry rather than fail: the
			// model occasionally answers in prose on an ambiguous prompt.
			msgs = append(msgs, assistantEcho(resp), userText("Call emit_dashboard. Do not reply with prose."))
			continue
		}

		tiles, notes, problems := validate(input, req.Catalog)
		if len(problems) == 0 {
			return &composeResponse{Tiles: tiles, Notes: notes, Attempts: attempt, Usage: total}, nil
		}
		if attempt > maxRepairAttempts {
			// Out of retries. Return whatever survived rather than nothing —
			// the client validates again and shows a diff the user can reject,
			// so a partial board is more useful than an error.
			return &composeResponse{
				Tiles:    tiles,
				Notes:    strings.TrimSpace(notes + "\n" + strings.Join(problems, "; ")),
				Attempts: attempt,
				Usage:    total,
			}, nil
		}
		// Repair: hand back the specific failures, not "invalid".
		msgs = append(msgs, assistantEcho(resp), userText(repairPrompt(problems)))
	}
	return nil, fmt.Errorf("no valid dashboard after %d attempts", maxRepairAttempts+1)
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
