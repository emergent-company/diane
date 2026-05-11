package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrefixToolsInData_Passthrough(t *testing.T) {
	// nil data returns nil
	got := prefixToolsInData(nil, "pre_")
	if got != nil {
		t.Error("nil input should return nil")
	}

	// missing "tools" key passthrough
	input := json.RawMessage(`{"type":"list","meta":{}}`)
	got = prefixToolsInData(input, "pre_")
	if string(got) != string(input) {
		t.Errorf("passthrough: got %q, want %q", string(got), string(input))
	}

	// empty tools array — passthrough ok (no tools to prefix)
	input = json.RawMessage(`{"tools":[]}`)
	got = prefixToolsInData(input, "pre_")
	if !strings.Contains(string(got), `"tools":[]`) {
		t.Errorf("empty tools should stay empty, got %q", string(got))
	}
}

func TestPrefixToolsInData_SingleTool(t *testing.T) {
	input := json.RawMessage(`{"tools":[{"name":"node_status","description":"Check node"}]}`)
	got := prefixToolsInData(input, "mcj-mini_")

	var result struct {
		Tools []map[string]interface{} `json:"tools"`
	}
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if name := result.Tools[0]["name"].(string); name != "mcj-mini_node_status" {
		t.Errorf("tool name = %q, want %q", name, "mcj-mini_node_status")
	}
}

func TestPrefixToolsInData_MultipleTools(t *testing.T) {
	input := json.RawMessage(`{"tools":[
		{"name":"weather","description":"Weather tool"},
		{"name":"memory","description":"Memory tool"}
	]}`)
	got := prefixToolsInData(input, "n1_")

	var result struct {
		Tools []map[string]interface{} `json:"tools"`
	}
	if err := json.Unmarshal(got, &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(result.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result.Tools))
	}

	expected := []string{"n1_weather", "n1_memory"}
	for i, want := range expected {
		if name := result.Tools[i]["name"].(string); name != want {
			t.Errorf("tool[%d].name = %q, want %q", i, name, want)
		}
	}
}

func TestGenerateInstanceID_Format(t *testing.T) {
	id := generateInstanceID()
	if !strings.HasPrefix(id, "diane-") {
		t.Errorf("instance ID should start with 'diane-', got %q", id)
	}
	if len(id) != len("diane-")+4 { // "diane-" (6) + 4 hex chars
		t.Errorf("unexpected instance ID length: %d, want %d", len(id), len("diane-")+4)
	}
	// check hex chars after prefix
	hexPart := id[len("diane-"):]
	for _, c := range hexPart {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("non-hex character %c in instance ID hex part %q", c, hexPart)
			break
		}
	}
}

func TestMergeProxyConfigs_Empty(t *testing.T) {
	result := mergeProxyConfigs(nil)
	if !strings.Contains(result, `"servers":[]`) {
		t.Errorf("empty input should return empty servers, got: %s", result)
	}

	result = mergeProxyConfigs([]scoredConfig{})
	if !strings.Contains(result, `"servers":[]`) {
		t.Errorf("empty slice should return empty servers, got: %s", result)
	}
}

func TestMergeProxyConfigs_Single(t *testing.T) {
	cfg := `{"servers":[{"name":"sentry","type":"stdio","command":"sentry-cli","args":["mcp"],"enabled":true}]}`
	result := mergeProxyConfigs([]scoredConfig{
		{config: cfg, score: 0, version: 1},
	})

	if !strings.Contains(result, "sentry") {
		t.Errorf("single config not preserved, got: %s", result)
	}
}

func TestMergeProxyConfigs_MergeOverride(t *testing.T) {
	low := `{"servers":[{"name":"sentry","type":"stdio","command":"old","enabled":false}]}`
	high := `{"servers":[{"name":"sentry","type":"stdio","command":"new","enabled":true}]}`

	// Higher score should override
	result := mergeProxyConfigs([]scoredConfig{
		{config: low, score: 10, version: 1},
		{config: high, score: 50, version: 1},
	})

	if !strings.Contains(result, `"command": "new"`) || !strings.Contains(result, `"enabled": true`) {
		t.Errorf("higher score should override lower, got: %s", result)
	}
}

func TestMergeProxyConfigs_MergeDifferentServers(t *testing.T) {
	cfg1 := `{"servers":[{"name":"sentry","type":"stdio","command":"sentry-cli","args":["mcp"],"enabled":true}]}`
	cfg2 := `{"servers":[{"name":"weather","type":"stdio","command":"weather-srv","enabled":true}]}`

	result := mergeProxyConfigs([]scoredConfig{
		{config: cfg1, score: 10, version: 1},
		{config: cfg2, score: 20, version: 1},
	})

	// Both servers should be present
	if !strings.Contains(result, "sentry") || !strings.Contains(result, "weather") {
		t.Errorf("both servers should be present, got: %s", result)
	}
}

func TestMergeProxyConfigs_InvalidJSON(t *testing.T) {
	valid := `{"servers":[{"name":"sentry","type":"stdio","command":"sentry-cli","enabled":true}]}`
	invalid := `not-json`

	result := mergeProxyConfigs([]scoredConfig{
		{config: invalid, score: 10, version: 1},
		{config: valid, score: 20, version: 1},
	})

	if !strings.Contains(result, "sentry") {
		t.Errorf("valid config should survive invalid ones, got: %s", result)
	}
}
