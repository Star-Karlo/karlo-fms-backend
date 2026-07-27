// Package airule composes notification rules from a natural-language request,
// using Amazon Nova Pro on Bedrock — the same proposer/validate/repair pattern
// as internal/aidash.
//
// The user types "warn me when a driver smokes more than 10 times an hour, or
// yawns a lot" and gets back a set of frequency rules ready to review and save.
// The model is a PROPOSER: every rule it emits is validated here against the
// alert taxonomy, and the client shows them for confirmation before they are
// created — the model never writes to the database.
//
// WHY THE CATALOG IS SERVER-SIDE (unlike aidash): the event taxonomy lives in
// this backend (internal/alert), not the client, so we build the catalog here
// rather than trusting the request for it.
package airule

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

	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Same inference profile as aidash — Nova Pro is exposed only as a cross-region
// profile in ap-southeast-1.
const defaultModelID = "apac.amazon.nova-pro-v1:0"

const maxRepairAttempts = 2

// Handler serves the rule-compose endpoint.
type Handler struct {
	client  *bedrockruntime.Client
	modelID string
}

// NewHandler builds the handler. A nil client disables the feature (503) rather
// than failing app start — a missing Bedrock permission can't take down the API.
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
	r.Post("/v1/ai/notification-rules/compose", h.compose)
}

// ---- wire types -------------------------------------------------------------

type composeRequest struct {
	Prompt string `json:"prompt"`
	Locale string `json:"locale"`
}

type composeResponse struct {
	Rules    json.RawMessage `json:"rules"`
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
//	@Summary		Compose notification rules from a description
//	@Description	Given a natural-language request, returns a list of proposed
//	@Description	notification rules (event + count threshold + time window +
//	@Description	urgency). The caller reviews and saves them — the model proposes,
//	@Description	it does not persist.
//	@Tags			ai
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		airule.composeRequest	true	"prompt"
//	@Success		200		{object}	airule.composeResponse
//	@Failure		400		{object}	map[string]string	"prompt missing"
//	@Failure		503		{object}	map[string]string	"AI composition not configured"
//	@Router			/v1/ai/notification-rules/compose [post]
func (h *Handler) compose(w http.ResponseWriter, r *http.Request) {
	if h.client == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "AI rule composition is not configured")
		return
	}
	var req composeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		httpx.Error(w, http.StatusBadRequest, "prompt is required")
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
			System: []brtypes.SystemContentBlock{
				&brtypes.SystemContentBlockMemberText{Value: systemPrompt},
			},
			ToolConfig: &brtypes.ToolConfiguration{
				Tools: []brtypes.Tool{tool},
				ToolChoice: &brtypes.ToolChoiceMemberTool{
					Value: brtypes.SpecificToolChoice{Name: aws.String("emit_rules")},
				},
			},
			InferenceConfig: &brtypes.InferenceConfiguration{MaxTokens: aws.Int32(2000)},
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
			msgs = append(msgs, assistantEcho(resp), userText("Call emit_rules. Do not reply with prose."))
			continue
		}

		rules, notes, problems := validate(input)
		if len(problems) == 0 {
			return &composeResponse{Rules: rules, Notes: notes, Attempts: attempt, Usage: total}, nil
		}
		if attempt > maxRepairAttempts {
			return &composeResponse{
				Rules:    rules,
				Notes:    strings.TrimSpace(notes + "\n" + strings.Join(problems, "; ")),
				Attempts: attempt,
				Usage:    total,
			}, nil
		}
		msgs = append(msgs, assistantEcho(resp), userText(repairPrompt(problems)))
	}
	return nil, fmt.Errorf("no valid rules after %d attempts", maxRepairAttempts+1)
}

// ---- bedrock helpers (same shape as aidash) --------------------------------

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
