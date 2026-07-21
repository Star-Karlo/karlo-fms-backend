package aireport

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/karlo/fms-backend/internal/customreport"
)

// narratePrompt writes the prose for a PDF or Word report.
//
// The entire risk of this feature is a fluent paragraph containing a number
// nobody computed. A reader cannot tell an invented figure from a real one —
// prose reads as authoritative in a way a table does not — so the prompt's job
// is less about style than about refusing to go beyond the digest.
const narratePrompt = `You write the narrative for a fleet management report.

You are given a DIGEST of a report that has already been generated: for each
section, the row count and, per column, either totals/averages/ranges (numbers)
or the most frequent values (text), plus a couple of sample rows.

WRITE:
  summary  — 3 to 5 sentences for someone who will not read the tables. What
             happened over the period, what stands out, what deserves
             attention. Lead with the finding, not with the method.
  sections — one short paragraph per section, in the SAME ORDER, explaining
             what that table shows. 1 to 3 sentences each.

Each paragraph describes ONLY its own section, and the array must have exactly
one entry per section — they are printed above their tables by position, so
describing three sections in the first paragraph leaves the other two tables
bare and puts commentary above the wrong data.

THE ONE RULE THAT MATTERS: every number you write must appear in the digest.
A column's "sum" is given only where adding it up is meaningful. Where there is
no sum — percentages, per-day vehicle counts — do NOT add the values yourself;
use the average or the range instead. Adding Monday's vehicle count to
Tuesday's counts the same trucks twice.
Do not calculate, extrapolate, estimate, or compare against a period you were
not given. If the digest says a column totals 1,240 you may write 1,240; you
may not write "up from last month", because last month is not in front of you.
Where the digest is thin, write less. A short accurate paragraph is worth far
more than a full one that invents a trend — the reader cannot check you, and
this document goes to people who will act on it.

Each section carries an "about" describing what that report IS. Read it before
writing: an empty roster means there are no drivers recorded, not that nothing
changed. Describe what the section actually measures, not what its title
sounds like.

If a section has zero rows, say so plainly and briefly. That is a finding: no
overdue services is good news, and "no data" may mean nothing happened or that
nothing was recorded — do not assert which.

TONE: plain, factual, specific. No filler openings ("This report provides an
overview of…"), no recommendations you cannot support from the numbers, no
adjectives doing work the figures should do.

LANGUAGE: write in the same language as the report's title and section titles.
If they are in Bahasa Indonesia, so is the narrative.`

// narrateTool is the output shape.
func narrateTool() brtypes.Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{
				"type":        "string",
				"description": "Executive summary, 3-5 sentences. Only figures present in the digest.",
			},
			"sections": map[string]any{
				"type":        "array",
				"description": "One paragraph per section, same order as the digest.",
				"items":       map[string]any{"type": "string"},
			},
		},
		"required": []string{"summary", "sections"},
	}
	return &brtypes.ToolMemberToolSpec{
		Value: brtypes.ToolSpecification{
			Name:        aws.String("emit_narrative"),
			Description: aws.String("Emit the report narrative."),
			InputSchema: &brtypes.ToolInputSchemaMemberJson{Value: document.NewLazyDocument(schema)},
		},
	}
}

// narrateTimeout bounds the wait. A download the user is standing in front of
// must not hang on the model; past this the report renders with its tables and
// no prose, which is a worse report rather than no report.
const narrateTimeout = 30 * time.Second

// Narrate implements customreport.Narrator.
func (h *Handler) Narrate(ctx context.Context, d customreport.Digest) (*customreport.Narrative, error) {
	if h.client == nil {
		return nil, fmt.Errorf("bedrock not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, narrateTimeout)
	defer cancel()

	digest, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}

	resp, err := h.client.Converse(ctx, &bedrockruntime.ConverseInput{
		ModelId: aws.String(h.modelID),
		Messages: []brtypes.Message{{
			Role: brtypes.ConversationRoleUser,
			Content: []brtypes.ContentBlock{&brtypes.ContentBlockMemberText{
				Value: fmt.Sprintf("DIGEST:\n%s", digest),
			}},
		}},
		System: []brtypes.SystemContentBlock{&brtypes.SystemContentBlockMemberText{Value: narratePrompt}},
		ToolConfig: &brtypes.ToolConfiguration{
			Tools: []brtypes.Tool{narrateTool()},
			ToolChoice: &brtypes.ToolChoiceMemberTool{
				Value: brtypes.SpecificToolChoice{Name: aws.String("emit_narrative")},
			},
		},
		InferenceConfig: &brtypes.InferenceConfiguration{MaxTokens: aws.Int32(1200)},
	})
	if err != nil {
		return nil, fmt.Errorf("bedrock: %w", err)
	}

	input, ok := toolInput(resp)
	if !ok {
		return nil, fmt.Errorf("no narrative returned")
	}

	out := &customreport.Narrative{Summary: strings.TrimSpace(str(input["summary"]))}
	for _, s := range stringList(input["sections"]) {
		out.Sections = append(out.Sections, s)
	}

	// A narrative with no summary is not worth rendering a heading for; the
	// tables alone are more honest than an empty "Summary" section.
	if out.Summary == "" && len(out.Sections) == 0 {
		return nil, fmt.Errorf("empty narrative")
	}
	// Commentary is matched to sections BY POSITION, so a short list would
	// silently attach paragraph 2 to section 3. Pad rather than misalign.
	for len(out.Sections) < len(d.Sections) {
		out.Sections = append(out.Sections, "")
	}
	return out, nil
}
