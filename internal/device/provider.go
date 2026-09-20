package device

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"tt_worker/internal/model"
)

type Provider interface {
	Acquire(context.Context, int, string) ([]model.Device, error)
}

type UsageMarker interface {
	MarkUsed(context.Context, []model.Device, string) error
}

type Target struct {
	TaskID     string
	Business   string
	BusinessID string
}

type TargetProvider interface {
	AcquireForTarget(context.Context, int, Target) ([]model.Device, error)
	ReportResults(context.Context, []model.Device, []model.Device, bool) error
}

type HTTPProvider struct {
	endpoint string
	client   *http.Client
}

// PoolProvider adapts POST /api/v1/device-pools/{pool}/allocate.
type PoolProvider struct {
	endpoint     string
	apiKey       string
	allowPartial bool
	order        string
	client       *http.Client
}

// GroupProvider allocates devices across a server-managed logical pool group.
type GroupProvider struct {
	endpoint     string
	apiKey       string
	allowPartial bool
	order        string
	client       *http.Client
}

func NewGroupProvider(baseURL, groupID, apiKey string, allowPartial bool, order string, timeout time.Duration) *GroupProvider {
	return &GroupProvider{
		endpoint: strings.TrimRight(baseURL, "/") + "/api/v1/device-pool-groups/" + url.PathEscape(groupID) + "/allocate",
		apiKey:   apiKey, allowPartial: allowPartial, order: order,
		client: &http.Client{Timeout: timeout},
	}
}

func (p *GroupProvider) Acquire(ctx context.Context, count int, purpose string) ([]model.Device, error) {
	return nil, fmt.Errorf("pool group allocation requires a business target")
}

func (p *GroupProvider) AcquireForTarget(ctx context.Context, count int, target Target) ([]model.Device, error) {
	if count < 1 {
		return nil, fmt.Errorf("device count must be positive")
	}
	payload, _ := json.Marshal(map[string]any{
		"request_id": fmt.Sprintf("%s-%s-%d", target.TaskID, target.Business, time.Now().UnixNano()),
		"task_id":    target.TaskID, "business": target.Business, "business_id": target.BusinessID,
		"count": count, "allow_partial": p.allowPartial, "order": p.order,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", p.apiKey)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("allocate device pool group: %w", err)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("device pool group http %d: %s", resp.StatusCode, body)
	}
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			AllocationID string `json:"allocation_id"`
			Devices      []struct {
				UsageID     string       `json:"usage_id"`
				SourceTable string       `json:"source_table"`
				SourceID    int          `json:"source_id"`
				Device      model.Device `json:"devices"`
			} `json:"devices"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode device pool group: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("device pool group rejected request: %s", result.Message)
	}
	devices := make([]model.Device, 0, len(result.Data.Devices))
	for _, item := range result.Data.Devices {
		d := normalize(item.Device)
		d["_pool_allocation_id"] = result.Data.AllocationID
		d["_pool_usage_id"] = item.UsageID
		d["_pool_source_table"] = item.SourceTable
		d["_pool_device_id"] = item.SourceID
		if d["iid"] == nil || d["device_id"] == nil || fmt.Sprint(d["iid"]) == "" || fmt.Sprint(d["device_id"]) == "" {
			return nil, fmt.Errorf("device pool group returned device without iid or device_id")
		}
		devices = append(devices, d)
	}
	if !p.allowPartial && len(devices) < count {
		return nil, fmt.Errorf("device pool group allocated %d/%d devices", len(devices), count)
	}
	return devices, nil
}

func (p *GroupProvider) ReportResults(ctx context.Context, devices []model.Device, failed []model.Device, unknown bool) error {
	if len(devices) == 0 {
		return nil
	}
	failedIDs := make(map[string]struct{}, len(failed))
	for _, d := range failed {
		failedIDs[fmt.Sprint(d["_pool_usage_id"])] = struct{}{}
	}
	allocationID := fmt.Sprint(devices[0]["_pool_allocation_id"])
	if allocationID == "" || allocationID == "<nil>" {
		return fmt.Errorf("device result is missing allocation id")
	}
	results := make([]map[string]string, 0, len(devices))
	for _, d := range devices {
		usageID := fmt.Sprint(d["_pool_usage_id"])
		if usageID == "" || usageID == "<nil>" {
			return fmt.Errorf("device result is missing usage id")
		}
		status := "succeeded"
		if unknown {
			status = "unknown"
		} else if _, ok := failedIDs[usageID]; ok {
			status = "failed"
		}
		results = append(results, map[string]string{"usage_id": usageID, "status": status})
	}
	endpoint := strings.TrimSuffix(p.endpoint, "/allocate")
	endpoint = endpoint[:strings.LastIndex(endpoint, "/device-pool-groups/")] + "/device-allocations/" + url.PathEscape(allocationID) + "/results"
	payload, _ := json.Marshal(map[string]any{"results": results})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", p.apiKey)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("report device results: %w", err)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("report device results http %d: %s", resp.StatusCode, body)
	}
	return nil
}

func NewPoolProvider(baseURL, poolID, apiKey string, allowPartial bool, order string, timeout time.Duration) *PoolProvider {
	return &PoolProvider{
		endpoint: strings.TrimRight(baseURL, "/") + "/api/v1/device-pools/" + url.PathEscape(poolID) + "/allocate",
		apiKey:   apiKey, allowPartial: allowPartial, order: order,
		client: &http.Client{Timeout: timeout},
	}
}

func (p *PoolProvider) Acquire(ctx context.Context, count int, purpose string) ([]model.Device, error) {
	all := make([]model.Device, 0, count)
	for len(all) < count {
		batch := count - len(all)
		if batch > 30 {
			batch = 30
		}
		devices, err := p.allocate(ctx, batch, purpose)
		if err != nil {
			return nil, err
		}
		all = append(all, devices...)
	}
	return all, nil
}

func (p *PoolProvider) allocate(ctx context.Context, count int, purpose string) ([]model.Device, error) {
	if count < 1 {
		return nil, fmt.Errorf("device count must be positive")
	}
	requestID := fmt.Sprintf("%s-%d", purpose, time.Now().UnixNano())
	payload, _ := json.Marshal(map[string]any{"request_id": requestID, "count": count, "allow_partial": p.allowPartial, "order": p.order})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", p.apiKey)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("allocate device pool: %w", err)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("device pool http %d: %s", resp.StatusCode, body)
	}
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Requested int `json:"requested"`
			Allocated int `json:"allocated"`
			Devices   []struct {
				ID     int          `json:"id"`
				Device model.Device `json:"devices"`
			} `json:"devices"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode device pool: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("device pool rejected request: %s", result.Message)
	}
	if !p.allowPartial && len(result.Data.Devices) < count {
		return nil, fmt.Errorf("device pool allocated %d/%d devices", len(result.Data.Devices), count)
	}
	devices := make([]model.Device, 0, len(result.Data.Devices))
	for _, item := range result.Data.Devices {
		d := normalize(item.Device)
		// 保留设备池记录 ID，任务完成后用于 PATCH /devices。
		// 该内部字段不会替换核心请求中的 device_id。
		if id := item.ID; id > 0 {
			d["_pool_device_id"] = id
		}
		if d["iid"] == nil || d["device_id"] == nil || fmt.Sprint(d["iid"]) == "" || fmt.Sprint(d["device_id"]) == "" {
			return nil, fmt.Errorf("device pool returned device without iid or device_id")
		}
		devices = append(devices, d)
	}
	return devices, nil
}

func (p *PoolProvider) MarkUsed(ctx context.Context, devices []model.Device, reason string) error {
	ids := make([]int, 0, len(devices))
	for _, d := range devices {
		if id, ok := d["_pool_device_id"].(int); ok && id > 0 {
			ids = append(ids, id)
			continue
		}
		if n, ok := d["_pool_device_id"].(float64); ok && n > 0 {
			ids = append(ids, int(n))
		}
	}
	if len(ids) == 0 {
		return nil
	}
	base := strings.TrimSuffix(p.endpoint, "/allocate")
	payload, _ := json.Marshal(map[string]any{"ids": ids, "is_used": true, "reason": reason})
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, base+"/devices", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", p.apiKey)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("mark devices used: %w", err)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("mark devices used http %d: %s", resp.StatusCode, body)
	}
	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}
	if !result.Success {
		return fmt.Errorf("mark devices used rejected: %s", result.Message)
	}
	return nil
}

func normalize(d model.Device) model.Device {
	if _, ok := d["openudid"]; !ok {
		if v, ok := d["openuid"]; ok {
			d["openudid"] = v
		}
	}
	if _, ok := d["rom_version"]; !ok {
		if v, ok := d["rom"]; ok {
			d["rom_version"] = v
		}
	}
	if _, ok := d["dpi"]; !ok {
		if v, ok := d["density_dpi"]; ok {
			d["dpi"] = v
		}
	}
	return d
}

func NewHTTPProvider(baseURL, path string, timeout time.Duration) *HTTPProvider {
	return &HTTPProvider{endpoint: strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/"), client: &http.Client{Timeout: timeout}}
}

func (p *HTTPProvider) Acquire(ctx context.Context, count int, purpose string) ([]model.Device, error) {
	devices := make([]model.Device, 0, count)
	for len(devices) < count {
		raw, _ := json.Marshal(map[string]any{"purpose": purpose, "platform": "android", "app": "novelread"})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := p.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("acquire device: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("device api http %d: %s", resp.StatusCode, body)
		}
		var result struct {
			Success bool         `json:"success"`
			Data    model.Device `json:"data"`
			Message string       `json:"message"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("decode device: %w", err)
		}
		if !result.Success || len(result.Data) == 0 {
			return nil, fmt.Errorf("device api rejected request: %s", result.Message)
		}
		devices = append(devices, result.Data)
	}
	return devices, nil
}
