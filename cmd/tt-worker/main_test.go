package main

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"tt_worker/internal/config"
	"tt_worker/internal/device"
	"tt_worker/internal/model"
	"tt_worker/internal/proxygateway"
)

func TestUsesSupplement(t *testing.T) {
	if !usesSupplement("tt_dz") {
		t.Fatal("tt_dz should use supplement")
	}
	if usesSupplement("other") {
		t.Fatal("unknown business should not use supplement")
	}
}

func TestExecuteOrderUsesTenCandidatesForSingleDigitDeficit(t *testing.T) {
	type call struct{ candidates, target int }
	var calls []call
	result, err := executeOrder(context.Background(), "task", "tt_dz", 100, func(candidates, target int) (workResult, error) {
		calls = append(calls, call{candidates, target})
		if len(calls) == 1 {
			return workResult{Attempted: 100, Success: 98}, nil
		}
		return workResult{Attempted: 4, Success: 2}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Success != 100 || result.Attempted != 104 {
		t.Fatalf("unexpected result: %+v", result)
	}
	want := []call{{100, 0}, {10, 2}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls=%v want=%v", calls, want)
	}
}

func TestExecuteOrderAllowsThreeCompleteSupplementRounds(t *testing.T) {
	var calls int
	result, err := executeOrder(context.Background(), "task", "tt_dz", 100, func(candidates, target int) (workResult, error) {
		calls++
		return workResult{Attempted: candidates}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 4 || result.Attempted != 400 || result.Success != 0 {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
}

func TestExecuteOrderDoesNotSupplementUnknownBusiness(t *testing.T) {
	var calls int
	result, err := executeOrder(context.Background(), "task", "other", 100, func(candidates, target int) (workResult, error) {
		calls++
		return workResult{Attempted: candidates, Success: 98}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || result.Success != 98 {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
}

func TestExecutePassProcessesEveryPrimaryBatch(t *testing.T) {
	provider := &fakeTargetProvider{}
	batchIndex := 0
	result, err := executePass(
		context.Background(), testConfig(), provider, &fakeGateway{}, testClientFactory,
		fakeExecutor{}, model.Target{}, device.Target{TaskID: "task", Business: "tt_dz", BusinessID: "target"},
		745, 0, &batchIndex,
	)
	if err != nil {
		t.Fatal(err)
	}
	wantBatches := []int{100, 100, 100, 100, 100, 100, 100, 45}
	if !reflect.DeepEqual(provider.acquireCounts, wantBatches) {
		t.Fatalf("acquire counts=%v want=%v", provider.acquireCounts, wantBatches)
	}
	if result.Attempted != 745 || result.Success != 745 || batchIndex != 8 {
		t.Fatalf("unexpected result=%+v batchIndex=%d", result, batchIndex)
	}
}

func TestTailPassStopsAtTargetWithoutReservingUnusedCandidates(t *testing.T) {
	provider := &fakeTargetProvider{}
	executor := &sequenceExecutor{failures: map[string]bool{"0": true}}
	batchIndex := 0
	result, err := executePass(
		context.Background(), testConfig(), provider, &fakeGateway{}, testClientFactory,
		executor, model.Target{}, device.Target{TaskID: "task", Business: "tt_dz", BusinessID: "target"},
		10, 2, &batchIndex,
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempted != 3 || result.Success != 2 || executor.calls != 3 {
		t.Fatalf("result=%+v executor calls=%d", result, executor.calls)
	}
	wantAcquires := []int{1, 1, 1}
	if !reflect.DeepEqual(provider.acquireCounts, wantAcquires) {
		t.Fatalf("acquire counts=%v want=%v", provider.acquireCounts, wantAcquires)
	}
}

type fakeTargetProvider struct {
	nextID        int
	acquireCounts []int
}

func (p *fakeTargetProvider) Acquire(context.Context, int, string) ([]model.Device, error) {
	return nil, errors.New("unexpected non-target allocation")
}

func (p *fakeTargetProvider) AcquireForTarget(_ context.Context, count int, _ device.Target) ([]model.Device, error) {
	p.acquireCounts = append(p.acquireCounts, count)
	devices := make([]model.Device, count)
	for i := range devices {
		devices[i] = model.Device{"id": strconv.Itoa(p.nextID)}
		p.nextID++
	}
	return devices, nil
}

func (p *fakeTargetProvider) ReportResults(context.Context, []model.Device, []model.Device, bool) error {
	return nil
}

type fakeGateway struct{ allocations int }

func (g *fakeGateway) Allocate(context.Context, string, int) (proxygateway.Allocation, error) {
	g.allocations++
	return proxygateway.Allocation{AllocationID: strconv.Itoa(g.allocations)}, nil
}

func (*fakeGateway) Report(context.Context, proxygateway.Report) error { return nil }

type fakeExecutor struct{}

func (fakeExecutor) Execute(context.Context, *http.Client, model.Device, model.Target) error {
	return nil
}

type sequenceExecutor struct {
	failures map[string]bool
	calls    int
}

func (e *sequenceExecutor) Execute(_ context.Context, _ *http.Client, d model.Device, _ model.Target) error {
	e.calls++
	if e.failures[d["id"].(string)] {
		return errors.New("failed")
	}
	return nil
}

func testClientFactory(proxygateway.Allocation) (*http.Client, func(), error) {
	return http.DefaultClient, func() {}, nil
}

func testConfig() config.Config {
	return config.Config{
		DeviceBatchSize: 100, GroupSize: 30, FailureRatio: .8,
		MaxProxyRotations: 0, Concurrency: 30, WorkerID: "test-worker",
	}
}
