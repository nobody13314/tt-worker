package config

import "testing"

func TestLoadBusinessDefaults(t *testing.T) {
	t.Setenv("BUSINESS", "tt_dz")
	t.Setenv("PROXY_GATEWAY_URL", "http://gateway")
	t.Setenv("GATEWAY_API_KEY", "test")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.TaskTypeID != 38 || c.ProxyReuseLimit != 10 || c.GroupSize != 10 || c.LogDetail != "all" || c.GatewayBusiness != "tt_dz" {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestTaskTypeCanBeOverridden(t *testing.T) {
	t.Setenv("BUSINESS", "tt_dz")
	t.Setenv("TASK_TYPE_ID", "139")
	t.Setenv("PROXY_GATEWAY_URL", "http://gateway")
	t.Setenv("GATEWAY_API_KEY", "test")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.TaskTypeID != 139 {
		t.Fatalf("task type=%d, want 139", c.TaskTypeID)
	}
}
