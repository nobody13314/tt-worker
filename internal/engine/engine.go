package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"tt_worker/internal/model"
	"tt_worker/internal/proxygateway"
)

type Gateway interface {
	Allocate(context.Context, string, int) (proxygateway.Allocation, int, error)
	Report(context.Context, proxygateway.Report) error
	Invalidate(string)
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
	Concurrency   int
	RequestPrefix string
}

func (e *Engine) Run(ctx context.Context, devices []model.Device, target model.Target) (Result, error) {
	if e.GroupSize < 1 {
		e.GroupSize = 10
	}
	if e.Concurrency < 1 {
		e.Concurrency = e.GroupSize
	}
	result := Result{Total: len(devices)}
	for start, groupIndex := 0, 0; start < len(devices); groupIndex++ {
		wanted := e.GroupSize
		if remaining := len(devices) - start; wanted > remaining {
			wanted = remaining
		}
		requestID := fmt.Sprintf("%s-group-%d-%d", e.RequestPrefix, groupIndex, time.Now().UnixNano())
		allocation, granted, err := e.Gateway.Allocate(ctx, requestID, wanted)
		if err != nil {
			return result, err
		}
		if granted < 1 || granted > wanted {
			return result, fmt.Errorf("gateway granted invalid proxy use count %d for %d requests", granted, wanted)
		}
		group := devices[start : start+granted]
		ok, failed, rotate, err := e.runGroup(ctx, allocation, group, target, groupIndex)
		result.Success += ok
		result.FailedDevices = append(result.FailedDevices, failed...)
		if rotate {
			result.ProxyRotations++
		}
		if err != nil {
			return result, err
		}
		start += granted
	}
	result.Failed = result.Total - result.Success
	return result, nil
}

func (e *Engine) runGroup(ctx context.Context, allocation proxygateway.Allocation, group []model.Device, target model.Target, groupIndex int) (int, []model.Device, bool, error) {
	client, closeClient, err := e.NewClient(allocation)
	if err != nil {
		return 0, group, false, err
	}
	failed, totalLatency, reasons, proxyFailures := e.executeRound(ctx, client, group, target)
	closeClient()
	success := len(group) - len(failed)
	avg := int64(0)
	if len(group) > 0 {
		avg = totalLatency / int64(len(group))
	}
	if err := e.Gateway.Report(ctx, proxygateway.Report{AllocationID: allocation.AllocationID, Total: len(group), Success: success, Failed: len(failed), AverageLatencyMS: avg, FailureReasons: reasons}); err != nil {
		return success, failed, false, fmt.Errorf("report proxy result: %w", err)
	}
	rotate := proxyFailures >= 2
	if rotate {
		e.Gateway.Invalidate(allocation.AllocationID)
	}
	log.Printf(
		"proxy batch request_prefix=%s group=%d allocation_id=%s attempted=%d success=%d failed=%d proxy_failures=%d invalidated=%t",
		e.RequestPrefix, groupIndex+1, allocation.AllocationID, len(group), success, len(failed), proxyFailures, rotate,
	)
	return success, failed, rotate, nil
}

func (e *Engine) executeRound(ctx context.Context, client *http.Client, devices []model.Device, target model.Target) ([]model.Device, int64, map[string]int, int) {
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
	proxyFailures := 0
	reasons := map[string]int{}
	for r := range out {
		latency += r.latency
		if r.err != nil {
			failed = append(failed, r.device)
			if proxyFailure(r.err) {
				reasons["proxy_request_failed"]++
				proxyFailures++
			} else {
				reasons["target_business_failed"]++
			}
		}
	}
	return failed, latency, reasons, proxyFailures
}

func proxyFailure(err error) bool {
	type classified interface{ ProxyFailure() bool }
	var value classified
	return errors.As(err, &value) && value.ProxyFailure()
}
