package proxygateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAllocateHonorsRetryAfter(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "secret" {
			t.Error("missing api key")
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"retry_after_ms":1}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{"allocation_id":"a1","proxy_url":"http://127.0.0.1:8080"}}`)
	}))
	defer s.Close()
	a, err := New(s.URL, "secret", "hg_sc", "w1", time.Second).Allocate(context.Background(), "r1", 30)
	if err != nil || a.AllocationID != "a1" || calls.Load() != 2 {
		t.Fatalf("allocation=%+v calls=%d err=%v", a, calls.Load(), err)
	}
}
