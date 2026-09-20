package engine

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"tt_worker/internal/model"
	"tt_worker/internal/proxygateway"
)

type Gateway interface {
	Allocate(context.Context, string, int) (proxygateway.Allocation, error)
	Report(context.Context, proxygateway.Report) error
}
type ClientFactory func(proxygateway.Allocation) (*http.Client, func(), error)
type Executor interface {
	Execute(context.Context, *http.Client, model.Device, model.Target) error
}
type Result struct {
	Total, Success, Failed, ProxyRotations int
	FailedDevices                          []model.Device
}

type Engine struct {
	Gateway       Gateway
	NewClient     ClientFactory
	Executor      Executor
	GroupSize     int
	FailureRatio  float64
	MaxRotations  int
	Concurrency   int
	RequestPrefix string
}

func (e *Engine) Run(ctx context.Context, devices []model.Device, target model.Target) (Result, error) {
	if e.GroupSize < 1 {
		e.GroupSize = 30
	}
	if e.Concurrency < 1 {
		e.Concurrency = e.GroupSize
	}
	if e.FailureRatio <= 0 {
		e.FailureRatio = .8
	}
	result := Result{Total: len(devices)}
	for start := 0; start < len(devices); start += e.GroupSize {
		end := start + e.GroupSize
		if end > len(devices) {
			end = len(devices)
		}
		ok, failed, rot, err := e.runGroup(ctx, devices[start:end], target, start/e.GroupSize)
		result.Success += ok
		result.FailedDevices = append(result.FailedDevices, failed...)
		result.ProxyRotations += rot
		if err != nil {
			return result, err
		}
	}
	result.Failed = result.Total - result.Success
	return result, nil
}

func (e *Engine) runGroup(ctx context.Context, group []model.Device, target model.Target, groupIndex int) (int, []model.Device, int, error) {
	pending := append([]model.Device(nil), group...)
	success := 0
	rotations := 0
	for {
		requestID := fmt.Sprintf("%s-group-%d-round-%d-%d", e.RequestPrefix, groupIndex, rotations, time.Now().UnixNano())
		allocation, err := e.Gateway.Allocate(ctx, requestID, len(pending))
		if err != nil {
			return success, pending, rotations, err
		}
		client, closeClient, err := e.NewClient(allocation)
		if err != nil {
			return success, pending, rotations, err
		}
		failed, totalLatency, reasons := e.executeRound(ctx, client, pending, target)
		closeClient()
		roundSuccess := len(pending) - len(failed)
		success += roundSuccess
		avg := int64(0)
		if len(pending) > 0 {
			avg = totalLatency / int64(len(pending))
		}
		if err := e.Gateway.Report(ctx, proxygateway.Report{AllocationID: allocation.AllocationID, Total: len(pending), Success: roundSuccess, Failed: len(failed), AverageLatencyMS: avg, FailureReasons: reasons}); err != nil {
			return success, failed, rotations, fmt.Errorf("report proxy result: %w", err)
		}
		failureRatio := float64(len(failed)) / float64(len(pending))
		log.Printf(
			"proxy round request_prefix=%s group=%d round=%d allocation_id=%s attempted=%d success=%d failed=%d failure_ratio=%.1f%%",
			e.RequestPrefix, groupIndex+1, rotations+1, allocation.AllocationID, len(pending), roundSuccess, len(failed), failureRatio*100,
		)
		if len(failed) == 0 {
			return success, nil, rotations, nil
		}
		if failureRatio < e.FailureRatio {
			log.Printf(
				"proxy rotation skipped request_prefix=%s group=%d failure_ratio=%.1f%% threshold=%.1f%% remaining_failed=%d",
				e.RequestPrefix, groupIndex+1, failureRatio*100, e.FailureRatio*100, len(failed),
			)
			return success, failed, rotations, nil
		}
		if rotations >= e.MaxRotations {
			log.Printf(
				"proxy rotations exhausted request_prefix=%s group=%d replacements=%d remaining_failed=%d",
				e.RequestPrefix, groupIndex+1, rotations, len(failed),
			)
			return success, failed, rotations, nil
		}
		log.Printf(
			"proxy rotating request_prefix=%s group=%d replacement=%d/%d retry_devices=%d failure_ratio=%.1f%% threshold=%.1f%%",
			e.RequestPrefix, groupIndex+1, rotations+1, e.MaxRotations, len(failed), failureRatio*100, e.FailureRatio*100,
		)
		pending = failed
		rotations++
	}
}

func (e *Engine) executeRound(ctx context.Context, client *http.Client, devices []model.Device, target model.Target) ([]model.Device, int64, map[string]int) {
	type outcome struct {
		device  model.Device
		err     error
		latency int64
	}
	jobs := make(chan model.Device)
	out := make(chan outcome, len(devices))
	workers := e.Concurrency
	if workers > len(devices) {
		workers = len(devices)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range jobs {
				started := time.Now()
				err := e.Executor.Execute(ctx, client, d, target)
				out <- outcome{d, err, time.Since(started).Milliseconds()}
			}
		}()
	}
	go func() {
		for _, d := range devices {
			jobs <- d
		}
		close(jobs)
		wg.Wait()
		close(out)
	}()
	failed := make([]model.Device, 0)
	var latency int64
	reasons := map[string]int{}
	for r := range out {
		latency += r.latency
		if r.err != nil {
			failed = append(failed, r.device)
			reasons["target_business_failed"]++
		}
	}
	return failed, latency, reasons
}
