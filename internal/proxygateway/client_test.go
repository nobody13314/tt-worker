package proxygateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLeaseReusesExactlyTenRequestsAcrossWork(t *testing.T) {
	var allocations atomic.Int32
	var mu sync.Mutex
	var reportTotals []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "secret" {
			t.Error("missing api key")
		}
		switch r.URL.Path {
		case "/api/v1/disposable-proxies/allocate":
			id := allocations.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data":    map[string]any{"allocation_id": fmt.Sprintf("a%d", id), "proxy_url": "http://127.0.0.1:8080"},
			})
		case "/api/v1/disposable-proxies/report":
			var report Report
			if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
				t.Error(err)
			}
			mu.Lock()
			reportTotals = append(reportTotals, report.Total)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL, "secret", "tt_dz", "w1", time.Second, 10)
	var allocationIDs []string
	var grants []int
	remaining := 25
	for remaining > 0 {
		allocation, granted, err := client.Allocate(context.Background(), fmt.Sprintf("work-%d", len(grants)), remaining)
		if err != nil {
			t.Fatal(err)
		}
		allocationIDs = append(allocationIDs, allocation.AllocationID)
		grants = append(grants, granted)
		if err := client.Report(context.Background(), Report{AllocationID: allocation.AllocationID, Total: granted, Success: granted}); err != nil {
			t.Fatal(err)
		}
		remaining -= granted
	}
	wantIDs := []string{"a1", "a1", "a2", "a2", "a3", "a3"}
	wantGrants := []int{2, 8, 2, 8, 2, 3}
	if fmt.Sprint(allocationIDs) != fmt.Sprint(wantIDs) || fmt.Sprint(grants) != fmt.Sprint(wantGrants) {
		t.Fatalf("ids=%v grants=%v want ids=%v grants=%v", allocationIDs, grants, wantIDs, wantGrants)
	}
	continued, granted, err := client.Allocate(context.Background(), "next-task", 10)
	if err != nil || continued.AllocationID != "a3" || granted != 5 {
		t.Fatalf("continued=%+v granted=%d err=%v", continued, granted, err)
	}
	if err := client.Report(context.Background(), Report{AllocationID: continued.AllocationID, Total: granted, Success: granted}); err != nil {
		t.Fatal(err)
	}
	if allocations.Load() != 3 {
		t.Fatalf("supplier allocations=%d want=3", allocations.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	wantReportTotals := []int{2, 10, 2, 10, 2, 5, 10}
	if fmt.Sprint(reportTotals) != fmt.Sprint(wantReportTotals) {
		t.Fatalf("report totals=%v want=%v", reportTotals, wantReportTotals)
	}
}

func TestAllocateHonorsRetryAfterWithSameRequestID(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"code":"EXTRACTION_LIMIT_REACHED","retry_after_ms":1}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{"allocation_id":"a1","proxy_url":"http://127.0.0.1:8080"}}`)
	}))
	defer server.Close()
	allocation, granted, err := New(server.URL, "secret", "tt_dz", "w1", time.Second, 10).Allocate(context.Background(), "r1", 10)
	if err != nil || allocation.AllocationID != "a1" || granted != 2 || calls.Load() != 2 {
		t.Fatalf("allocation=%+v granted=%d calls=%d err=%v", allocation, granted, calls.Load(), err)
	}
}
