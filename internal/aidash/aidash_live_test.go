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

// TestReplaceVsAddLive checks the behaviour the prompt's DEFAULT TO REPLACING
// section exists to produce, because it is a property of the MODEL's judgement
// rather than of any code path — no unit test can assert it, and the previous
// wording ("when in doubt, keep it") made the model append to a board it was
// asked to rebuild.
//
// Two requests against the same fake current board:
//
//	a fresh-board request  -> must NOT carry the unrelated tiles over
//	an "also show" request -> MUST keep them
func TestReplaceVsAddLive(t *testing.T) {
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

	// A board about documents and drivers — deliberately unrelated to the
	// fresh request below, so carrying it over is unambiguous rather than a
	// judgement call about relevance.
	current := json.RawMessage(`[
	  {"metric":"documents.expiryStatus","viz":"statGroup","x":0,"y":0,"w":6,"h":3},
	  {"metric":"drivers.leaderboard","viz":"table","x":6,"y":0,"w":6,"h":4}
	]`)

	metrics := func(raw json.RawMessage) map[string]bool {
		var tiles []tileOut
		_ = json.Unmarshal(raw, &tiles)
		m := map[string]bool{}
		for _, tl := range tiles {
			m[tl.Metric] = true
		}
		return m
	}

	fresh, err := h.run(context.Background(), "manager", composeRequest{
		Prompt: "A dashboard for fuel and where my trucks are right now.", Catalog: cat, Current: current,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := metrics(fresh.Tiles)
	t.Logf("fresh board returned: %v", got)
	if got["documents.expiryStatus"] && got["drivers.leaderboard"] {
		t.Error("a fresh-board request carried the ENTIRE old board over; the replace default is not working")
	}

	added, err := h.run(context.Background(), "manager", composeRequest{
		Prompt: "Also show me overspeeding vehicles.", Catalog: cat, Current: current,
	})
	if err != nil {
		t.Fatal(err)
	}
	got = metrics(added.Tiles)
	t.Logf("after 'also show': %v", got)
	if !got["documents.expiryStatus"] || !got["drivers.leaderboard"] {
		t.Error("'also show X' dropped existing tiles; incremental requests must preserve the board")
	}
}
