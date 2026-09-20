package signer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"tt_worker/internal/model"
)

type Input struct {
	URL     string            `json:"url"`
	Params  map[string]string `json:"params"`
	Devices model.Device      `json:"devices"`
	Data    any               `json:"data"`
	Header  map[string]string `json:"header"`
	Common  any               `json:"common"`
	Lanusk  string            `json:"lanusk"`
}
type Output struct {
	URL    string            `json:"url"`
	Header map[string]string `json:"header"`
}
type Service interface {
	Sign(context.Context, Input) (Output, error)
}
type Client struct {
	endpoint string
	client   *http.Client
}

func New(endpoint string, timeout time.Duration) *Client {
	return &Client{endpoint, &http.Client{Timeout: timeout}}
}
func (c *Client) Sign(ctx context.Context, in Input) (Output, error) {
	raw, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(raw))
	if err != nil {
		return Output{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return Output{}, fmt.Errorf("sign: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Output{}, err
	}
	if resp.StatusCode/100 != 2 {
		return Output{}, fmt.Errorf("signer http %d: %s", resp.StatusCode, body)
	}
	var out Output
	if err := json.Unmarshal(body, &out); err != nil {
		return Output{}, err
	}
	if out.URL == "" {
		return Output{}, fmt.Errorf("signer returned empty url")
	}
	return out, nil
}
