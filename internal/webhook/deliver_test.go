package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeliverSignsAndSucceeds(t *testing.T) {
	secret := "s3cr3t"
	var gotSig, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotSig = r.Header.Get("X-FMS-Signature")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	status, err := deliver(context.Background(), srv.URL, secret, map[string]any{"type": "alert", "alert_id": 7})
	if err != nil || status != 200 {
		t.Fatalf("deliver: status=%d err=%v", status, err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(gotBody))
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSig != want {
		t.Errorf("signature mismatch:\n got %s\nwant %s", gotSig, want)
	}
}

func TestDeliverNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	status, err := deliver(context.Background(), srv.URL, "", map[string]any{"x": 1})
	if err == nil || status != 500 {
		t.Errorf("want error with status 500, got status=%d err=%v", status, err)
	}
}
