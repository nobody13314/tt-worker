package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"tt_worker/internal/business"
	"tt_worker/internal/config"
	"tt_worker/internal/device"
	"tt_worker/internal/engine"
	"tt_worker/internal/linkparser"
	"tt_worker/internal/model"
	"tt_worker/internal/proxygateway"
	"tt_worker/internal/runner"
	"tt_worker/internal/signer"
	"tt_worker/internal/taskapi"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	handler, err := business.New(cfg.Business)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	tasks := taskapi.New(cfg.TaskBaseURL, cfg.TaskGetPath, cfg.TaskCompletePath, cfg.TaskUID, cfg.RequestTimeout)
	var devices device.Provider
	if cfg.DevicePoolGroupID != "" {
		devices = device.NewGroupProvider(cfg.DeviceBaseURL, cfg.DevicePoolGroupID, cfg.DeviceAPIKey, cfg.DeviceAllowPartial, cfg.DeviceOrder, cfg.RequestTimeout)
	} else if cfg.DevicePoolID != "" {
		devices = device.NewPoolProvider(cfg.DeviceBaseURL, cfg.DevicePoolID, cfg.DeviceAPIKey, cfg.DeviceAllowPartial, cfg.DeviceOrder, cfg.RequestTimeout)
	} else {
		devices = device.NewHTTPProvider(cfg.DeviceBaseURL, cfg.DeviceAcquirePath, cfg.RequestTimeout)
	}
	gateway := proxygateway.New(cfg.GatewayURL, cfg.GatewayAPIKey, cfg.GatewayBusiness, cfg.WorkerID, cfg.RequestTimeout)
	executor := runner.Runner{Handler: handler, Signer: signer.New(cfg.SignerURL, cfg.RequestTimeout), LogDetail: cfg.LogDetail}
	log.Printf("tt_worker started business=%s task_type=%d group=%d failure_ratio=%.0f%%", cfg.Business, cfg.TaskTypeID, cfg.GroupSize, cfg.FailureRatio*100)
	for ctx.Err() == nil {
		task, err := tasks.Get(ctx, cfg.TaskTypeID)
		if err != nil {
			log.Printf("get task: %v", err)
			wait(ctx, cfg.PollInterval)
			continue
		}
		if task == nil {
			wait(ctx, cfg.PollInterval)
			continue
		}
		quantity := task.TotalQuantity - task.CompletedQuantity
		if quantity <= 0 {
			quantity = task.ExternalAPIData.BuyNumber
		}
		if quantity <= 0 {
			log.Printf("task %s has no remaining quantity", task.TaskID)
			wait(ctx, cfg.PollInterval)
			continue
		}
		resolution, err := linkparser.ResolveTask(ctx, *task, cfg.RequestTimeout)
		if err != nil {
			log.Printf("task %s resolve target: %v", task.TaskID, err)
			wait(ctx, cfg.PollInterval)
			continue
		}
		target := resolution.Target
		resolvedBusinessID := businessID(cfg.Business, target)
		if resolvedBusinessID == "" {
			log.Printf("task %s resolve target: empty business id", task.TaskID)
			wait(ctx, cfg.PollInterval)
			continue
		}
		log.Printf(
			"task %s target_resolved business=%s source_url=%s resolved_url=%s object_id=%s vid=%s series_id=%s item_id=%s business_id=%s",
			task.TaskID, cfg.Business, resolution.SourceURL, resolution.ResolvedURL,
			target.ObjectID, target.Vid, target.SeriesID, target.ItemID, resolvedBusinessID,
		)
		log.Printf(
			"task %s started business=%s task_type=%d platform_id=%d quantity=%d post_id=%s object_id=%s series_id=%s item_id=%s business_id=%s",
			task.TaskID, cfg.Business, cfg.TaskTypeID, task.ID, quantity, task.PostID,
			target.ObjectID, target.SeriesID, target.ItemID, businessID(cfg.Business, target),
		)
		batchIndex := 0
		work, workErr := executeOrder(ctx, task.TaskID, cfg.Business, quantity, func(candidateCount, successTarget int) (workResult, error) {
			return executePass(
				ctx, cfg, devices, gateway,
				func(a proxygateway.Allocation) (*http.Client, func(), error) { return a.HTTPClient(cfg.RequestTimeout) },
				executor, target, device.Target{TaskID: task.TaskID, Business: cfg.Business, BusinessID: resolvedBusinessID},
				candidateCount, successTarget, &batchIndex,
			)
		})
		if workErr != nil {
			log.Printf("task %s processing stopped: %v", task.TaskID, workErr)
		}
		attempted, succeeded, rotations := work.Attempted, work.Success, work.ProxyRotations
		failed := attempted - succeeded
		if attempted > 0 && ctx.Err() == nil {
			completion := taskapi.Completion{Requested: quantity, Attempted: attempted, Success: succeeded, Failed: failed, ProxyRotations: rotations}
			if err := tasks.Complete(ctx, task.TaskID, completion); err != nil {
				log.Printf("complete task %s: %v", task.TaskID, err)
			} else {
				log.Printf("task %s completion submitted requested=%d attempted=%d success=%d failed=%d rotations=%d", task.TaskID, quantity, attempted, succeeded, failed, rotations)
			}
		}
		log.Printf("task %s done business=%s business_id=%s requested=%d attempted=%d success=%d failed=%d rotations=%d", task.TaskID, cfg.Business, resolvedBusinessID, quantity, attempted, succeeded, failed, rotations)
	}
}

func usesSupplement(business string) bool {
	return business == "tt_dz"
}

const (
	maxSupplementRounds = 3
	tailCandidateCount  = 10
)

type workResult struct {
	Attempted, Success, ProxyRotations int
}

type passRunner func(candidateCount, successTarget int) (workResult, error)

func executeOrder(ctx context.Context, taskID, business string, quantity int, run passRunner) (workResult, error) {
	total, err := run(quantity, 0)
	log.Printf("task %s phase=primary requested=%d attempted=%d success=%d deficit=%d", taskID, quantity, total.Attempted, total.Success, quantity-total.Success)
	if err != nil || !usesSupplement(business) {
		return total, err
	}
	for round := 1; round <= maxSupplementRounds && total.Success < quantity && ctx.Err() == nil; round++ {
		deficit := quantity - total.Success
		candidateCount, successTarget := deficit, 0
		if deficit < tailCandidateCount {
			candidateCount, successTarget = tailCandidateCount, deficit
		}
		part, runErr := run(candidateCount, successTarget)
		total.Attempted += part.Attempted
		total.Success += part.Success
		total.ProxyRotations += part.ProxyRotations
		remaining := quantity - total.Success
		if remaining < 0 {
			remaining = 0
		}
		log.Printf("task %s phase=supplement round=%d/%d requested=%d candidates=%d attempted=%d success=%d deficit=%d", taskID, round, maxSupplementRounds, deficit, candidateCount, part.Attempted, part.Success, remaining)
		if runErr != nil {
			return total, runErr
		}
	}
	if total.Success < quantity {
		log.Printf("task %s supplement exhausted rounds=%d deficit=%d", taskID, maxSupplementRounds, quantity-total.Success)
	}
	return total, ctx.Err()
}

func executePass(
	ctx context.Context,
	cfg config.Config,
	provider device.Provider,
	gateway engine.Gateway,
	newClient engine.ClientFactory,
	executor engine.Executor,
	target model.Target,
	deviceTarget device.Target,
	candidateCount, successTarget int,
	batchIndex *int,
) (workResult, error) {
	if successTarget > 0 {
		return executeTailPass(ctx, cfg, provider, gateway, newClient, executor, target, deviceTarget, candidateCount, successTarget, batchIndex)
	}
	var total workResult
	remaining := candidateCount
	for remaining > 0 && ctx.Err() == nil {
		count := remaining
		if count > cfg.DeviceBatchSize {
			count = cfg.DeviceBatchSize
		}
		batch, err := acquireDevices(ctx, provider, count, deviceTarget)
		if err != nil {
			return total, fmt.Errorf("acquire devices: %w", err)
		}
		if len(batch) == 0 {
			return total, fmt.Errorf("device pool returned no devices")
		}
		part, runErr := executeBatch(ctx, cfg, provider, gateway, newClient, executor, batch, target, deviceTarget.TaskID, batchIndex)
		total.Attempted += len(batch)
		total.Success += part.Success
		total.ProxyRotations += part.ProxyRotations
		remaining -= len(batch)
		if runErr != nil {
			return total, runErr
		}
	}
	return total, ctx.Err()
}

func executeTailPass(
	ctx context.Context,
	cfg config.Config,
	provider device.Provider,
	gateway engine.Gateway,
	newClient engine.ClientFactory,
	executor engine.Executor,
	target model.Target,
	deviceTarget device.Target,
	candidateCount, successTarget int,
	batchIndex *int,
) (workResult, error) {
	var total workResult
	for total.Attempted < candidateCount && total.Success < successTarget && ctx.Err() == nil {
		batch, err := acquireDevices(ctx, provider, 1, deviceTarget)
		if err != nil {
			return total, fmt.Errorf("acquire tail device: %w", err)
		}
		if len(batch) == 0 {
			return total, fmt.Errorf("device pool returned no tail device")
		}
		part, runErr := executeBatch(ctx, cfg, provider, gateway, newClient, executor, batch[:1], target, deviceTarget.TaskID, batchIndex)
		total.Attempted++
		total.Success += part.Success
		total.ProxyRotations += part.ProxyRotations
		if runErr != nil {
			return total, runErr
		}
	}
	return total, ctx.Err()
}

func executeBatch(ctx context.Context, cfg config.Config, provider device.Provider, gateway engine.Gateway, newClient engine.ClientFactory, executor engine.Executor, batch []model.Device, target model.Target, taskID string, batchIndex *int) (engine.Result, error) {
	e := engine.Engine{
		Gateway: gateway, NewClient: newClient, Executor: executor,
		GroupSize: cfg.GroupSize, FailureRatio: cfg.FailureRatio, MaxRotations: cfg.MaxProxyRotations, Concurrency: cfg.Concurrency,
		RequestPrefix: cfg.WorkerID + "-task-" + taskID + "-batch-" + fmt.Sprint(*batchIndex),
	}
	(*batchIndex)++
	result, runErr := e.Run(ctx, batch, target)
	if reporter, ok := provider.(device.TargetProvider); ok {
		if err := reporter.ReportResults(ctx, batch, result.FailedDevices, runErr != nil); err != nil {
			log.Printf("report device results: %v", err)
		}
	} else if marker, ok := provider.(device.UsageMarker); ok {
		if err := marker.MarkUsed(ctx, batch, "批量任务完成"); err != nil {
			log.Printf("mark devices used: %v", err)
		}
	}
	return result, runErr
}

func acquireDevices(ctx context.Context, provider device.Provider, count int, target device.Target) ([]model.Device, error) {
	if targeted, ok := provider.(device.TargetProvider); ok {
		return targeted.AcquireForTarget(ctx, count, target)
	}
	return provider.Acquire(ctx, count, target.Business)
}

func businessID(name string, target model.Target) string {
	return target.ObjectID
}

func wait(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
