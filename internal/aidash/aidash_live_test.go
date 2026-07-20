package aidash

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// Live check against real Bedrock. Skipped unless AIDASH_LIVE=1.
func TestComposeLive(t *testing.T) {
	if os.Getenv("AIDASH_LIVE") != "1" {
		t.Skip("set AIDASH_LIVE=1")
	}
	cat, err := os.ReadFile(os.Getenv("CATALOG"))
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(context.Background(), "ap-southeast-1", "")
	if h.client == nil {
		t.Fatal("no bedrock client")
	}
	for _, prompt := range []string{
		"A dashboard for a maintenance manager: what needs servicing, expiring documents, and alert trends.",
		"I run night operations. Show me what is moving right now and anything urgent.",
	} {
		out, err := h.run(context.Background(), "manager", composeRequest{Prompt: prompt, Catalog: cat})
		if err != nil {
			t.Fatalf("%s: %v", prompt, err)
		}
		var tiles []tileOut
		_ = json.Unmarshal(out.Tiles, &tiles)
		t.Logf("\nPROMPT: %s\n  attempts=%d tokens=%d/%d tiles=%d notes=%s",
			prompt, out.Attempts, out.Usage.InputTokens, out.Usage.OutputTokens, len(tiles), out.Notes)
		for _, tl := range tiles {
			t.Logf("    %-28s %-9s  x=%-2d y=%-2d w=%-2d h=%d  %s", tl.Metric, tl.Viz, tl.X, tl.Y, tl.W, tl.H, tl.Title)
		}
		if len(tiles) == 0 {
			t.Error("no tiles")
		}
	}
}
