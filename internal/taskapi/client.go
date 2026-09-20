package taskapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"tt_worker/internal/model"
)

type Client struct {
	baseURL, getPath, completePath, uid string
	client                              *http.Client
}

type Completion struct {
	Requested      int `json:"requested"`
	Attempted      int `json:"attempted"`
	Success        int `json:"success"`
	Failed         int `json:"failed"`
	ProxyRotations int `json:"proxy_rotations"`
}

func New(baseURL, getPath, completePath, uid string, timeout time.Duration) *Client {
	return &Client{strings.TrimRight(baseURL, "/"), getPath, completePath, uid, &http.Client{Timeout: timeout}}
}
func (c *Client) Get(ctx context.Context, taskType int) (*model.Task, error) {
	var result struct {
		Success bool        `json:"success"`
		Task    *model.Task `json:"task"`
		Message string      `json:"message"`
	}
	if err := c.post(ctx, c.getPath, map[string]any{"task_type_id": taskType, "uid": c.uid}, &result); err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, fmt.Errorf("task platform: %s", result.Message)
	}
	return result.Task, nil
}
func (c *Client) Complete(ctx context.Context, taskID string, completion Completion) error {
	payload := map[string]any{"uid": c.uid, "task_id": taskID, "completed_quantity": completion.Success, "result_data": completion}
	var response map[string]any
	return c.post(ctx, c.completePath, payload, &response)
}
func (c *Client) post(ctx context.Context, path string, input, out any) error {
	raw, _ := json.Marshal(input)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+strings.TrimLeft(path, "/"), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("task platform http %d: %s", resp.StatusCode, body)
	}
	if len(body) > 0 && out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return err
		}
	}
	return nil
}
