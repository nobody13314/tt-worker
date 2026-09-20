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
	"time"
)

type Allocation struct {
	AllocationID string `json:"allocation_id"`
	ProxyURL     string `json:"proxy_url"`
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
}

func New(baseURL, apiKey, business, workerID string, timeout time.Duration) *Client {
	contentType := strings.TrimSpace(os.Getenv("PROXY_GATEWAY_ALLOCATE_CONTENT_TYPE"))
	if contentType == "" {
		contentType = "application/json"
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, business: business, workerID: workerID, allocateContentType: contentType, client: &http.Client{Timeout: timeout}}
}

func (c *Client) Allocate(ctx context.Context, requestID string, uses int) (Allocation, error) {
	raw, _ := json.Marshal(map[string]any{"request_id": requestID, "business": c.business, "worker_id": c.workerID, "expected_uses": uses})
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/proxies/allocate", bytes.NewReader(raw))
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
		if resp.StatusCode == http.StatusServiceUnavailable {
			var v struct {
				Retry int `json:"retry_after_ms"`
			}
			_ = json.Unmarshal(body, &v)
			if v.Retry < 1 {
				v.Retry = 1000
			}
			timer := time.NewTimer(time.Duration(v.Retry) * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return Allocation{}, ctx.Err()
			case <-timer.C:
				continue
			}
		}
		if resp.StatusCode/100 != 2 {
			return Allocation{}, fmt.Errorf("proxy gateway http %d: %s", resp.StatusCode, body)
		}
		var result struct {
			Success bool       `json:"success"`
			Data    Allocation `json:"data"`
			Code    string     `json:"code"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return Allocation{}, err
		}
		if !result.Success || result.Data.AllocationID == "" || result.Data.ProxyURL == "" {
			return Allocation{}, fmt.Errorf("proxy allocation failed: %s", result.Code)
		}
		return result.Data, nil
	}
}

func (c *Client) Report(ctx context.Context, report Report) error {
	raw, _ := json.Marshal(report)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/proxies/report", bytes.NewReader(raw))
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
