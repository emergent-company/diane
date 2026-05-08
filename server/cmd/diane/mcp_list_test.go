package main

import (
	"testing"

	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
)

func TestPlural(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "s"},
		{1, ""},
		{2, "s"},
		{100, "s"},
	}
	for _, tt := range tests {
		got := plural(tt.n)
		if got != tt.want {
			t.Errorf("plural(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestCollectTools(t *testing.T) {
	// With nil
	tools := collectTools(nil)
	if tools == nil {
		t.Fatal("collectTools(nil) returned nil")
	}
	if len(tools) != 0 {
		t.Errorf("collectTools(nil) returned %d entries, want 0", len(tools))
	}

	// With empty slice
	tools = collectTools([]mcpproxy.ServerConfig{})
	if tools == nil {
		t.Fatal("collectTools([]) returned nil")
	}
	if len(tools) != 0 {
		t.Errorf("collectTools([]) returned %d entries, want 0", len(tools))
	}
}
