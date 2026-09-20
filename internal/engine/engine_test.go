package engine

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"tt_worker/internal/model"
	"tt_worker/internal/proxygateway"
)

type fakeGateway struct {
	mu          sync.Mutex
	grants      []int
	allocs      int
	reports     []proxygateway.Report
	invalidated []string
}

func (f *fakeGateway) Allocate(_ context.Context, _ string, uses int) (proxygateway.Allocation, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.allocs++
	granted := uses
	if len(f.grants) >= f.allocs && f.grants[f.allocs-1] < granted {
		granted = f.grants[f.allocs-1]
	}
	id := strconv.Itoa(f.allocs)
	return proxygateway.Allocation{AllocationID: id, ProxyURL: "http://proxy"}, granted, nil
}

func (f *fakeGateway) Report(_ context.Context, report proxygateway.Report) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reports = append(f.reports, report)
	return nil
}

func (f *fakeGateway) Invalidate(allocationID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if allocationID != "" {
		f.invalidated = append(f.invalidated, allocationID)
	}
}

type successExecutor struct{}

func (successExecutor) Execute(context.Context, *http.Client, model.Device, model.Target) error {
	return nil
}

func TestRunHonorsRemainingLeaseSlots(t *testing.T) {
	devices := make([]model.Device, 25)
	for i := range devices {
		devices[i] = model.Device{"id": strconv.Itoa(i)}
	}
	gateway := &fakeGateway{grants: []int{5, 5, 10, 5}}
	worker := Engine{Gateway: gateway, NewClient: testClientFactory, Executor: successExecutor{}, GroupSize: 10, Concurrency: 10, RequestPrefix: "test"}
	result, err := worker.Run(context.Background(), devices, model.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success != 25 || result.Failed != 0 || gateway.allocs != 4 {
		t.Fatalf("result=%+v allocations=%d", result, gateway.allocs)
	}
	wantTotals := []int{5, 5, 10, 5}
	for i, report := range gateway.reports {
		if report.Total != wantTotals[i] {
			t.Fatalf("report %d total=%d want=%d", i, report.Total, wantTotals[i])
		}
	}
}

type proxyFailureError struct{}

func (proxyFailureError) Error() string      { return "proxy failed" }
func (proxyFailureError) ProxyFailure() bool { return true }

type sequenceExecutor struct {
	mu           sync.Mutex
	proxyFailure map[string]bool
	businessFail map[string]bool
}

func (e *sequenceExecutor) Execute(_ context.Context, _ *http.Client, device model.Device, _ model.Target) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	id := device["id"].(string)
	if e.proxyFailure[id] {
		return proxyFailureError{}
	}
	if e.businessFail[id] {
		return errors.New("business failed")
	}
	return nil
}

func TestTwoProxyFailuresInvalidateLease(t *testing.T) {
	devices := []model.Device{{"id": "0"}, {"id": "1"}, {"id": "2"}}
	gateway := &fakeGateway{}
	executor := &sequenceExecutor{proxyFailure: map[string]bool{"0": true, "1": true}}
	worker := Engine{Gateway: gateway, NewClient: testClientFactory, Executor: executor, GroupSize: 10, Concurrency: 3}
	result, err := worker.Run(context.Background(), devices, model.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProxyRotations != 1 || len(gateway.invalidated) != 1 || gateway.invalidated[0] != "1" {
		t.Fatalf("result=%+v invalidated=%v", result, gateway.invalidated)
	}
	if gateway.reports[0].FailureReasons["proxy_request_failed"] != 2 {
		t.Fatalf("report=%+v", gateway.reports[0])
	}
}

func TestBusinessFailuresDoNotInvalidateLease(t *testing.T) {
	devices := []model.Device{{"id": "0"}, {"id": "1"}}
	gateway := &fakeGateway{}
	executor := &sequenceExecutor{businessFail: map[string]bool{"0": true, "1": true}}
	worker := Engine{Gateway: gateway, NewClient: testClientFactory, Executor: executor, GroupSize: 10, Concurrency: 2}
	result, err := worker.Run(context.Background(), devices, model.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProxyRotations != 0 || len(gateway.invalidated) != 0 {
		t.Fatalf("result=%+v invalidated=%v", result, gateway.invalidated)
	}
	if gateway.reports[0].FailureReasons["target_business_failed"] != 2 {
		t.Fatalf("report=%+v", gateway.reports[0])
	}
}

func testClientFactory(proxygateway.Allocation) (*http.Client, func(), error) {
	return http.DefaultClient, func() {}, nil
}
