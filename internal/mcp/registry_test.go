package mcp

import (
	"context"
	"testing"
)

func TestRegistryExposesOnboardingTools(t *testing.T) {
	tools := NewRegistry().Tools(context.Background())
	if len(tools) != 5 {
		t.Fatalf("tool count: %d", len(tools))
	}
	if tools[0].Name != "get_onboarding_plan" || tools[4].Name != "get_access_request_status" {
		t.Fatalf("unexpected tool catalog: %#v", tools)
	}
}
