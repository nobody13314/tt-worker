package signer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSign(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"url":"https://signed.example/action","header":{"x-sign":"ok"}}`)
	}))
	defer server.Close()
	out, err := New(server.URL, time.Second).Sign(context.Background(), Input{URL: "https://example/action"})
	if err != nil || out.URL != "https://signed.example/action" || out.Header["x-sign"] != "ok" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}
