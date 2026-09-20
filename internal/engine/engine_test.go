package engine

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"tt_worker/internal/model"
	"tt_worker/internal/proxygateway"
)

type fakeGateway struct {
	mu      sync.Mutex
	allocs  int
	reports []proxygateway.Report
}

func (f *fakeGateway) Allocate(context.Context, string, int) (proxygateway.Allocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.allocs++
	return proxygateway.Allocation{AllocationID: string(rune('0' + f.allocs)), ProxyURL: "http://proxy"}, nil
}
func (f *fakeGateway) Report(_ context.Context, r proxygateway.Report) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reports = append(f.reports, r)
	return nil
}

type flaky struct {
	mu    sync.Mutex
	calls map[string]int
}

func (f *flaky) Execute(_ context.Context, _ *http.Client, d model.Device, _ model.Target) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := d["id"].(string)
	f.calls[id]++
	if id == "0" || f.calls[id] > 1 {
		return nil
	}
	return errors.New("fail")
}

func TestRotateProxyAndKeepSameFailedDevices(t *testing.T) {
	devices := make([]model.Device, 30)
	for i := range devices {
		devices[i] = model.Device{"id": string(rune('0' + i))}
	}
	g := &fakeGateway{}
	x := &flaky{calls: map[string]int{}}
	e := Engine{Gateway: g, NewClient: func(proxygateway.Allocation) (*http.Client, func(), error) { return http.DefaultClient, func() {}, nil }, Executor: x, GroupSize: 30, FailureRatio: .8, MaxRotations: 3, Concurrency: 5, RequestPrefix: "t"}
	r, err := e.Run(context.Background(), devices, model.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Success != 30 || r.ProxyRotations != 1 || g.allocs != 2 {
		t.Fatalf("result=%+v allocs=%d", r, g.allocs)
	}
	if x.calls["0"] != 1 || x.calls["1"] != 2 {
		t.Fatalf("successful device resent or failed device not retried: %+v", x.calls)
	}
	if g.reports[0].Failed != 29 || g.reports[1].Total != 29 {
		t.Fatalf("reports=%+v", g.reports)
	}
}

func TestBelowThresholdDoesNotRotate(t *testing.T) {
	devices := []model.Device{{"id": "0"}, {"id": "1"}, {"id": "2"}, {"id": "3"}, {"id": "4"}}
	g := &fakeGateway{}
	x := &flaky{calls: map[string]int{"2": 1, "3": 1, "4": 1}}
	e := Engine{Gateway: g, NewClient: func(proxygateway.Allocation) (*http.Client, func(), error) { return http.DefaultClient, func() {}, nil }, Executor: x, GroupSize: 30, FailureRatio: .8, MaxRotations: 3, Concurrency: 1}
	r, err := e.Run(context.Background(), devices, model.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if r.ProxyRotations != 0 || g.allocs != 1 {
		t.Fatalf("unexpected rotation: %+v", r)
	}
	if len(r.FailedDevices) != 1 || r.FailedDevices[0]["id"] != "1" {
		t.Fatalf("failed devices=%+v", r.FailedDevices)
	}
}

func TestExactThresholdRotates(t *testing.T) {
	devices := []model.Device{{"id": "0"}, {"id": "1"}, {"id": "2"}, {"id": "3"}, {"id": "4"}}
	g := &fakeGateway{}
	x := &flaky{calls: map[string]int{}}
	e := Engine{Gateway: g, NewClient: func(proxygateway.Allocation) (*http.Client, func(), error) { return http.DefaultClient, func() {}, nil }, Executor: x, GroupSize: 30, FailureRatio: .8, MaxRotations: 3, Concurrency: 5}
	r, err := e.Run(context.Background(), devices, model.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Success != 5 || r.ProxyRotations != 1 || g.allocs != 2 {
		t.Fatalf("result=%+v allocations=%d", r, g.allocs)
	}
}

type alwaysFail struct {
	mu    sync.Mutex
	calls map[string]int
}

func (f *alwaysFail) Execute(_ context.Context, _ *http.Client, d model.Device, _ model.Target) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[d["id"].(string)]++
	return errors.New("fail")
}

func TestStopsAfterThreeProxyReplacements(t *testing.T) {
	devices := []model.Device{{"id": "0"}, {"id": "1"}}
	g := &fakeGateway{}
	x := &alwaysFail{calls: map[string]int{}}
	e := Engine{Gateway: g, NewClient: func(proxygateway.Allocation) (*http.Client, func(), error) { return http.DefaultClient, func() {}, nil }, Executor: x, GroupSize: 30, FailureRatio: .8, MaxRotations: 3, Concurrency: 2}
	r, err := e.Run(context.Background(), devices, model.Target{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Success != 0 || r.Failed != 2 || r.ProxyRotations != 3 || g.allocs != 4 {
		t.Fatalf("result=%+v allocations=%d", r, g.allocs)
	}
	if x.calls["0"] != 4 || x.calls["1"] != 4 {
		t.Fatalf("calls=%+v", x.calls)
	}
}
