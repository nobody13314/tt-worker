package proxygateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Allocation struct {
	AllocationID string    `json:"allocation_id"`
	ProxyURL     string    `json:"proxy_url"`
	ExpiresAt    time.Time `json:"expires_at"`
}
type Report struct {
	AllocationID     string         `json:"allocation_id"`
	Total            int            `json:"total"`
	Success          int            `json:"success"`
	Failed           int            `json:"failed"`
	AverageLatencyMS int64          `json:"average_latency_ms"`
	FailureReasons   map[string]int `json:"failure_reasons,omitempty"`
}
type Client struct {
	baseURL, apiKey, business, workerID, allocateContentType string
	client                                                   *http.Client
	reuseLimit                                               int
	mu                                                       sync.Mutex
	active                                                   *lease
}

type lease struct {
	allocation Allocation
	used       int
	report     Report
}

const initialProbeUses = 2

func New(baseURL, apiKey, business, workerID string, timeout time.Duration, reuseLimit int) *Client {
	contentType := strings.TrimSpace(os.Getenv("PROXY_GATEWAY_ALLOCATE_CONTENT_TYPE"))
	if contentType == "" {
		contentType = "application/json"
	}
	if reuseLimit < 1 {
		reuseLimit = 10
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, business: business, workerID: workerID, allocateContentType: contentType, client: &http.Client{Timeout: timeout}, reuseLimit: reuseLimit}
}

func (c *Client) Allocate(ctx context.Context, requestID string, uses int) (Allocation, int, error) {
	if uses < 1 {
		return Allocation{}, 0, fmt.Errorf("proxy uses must be positive")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != nil && (c.active.used >= c.reuseLimit || leaseExpired(c.active.allocation)) {
		c.active = nil
	}
	fresh := c.active == nil
	if fresh {
		allocation, err := c.allocate(ctx, requestID)
		if err != nil {
			return Allocation{}, 0, err
		}
		c.active = &lease{allocation: allocation, report: Report{AllocationID: allocation.AllocationID}}
	}
	granted := c.reuseLimit - c.active.used
	if granted > uses {
		granted = uses
	}
	if fresh && granted > initialProbeUses {
		granted = initialProbeUses
	}
	c.active.used += granted
	return c.active.allocation, granted, nil
}

func (c *Client) allocate(ctx context.Context, requestID string) (Allocation, error) {
	raw, _ := json.Marshal(map[string]any{"request_id": requestID, "business": c.business, "worker_id": c.workerID, "expected_uses": c.reuseLimit})
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/disposable-proxies/allocate", bytes.NewReader(raw))
		c.headers(req)
		req.Header.Set("Content-Type", c.allocateContentType)
		resp, err := c.client.Do(req)
		if err != nil {
			return Allocation{}, fmt.Errorf("allocate proxy: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if readErr != nil {
			return Allocation{}, readErr
		}
		var result struct {
			Success bool       `json:"success"`
			Data    Allocation `json:"data"`
			Code    string     `json:"code"`
			Retry   int        `json:"retry_after_ms"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return Allocation{}, err
		}
		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable) && result.Retry > 0 {
			timer := time.NewTimer(time.Duration(result.Retry) * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return Allocation{}, ctx.Err()
			case <-timer.C:
				continue
			}
		}
		if resp.StatusCode/100 != 2 {
			return Allocation{}, fmt.Errorf("proxy gateway http %d code=%s: %s", resp.StatusCode, result.Code, body)
		}
		if !result.Success || result.Data.AllocationID == "" || result.Data.ProxyURL == "" {
			return Allocation{}, fmt.Errorf("proxy allocation failed: %s", result.Code)
		}
		return result.Data, nil
	}
}

func (c *Client) Report(ctx context.Context, report Report) error {
	c.mu.Lock()
	if c.active == nil || c.active.allocation.AllocationID != report.AllocationID {
		c.mu.Unlock()
		return fmt.Errorf("proxy allocation %s is not active", report.AllocationID)
	}
	c.active.report.Total += report.Total
	c.active.report.Success += report.Success
	c.active.report.Failed += report.Failed
	if c.active.report.FailureReasons == nil {
		c.active.report.FailureReasons = map[string]int{}
	}
	for reason, count := range report.FailureReasons {
		c.active.report.FailureReasons[reason] += count
	}
	if c.active.report.Total > 0 {
		weighted := c.active.report.AverageLatencyMS*int64(c.active.report.Total-report.Total) + report.AverageLatencyMS*int64(report.Total)
		c.active.report.AverageLatencyMS = weighted / int64(c.active.report.Total)
	}
	cumulative := c.active.report
	c.mu.Unlock()

	raw, _ := json.Marshal(cumulative)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/disposable-proxies/report", bytes.NewReader(raw))
	c.headers(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("proxy report http %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) Invalidate(allocationID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != nil && c.active.allocation.AllocationID == allocationID {
		c.active = nil
	}
}

func leaseExpired(allocation Allocation) bool {
	return !allocation.ExpiresAt.IsZero() && !allocation.ExpiresAt.After(time.Now().Add(5*time.Second))
}
func (c *Client) headers(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
}
func (a Allocation) HTTPClient(timeout time.Duration) (*http.Client, func(), error) {
	u, err := url.Parse(a.ProxyURL)
	if err != nil {
		return nil, nil, err
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyURL(u)
	return &http.Client{Transport: tr, Timeout: timeout}, tr.CloseIdleConnections, nil
}
