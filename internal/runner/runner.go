package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"tt_worker/internal/business"
	"tt_worker/internal/model"
	"tt_worker/internal/signer"
)

type Runner struct {
	Handler   business.Handler
	Signer    signer.Service
	LogDetail string
}

func (r Runner) Execute(ctx context.Context, client *http.Client, device model.Device, target model.Target) error {
	requestDevice := make(model.Device, len(device))
	for key, value := range device {
		if len(key) > 0 && key[0] == '_' {
			continue
		}
		requestDevice[key] = value
	}
	in, body, err := r.Handler.Build(requestDevice, target)
	if err != nil {
		return err
	}
	signed, err := r.Signer.Sign(ctx, in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, signed.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	for k, v := range signed.Header {
		req.Header.Set(k, v)
	}
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		r.logRequest(target, requestDevice, signed.URL, 0, time.Since(started), nil, err)
		return fmt.Errorf("target request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	validationErr := r.Handler.Validate(resp.StatusCode, raw)
	r.logRequest(target, requestDevice, signed.URL, resp.StatusCode, time.Since(started), raw, validationErr)
	return validationErr
}

func (r Runner) logRequest(target model.Target, device model.Device, requestURL string, status int, duration time.Duration, response []byte, requestErr error) {
	detail := strings.ToLower(strings.TrimSpace(r.LogDetail))
	if detail == "" {
		detail = "all"
	}
	if detail == "summary" || detail == "failures" && requestErr == nil {
		return
	}
	log.Printf(
		"business request business=%s business_id=%s object_id=%s vid=%s body_object_id=%s series_id=%s item_id=%s iid=%s device_id=%s url=%s status=%d success=%t duration_ms=%d response=%q error=%q",
		r.Handler.Name(), targetID(r.Handler.Name(), target), target.ObjectID, target.Vid, bodyObjectID(r.Handler.Name(), target), target.SeriesID,
		target.ItemID, deviceValue(device, "iid"), deviceValue(device, "device_id"), requestURL,
		status, requestErr == nil, duration.Milliseconds(), clip(response, 500), errorText(requestErr),
	)
}

func bodyObjectID(name string, target model.Target) string {
	if name == "dz" && target.Vid != "" {
		return target.Vid
	}
	return target.ObjectID
}

func targetID(name string, target model.Target) string {
	if (name == "sc" || name == "fx") && target.SeriesID != "" {
		return target.SeriesID
	}
	if name == "yy" && target.ItemID != "" {
		return target.ItemID
	}
	return target.ObjectID
}

func deviceValue(device model.Device, key string) string {
	if value := device[key]; value != nil {
		return fmt.Sprint(value)
	}
	return ""
}

func clip(value []byte, limit int) string {
	if len(value) <= limit {
		return string(value)
	}
	return string(value[:limit]) + "..."
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
