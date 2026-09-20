package linkparser

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"tt_worker/internal/model"
)

var numeric = regexp.MustCompile(`^\d+$`)

type Resolution struct {
	Target      model.Target
	SourceURL   string
	ResolvedURL string
}

func ResolveTask(ctx context.Context, task model.Task, timeout time.Duration) (Resolution, error) {
	client := &http.Client{
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return resolveTask(ctx, task, client, allowedTTHost)
}

func resolveTask(ctx context.Context, task model.Task, client *http.Client, allowedHost func(string) bool) (Resolution, error) {
	for _, candidate := range taskCandidates(task) {
		if target := Parse(candidate); target.ObjectID != "" {
			return Resolution{Target: target, SourceURL: candidate, ResolvedURL: candidate}, nil
		}
		parsed, err := url.Parse(candidate)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || !allowedHost(parsed.Hostname()) || !isShortLink(parsed) {
			continue
		}
		resolved, err := resolveShortLink(ctx, client, candidate, allowedHost)
		if err != nil {
			continue
		}
		if target := Parse(resolved); target.ObjectID != "" {
			return Resolution{Target: target, SourceURL: candidate, ResolvedURL: resolved}, nil
		}
	}
	return Resolution{}, fmt.Errorf("task contains no resolvable Toutiao target")
}

func resolveShortLink(ctx context.Context, client *http.Client, rawURL string, allowedHost func(string) bool) (string, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		location := strings.TrimSpace(resp.Header.Get("Location"))
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if location != "" && resp.StatusCode >= 300 && resp.StatusCode < 400 {
			base, _ := url.Parse(rawURL)
			resolved, err := base.Parse(location)
			if err == nil && allowedHost(resolved.Hostname()) {
				return resolved.String(), nil
			}
			lastErr = fmt.Errorf("short link redirected to untrusted host")
			continue
		}
		if resolved := extractToutiaoURL(string(body)); resolved != "" {
			parsed, err := url.Parse(resolved)
			if err == nil && allowedHost(parsed.Hostname()) {
				return resolved, nil
			}
		}
		lastErr = fmt.Errorf("short link response contains no Toutiao target")
	}
	return "", fmt.Errorf("resolve short link after 3 attempts: %w", lastErr)
}

func extractToutiaoURL(body string) string {
	for _, prefix := range []string{"https://", "http://"} {
		searchFrom := 0
		for {
			index := strings.Index(body[searchFrom:], prefix)
			if index < 0 {
				break
			}
			start := searchFrom + index
			remaining := body[start:]
			end := strings.IndexAny(remaining, `"' <>&`)
			if end < 0 {
				end = len(remaining)
			}
			candidate := remaining[:end]
			if parsed, err := url.Parse(candidate); err == nil && allowedTTHost(parsed.Hostname()) {
				return candidate
			}
			searchFrom = start + len(prefix)
		}
	}
	return ""
}

func Parse(raw string) model.Target {
	raw = strings.TrimSpace(raw)
	if numeric.MatchString(raw) {
		return model.Target{ObjectID: raw, ItemID: raw}
	}
	parsed, err := url.Parse(raw)
	if err != nil || !allowedTTHost(parsed.Hostname()) {
		return model.Target{}
	}
	path := strings.TrimRight(parsed.Path, "/")
	for _, marker := range []string{"/article/", "/w/", "/video/", "/item/"} {
		index := strings.Index(path, marker)
		if index < 0 {
			continue
		}
		value := path[index+len(marker):]
		if slash := strings.IndexByte(value, '/'); slash >= 0 {
			value = value[:slash]
		}
		if numeric.MatchString(value) {
			return model.Target{ObjectID: value, ItemID: value}
		}
	}
	return model.Target{}
}

func taskCandidates(task model.Task) []string {
	values := make([]string, 0, len(task.ExternalAPIData.BuyParams)+2)
	if value := strings.TrimSpace(task.PostID); value != "" {
		values = append(values, value)
	}
	for _, param := range task.ExternalAPIData.BuyParams {
		if value := strings.TrimSpace(param.Value); value != "" {
			values = append(values, value)
		}
	}
	if value := strings.TrimSpace(task.ExternalAPIData.Parameter); value != "" {
		values = append(values, value)
	}
	return values
}

func allowedTTHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "toutiao.com" || strings.HasSuffix(host, ".toutiao.com")
}

func isShortLink(parsed *url.URL) bool {
	return strings.HasPrefix(parsed.Path, "/is/")
}

func FromTask(task model.Task) model.Target {
	for _, candidate := range taskCandidates(task) {
		if target := Parse(candidate); target.ObjectID != "" {
			return target
		}
	}
	return model.Target{}
}
