package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Business           string
	TaskTypeID         int
	TaskBaseURL        string
	TaskGetPath        string
	TaskCompletePath   string
	TaskUID            string
	DeviceBaseURL      string
	DeviceAcquirePath  string
	DevicePoolID       string
	DevicePoolGroupID  string
	DeviceBatchSize    int
	DeviceAPIKey       string
	DeviceAllowPartial bool
	DeviceOrder        string
	SignerURL          string
	GatewayURL         string
	GatewayAPIKey      string
	GatewayBusiness    string
	WorkerID           string
	ProxyReuseLimit    int
	GroupSize          int
	Concurrency        int
	LogDetail          string
	PollInterval       time.Duration
	RequestTimeout     time.Duration
}

var taskTypes = map[string]int{"tt_dz": 38}

func Load() (Config, error) {
	business := strings.ToLower(env("BUSINESS", "tt_dz"))
	defaultTaskType, ok := taskTypes[business]
	if !ok {
		return Config{}, fmt.Errorf("BUSINESS must be tt_dz")
	}
	c := Config{
		Business: business, TaskTypeID: envInt("TASK_TYPE_ID", defaultTaskType),
		TaskBaseURL:        env("TASK_PLATFORM_URL", "http://127.0.0.1:5000"),
		TaskGetPath:        env("TASK_GET_PATH", "/api/worker/get-task"),
		TaskCompletePath:   env("TASK_COMPLETE_PATH", "/api/worker/complete-batch"),
		TaskUID:            env("TASK_UID", "worker_001"),
		DeviceBaseURL:      env("DEVICE_API_URL", "http://127.0.0.1:5002"),
		DeviceAcquirePath:  env("DEVICE_ACQUIRE_PATH", "/api/v1/devices/acquire"),
		DevicePoolID:       os.Getenv("DEVICE_POOL_ID"),
		DevicePoolGroupID:  os.Getenv("DEVICE_POOL_GROUP_ID"),
		DeviceBatchSize:    envInt("DEVICE_BATCH_SIZE", 100),
		DeviceAPIKey:       os.Getenv("DEVICE_API_KEY"),
		DeviceAllowPartial: env("DEVICE_ALLOW_PARTIAL", "false") == "true",
		DeviceOrder:        env("DEVICE_ORDER", "oldest"),
		SignerURL:          env("SIGNER_URL", "http://127.0.0.1:8001/api/sign"),
		GatewayURL:         strings.TrimRight(os.Getenv("PROXY_GATEWAY_URL"), "/"),
		GatewayAPIKey:      os.Getenv("GATEWAY_API_KEY"),
		GatewayBusiness:    env("PROXY_GATEWAY_BUSINESS", business),
		WorkerID:           env("PROXY_GATEWAY_WORKER_ID", "tt-worker"),
		ProxyReuseLimit:    envInt("PROXY_REUSE_LIMIT", 10),
		GroupSize:          envInt("GROUP_SIZE", 10), Concurrency: envInt("CONCURRENCY", 30),
		LogDetail:      strings.ToLower(env("LOG_DETAIL", "all")),
		PollInterval:   time.Duration(envInt("POLL_INTERVAL_SECONDS", 5)) * time.Second,
		RequestTimeout: time.Duration(envInt("REQUEST_TIMEOUT_SECONDS", 30)) * time.Second,
	}
	if c.TaskTypeID < 1 {
		return Config{}, fmt.Errorf("TASK_TYPE_ID must be positive")
	}
	if c.GatewayURL == "" || c.GatewayAPIKey == "" {
		return Config{}, fmt.Errorf("PROXY_GATEWAY_URL and GATEWAY_API_KEY are required")
	}
	if c.ProxyReuseLimit < 1 || c.GroupSize < 1 || c.Concurrency < 1 || c.DeviceBatchSize < 1 {
		return Config{}, fmt.Errorf("proxy reuse, group, concurrency and device batch configuration must be positive")
	}
	if c.LogDetail != "summary" && c.LogDetail != "failures" && c.LogDetail != "all" {
		return Config{}, fmt.Errorf("LOG_DETAIL must be summary, failures or all")
	}
	return c, nil
}

func env(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}
func envInt(name string, fallback int) int {
	v, err := strconv.Atoi(os.Getenv(name))
	if err == nil {
		return v
	}
	return fallback
}
