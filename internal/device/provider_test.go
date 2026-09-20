package device

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGroupProviderCarriesTargetAndReportsResults(t *testing.T) {
	var allocateBody, resultsBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/allocate") {
			_ = json.NewDecoder(r.Body).Decode(&allocateBody)
			fmt.Fprint(w, `{"success":true,"data":{"allocation_id":"a1","allocated":1,"devices":[{"usage_id":"u1","source_table":"pool_a","source_id":7,"devices":{"iid":"i1","device_id":"d1"}}]}}`)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&resultsBody)
		fmt.Fprint(w, `{"success":true}`)
	}))
	defer server.Close()
	p := NewGroupProvider(server.URL, "main", "key", false, "oldest", time.Second)
	devices, err := p.AcquireForTarget(context.Background(), 1, Target{TaskID: "t1", Business: "sc", BusinessID: "series-1"})
	if err != nil {
		t.Fatal(err)
	}
	if allocateBody["task_id"] != "t1" || allocateBody["business"] != "sc" || allocateBody["business_id"] != "series-1" {
		t.Fatalf("allocate=%+v", allocateBody)
	}
	if err := p.ReportResults(context.Background(), devices, devices, false); err != nil {
		t.Fatal(err)
	}
	results := resultsBody["results"].([]any)
	if results[0].(map[string]any)["usage_id"] != "u1" || results[0].(map[string]any)["status"] != "failed" {
		t.Fatalf("results=%+v", resultsBody)
	}
}

func TestAcquireRequestedCount(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := calls.Add(1)
		fmt.Fprintf(w, `{"success":true,"data":{"iid":"%d","device_id":"%d"}}`, id, id)
	}))
	defer server.Close()
	devices, err := NewHTTPProvider(server.URL, "/acquire", time.Second).Acquire(context.Background(), 3, "hg_sc")
	if err != nil || len(devices) != 3 || calls.Load() != 3 {
		t.Fatalf("devices=%d calls=%d err=%v", len(devices), calls.Load(), err)
	}
}

func TestPoolProviderReplacesIIDAndDIDFromNestedDevice(t *testing.T) {
	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		fmt.Fprint(w, `{"success":true,"data":{"allocated":1,"devices":[{"id":1,"devices":{"iid":"new-iid","device_id":"new-did","openuid":"open"}}]}}`)
	}))
	defer server.Close()
	devices, err := NewPoolProvider(server.URL, "mssdk_pool", "device-secret", false, "oldest", time.Second).Acquire(context.Background(), 1, "hg_yy")
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "device-secret" || len(devices) != 1 || devices[0]["iid"] != "new-iid" || devices[0]["device_id"] != "new-did" || devices[0]["openudid"] != "open" {
		t.Fatalf("key=%q devices=%+v", gotKey, devices)
	}
}
